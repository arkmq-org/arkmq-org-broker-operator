/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"software.sslmate.com/src/go-pkcs12"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	tm "github.com/cert-manager/trust-manager/pkg/apis/trust/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/tools/remotecommand"
)

var chars = []rune("hgjkmnpqrtvwxyzslbcdaefiou")
var defaultPassword string = "password"
var defaultSanDnsNames = []string{"*.apps.arkmq-org.io", "*.tests.arkmq-org.io"}
var okDefaultPwd = "okdefaultpassword"
var helmCmd = GetHelmCmd()

func GetHelmCmd() string {
	if localHelm, ok := os.LookupEnv("HELM"); ok {
		return localHelm
	}
	return "helm"
}

func randStringWithPrefix(prefix string) string {
	length := 6
	var b strings.Builder
	b.WriteString(prefix)
	for i := 0; i < length; i++ {
		b.WriteRune(chars[rand.Intn(len(chars))])
	}
	return b.String()
}

func randString() string {
	return randStringWithPrefix("br-")
}

func CleanResourceWithTimeouts(res client.Object, name string, namespace string, cleanTimeout time.Duration, cleanInterval time.Duration) {
	//batch.kubernetes.io/job-name: consumer

	// Use foreground propagation for Jobs to ensure dependent Pods are deleted first
	resType := reflect.ValueOf(res).Elem().Type()
	jobType := reflect.TypeOf(batchv1.Job{})
	var err error
	if resType == jobType {
		cascade_foreground_policy := metav1.DeletePropagationForeground
		err = k8sClient.Delete(ctx, res, &client.DeleteOptions{PropagationPolicy: &cascade_foreground_policy})
	} else {
		err = k8sClient.Delete(ctx, res)
	}
	if errors.IsNotFound(err) {
		return
	}
	Expect(err).To(BeNil())
	Eventually(func(g Gomega) {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, res)
		By("eventually make sure resource is gone")
		if err != nil && !errors.IsNotFound(err) {
			fmt.Printf("Error from get %v is %v\n", namespace+"/"+name, err)
		}
		g.Expect(errors.IsNotFound(err)).To(BeTrue())
	}, cleanTimeout, cleanInterval).Should(Succeed())

	// With foreground propagation, dependent pods should be deleted automatically,
	// but verify cleanup as a safety measure
	if resType == jobType {
		cfg, err := config.GetConfig()
		Expect(err).To(BeNil())

		clientset, err := kubernetes.NewForConfig(cfg)
		Expect(err).To(BeNil())

		// Define the label selector (replace with your label selector)
		labelSelector := "batch.kubernetes.io/job-name=" + res.GetName()

		// Get the list of pods with the label selector
		pods, err := clientset.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			log.Fatalf("Failed to list pods: %v", err)
		}

		// Delete the selected pods
		for _, pod := range pods.Items {
			fmt.Printf("Deleting pod: %s\n", pod.Name)
			CleanResourceWithTimeouts(&pod, pod.GetName(), pod.GetNamespace(), cleanTimeout, cleanInterval)
		}

	}
}

func CleanResource(res client.Object, name string, namespace string) {
	CleanResourceWithTimeouts(res, name, namespace, timeout, interval)
}

func CleanClusterResource(res client.Object, name string, namespace string) {
	CleanResourceWithTimeouts(res, name, namespace, existingClusterTimeout, existingClusterInterval)
}

func checkSecretHasCorrectKeyValue(g Gomega, ns types.NamespacedName, key string, expectedValue string) {
	g.Eventually(func(g Gomega) {
		secret, err := secrets.RetriveSecret(ns, make(map[string]string), k8sClient)
		g.Expect(err).Should(BeNil())
		data := secret.Data[key]
		g.Expect(strings.Contains(string(data), expectedValue)).Should(BeTrue())
	}, timeout, interval).Should(Succeed())
}

func hexShaHashOfMap(props []string) string {
	return hex.EncodeToString(alder32Of(props))
}

func CurrentSpecShortName() string {

	name := path.Base(CurrentSpecReport().LeafNodeLocation.FileName)
	name = strings.ReplaceAll(name, "activemqartemis", "aa")
	name = strings.ReplaceAll(name, "deploy_operator", "do")
	name = strings.ReplaceAll(name, "_test", "")
	name = strings.ReplaceAll(name, ".go", "")
	name = strings.ReplaceAll(name, "_", "-")

	lineNumber := strconv.Itoa(CurrentSpecReport().LeafNodeLocation.LineNumber)

	nameLimit := specShortNameLimit - len(lineNumber)

	if len(name) > nameLimit {
		nameTokens := strings.Split(name, "-")
		name = nameTokens[0]
		for i := 1; i < len(nameTokens) && len(name) < nameLimit; i++ {
			if len(nameTokens[i]) > 3 {
				name += "-" + nameTokens[i][0:3]
			} else if len(nameTokens[i]) > 0 {
				name += "-" + nameTokens[i]
			}
		}
	}

	if len(name) > nameLimit {
		name = name[0:nameLimit]
	}

	name += lineNumber

	return name
}

// The spec resource names are based on current spec short name which has
// max 25 characters (see specShortNameLimit) because the maximum service
// name length is 63 characters.
func NextSpecResourceName() string {
	// The resCount is converted to a letter(97+resCount%25) and appened
	// to the current spec short name to generate a unique resource name.
	// The rune type is an alias for int32 and it is used to distinguish
	// character values from integer values.
	name := CurrentSpecShortName() + string(rune(97+resCount%25))
	resCount++

	return name
}

func newArtemisSpecWithFastProbes() brokerv1beta1.ActiveMQArtemisSpec {
	spec := brokerv1beta1.ActiveMQArtemisSpec{}

	// sensible fast defaults for tests against existing cluster
	spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
		InitialDelaySeconds: 1,
		PeriodSeconds:       3,
	}

	return spec
}

func generateArtemisSpec(namespace string) brokerv1beta1.ActiveMQArtemis {

	toCreate := brokerv1beta1.ActiveMQArtemis{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ActiveMQArtemis",
			APIVersion: brokerv1beta1.GroupVersion.Identifier(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      NextSpecResourceName(),
			Namespace: namespace,
		},
		Spec: newArtemisSpecWithFastProbes(),
	}

	return toCreate
}

func DeployCustomBroker(targetNamespace string, customFunc func(candidate *brokerv1beta1.ActiveMQArtemis)) (*brokerv1beta1.ActiveMQArtemis, *brokerv1beta1.ActiveMQArtemis) {
	ctx := context.Background()
	brokerCrd := generateArtemisSpec(targetNamespace)

	brokerCrd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)

	if customFunc != nil {
		customFunc(&brokerCrd)
	}

	Expect(k8sClient.Create(ctx, &brokerCrd)).Should(Succeed())

	createdBrokerCrd := brokerv1beta1.ActiveMQArtemis{}

	Eventually(func() bool {
		return getPersistedVersionedCrd(brokerCrd.Name, targetNamespace, &createdBrokerCrd)
	}, timeout, interval).Should(BeTrue())
	Expect(createdBrokerCrd.Name).Should(Equal(brokerCrd.ObjectMeta.Name))
	Expect(createdBrokerCrd.Namespace).Should(Equal(targetNamespace))

	return &brokerCrd, &createdBrokerCrd
}

func generateBrokerSpec(namespace string) brokerv1beta2.Broker {
	return brokerv1beta2.Broker{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Broker",
			APIVersion: brokerv1beta2.GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      NextSpecResourceName(),
			Namespace: namespace,
		},
		Spec: brokerv1beta2.BrokerSpec{
			DeploymentPlan: brokerv1beta2.DeploymentPlanType{
				ReadinessProbe: &corev1.Probe{InitialDelaySeconds: 1, PeriodSeconds: 3},
			},
		},
	}
}

func DeployCustomBrokerV1(targetNamespace string, customFunc func(candidate *brokerv1beta2.Broker)) (*brokerv1beta2.Broker, *brokerv1beta2.Broker) {
	ctx := context.Background()
	brokerCrd := generateBrokerSpec(targetNamespace)

	brokerCrd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)

	if customFunc != nil {
		customFunc(&brokerCrd)
	}

	Expect(k8sClient.Create(ctx, &brokerCrd)).Should(Succeed())

	createdBrokerCrd := brokerv1beta2.Broker{}

	Eventually(func() bool {
		return getPersistedVersionedCrd(brokerCrd.Name, targetNamespace, &createdBrokerCrd)
	}, timeout, interval).Should(BeTrue())
	Expect(createdBrokerCrd.Name).Should(Equal(brokerCrd.ObjectMeta.Name))
	Expect(createdBrokerCrd.Namespace).Should(Equal(targetNamespace))

	return &brokerCrd, &createdBrokerCrd
}

func getPersistedVersionedCrd(name string, nameSpace string, object client.Object) bool {
	key := types.NamespacedName{Name: name, Namespace: nameSpace}
	if err := k8sClient.Get(ctx, key, object); err == nil {
		return object.GetResourceVersion() != ""
	}
	return false
}

func DeploySecret(targetNamespace string, customFunc func(candidate *corev1.Secret)) (*corev1.Secret, *corev1.Secret) {
	ctx := context.Background()

	secretName := NextSpecResourceName()
	secretDefinition := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: targetNamespace,
		},
	}

	if customFunc != nil {
		customFunc(&secretDefinition)
	}

	Expect(k8sClient.Create(ctx, &secretDefinition)).Should(Succeed())

	createdSecret := corev1.Secret{}

	Eventually(func() bool {
		return getPersistedVersionedCrd(secretDefinition.Name, targetNamespace, &createdSecret)
	}, timeout, interval).Should(BeTrue())
	Expect(createdSecret.Name).Should(Equal(secretDefinition.ObjectMeta.Name))
	Expect(createdSecret.Namespace).Should(Equal(targetNamespace))

	return &secretDefinition, &createdSecret
}

func generateOriginalArtemisSpec(namespace string, name string) *brokerv1beta1.ActiveMQArtemis {

	spec := newArtemisSpecWithFastProbes()

	toCreate := brokerv1beta1.ActiveMQArtemis{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ActiveMQArtemis",
			APIVersion: brokerv1beta1.GroupVersion.Identifier(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec,
	}

	return &toCreate
}

func DeployBroker(brokerName string, targetNamespace string) (*brokerv1beta1.ActiveMQArtemis, *brokerv1beta1.ActiveMQArtemis) {
	ctx := context.Background()
	brokerCrd := generateOriginalArtemisSpec(targetNamespace, brokerName)

	Expect(k8sClient.Create(ctx, brokerCrd)).Should(Succeed())

	createdBrokerCrd := &brokerv1beta1.ActiveMQArtemis{}

	Eventually(func() bool {
		return getPersistedVersionedCrd(brokerCrd.ObjectMeta.Name, targetNamespace, createdBrokerCrd)
	}, timeout, interval).Should(BeTrue())
	Expect(createdBrokerCrd.Name).Should(Equal(createdBrokerCrd.ObjectMeta.Name))

	return brokerCrd, createdBrokerCrd

}

func DeploySecurity(secName string, targetNamespace string, customFunc func(candidate *brokerv1beta1.ActiveMQArtemisSecurity)) (*brokerv1beta1.ActiveMQArtemisSecurity, *brokerv1beta1.ActiveMQArtemisSecurity) {
	ctx := context.Background()
	secCrd := generateSecuritySpec(secName, targetNamespace)

	brokerDomainName := "activemq"
	loginModuleName := "module1"
	loginModuleFlag := "sufficient"

	loginModuleList := make([]brokerv1beta1.PropertiesLoginModuleType, 1)
	propLoginModule := brokerv1beta1.PropertiesLoginModuleType{
		Name: loginModuleName,
		Users: []brokerv1beta1.UserType{
			{
				Name:     "user1",
				Password: &okDefaultPwd,
				Roles: []string{
					"admin", "amq",
				},
			},
		},
	}
	loginModuleList = append(loginModuleList, propLoginModule)
	secCrd.Spec.LoginModules.PropertiesLoginModules = loginModuleList

	secCrd.Spec.SecurityDomains.BrokerDomain = brokerv1beta1.BrokerDomainType{
		Name: &brokerDomainName,
		LoginModules: []brokerv1beta1.LoginModuleReferenceType{
			{
				Name: &loginModuleName,
				Flag: &loginModuleFlag,
			},
		},
	}

	if customFunc != nil {
		customFunc(secCrd)
	}

	Expect(k8sClient.Create(ctx, secCrd)).Should(Succeed())

	createdSecCrd := &brokerv1beta1.ActiveMQArtemisSecurity{}

	Eventually(func() bool {
		return getPersistedVersionedCrd(secCrd.ObjectMeta.Name, targetNamespace, createdSecCrd)
	}, timeout, interval).Should(BeTrue())
	Expect(createdSecCrd.Name).Should(Equal(secCrd.ObjectMeta.Name))

	return secCrd, createdSecCrd
}

func generateSecuritySpec(secName string, targetNamespace string) *brokerv1beta1.ActiveMQArtemisSecurity {

	spec := brokerv1beta1.ActiveMQArtemisSecuritySpec{}

	theName := secName
	if secName == "" {
		theName = randString()
	}

	toCreate := brokerv1beta1.ActiveMQArtemisSecurity{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ActiveMQArtemisSecurity",
			APIVersion: brokerv1beta1.GroupVersion.Identifier(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      theName,
			Namespace: targetNamespace,
		},
		Spec: spec,
	}

	return &toCreate
}

func RunCommandInPod(podName string, containerName string, command []string) (*string, error) {
	return RunCommandInPodWithNamespace(podName, defaultNamespace, containerName, command)
}

func RunCommandInPodWithNamespace(podName string, podNamespace string, containerName string, command []string) (*string, error) {
	gvk := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}
	httpClient, err := rest.HTTPClientFor(restConfig)
	Expect(err).To(BeNil())
	restClient, err := apiutil.RESTClientForGVK(gvk, false, restConfig, serializer.NewCodecFactory(scheme.Scheme), httpClient)
	Expect(err).To(BeNil())
	execReq := restClient.
		Post().
		Namespace(podNamespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: containerName,
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
		}, runtime.NewParameterCodec(scheme.Scheme))

	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", execReq.URL())

	if err != nil {
		return nil, err
	}

	var consumerCapturedOut bytes.Buffer
	var consumerCapturedErr bytes.Buffer

	execCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()

	err = exec.StreamWithContext(execCtx, remotecommand.StreamOptions{
		Stdin:  os.Stdin,
		Stdout: &consumerCapturedOut,
		Stderr: &consumerCapturedErr,
		Tty:    false,
	})
	if err != nil {
		errMsg := err.Error()
		if consumerCapturedErr.Len() > 0 {
			errMsg += ", stderr: " + consumerCapturedErr.String()
		}
		if consumerCapturedOut.Len() > 0 {
			errMsg += ", stdout: " + consumerCapturedOut.String()
		}
		return nil, fmt.Errorf("%s", errMsg)
	}

	//try get some content if any
	Eventually(func(g Gomega) {
		g.Expect(consumerCapturedOut.Len() > 0 || consumerCapturedErr.Len() > 0)
	}, timeout, interval).Should(Succeed())

	content := consumerCapturedOut.String()
	if consumerCapturedErr.Len() > 0 {
		content += ", err: " + consumerCapturedErr.String()
	}
	return &content, nil
}

func EventsOfPod(podWithOrdinal string, namespace string, g Gomega) *corev1.EventList {

	cfg, err := config.GetConfig()
	g.Expect(err).To(BeNil())

	clientset, err := kubernetes.NewForConfig(cfg)
	g.Expect(err).To(BeNil())

	events, err := clientset.CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{
		FieldSelector: "involvedObject.name=" + podWithOrdinal,
		TypeMeta:      metav1.TypeMeta{Kind: "Pod"},
	})
	g.Expect(err).To(BeNil())

	return events
}

func LogsOfPod(podWithOrdinal string, brokerName string, namespace string, g Gomega) string {

	gvk := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}
	httpClient, err := rest.HTTPClientFor(restConfig)
	Expect(err).To(BeNil())
	restClient, err := apiutil.RESTClientForGVK(gvk, false, restConfig, serializer.NewCodecFactory(scheme.Scheme), httpClient)
	g.Expect(err).To(BeNil())

	readCloser, err := restClient.
		Get().
		Namespace(namespace).
		Resource("pods").
		Name(podWithOrdinal).
		SubResource("log").
		VersionedParams(&corev1.PodLogOptions{
			Container: brokerName + "-container",
		}, runtime.NewParameterCodec(scheme.Scheme)).Stream(context.TODO())
	g.Expect(err).To(BeNil())

	defer readCloser.Close()

	result, err := io.ReadAll(readCloser)
	g.Expect(err).To(BeNil())

	return string(result)
}

func ExecOnPod(podWithOrdinal string, brokerName string, namespace string, command []string, g Gomega) string {

	gvk := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}
	httpClient, err := rest.HTTPClientFor(restConfig)
	g.Expect(err).To(BeNil())
	restClient, err := apiutil.RESTClientForGVK(gvk, false, restConfig, serializer.NewCodecFactory(scheme.Scheme), httpClient)
	g.Expect(err).To(BeNil())

	execReq := restClient.
		Post().
		Namespace(namespace).
		Resource("pods").
		Name(podWithOrdinal).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: brokerName + "-container",
			Command:   command,
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
		}, runtime.NewParameterCodec(scheme.Scheme))

	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", execReq.URL())
	g.Expect(err).To(BeNil())

	var outPutbuffer bytes.Buffer
	var errBuffer bytes.Buffer

	By("executing " + fmt.Sprintf(" command: %v", command))
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	err = exec.StreamWithContext(execCtx, remotecommand.StreamOptions{
		Stdin:  os.Stdin,
		Stdout: &outPutbuffer,
		Stderr: &errBuffer,
		Tty:    false,
	})
	g.Expect(err).To(BeNil(), "stderr:", errBuffer.String(), "stdout:", outPutbuffer.String())

	g.Eventually(func(g Gomega) {
		By("Checking for output from " + fmt.Sprintf(" command: %v", command))
		g.Expect(outPutbuffer.Len() > 0)
		if verbose {
			fmt.Printf("\n%v %v resulted in %s\n", time.Now(), command, outPutbuffer.String())
		}
	}, timeout, interval*5).Should(Succeed())

	return outPutbuffer.String()
}

func GenerateAddressSpec(name string, ns string, address string, queue string, isMulticast bool, autoDelete bool) *brokerv1beta1.ActiveMQArtemisAddress {

	spec := brokerv1beta1.ActiveMQArtemisAddressSpec{}

	spec.AddressName = address
	spec.QueueName = &queue

	routingType := "anycast"
	if isMulticast {
		routingType = "multicast"
	}
	spec.RoutingType = &routingType

	toCreate := &brokerv1beta1.ActiveMQArtemisAddress{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ActiveMQArtemisAddress",
			APIVersion: brokerv1beta1.GroupVersion.Identifier(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: spec,
	}

	return toCreate
}

func DeployAddress(candidate *brokerv1beta1.ActiveMQArtemisAddress) {
	ctx := context.Background()

	Expect(k8sClient.Create(ctx, candidate)).Should(Succeed())

	createdAddressCrd := &brokerv1beta1.ActiveMQArtemisAddress{}

	Eventually(func() bool {
		return getPersistedVersionedCrd(candidate.ObjectMeta.Name, candidate.Namespace, createdAddressCrd)
	}, timeout, interval).Should(BeTrue())
	Expect(createdAddressCrd.Name).Should(Equal(candidate.ObjectMeta.Name))
}

func DeployCustomAddress(targetNamespace string, customFunc func(candidate *brokerv1beta1.ActiveMQArtemisAddress)) (*brokerv1beta1.ActiveMQArtemisAddress, *brokerv1beta1.ActiveMQArtemisAddress) {

	ctx := context.Background()
	addressCr := GenerateAddressSpec(NextSpecResourceName(), targetNamespace, "myAddress", "myQueue", false, true)

	if customFunc != nil {
		customFunc(addressCr)
	}

	Expect(k8sClient.Create(ctx, addressCr)).Should(Succeed())

	createdAddressCr := brokerv1beta1.ActiveMQArtemisAddress{}

	Eventually(func() bool {
		return getPersistedVersionedCrd(addressCr.Name, targetNamespace, &createdAddressCr)
	}, timeout, interval).Should(BeTrue())
	Expect(createdAddressCr.Name).Should(Equal(addressCr.Name))
	Expect(createdAddressCr.Namespace).Should(Equal(targetNamespace))

	return addressCr, &createdAddressCr
}

func GetOperatorLog(ns string) (*string, error) {
	var cfg *rest.Config
	var err error
	if cfg, err = config.GetConfig(); err == nil {

		var labelSelector labels.Selector
		if labelSelector, err = labels.Parse("control-plane=controller-manager,name=" + oprName); err == nil {

			var clientset *kubernetes.Clientset
			if clientset, err = kubernetes.NewForConfig(cfg); err == nil {

				listOpts := metav1.ListOptions{
					LabelSelector: labelSelector.String(),
				}
				var podList *corev1.PodList
				if podList, err = clientset.CoreV1().Pods(ns).List(ctx, listOpts); err == nil {

					if len(podList.Items) != 1 {
						err = fmt.Errorf("expect a single pod")
					} else {

						operatorPod := podList.Items[0]
						podLogOpts := corev1.PodLogOptions{}
						req := clientset.CoreV1().Pods(ns).GetLogs(operatorPod.Name, &podLogOpts)

						var podLogs io.ReadCloser
						if podLogs, err = req.Stream(context.Background()); err == nil {
							defer podLogs.Close()
							buf := new(bytes.Buffer)
							if _, err = io.Copy(buf, podLogs); err == nil {
								str := buf.String()
								return &str, nil
							}
						}
					}
				}
			}
		}
	}
	return nil, err
}

func NewPriveKey() (*rsa.PrivateKey, error) {
	caPrivKey, err := rsa.GenerateKey(crand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	if err := caPrivKey.Validate(); err != nil {
		return nil, err
	}
	return caPrivKey, nil
}

// generate a keystore file in bytes
// the keystore contains a self signed cert
func GenerateKeystore(password string, dnsNames []string) ([]byte, error) {
	// create the key pair
	caPrivKey, err := NewPriveKey()
	if err != nil {
		return nil, err
	}
	if err := caPrivKey.Validate(); err != nil {
		return nil, err
	}

	// set up our CA certificate
	ca := &x509.Certificate{
		SerialNumber: big.NewInt(202305071030),
		Subject: pkix.Name{
			CommonName:         "arkmq-org Broker",
			OrganizationalUnit: []string{"Broker"},
			Organization:       []string{"arkmq-org"},
		},
		NotBefore:          time.Now(),
		NotAfter:           time.Now().AddDate(10, 0, 0),
		IsCA:               false,
		SignatureAlgorithm: x509.SHA256WithRSA,
		ExtKeyUsage:        []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if len(dnsNames) > 0 {
		// Subject Alternative Names
		ca.DNSNames = dnsNames
	}

	// create the self-signed CA
	caBytes, err := x509.CreateCertificate(crand.Reader, ca, ca, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(caBytes)
	if err != nil {
		return nil, err
	}

	ksBytes, err := pkcs12.Modern.Encode(caPrivKey, cert, []*x509.Certificate{}, password)
	if err != nil {
		return nil, err
	}

	return ksBytes, nil
}

func GenerateTrustStoreFromKeyStore(ksBytes []byte, password string) ([]byte, error) {

	_, cert, _, err := pkcs12.DecodeChain(ksBytes, password)

	if err != nil {
		return nil, err
	}

	pfxBytes, err := pkcs12.Modern.EncodeTrustStore([]*x509.Certificate{cert}, password)

	if err != nil {
		return nil, err
	}

	return pfxBytes, nil
}

func CreateTlsSecret(secretName string, ns string, ksPassword string, nsNames []string) (*corev1.Secret, error) {

	certData := make(map[string][]byte)
	stringData := make(map[string]string)

	brokerKs, ferr := GenerateKeystore(ksPassword, nsNames)
	if ferr != nil {
		return nil, ferr
	}
	clientTs, ferr := GenerateTrustStoreFromKeyStore(brokerKs, ksPassword)
	if ferr != nil {
		return nil, ferr
	}

	certData["broker.ks"] = brokerKs
	certData["client.ts"] = clientTs
	stringData["keyStorePassword"] = ksPassword
	stringData["trustStorePassword"] = ksPassword

	tlsSecret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: ns,
		},
		Data:       certData,
		StringData: stringData,
	}
	return &tlsSecret, nil
}

func StringToPtr(v string) *string {
	return &v
}

func DeployCustomPVC(name string, targetNamespace string, customFunc func(candidate *corev1.PersistentVolumeClaim)) (*corev1.PersistentVolumeClaim, *corev1.PersistentVolumeClaim) {
	ctx := context.Background()
	pvc := corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: targetNamespace,
		},
	}

	if customFunc != nil {
		customFunc(&pvc)
	}

	Expect(k8sClient.Create(ctx, &pvc)).Should(Succeed())

	createdPvc := corev1.PersistentVolumeClaim{}

	Eventually(func() bool {
		return getPersistedVersionedCrd(pvc.Name, targetNamespace, &createdPvc)
	}, timeout, interval).Should(BeTrue())
	Expect(createdPvc.Name).Should(Equal(pvc.Name))
	Expect(createdPvc.Namespace).Should(Equal(targetNamespace))

	return &pvc, &createdPvc
}

func WaitForPod(crName string) {
	WaitForPods(crName, 0)
}

func WaitForPods(crName string, iPods ...int32) {
	ssKey := types.NamespacedName{
		Name:      namer.CrToSS(crName),
		Namespace: defaultNamespace,
	}

	currentSS := &appsv1.StatefulSet{}

	for podOrdinal := range iPods {
		podKey := types.NamespacedName{Name: namer.CrToSSOrdinal(crName, podOrdinal), Namespace: defaultNamespace}
		pod := &corev1.Pod{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
			g.Expect(k8sClient.Get(ctx, podKey, pod)).Should(Succeed())
			g.Expect(len(pod.Status.ContainerStatuses)).Should(Equal(1))
			if verbose {
				fmt.Printf("container status for pod %s in %v\n", podKey.Name, pod.Status.ContainerStatuses[0])
			}
			g.Expect(pod.Status.ContainerStatuses[0].State.Running).ShouldNot(BeNil())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	}
}

func InstallCertManager() error {
	fmt.Printf("Installing cert-manager using %s\n", helmCmd)

	cmd := exec.Command(helmCmd, "repo", "add", "jetstack", "https://charts.jetstack.io", "--force-update")
	err := cmd.Run()
	if err != nil {
		return err
	}
	// cert manager
	cmd = exec.Command(helmCmd, "upgrade", "-i", "-n", "cert-manager", "cert-manager", "jetstack/cert-manager", "--set", "installCRDs=true", "--wait", "--create-namespace")
	err = cmd.Run()
	if err != nil {
		return err
	}
	// Wait for cert-manager-webhook to be ready, which can take time if cert-manager
	// was re-installed after uninstalling on a cluster.
	cmd = exec.Command(kubeTool, "wait", "deployment.apps/cert-manager-webhook",
		"--for", "condition=Available",
		"--namespace", "cert-manager",
		"--timeout", "5m")
	err = cmd.Run()
	if err != nil {
		fmt.Printf("error waiting cert-manager %v\n", err)
		return err
	}
	// trust manager
	// https://cert-manager.io/docs/trust/trust-manager/installation/
	cmd = exec.Command(helmCmd, "upgrade", "-i", "-n", "cert-manager", "trust-manager", "jetstack/trust-manager", "--set", "secretTargets.enabled=true", "--set", "secretTargets.authorizedSecretsAll=true", "--wait")
	err = cmd.Run()

	if err != nil {
		fmt.Printf("error waiting cert-manager %v\n", err)
	}

	return err
}

func UninstallCertManager() error {

	//trust manager
	cmd := exec.Command(helmCmd, "uninstall", "-n", "cert-manager", "trust-manager")
	err := cmd.Run()
	if err != nil {
		return err
	}
	//cert manager
	cmd = exec.Command(helmCmd, "uninstall", "-n", "cert-manager", "cert-manager")
	err = cmd.Run()
	if err != nil {
		return err
	}
	//namespace
	cmd = exec.Command(kubeTool, "delete", "namespace", "cert-manager")
	err = cmd.Run()
	return err
}

func CertManagerInstalled() bool {
	cmDeploymentKey := types.NamespacedName{Name: "cert-manager", Namespace: "cert-manager"}
	cmDeployment := &appsv1.Deployment{}
	err := k8sClient.Get(ctx, cmDeploymentKey, cmDeployment)
	return err == nil
}

func InstallClusteredIssuer(issuerName string, customFunc func(*cmv1.ClusterIssuer)) *cmv1.ClusterIssuer {
	issuer := cmv1.ClusterIssuer{}
	if k8sClient.Get(ctx, types.NamespacedName{Name: issuerName, Namespace: defaultNamespace}, &issuer) == nil {
		CleanResource(&issuer, issuerName, defaultNamespace)
	}

	issuer = cmv1.ClusterIssuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ClusterIssuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: issuerName,
		},
		Spec: cmv1.IssuerSpec{
			IssuerConfig: cmv1.IssuerConfig{
				SelfSigned: &cmv1.SelfSignedIssuer{},
			},
		},
	}
	if customFunc != nil {
		customFunc(&issuer)
	}
	k8sClient.Delete(ctx, &issuer)
	Expect(k8sClient.Create(ctx, &issuer, &client.CreateOptions{})).To(Succeed())
	issKey := types.NamespacedName{Name: issuerName, Namespace: defaultNamespace}
	currentIssuer := &cmv1.ClusterIssuer{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, issKey, currentIssuer)).Should(Succeed())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	return currentIssuer
}

func InstallCert(certName string, namespace string, customFunc func(candidate *cmv1.Certificate)) *cmv1.Certificate {
	cmCert := cmv1.Certificate{}
	if k8sClient.Get(ctx, types.NamespacedName{Name: certName, Namespace: defaultNamespace}, &cmCert) == nil {
		CleanResource(&cmCert, certName, defaultNamespace)
	}

	cmCert = cmv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      certName,
			Namespace: namespace,
		},
		Spec: cmv1.CertificateSpec{
			SecretName: certName + "-secret",
			DNSNames:   defaultSanDnsNames,
			Subject: &cmv1.X509Subject{
				Organizations: []string{"www.arkmq-org.io"},
			},
		},
	}
	if customFunc != nil {
		customFunc(&cmCert)
	}

	k8sClient.Delete(ctx, &cmCert)
	Expect(k8sClient.Create(ctx, &cmCert, &client.CreateOptions{})).To(Succeed())

	certKey := types.NamespacedName{Name: certName, Namespace: namespace}
	cert := &cmv1.Certificate{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, certKey, cert)).Should(Succeed())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	secretKey := types.NamespacedName{Name: cmCert.Spec.SecretName, Namespace: namespace}
	secret := corev1.Secret{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, secretKey, &secret)).Should(Succeed())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	return &cmCert
}

func InstallCaBundle(bundleName string, sourceSecret string, caFileName string) *tm.Bundle {
	bundle := tm.Bundle{}
	if k8sClient.Get(ctx, types.NamespacedName{Name: bundleName, Namespace: defaultNamespace}, &bundle) == nil {
		CleanResource(&bundle, bundleName, defaultNamespace)
	}

	bundle = tm.Bundle{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "trust.cert-manager.io/v1alpha1",
			Kind:       "Bundle",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      bundleName,
			Namespace: "cert-manager",
		},
		Spec: tm.BundleSpec{
			Sources: []tm.BundleSource{
				{
					Secret: &tm.SourceObjectKeySelector{
						Name: sourceSecret,
						KeySelector: tm.KeySelector{
							Key: "tls.crt",
						},
					},
				},
			},
			Target: tm.BundleTarget{
				Secret: &tm.KeySelector{
					Key: caFileName,
				},
			},
		},
	}

	k8sClient.Delete(ctx, &bundle)
	Expect(k8sClient.Create(ctx, &bundle, &client.CreateOptions{})).To(Succeed())
	bundleKey := types.NamespacedName{Name: bundleName, Namespace: "cert-manager"}
	newBundle := &tm.Bundle{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, bundleKey, newBundle)).Should(Succeed())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	return newBundle
}

func UnInstallCaBundle(bundleName string) {
	bundleKey := types.NamespacedName{Name: bundleName, Namespace: "cert-manager"}
	bundle := &tm.Bundle{}

	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, bundleKey, bundle)).Should(Succeed())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	CleanResource(bundle, bundleName, bundle.Namespace)
}

func InstallSecret(secretName string, namespace string, configFunc func(candidate *corev1.Secret)) *corev1.Secret {
	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		StringData: make(map[string]string),
	}
	if configFunc != nil {
		configFunc(&secret)
	}

	Expect(k8sClient.Create(ctx, &secret, &client.CreateOptions{})).To(Succeed())
	certKey := types.NamespacedName{Name: secretName, Namespace: namespace}
	newSecret := &corev1.Secret{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, certKey, newSecret)).Should(Succeed())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	return &secret
}

func UninstallCert(certName string, namespace string) {
	certKey := types.NamespacedName{Name: certName, Namespace: namespace}
	cert := &cmv1.Certificate{}

	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, certKey, cert)).Should(Succeed())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	CleanResource(cert, certName, namespace)

	certSecret := corev1.Secret{}
	secretKey := types.NamespacedName{Name: cert.Spec.SecretName, Namespace: namespace}
	Expect(k8sClient.Get(ctx, secretKey, &certSecret)).To(Succeed())

	CleanResource(&certSecret, certSecret.Name, certSecret.Namespace)
}

func UninstallSecret(secretName string, namespace string) {
	secretKey := types.NamespacedName{Name: secretName, Namespace: namespace}
	secret := &corev1.Secret{}

	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, secretKey, secret)).Should(Succeed())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	CleanResource(secret, secretName, namespace)
}

func UninstallClusteredIssuer(issuerName string) {
	issKey := types.NamespacedName{Name: issuerName, Namespace: defaultNamespace}
	currentIssuer := &cmv1.ClusterIssuer{}

	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, issKey, currentIssuer)).Should(Succeed())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	CleanResource(currentIssuer, issuerName, defaultNamespace)
}

type NillCluster struct {
}

func (m *NillCluster) GetHTTPClient() *http.Client {
	return nil
}
func (m *NillCluster) GetConfig() *rest.Config {
	return nil
}
func (m *NillCluster) GetCache() cache.Cache {
	return nil
}
func (m *NillCluster) GetScheme() *runtime.Scheme {
	return nil
}
func (m *NillCluster) GetClient() client.Client {
	return nil
}
func (m *NillCluster) GetFieldIndexer() client.FieldIndexer {
	return nil
}
func (m *NillCluster) GetEventRecorderFor(name string) record.EventRecorder {
	return nil
}
func (m *NillCluster) GetRESTMapper() meta.RESTMapper {
	return nil
}
func (m *NillCluster) GetAPIReader() client.Reader {
	return nil
}
func (m *NillCluster) Start(ctx context.Context) error {
	return nil
}

func CloneStringMap(original map[string]string) map[string]string {
	copy := make(map[string]string)
	for key, value := range original {
		copy[key] = value
	}
	return copy
}

func CreateOrOverwriteResource(res client.Object) {
	err := k8sClient.Create(ctx, res)
	if errors.IsAlreadyExists(err) {
		k8sClient.Delete(ctx, res)

		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Create(ctx, res)).To(Succeed())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	} else {
		Expect(err).To(Succeed())
	}
}

func FromUnstructuredToRawExtension(u *unstructured.Unstructured) runtime.RawExtension {
	rawBytes, err := json.Marshal(u.Object)
	if err != nil {
		PanicWith(err)
	}
	return runtime.RawExtension{Raw: rawBytes}
}
