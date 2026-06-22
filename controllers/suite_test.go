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
	"container/list"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	"go.uber.org/zap/zapcore"
	"golang.org/x/crypto/ssh"

	"path/filepath"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	brokerv1alpha1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1alpha1"
	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	brokerv2alpha1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha1"
	brokerv2alpha2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha2"
	brokerv2alpha3 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha3"
	brokerv2alpha4 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha4"
	brokerv2alpha5 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v2alpha5"

	//+kubebuilder:scaffold:imports

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/ingresses"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	tm "github.com/cert-manager/trust-manager/pkg/apis/trust/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

// Define utility constants for object names and testing timeouts/durations and intervals.
const (
	defaultNamespace                   = "test"
	otherNamespace                     = "other"
	restrictedNamespace                = "restricted"
	timeout                            = time.Second * 30
	duration                           = time.Second * 10
	interval                           = time.Millisecond * 500
	existingClusterTimeout             = time.Second * 180
	existingClusterConsistentlyTimeout = time.Second * 20
	existingClusterInterval            = time.Second * 2
	namespace1                         = "namespace1"
	namespace2                         = "namespace2"
	namespace3                         = "namespace3"
	specShortNameLimit                 = 25

	// Default ingress domain for tests
	defaultTestIngressDomain = "tests.arkmq-org.io"
)

var (
	resCount   int64
	specCount  int64
	currentDir string
	k8sClient  client.Client
	restConfig *rest.Config
	testEnv    *envtest.Environment
	ctx        context.Context
	cancel     context.CancelFunc

	// the cluster url
	clusterUrl *url.URL

	// the cluster ingress host
	clusterIngressHost string

	// the manager may be stopped/restarted via tests
	managerCtx    context.Context
	managerCancel context.CancelFunc
	k8Manager     manager.Manager

	brokerReconciler   *ActiveMQArtemisReconciler
	securityReconciler *ActiveMQArtemisSecurityReconciler

	depName string
	oprName string
	oprRes  = []string{
		"../deploy/service_account.yaml",
		"../deploy/role.yaml",
		"../deploy/role_binding.yaml",
		"../deploy/election_role.yaml",
		"../deploy/election_role_binding.yaml",
		"../deploy/operator.yaml",
	}
	managerChannel chan struct{}

	capturingLogWriter = common.BufferWriter{}

	artemisGvk = schema.GroupVersionKind{Group: "broker", Version: "v1beta1", Kind: "ActiveMQArtemis"}

	isFIPSEnabled                             = false
	isOpenshift                               = false
	isIngressSSLPassthroughEnabled            = false
	verbose                                   = false
	verboseWithWatch                          = false
	kubeTool                                  = "kubectl"
	defaultOperatorInstalled                  = true
	defaultUid                                = int64(185)
	watchClientList                *list.List = nil
	testProxyLog                   logr.Logger
)

func init() {
	if isVerboseStr, defined := os.LookupEnv("TEST_VERBOSE"); defined {
		if isVerbose, err := strconv.ParseBool(isVerboseStr); err == nil {
			verbose = isVerbose
		}
	}
	if isVerboseStr, defined := os.LookupEnv("TEST_VERBOSE_WITH_WATCH"); defined {
		if isVerbose, err := strconv.ParseBool(isVerboseStr); err == nil {
			verboseWithWatch = isVerbose
		}
	}
}

func TestAPIs(t *testing.T) {

	RegisterFailHandler(Fail)

	suiteConfig, _ := GinkgoConfiguration()
	suiteConfig.Timeout = time.Duration(duration.Minutes()) * 20
	RunSpecs(t, "Controller Suite", suiteConfig)
}

func setUpEnvTest() {
	// for run in ide
	// os.Setenv("KUBEBUILDER_ASSETS", " .. <path from makefile> /kubebuilder-envtest/k8s/1.22.1-linux-amd64")
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	restConfig, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(restConfig).NotTo(BeNil())

	clusterUrl, err = url.Parse(testEnv.Config.Host)
	Expect(err).NotTo(HaveOccurred())

	setUpK8sClient()

	isOpenshift, err = common.DetectOpenshiftWith(restConfig)
	Expect(err).NotTo(HaveOccurred())

	if isOpenshift {
		kubeTool = "oc"
	}

	if isOpenshift {
		clusterConfig := &corev1.ConfigMap{}
		clusterConfigKey := types.NamespacedName{Name: "cluster-config-v1", Namespace: "kube-system"}
		clusterConfigErr := k8sClient.Get(ctx, clusterConfigKey, clusterConfig)
		if clusterConfigErr == nil {
			isFIPSEnabled = strings.Contains(clusterConfig.Data["install-config"], "fips: true")
		}
	}

	setUpIngress()

	setUpNamespace()

	setUpTestProxy()

	createControllerManagerForSuite()
}

func setUpNamespace() {
	testNamespace := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultNamespace,
			Namespace: defaultNamespace,
		},
		Spec: corev1.NamespaceSpec{},
	}

	err := k8sClient.Create(ctx, &testNamespace)
	Expect(err == nil || errors.IsAlreadyExists(err)).To(BeTrue())

	if isOpenshift {
		Eventually(func(g Gomega) {
			testNamespaceKey := types.NamespacedName{Name: defaultNamespace}
			g.Expect(k8sClient.Get(ctx, testNamespaceKey, &testNamespace)).Should(Succeed())
			uidRange := testNamespace.Annotations["openshift.io/sa.scc.uid-range"]
			g.Expect(uidRange).ShouldNot(BeEmpty())
			uidRangeTokens := strings.Split(uidRange, "/")
			defaultUid, err = strconv.ParseInt(uidRangeTokens[0], 10, 64)
			g.Expect(err).Should(Succeed())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	}
}

func setUpIngress() {
	isIngressSSLPassthroughEnabled = isOpenshift
	clusterIngressHost = clusterUrl.Hostname()

	ingressConfig := &configv1.Ingress{}
	ingressConfigKey := types.NamespacedName{Name: "cluster"}
	ingressConfigErr := k8sClient.Get(ctx, ingressConfigKey, ingressConfig)

	if ingressConfigErr == nil {
		if isOpenshift {
			clusterIngressHost = "console-openshift-console." + ingressConfig.Spec.Domain
		} else {
			clusterIngressHost = "ingress." + ingressConfig.Spec.Domain
		}
	} else {
		ingressNginxControllerDeployment := &appsv1.Deployment{}
		ingressNginxControllerDeploymentKey := types.NamespacedName{Name: "ingress-nginx-controller", Namespace: "ingress-nginx"}
		err := k8sClient.Get(ctx, ingressNginxControllerDeploymentKey, ingressNginxControllerDeployment)

		if err == nil {
			if len(ingressNginxControllerDeployment.Spec.Template.Spec.Containers) > 0 {
				ingressNginxControllerContainer := &ingressNginxControllerDeployment.Spec.Template.Spec.Containers[0]

				for i := 0; i < len(ingressNginxControllerContainer.Args); i++ {
					if ingressNginxControllerContainer.Args[i] == "--enable-ssl-passthrough" {
						isIngressSSLPassthroughEnabled = true
						break
					}
				}

				if !isIngressSSLPassthroughEnabled && os.Getenv("ENABLE_INGRESS_SSL_PASSTHROUGH") == "true" {
					ingressNginxControllerContainer.Args = append(ingressNginxControllerContainer.Args, "--enable-ssl-passthrough")

					Expect(k8sClient.Update(ctx, ingressNginxControllerDeployment)).Should(Succeed())

					isIngressSSLPassthroughEnabled = true
				}
			}
		}
	}
}

// Set up test-proxy for external http requests
func setUpTestProxy() {

	testProxyPort := int32(44322)
	testProxyDeploymentReplicas := int32(1)
	testProxyName := "test-proxy"
	testProxyNamespace := "default"
	testProxyHost := testProxyName + ".tests.arkmq-org.io"
	testProxyLabels := map[string]string{"app": "test-proxy"}
	testProxyScript := fmt.Sprintf("yum -y install openssh-server openssl stunnel && "+
		"adduser --system -u 1000 tunnel && echo secret | passwd tunnel --stdin && "+
		"sed -i 's/#Port.*$/Port 2022/' /etc/ssh/sshd_config && ssh-keygen -A && "+
		"echo -e 'cert=%[1]s \n[ssh]\naccept=44322\nconnect=2022' > %[2]s && "+
		"openssl req -new -x509 -days 365 -nodes -subj '/CN=test-proxy' -keyout %[1]s -out %[1]s && "+
		"stunnel %[2]s && /usr/sbin/sshd -eD", "/etc/stunnel/stunnel.pem", "/etc/stunnel/stunnel.conf")

	testProxyLog = ctrl.Log.WithName(testProxyName)

	testProxyDeployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testProxyName + "-dep",
			Namespace: testProxyNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: testProxyLabels,
			},
			Replicas: &testProxyDeploymentReplicas,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: testProxyLabels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    testProxyName + "-con",
							Image:   "registry.access.redhat.com/ubi8/ubi:8.9",
							Command: []string{"sh", "-c", testProxyScript},
							Env: []corev1.EnvVar{
								{Name: "HTTP_PROXY", Value: os.Getenv("HTTP_PROXY")},
								{Name: "HTTPS_PROXY", Value: os.Getenv("HTTPS_PROXY")},
								{Name: "http_proxy", Value: os.Getenv("http_proxy")},
								{Name: "https_proxy", Value: os.Getenv("https_proxy")},
							},
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: testProxyPort,
									Protocol:      "TCP",
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Capabilities: &corev1.Capabilities{
									Add: []corev1.Capability{
										"SYS_CHROOT",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	CreateOrOverwriteResource(&testProxyDeployment)

	testProxyService := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testProxyName + "-dep-svc",
			Namespace: testProxyNamespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: testProxyLabels,
			Ports: []corev1.ServicePort{
				{
					Port:       testProxyPort,
					TargetPort: intstr.IntOrString{IntVal: testProxyPort},
				},
			},
		},
	}
	CreateOrOverwriteResource(&testProxyService)

	testProxyIngress := ingresses.NewIngressForCRWithSSL(
		nil, types.NamespacedName{Name: testProxyName, Namespace: testProxyNamespace},
		map[string]string{}, testProxyName+"-dep-svc", strconv.FormatInt(int64(testProxyPort), 10),
		true, "", testProxyHost, isOpenshift)
	CreateOrOverwriteResource(testProxyIngress)

	if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
		Eventually(func(g Gomega) {
			tlsDialer := new(net.Dialer)
			// The tls proxy dialer timeout reduce the proxy setup time because the
			// fisrt connection usually fails and TCP timeouts are often around 3 mins
			tlsDialer.Timeout = 3 * time.Second
			tlsConn, tlsErr := tls.DialWithDialer(tlsDialer, "tcp", clusterIngressHost+":443",
				&tls.Config{ServerName: testProxyHost, InsecureSkipVerify: true})
			g.Expect(tlsErr).Should(BeNil())
			tlsConn.Close()
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	}

	http.DefaultTransport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			tlsConn, tlsErr := tls.Dial("tcp", clusterIngressHost+":443",
				&tls.Config{ServerName: testProxyHost, InsecureSkipVerify: true})
			if tlsErr != nil {
				testProxyLog.V(1).Info("Error creating tls connection", "addr", addr, "error", tlsErr)
				return nil, fmt.Errorf("Error creating tls connection to %s: %v", addr, tlsErr)
			}

			sshConn, sshChans, sshReqs, sshErr := ssh.NewClientConn(tlsConn, "127.0.0.1:2022", &ssh.ClientConfig{
				User:            "tunnel",
				Auth:            []ssh.AuthMethod{ssh.Password("secret")},
				HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			})
			if sshErr != nil {
				testProxyLog.V(1).Info("Error creating SSH connection", "addr", addr, "error", sshErr)
				tlsConn.Close()
				return nil, fmt.Errorf("Error creating SSH connection to %s: %v", addr, sshErr)
			}

			sshClient := ssh.NewClient(sshConn, sshChans, sshReqs)

			sshClientConn, sshClientErr := sshClient.DialContext(ctx, network, addr)
			if sshClientErr != nil {
				testProxyLog.V(1).Info("Error creating SSH tunnel", "addr", addr, "error", sshClientErr)
				sshClient.Close()
				sshConn.Close()
				tlsConn.Close()
				return nil, fmt.Errorf("Error creating SSH tunnel to %s: %v", addr, sshClientErr)
			}

			testProxyLog.V(1).Info("Opened SSH tunnel", "addr", addr)
			return &testProxyConn{sshClientConn, addr, sshClient, &sshConn, tlsConn}, nil
		},
	}
}

type testProxyConn struct {
	net.Conn
	addr      string
	sshClient *ssh.Client
	sshConn   *ssh.Conn
	tlsConn   *tls.Conn
}

func (w *testProxyConn) Close() error {
	testProxyLog.V(1).Info("Closed SSH tunnel", "addr", w.addr)
	w.Conn.Close()
	(*w.sshClient).Close()
	(*w.sshConn).Close()
	return (*w.tlsConn).Close()
}

func cleanUpTestProxy() {
	var err error

	testProxyName := "test-proxy"
	testProxyNamespace := "default"

	testProxyDeployment := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testProxyName + "-dep",
			Namespace: testProxyNamespace,
		},
	}

	err = k8sClient.Delete(ctx, &testProxyDeployment)
	Expect(err == nil || errors.IsNotFound(err)).To(BeTrue())

	testProxyService := corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testProxyName + "-dep-svc",
			Namespace: testProxyNamespace,
		},
	}

	err = k8sClient.Delete(ctx, &testProxyService)
	Expect(err == nil || errors.IsNotFound(err)).To(BeTrue())

	testProxyIngress := netv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testProxyName + "-dep-svc-ing",
			Namespace: testProxyNamespace,
		},
	}

	err = k8sClient.Delete(ctx, &testProxyIngress)
	Expect(err == nil || errors.IsNotFound(err)).To(BeTrue())
}

func createControllerManagerForSuite() {
	createControllerManager(false, "")
}

func createControllerManager(disableMetrics bool, watchNamespace string) {

	managerCtx, managerCancel = context.WithCancel(ctx)

	mgrOptions := ctrl.Options{
		Scheme: scheme.Scheme,
	}

	isLocal, watchList := common.ResolveWatchNamespaceForManager(defaultNamespace, watchNamespace)
	if isLocal {
		ctrl.Log.Info("setting up operator to watch local namespace")
		mgrOptions.Cache.DefaultNamespaces = map[string]cache.Config{
			defaultNamespace: {}}
	} else {
		if watchList != nil {
			if len(watchList) == 1 {
				ctrl.Log.Info("setting up operator to watch single namespace")
			} else {
				ctrl.Log.Info("setting up operator to watch multiple namespaces", "namespace(s)", watchList)
			}
			nsMap := map[string]cache.Config{}
			for _, ns := range watchList {
				nsMap[ns] = cache.Config{}
			}
			mgrOptions.Cache.DefaultNamespaces = nsMap
		} else {
			ctrl.Log.Info("setting up operator to watch all namespaces")
		}
	}

	if disableMetrics {
		// if we can shutdown metrics port, we don't need disable it.
		mgrOptions.Metrics.BindAddress = "0"
	}

	waitforever := time.Duration(-1)
	mgrOptions.GracefulShutdownTimeout = &waitforever
	mgrOptions.LeaderElectionReleaseOnCancel = true

	// start our controler
	var err error
	k8Manager, err = ctrl.NewManager(restConfig, mgrOptions)
	Expect(err).ToNot(HaveOccurred())

	brokerReconciler = NewActiveMQArtemisReconciler(k8Manager, ctrl.Log, isOpenshift)

	if err = brokerReconciler.SetupWithManager(k8Manager); err != nil {
		ctrl.Log.Error(err, "unable to create controller", "controller", "ActiveMQArtemisReconciler")
	}

	brokerV1beta2Reconciler := NewBrokerClusterReconciler(k8Manager, ctrl.Log, isOpenshift)

	if err = brokerV1beta2Reconciler.SetupWithManager(k8Manager); err != nil {
		ctrl.Log.Error(err, "unable to create controller", "controller", "BrokerClusterReconciler")
	}

	securityReconciler = &ActiveMQArtemisSecurityReconciler{
		Client:           k8Manager.GetClient(),
		Scheme:           k8Manager.GetScheme(),
		BrokerReconciler: brokerReconciler,
		log:              ctrl.Log,
	}

	err = securityReconciler.SetupWithManager(k8Manager)
	Expect(err).ToNot(HaveOccurred(), "failed to create security controller")

	addressReconciler := &ActiveMQArtemisAddressReconciler{
		Client: k8Manager.GetClient(),
		Scheme: k8Manager.GetScheme(),
		log:    ctrl.Log,
	}

	err = addressReconciler.SetupWithManager(k8Manager, managerCtx)
	Expect(err).ToNot(HaveOccurred(), "failed to create address reconciler")

	scaleDownRconciler := &ActiveMQArtemisScaledownReconciler{
		Client: k8Manager.GetClient(),
		Scheme: k8Manager.GetScheme(),
		Config: k8Manager.GetConfig(),
		log:    ctrl.Log,
	}

	err = scaleDownRconciler.SetupWithManager(k8Manager)
	Expect(err).ShouldNot(HaveOccurred(), "failed to create scale down reconciler")

	serviceReconciler := NewBrokerServiceReconciler(
		k8Manager.GetClient(),
		k8Manager.GetScheme(),
		k8Manager.GetConfig(),
		ctrl.Log,
	)

	err = serviceReconciler.SetupWithManager(k8Manager)
	Expect(err).ShouldNot(HaveOccurred(), "failed to create service reconciler")

	appReconciler := NewBrokerAppReconciler(
		k8Manager.GetClient(),
		k8Manager.GetScheme(),
		k8Manager.GetConfig(),
		ctrl.Log,
	)

	err = appReconciler.SetupWithManager(k8Manager)
	Expect(err).ShouldNot(HaveOccurred(), "failed to create app reconciler")

	managerChannel = make(chan struct{}, 1)
	go func() {
		defer GinkgoRecover()
		err = k8Manager.Start(managerCtx)
		managerChannel <- struct{}{}
		Expect(err).ToNot(HaveOccurred(), "failed to run manager")
	}()
}

func shutdownControllerManager() {
	managerCancel()
	// wait for start routine to exit
	<-managerChannel
}

func setUpRealOperator() {
	var err error
	restConfig, err = config.GetConfig()
	Expect(err).ShouldNot(HaveOccurred(), "failed to get the config")

	setUpK8sClient()

	setUpNamespace()

	ctrl.Log.Info("Installing CRDs")
	crds := []string{"../deploy/crds"}
	options := envtest.CRDInstallOptions{
		Paths:              crds,
		ErrorIfPathMissing: false,
	}
	_, err = envtest.InstallCRDs(restConfig, options)
	Expect(err).To(Succeed())

	err = installOperator(nil, defaultNamespace)
	Expect(err).To(Succeed(), "failed to install operator")
}

// Deploy operator resources
// TODO: provide 'watch all namespaces' option
func installOperator(envMap map[string]string, namespace string) error {
	ctrl.Log.Info("#### Installing Operator ####", "ns", namespace)
	for _, res := range oprRes {
		if err := installYamlResource(res, envMap, namespace); err != nil {
			return err
		}
	}

	if namespace == defaultNamespace {
		defaultOperatorInstalled = true
	}

	return waitForOperator(namespace)
}

func uninstallOperator(deleteCrds bool, namespace string) error {
	ctrl.Log.Info("#### Uninstalling Operator ####", "ns", namespace)
	if namespace != defaultNamespace || defaultOperatorInstalled {
		for _, res := range oprRes {
			if err := uninstallYamlResource(res, namespace); err != nil {
				return err
			}
		}
		if namespace == defaultNamespace {
			defaultOperatorInstalled = false
		}
	}

	if deleteCrds {
		// the envtest UninstallCRDs function is flaky on ROSA
		uninstallCRDs()
	}

	return nil
}

func waitForOperator(namespace string) error {
	podList := &corev1.PodList{}
	labelSelector, err := labels.Parse("name=" + oprName)
	Expect(err).To(BeNil())
	opts := &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: labelSelector,
	}

	Eventually(func(g Gomega) {
		g.Expect(k8sClient.List(ctx, podList, opts)).Should(Succeed())
		g.Expect(len(podList.Items)).Should(BeEquivalentTo(1))
		oprPod := podList.Items[0]
		g.Expect(len(oprPod.Status.ContainerStatuses)).Should(BeEquivalentTo(1))
		g.Expect(oprPod.Status.ContainerStatuses[0].Ready).Should(BeTrue())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	return nil
}

func loadYamlResource(yamlFile string) (runtime.Object, *schema.GroupVersionKind, error) {
	var info os.FileInfo
	var err error
	var filePath string

	// Return the error if ErrorIfPathMissing exists
	if info, err = os.Stat(yamlFile); os.IsNotExist(err) {
		return nil, nil, err
	}
	filePath = filepath.Dir(yamlFile)

	var b []byte
	b, err = os.ReadFile(filepath.Join(filePath, info.Name()))
	if err != nil {
		return nil, nil, err
	}

	decode := scheme.Codecs.UniversalDeserializer().Decode
	robj, gKV, _ := decode(b, nil, nil)
	ctrl.Log.Info("Loaded resource", "kind", gKV.Kind)
	return robj, gKV, nil
}

func uninstallYamlResource(resPath string, namespace string) error {
	ctrl.Log.Info("Uninstalling yaml resource", "yaml", resPath)
	robj, _, err := loadYamlResource(resPath)
	if err != nil {
		return err
	}
	cobj := robj.(client.Object)
	cobj.SetNamespace(namespace)

	err = k8sClient.Delete(ctx, cobj)
	if err != nil {
		return err
	}

	ctrl.Log.Info("Successfully uninstalled", "yaml", resPath)
	return nil
}

func installYamlResource(resPath string, setupEnvs map[string]string, namespace string) error {
	ctrl.Log.Info("Installing yaml resource", "yaml", resPath)
	robj, gkv, err := loadYamlResource(resPath)
	if err != nil {
		return err
	}
	cobj := robj.(client.Object)
	cobj.SetNamespace(namespace)

	if gkv.Kind == "Deployment" {
		oprObj := cobj.(*appsv1.Deployment)
		depName = oprObj.Name
		oprName = oprObj.Spec.Template.Labels["name"]
		if oprImg := os.Getenv("IMG"); oprImg != "" {
			ctrl.Log.Info("Using custom operator image", "url", oprImg)
			oprObj.Spec.Template.Spec.Containers[0].Image = oprImg
		}

		// update default envs
		defaultEnvs := []string{
			"DEFAULT_BROKER_VERSION",
			"DEFAULT_BROKER_COMPACT_VERSION",
			"DEFAULT_BROKER_KUBE_IMAGE",
			"DEFAULT_BROKER_INIT_IMAGE",
		}
		for _, defaultEnv := range defaultEnvs {
			defaultEnvValue := os.Getenv(defaultEnv)
			if defaultEnvValue != "" {
				oprObj.Spec.Template.Spec.Containers[0].Env =
					putEnv(oprObj.Spec.Template.Spec.Containers[0].Env,
						defaultEnv, defaultEnvValue)
			}
		}

		// update releated image envs
		for _, env := range os.Environ() {
			if strings.HasPrefix(env, "RELATED_IMAGE_") {
				relatedImageEnv := strings.SplitN(env, "=", 2)
				oprObj.Spec.Template.Spec.Containers[0].Env =
					putEnv(oprObj.Spec.Template.Spec.Containers[0].Env,
						relatedImageEnv[0], relatedImageEnv[1])
			}
		}

		// update setup envs
		for setupEnvName, setupEnvValue := range setupEnvs {
			oprObj.Spec.Template.Spec.Containers[0].Env =
				putEnv(oprObj.Spec.Template.Spec.Containers[0].Env,
					setupEnvName, setupEnvValue)
		}
	}

	err = k8sClient.Create(ctx, cobj)
	if err != nil {
		return err
	}

	//make sure the create ok
	Eventually(func() bool {
		key := types.NamespacedName{Name: cobj.GetName(), Namespace: namespace}
		err := k8sClient.Get(ctx, key, cobj)
		return err == nil
	}, timeout, interval).Should(Equal(true))

	ctrl.Log.Info("Successfully installed", "yaml", resPath)
	return nil
}

func putEnv(envs []corev1.EnvVar, envName string, envValue string) []corev1.EnvVar {
	ctrl.Log.Info("Putting env var into operator", "name", envName, "value", envValue)

	envFound := false
	for i := 0; i < len(envs) && !envFound; i++ {
		envFound = envName == envs[i].Name
		if envFound {
			envs[i].Value = envValue
		}
	}

	if !envFound {
		envs = append(envs, corev1.EnvVar{Name: envName, Value: envValue})
	}

	return envs
}

func getOperatorEnv(envName string) (string, error) {
	robj, _, err := loadYamlResource("../deploy/operator.yaml")
	if err != nil {
		return "", err
	}
	oprObj := robj.(*appsv1.Deployment)
	for _, env := range oprObj.Spec.Template.Spec.Containers[0].Env {
		if env.Name == envName {
			return env.Value, nil
		}
	}
	return "", fmt.Errorf("environment variable %s not found", envName)
}

func setUpK8sClient() {

	ctrl.Log.Info("Setting up k8s client")

	err := configv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = routev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = cmv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = tm.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = brokerv2alpha5.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = brokerv2alpha4.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = brokerv2alpha3.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = brokerv2alpha2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = brokerv2alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = brokerv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = brokerv1beta1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = brokerv1beta2.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.NewWithWatch(restConfig, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
}

var _ = BeforeSuite(func() {
	opts := zap.Options{
		Development: true,
		DestWriter:  GinkgoWriter,
		TimeEncoder: zapcore.ISO8601TimeEncoder,
	}

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if verbose {
		GinkgoWriter.TeeTo(os.Stderr)
	}

	GinkgoWriter.TeeTo(&capturingLogWriter)

	ctx, cancel = context.WithCancel(context.TODO())

	// force isLocalOnly=false check from artemis reconciler such that scale down controller will create
	// role binding to service account for the drainer pod
	os.Setenv("OPERATOR_WATCH_NAMESPACE", "SomeValueToCauesEqualitytoFailInIsLocalSoDrainControllerSortsCreds")

	// pulled from service account when on a pod
	os.Setenv("OPERATOR_NAMESPACE", defaultNamespace)

	var err error
	currentDir, err = os.Getwd()
	Expect(err).To(Succeed())
	ctrl.Log.Info("#### Starting test ####", "working dir", currentDir)
	if os.Getenv("DEPLOY_OPERATOR") == "true" {
		ctrl.Log.Info("#### Setting up a real operator ####")
		setUpRealOperator()
	} else {
		ctrl.Log.Info("#### Setting up EnvTest ####")
		setUpEnvTest()
	}
})

func cleanUpPVC() {
	pvcs := &corev1.PersistentVolumeClaimList{}
	opts := []client.ListOption{}
	k8sClient.List(context.TODO(), pvcs, opts...)
	for _, pvc := range pvcs.Items {
		ctrl.Log.Info("Deleting/GC PVC: " + pvc.Name)
		k8sClient.Delete(context.TODO(), &pvc, &client.DeleteOptions{})
	}
}

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
		cleanUpPVC()
		cleanUpTestProxy()
	}

	os.Unsetenv("OPERATOR_WATCH_NAMESPACE")

	if os.Getenv("DEPLOY_OPERATOR") == "true" {
		err := uninstallOperator(true, defaultNamespace)
		Expect(err).NotTo(HaveOccurred())
	} else {
		shutdownControllerManager()

		// scaledown controller lifecycle seems a little loose, it does not complete on signal hander like the others
		for _, drainController := range controllers {
			close(*drainController.GetStopCh())
		}

		// the envtest UninstallCRDs function is flaky on ROSA
		uninstallCRDs()

		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})

func uninstallCRDs() {

	crd := apiextensionsv1.CustomResourceDefinition{}

	crdNames := [...]string{
		"activemqartemises.broker.amq.io",
		"activemqartemisaddresses.broker.amq.io",
		"activemqartemisscaledowns.broker.amq.io",
		"activemqartemissecurities.broker.amq.io",
		"brokerclusters.broker.arkmq.org",
		"brokerapps.broker.arkmq.org",
		"brokerservices.broker.arkmq.org",
	}

	for _, crdName := range crdNames {
		Eventually(func(g Gomega) {
			err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: crdName}, &crd)
			g.Expect(err == nil || errors.IsNotFound(err)).To(BeTrue())

			if !errors.IsNotFound(err) {
				// delete CRD
				err := k8sClient.Delete(context.TODO(), &crd)
				g.Expect(err).NotTo(HaveOccurred())

				// check CRD is not found
				Eventually(func(g Gomega) {
					err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: crdName}, &crd)
					g.Expect(errors.IsNotFound(err)).To(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}
		}, timeout, interval).Should(Succeed())
	}
}

func StartCapturingLog() {
	capturingLogWriter.Buffer = bytes.NewBuffer(nil)
}

func MatchInCapturingLog(pattern string) (matched bool, err error) {
	return regexp.Match(pattern, capturingLogWriter.Buffer.Bytes())
}

func FindAllInCapturingLog(pattern string) ([]string, error) {
	re, err := regexp.Compile(pattern)
	if err == nil {
		return re.FindAllString(capturingLogWriter.Buffer.String(), -1), nil
	}
	return nil, err
}

func StopCapturingLog() {
	capturingLogWriter.Buffer = nil
}

func BeforeEachSpec() {
	specCount++

	//Print running spec
	currentSpecReport := CurrentSpecReport()
	fmt.Printf("\n\033[1m\033[32mSpec %d running: %s \033[33m[%s]\033[0m\n%s:%d",
		specCount, currentSpecReport.FullText(), CurrentSpecShortName(),
		currentSpecReport.LeafNodeLocation.FileName, currentSpecReport.LeafNodeLocation.LineNumber)

	if verboseWithWatch && os.Getenv("USE_EXISTING_CLUSTER") == "true" {

		watchClientList = list.New()

		// see what has changed from the controllers perspective, what we watch
		toWatch := []client.ObjectList{&brokerv1beta1.ActiveMQArtemisList{}, &appsv1.StatefulSetList{}, &corev1.PodList{}}
		for _, li := range toWatch {

			wc, ok := k8sClient.(client.WithWatch)
			if !ok {
				fmt.Printf("k8sClient is not a WithWatch:  %v\n", k8sClient)
				return
			}
			// see what changed
			wi, err := wc.Watch(ctx, li, &client.ListOptions{})
			if err != nil {
				fmt.Printf("Err on watch:  %v\n", err)
			}
			watchClientList.PushBack(wi)

			go func() {
				for event := range wi.ResultChan() {
					fmt.Printf("%v : Object: %v\n", event.Type, event.Object)
				}
			}()
		}
	}
}

func AfterEachSpec() {

	if watchClientList != nil {
		for e := watchClientList.Front(); e != nil; e = e.Next() {
			e.Value.(watch.Interface).Stop()
		}
	}

	//Print ran spec
	currentSpecReport := CurrentSpecReport()
	fmt.Printf("\n\033[1m\033[32mSpec %d ran in %f seconds \033[0m\n",
		specCount, currentSpecReport.RunTime.Seconds())
}
