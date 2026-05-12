/*
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
// +kubebuilder:docs-gen:collapse=Apache License

/*
As usual, we start with the necessary imports. We also define some utility variables.
*/
package controllers

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomegaTypes "github.com/onsi/gomega/types"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/api/v2alpha1"
	"github.com/arkmq-org/arkmq-org-broker-operator/api/v2alpha2"
	"github.com/arkmq-org/arkmq-org-broker-operator/api/v2alpha3"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/jolokia"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/namer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

var _ = Describe("Address controller tests", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("address queue config defaults", Label("queue-config-defaults"), func() {
		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			queueName := "myqueue"
			addressName := "myaddress"
			anycastType := "ANYCAST"
			It("configurationManaged default should be true (without queue configuration)", func() {
				By("deploy a broker cr")
				brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, nil)

				By("deploy an address cr")
				addressCr, createdAddressCr := DeployCustomAddress(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemisAddress) {
					candidate.Spec.AddressName = addressName
					candidate.Spec.QueueName = &queueName
					candidate.Spec.RoutingType = &anycastType
				})

				By("verify the configurationManaged attribute of the queue is true")
				podOrdinal := strconv.FormatInt(0, 10)
				podName := namer.CrToSS(brokerCr.Name) + "-" + podOrdinal

				CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)

				CheckQueueAttribute(brokerCr.Name, podName, defaultNamespace, queueName, addressName, "anycast", "ConfigurationManaged", "true")

				//cleanup
				CleanResource(createdBrokerCr, brokerCr.Name, defaultNamespace)
				CleanResource(createdAddressCr, addressCr.Name, defaultNamespace)
			})

			It("Verifying the lscrs secret is gone after deleting address CR", Label("delete-secret-check"), func() {
				By("deploy a broker cr")
				brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, nil)

				By("deploy an address cr")
				addressCr, createdAddressCr := DeployCustomAddress(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemisAddress) {
					candidate.Spec.AddressName = addressName
					candidate.Spec.QueueName = &queueName
					candidate.Spec.RoutingType = &anycastType
					candidate.Spec.RemoveFromBrokerOnDelete = true
				})

				By("verify the configurationManaged attribute of the queue is true")
				podOrdinal := strconv.FormatInt(0, 10)
				podName := namer.CrToSS(brokerCr.Name) + "-" + podOrdinal

				CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)
				Eventually(func(g Gomega) {
					expectedSecuritySecret := &corev1.Secret{}
					expectedSecuritySecretKey := types.NamespacedName{Name: "secret-address-" + addressCr.Name, Namespace: defaultNamespace}
					g.Expect(k8sClient.Get(ctx, expectedSecuritySecretKey, expectedSecuritySecret)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("delete the address cr " + addressCr.Name)

				CleanResource(createdAddressCr, addressCr.Name, defaultNamespace)

				Eventually(func(g Gomega) {
					expectedSecuritySecret := &corev1.Secret{}
					expectedSecuritySecretKey := types.NamespacedName{Name: "secret-address-" + addressCr.Name, Namespace: defaultNamespace}
					g.Expect(k8sClient.Get(ctx, expectedSecuritySecretKey, expectedSecuritySecret)).ShouldNot(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				//cleanup
				By("clean up resources")
				CleanResource(createdBrokerCr, brokerCr.Name, defaultNamespace)
			})

			It("configurationManaged default should be true (with queue configuration)", func() {
				By("deploy a broker cr")
				brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, nil)

				By("deploy an address cr")
				_, createdAddressCr := DeployCustomAddress(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemisAddress) {
					candidate.Spec.AddressName = addressName
					candidate.Spec.QueueName = &queueName
					candidate.Spec.QueueConfiguration = &brokerv1beta1.QueueConfigurationType{
						RoutingType: &anycastType,
					}
				})

				By("verify the configurationManaged attribute of the queue is true")
				podOrdinal := strconv.FormatInt(0, 10)
				podName := namer.CrToSS(brokerCr.Name) + "-" + podOrdinal

				CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)

				CheckQueueAttribute(brokerCr.Name, podName, defaultNamespace, queueName, addressName, "anycast", "ConfigurationManaged", "true")

				//cleanup
				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
				CleanResource(createdAddressCr, createdAddressCr.Name, defaultNamespace)
			})
		} else {
			fmt.Println("Test skipped as it requires an existing cluster")
		}
	})

	Context("broker with address custom resources", Label("broker-address-res"), func() {
		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			queueName := "myqueue"
			It("address after recreating broker cr", func() {
				By("deploy a broker cr")
				brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, nil)

				By("deploy an address cr")
				_, createdAddressCr := DeployCustomAddress(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemisAddress) {
					candidate.Spec.AddressName = "myaddress"
					candidate.Spec.QueueName = &queueName
				})

				By("verify the queue is created")
				podOrdinal := strconv.FormatInt(0, 10)
				podName := namer.CrToSS(brokerCr.Name) + "-" + podOrdinal

				CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)

				By("delete the broker cr")
				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)

				By("re-deploy the broker cr")
				brokerCr, createdBrokerCr = DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
					candidate.Name = brokerCr.Name
				})

				By("verify the queue is re-created")
				CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)

				//cleanup
				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
				CleanResource(createdAddressCr, createdAddressCr.Name, defaultNamespace)
			})
		} else {
			fmt.Println("Test skipped as it requires an existing cluster")
		}
	})

	Context("address controller test with reconcile", func() {

		It("Deploy CR with size 5 (pods)", Label("verySlow"), func() {

			ctx := context.Background()

			brokerCrd := generateArtemisSpec(defaultNamespace)

			brokerName := brokerCrd.Name

			brokerCrd.Spec.DeploymentPlan.Size = common.Int32ToPtr(5)

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				Expect(k8sClient.Create(ctx, &brokerCrd)).Should(Succeed())
				createdBrokerCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("Waiting for all pods to be started and ready")
				Eventually(func(g Gomega) {

					getPersistedVersionedCrd(brokerCrd.ObjectMeta.Name, defaultNamespace, createdBrokerCrd)
					g.Expect(len(createdBrokerCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(*brokerCrd.Spec.DeploymentPlan.Size))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("creating 5 queue resources and 1 security")
				addressCrs := make([]*brokerv1beta1.ActiveMQArtemisAddress, 5)
				for i := 0; i < 5; i++ {
					ordinal := strconv.FormatInt(int64(i), 10)
					addressCrs[i] = GenerateAddressSpec("ex-aaoaddress"+ordinal, defaultNamespace, "myAddress"+ordinal, "myQueue"+ordinal, true, true)
				}

				// This will trigger another issue where some secrets are deleted during pod restart due to security and artemis controller thread
				// contention. Fixing by having security controller fire a reconcile event rather than doing reconcile in line
				propLoginModules := make([]brokerv1beta1.PropertiesLoginModuleType, 1)
				pwd := "geezrick"
				moduleName := "prop-module"
				flag := "sufficient"
				propLoginModules[0] = brokerv1beta1.PropertiesLoginModuleType{
					Name: moduleName,
					Users: []brokerv1beta1.UserType{
						{Name: "morty",
							Password: &pwd,
							Roles:    []string{"admin", "random"}},
					},
				}

				brokerDomainName := "activemq"
				loginModules := make([]brokerv1beta1.LoginModuleReferenceType, 1)
				loginModules[0] = brokerv1beta1.LoginModuleReferenceType{
					Name: &moduleName,
					Flag: &flag,
				}
				brokerDomain := brokerv1beta1.BrokerDomainType{
					Name:         &brokerDomainName,
					LoginModules: loginModules,
				}

				By("Deploying all resources at once")
				_, deployedSecCrd := DeploySecurity("ex-proper", defaultNamespace, func(secCrdToDeploy *brokerv1beta1.ActiveMQArtemisSecurity) {
					secCrdToDeploy.Spec.LoginModules.PropertiesLoginModules = propLoginModules
					secCrdToDeploy.Spec.SecurityDomains.BrokerDomain = brokerDomain
				})

				for _, addr := range addressCrs {
					DeployAddress(addr)
				}

				By("Waiting for all pods to be restarted and ready")
				Eventually(func(g Gomega) {

					getPersistedVersionedCrd(brokerCrd.ObjectMeta.Name, defaultNamespace, createdBrokerCrd)
					g.Expect(len(createdBrokerCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(*brokerCrd.Spec.DeploymentPlan.Size))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Checking all addresses are created on all pods")

				gvk := schema.GroupVersionKind{
					Group:   "",
					Version: "v1",
					Kind:    "Pod",
				}
				httpClient, err := rest.HTTPClientFor(restConfig)
				Expect(err).To(BeNil())
				restClient, err := apiutil.RESTClientForGVK(gvk, false, restConfig, serializer.NewCodecFactory(scheme.Scheme), httpClient)
				Expect(err).To(BeNil())
				deploymentSize := *brokerCrd.Spec.DeploymentPlan.Size
				for ipod := deploymentSize - 1; ipod >= 0; ipod-- {
					podOrdinal := strconv.FormatInt(int64(ipod), 10)
					podName := namer.CrToSS(brokerCrd.Name) + "-" + podOrdinal

					By("Checking all addresses are created on " + podName)
					Eventually(func(g Gomega) {
						execReq := restClient.
							Post().
							Namespace(defaultNamespace).
							Resource("pods").
							Name(podName).
							SubResource("exec").
							VersionedParams(&corev1.PodExecOptions{
								Container: brokerName + "-container",
								Command:   []string{"amq-broker/bin/artemis", "queue", "stat", "--user", "morty", "--password", "geezrick", "--url", "tcp://" + podName + ":61616"},
								Stdin:     true,
								Stdout:    true,
								Stderr:    true,
							}, runtime.NewParameterCodec(scheme.Scheme))

						exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", execReq.URL())

						if err != nil {
							fmt.Printf("error while creating remote command executor: %v", err)
						}
						Expect(err).To(BeNil())
						var capturedOut bytes.Buffer

						err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
							Stdin:  os.Stdin,
							Stdout: &capturedOut,
							Stderr: os.Stderr,
							Tty:    false,
						})
						g.Expect(err).To(BeNil())

						By("Checking for output on " + podName)
						g.Expect(capturedOut.Len() > 0)
						content := capturedOut.String()
						deploymentSize := *brokerCrd.Spec.DeploymentPlan.Size
						for ipod := deploymentSize - 1; ipod >= 0; ipod-- {
							queueName := fmt.Sprintf("myQueue%d", ipod)
							By("finding Q " + queueName)
							g.Expect(content).Should(ContainSubstring(queueName))
						}
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				//clean up all resources
				Expect(k8sClient.Delete(ctx, createdBrokerCrd)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, deployedSecCrd)).Should(Succeed())
				for _, addr := range addressCrs {
					Expect(k8sClient.Delete(ctx, addr)).Should((Succeed()))
				}
			}
		})
	})

	Context("Address delete and scale down", func() {

		It("Scale down, verify RemoveFromBrokerOnDelete", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)

			By("By deploying address cr for a2 for this broker in advance")

			addressName := "A2"
			addressCrd := brokerv1beta1.ActiveMQArtemisAddress{}
			addressCrd.SetName("address-" + randString())
			addressCrd.SetNamespace(defaultNamespace)
			addressCrd.Spec.AddressName = addressName // note no queue
			routingTypeShouldBeOptional := "multicast"
			addressCrd.Spec.RoutingType = &routingTypeShouldBeOptional
			addressCrd.Spec.RemoveFromBrokerOnDelete = true

			Expect(k8sClient.Create(ctx, &addressCrd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Deploying a broker pair")
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				By("Checking ready on SS")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
					sfsFound := &appsv1.StatefulSet{}

					g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
					g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(2))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verfying address is present")
				ordinals := []string{"0", "1"}
				for _, ordinal := range ordinals {

					podWithOrdinal := namer.CrToSS(crd.Name) + "-" + ordinal
					command := []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
						g.Expect(stdOutContent).Should(ContainSubstring(addressName))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				By("Deleting address CR")
				Expect(k8sClient.Delete(ctx, &addressCrd)).Should(Succeed())

				By("Verifying address gone from both brokers")
				for _, ordinal := range ordinals {
					podWithOrdinal := namer.CrToSS(crd.Name) + "-" + ordinal
					command := []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
						g.Expect(stdOutContent).ShouldNot(ContainSubstring(addressName))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

			}

			// cleanup
			k8sClient.Delete(ctx, &addressCrd)
			k8sClient.Delete(ctx, &crd)
		})
	})

	Context("Address creation test", Label("address-creation-test"), func() {

		It("create a queue with non-existing address, no auto-create", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			applyRule := "merge_all"
			dla := "DLA"
			dla1 := "DLQ.XxxxxxXXxxXdata"
			dlqPrefix := "DLQ"
			maxConsumers := int32(10)
			maxAttempts := int32(10)
			crd.Spec.AddressSettings.ApplyRule = &applyRule
			crd.Spec.AddressSettings.AddressSetting = []brokerv1beta1.AddressSettingType{
				{
					Match:                         "#",
					EnableMetrics:                 &boolTrue,
					AutoCreateExpiryResources:     &boolFalse,
					DeadLetterAddress:             &dla,
					AutoCreateQueues:              &boolFalse,
					AutoCreateDeadLetterResources: &boolTrue,
					AutoCreateAddresses:           &boolFalse,
					DeadLetterQueuePrefix:         &dlqPrefix,
					DefaultMaxConsumers:           &maxConsumers,
					MaxDeliveryAttempts:           &maxAttempts,
				},
				{
					Match:                         "XxxxxxXXxxXdata#",
					AutoCreateDeadLetterResources: &boolTrue,
					DeadLetterAddress:             &dla1,
					DefaultMaxConsumers:           &maxConsumers,
				},
			}

			addressName := "myAddress0"
			queueName := "myQueue0"
			routingType := "anycast"
			addressCrd := brokerv1beta1.ActiveMQArtemisAddress{}
			addressCrd.SetName("address-" + randString())
			addressCrd.SetNamespace(defaultNamespace)
			addressCrd.Spec.AddressName = addressName
			addressCrd.Spec.QueueName = &queueName
			addressCrd.Spec.RoutingType = &routingType

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Deploying a broker without auto-create")
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				By("Checking ready on SS")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
					sfsFound := &appsv1.StatefulSet{}

					g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
					g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verfying address is not present")

				podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
				command := []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

				Eventually(func(g Gomega) {
					stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
					g.Expect(stdOutContent).ShouldNot(ContainSubstring(addressName))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Deploying address CR")
				Expect(k8sClient.Create(ctx, &addressCrd)).Should(Succeed())

				By("Verifying queue is created")
				podWithOrdinal = namer.CrToSS(crd.Name) + "-0"
				command = []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

				Eventually(func(g Gomega) {
					stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
					g.Expect(stdOutContent).Should(ContainSubstring(addressName))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				command = []string{"amq-broker/bin/artemis", "queue", "stat", "--url", "tcp://" + podWithOrdinal + ":61616"}

				Eventually(func(g Gomega) {
					stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
					g.Expect(stdOutContent).Should(ContainSubstring(queueName))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				// cleanup
				Expect(k8sClient.Delete(ctx, &addressCrd)).Should(Succeed())
				Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
			} else {
				fmt.Println("Test skipped as it requires existing cluster with operator installed")
			}

		})

		It("create address with name only", func() {
			crd := generateArtemisSpec(defaultNamespace)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			addressName := "my-address"
			addressCrd := brokerv1beta1.ActiveMQArtemisAddress{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemisAddress",
					APIVersion: brokerv1beta1.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      NextSpecResourceName(),
					Namespace: defaultNamespace,
				},
				Spec: brokerv1beta1.ActiveMQArtemisAddressSpec{
					AddressName: addressName,
				},
			}
			Expect(k8sClient.Create(ctx, &addressCrd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}
				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}

				By("Verifying started via Status")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying address is created")
				podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
				command = []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

				Eventually(func(g Gomega) {
					stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
					g.Expect(stdOutContent).Should(ContainSubstring(addressName))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			// cleanup
			Expect(k8sClient.Delete(ctx, &addressCrd)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
		})
	})

	Context("Address CR with console agent", func() {

		It("address creation via agent", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			crd.Spec.DeploymentPlan.JolokiaAgentEnabled = true

			By("By deploying address cr in advance")

			addressName := "A2"
			addressCrd := brokerv1beta1.ActiveMQArtemisAddress{}
			addressCrd.SetName("address-" + randString())
			addressCrd.SetNamespace(defaultNamespace)
			addressCrd.Spec.AddressName = addressName
			addressCrd.Spec.QueueName = &addressName
			routingTypeShouldBeOptional := "anycast"
			addressCrd.Spec.RoutingType = &routingTypeShouldBeOptional

			Expect(k8sClient.Create(ctx, &addressCrd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Deploying a broker")
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				By("Checking ready on SS")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
					sfsFound := &appsv1.StatefulSet{}

					g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
					g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verfying address is present")
				ordinals := []string{"0"}
				for _, ordinal := range ordinals {

					podWithOrdinal := namer.CrToSS(crd.Name) + "-" + ordinal
					command := []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
						g.Expect(stdOutContent).Should(ContainSubstring(addressName))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}
			}

			// cleanup
			k8sClient.Delete(ctx, &addressCrd)
			k8sClient.Delete(ctx, &crd)
		})

		It("address creation before controller manager restart", func() {

			By("By deploying address cr in advance")

			addressName := "A2"
			addressCrd := brokerv1beta1.ActiveMQArtemisAddress{}
			addressCrd.SetName("address-" + randString())
			addressCrd.SetNamespace(defaultNamespace)
			addressCrd.Spec.AddressName = addressName
			addressCrd.Spec.QueueName = &addressName
			routingTypeShouldBeOptional := "anycast"
			addressCrd.Spec.RoutingType = &routingTypeShouldBeOptional

			Expect(k8sClient.Create(ctx, &addressCrd)).Should(Succeed())

			// Restart the controller manager
			shutdownControllerManager()
			createControllerManagerForSuite()

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
				InitialDelaySeconds: 5,
				PeriodSeconds:       10,
			}
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			crd.Spec.DeploymentPlan.JolokiaAgentEnabled = true

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Deploying a broker")
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				By("Checking ready on SS")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
					sfsFound := &appsv1.StatefulSet{}

					g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
					g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verfying address is present")
				deploymentSize := *crd.Spec.DeploymentPlan.Size
				for i := int32(0); i < deploymentSize; i++ {
					Eventually(func(g Gomega) {
						pod := &corev1.Pod{}
						podName := namer.CrToSS(crd.Name) + "-" + strconv.FormatInt(int64(i), 10)
						podNamespacedName := types.NamespacedName{Name: podName, Namespace: defaultNamespace}
						g.Expect(k8sClient.Get(ctx, podNamespacedName, pod)).Should(Succeed())

						jolokia := jolokia.GetJolokia(k8sClient, pod.Status.PodIP, "8161", "/console/jolokia", "", "", "http")
						data, err := jolokia.Exec("", `{ "type":"EXEC","mbean":"org.apache.activemq.artemis:broker=\"amq-broker\"","operation":"listAddresses(java.lang.String)","arguments":[","] }`)
						g.Expect(err).To(BeNil())
						g.Expect(data.Value).Should(ContainSubstring(addressName))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}
			}

			// cleanup
			k8sClient.Delete(ctx, &addressCrd)
			k8sClient.Delete(ctx, &crd)
		})

		It("address creation via with ApplyToCrNames before broker", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			crd.Spec.DeploymentPlan.JolokiaAgentEnabled = true

			By("By deploying address cr before artermis cr")

			addressName := "A3"
			addressCrd := brokerv1beta1.ActiveMQArtemisAddress{}
			addressCrd.SetName("address-" + randString())
			addressCrd.SetNamespace(defaultNamespace)
			addressCrd.Spec.AddressName = addressName
			addressCrd.Spec.QueueName = &addressName
			routingTypeShouldBeOptional := "anycast"
			addressCrd.Spec.RoutingType = &routingTypeShouldBeOptional
			addressCrd.Spec.ApplyToCrNames = []string{crd.Name}

			Expect(k8sClient.Create(ctx, &addressCrd)).Should(Succeed())

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Deploying a broker")
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				By("Checking ready on SS")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
					sfsFound := &appsv1.StatefulSet{}

					g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
					g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verfying address is present")
				ordinals := []string{"0"}
				for _, ordinal := range ordinals {

					podWithOrdinal := namer.CrToSS(crd.Name) + "-" + ordinal
					command := []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
						g.Expect(stdOutContent).Should(ContainSubstring(addressName))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

			}

			// cleanup
			k8sClient.Delete(ctx, &addressCrd)
			k8sClient.Delete(ctx, &crd)
		})

		It("address creation via with ApplyToCrNames after broker", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			crd.Spec.DeploymentPlan.JolokiaAgentEnabled = true

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Deploying a broker")
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				By("Checking ready on SS")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
					sfsFound := &appsv1.StatefulSet{}

					g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
					g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("By deploying address cr after ready")

				addressName := "A4"
				addressCrd := brokerv1beta1.ActiveMQArtemisAddress{}
				addressCrd.SetName("address-" + randString())
				addressCrd.SetNamespace(defaultNamespace)
				addressCrd.Spec.AddressName = addressName
				addressCrd.Spec.QueueName = &addressName
				routingTypeShouldBeOptional := "anycast"
				addressCrd.Spec.RoutingType = &routingTypeShouldBeOptional
				addressCrd.Spec.ApplyToCrNames = []string{crd.Name}

				Expect(k8sClient.Create(ctx, &addressCrd)).Should(Succeed())

				By("Verfying address is present")
				ordinals := []string{"0"}
				for _, ordinal := range ordinals {

					podWithOrdinal := namer.CrToSS(crd.Name) + "-" + ordinal
					command := []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
						g.Expect(stdOutContent).Should(ContainSubstring(addressName))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				k8sClient.Delete(ctx, &addressCrd)
			}

			// cleanup
			k8sClient.Delete(ctx, &crd)
		})

		It("address creation with multiple namespaces before broker", func() {

			ctx := context.Background()

			otherNS := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: otherNamespace,
				},
			}
			err := k8sClient.Create(ctx, otherNS)
			if err != nil {
				Expect(errors.IsConflict(err))
			}

			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
			crd.Spec.DeploymentPlan.JolokiaAgentEnabled = true

			otherCrd := generateArtemisSpec(otherNamespace)
			otherCrd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)
			otherCrd.Spec.DeploymentPlan.JolokiaAgentEnabled = true

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("By deploying address cr after ready")
				addressName := "A4"
				otherAddressName := "O4"
				ordinals := []string{"0", "1"}
				routingTypeShouldBeOptional := "anycast"

				addressCrd := brokerv1beta1.ActiveMQArtemisAddress{}
				addressCrd.SetName("address-" + randString())
				addressCrd.SetNamespace(defaultNamespace)
				addressCrd.Spec.AddressName = addressName
				addressCrd.Spec.QueueName = &addressName
				addressCrd.Spec.RoutingType = &routingTypeShouldBeOptional

				Expect(k8sClient.Create(ctx, &addressCrd)).Should(Succeed())

				otherAddressCrd := brokerv1beta1.ActiveMQArtemisAddress{}
				otherAddressCrd.SetName("address-" + randString())
				otherAddressCrd.SetNamespace(otherNamespace)
				otherAddressCrd.Spec.AddressName = otherAddressName
				otherAddressCrd.Spec.QueueName = &otherAddressName
				otherAddressCrd.Spec.RoutingType = &routingTypeShouldBeOptional

				Expect(k8sClient.Create(ctx, &otherAddressCrd)).Should(Succeed())

				By("Deploying a broker")
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				By("Checking ready on SS")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
					sfsFound := &appsv1.StatefulSet{}

					g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
					g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(2))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verfying address is present in broker")
				for _, ordinal := range ordinals {

					podWithOrdinal := namer.CrToSS(crd.Name) + "-" + ordinal
					command := []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
						g.Expect(stdOutContent).Should(ContainSubstring(addressName))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				By("Deploying another broker")
				Expect(k8sClient.Create(ctx, &otherCrd)).Should(Succeed())

				By("Checking ready on SS")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(otherCrd.Name), Namespace: otherNamespace}
					sfsFound := &appsv1.StatefulSet{}

					g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
					g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(2))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verfying address is present in other broker")
				for _, ordinal := range ordinals {

					podWithOrdinal := namer.CrToSS(otherCrd.Name) + "-" + ordinal
					command := []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, otherCrd.Name, otherNamespace, command, g)
						g.Expect(stdOutContent).Should(ContainSubstring(otherAddressName))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				k8sClient.Delete(ctx, &addressCrd)
				k8sClient.Delete(ctx, &otherAddressCrd)
			}

			// cleanup
			k8sClient.Delete(ctx, &crd)
			k8sClient.Delete(ctx, &otherCrd)
			k8sClient.Delete(ctx, otherNS)
		})

		It("address creation with multiple ApplyToCrNames", func() {

			ctx := context.Background()
			crd0 := generateOriginalArtemisSpec(defaultNamespace, "broker")
			crd0.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			crd0.Spec.DeploymentPlan.JolokiaAgentEnabled = true

			crd1 := generateOriginalArtemisSpec(defaultNamespace, "broker1")
			crd1.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			crd1.Spec.DeploymentPlan.JolokiaAgentEnabled = true

			crd2 := generateOriginalArtemisSpec(defaultNamespace, "broker2")
			crd2.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			crd2.Spec.DeploymentPlan.JolokiaAgentEnabled = true

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Deploying a broker 0")
				Expect(k8sClient.Create(ctx, crd0)).Should(Succeed())

				By("Deploying a broker 1")
				Expect(k8sClient.Create(ctx, crd1)).Should(Succeed())

				By("Deploying a broker 2")
				Expect(k8sClient.Create(ctx, crd2)).Should(Succeed())

				By("Checking ready on SS 0")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(crd0.Name), Namespace: defaultNamespace}
					sfsFound := &appsv1.StatefulSet{}

					g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
					g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Checking ready on SS 1")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(crd1.Name), Namespace: defaultNamespace}
					sfsFound := &appsv1.StatefulSet{}

					g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
					g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Checking ready on SS 2")
				Eventually(func(g Gomega) {
					key := types.NamespacedName{Name: namer.CrToSS(crd2.Name), Namespace: defaultNamespace}
					sfsFound := &appsv1.StatefulSet{}

					g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
					g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("By deploying address cr after ready")

				addressName := "A4"
				addressCrd := brokerv1beta1.ActiveMQArtemisAddress{}
				addressCrd.SetName("address-" + randString())
				addressCrd.SetNamespace(defaultNamespace)
				addressCrd.Spec.AddressName = addressName
				addressCrd.Spec.QueueName = &addressName
				routingTypeShouldBeOptional := "anycast"
				addressCrd.Spec.RoutingType = &routingTypeShouldBeOptional
				addressCrd.Spec.ApplyToCrNames = []string{crd0.Name, crd1.Name}

				Expect(k8sClient.Create(ctx, &addressCrd)).Should(Succeed())

				By("Verfying address is present")
				ordinals := []string{"0"}
				for _, ordinal := range ordinals {

					podWithOrdinal := namer.CrToSS(crd0.Name) + "-" + ordinal
					command := []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, crd0.Name, defaultNamespace, command, g)
						g.Expect(stdOutContent).Should(ContainSubstring(addressName))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				for _, ordinal := range ordinals {

					podWithOrdinal := namer.CrToSS(crd1.Name) + "-" + ordinal
					command := []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, crd1.Name, defaultNamespace, command, g)
						g.Expect(stdOutContent).Should(ContainSubstring(addressName))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				// no match for crd2
				for _, ordinal := range ordinals {

					podWithOrdinal := namer.CrToSS(crd2.Name) + "-" + ordinal
					command := []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, crd2.Name, defaultNamespace, command, g)
						g.Expect(stdOutContent).ShouldNot(ContainSubstring(addressName))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				k8sClient.Delete(ctx, &addressCrd)
				// cleanup
				CleanResource(crd0, crd0.Name, defaultNamespace)
				CleanResource(crd1, crd1.Name, defaultNamespace)
				CleanResource(crd2, crd2.Name, defaultNamespace)
			}

		})
	})

	Context("Address conversion test", Label("address-conversion-test"), func() {
		It("convert empty address CR", func() {
			v1beta1AddressCR := brokerv1beta1.ActiveMQArtemisAddress{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemisAddress",
					APIVersion: brokerv1beta1.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      NextSpecResourceName(),
					Namespace: defaultNamespace,
				},
			}
			Expect(k8sClient.Create(ctx, &v1beta1AddressCR)).Should(Succeed())

			addressCRKey := types.NamespacedName{Name: v1beta1AddressCR.Name, Namespace: v1beta1AddressCR.Namespace}

			Eventually(func(g Gomega) {
				v1beta1CreatedAddressCR := &brokerv1beta1.ActiveMQArtemisAddress{}
				g.Expect(k8sClient.Get(ctx, addressCRKey, v1beta1CreatedAddressCR)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				v2alpha3AddressCR := &v2alpha3.ActiveMQArtemisAddress{}
				g.Expect(k8sClient.Get(ctx, addressCRKey, v2alpha3AddressCR)).Should(Succeed())
				g.Expect(k8sClient.Update(ctx, v2alpha3AddressCR)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				v2alpha2AddressCR := &v2alpha2.ActiveMQArtemisAddress{}
				g.Expect(k8sClient.Get(ctx, addressCRKey, v2alpha2AddressCR)).Should(Succeed())
				g.Expect(k8sClient.Update(ctx, v2alpha2AddressCR)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				v2alpha1AddressCR := &v2alpha1.ActiveMQArtemisAddress{}
				g.Expect(k8sClient.Get(ctx, addressCRKey, v2alpha1AddressCR)).Should(Succeed())
				g.Expect(k8sClient.Update(ctx, v2alpha1AddressCR)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			Eventually(func(g Gomega) {
				v1beta1CreatedAddressCR := &brokerv1beta1.ActiveMQArtemisAddress{}
				g.Expect(k8sClient.Get(ctx, addressCRKey, v1beta1CreatedAddressCR)).Should(Succeed())
				g.Expect(k8sClient.Delete(ctx, v1beta1CreatedAddressCR)).Should(Succeed())
			}, timeout, interval).Should(Succeed())
		})
	})

	Context("Address CR migration test", Label("address-migration-test"), func() {
		It("Migrate to broker properties when configDelete is FORCE", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				Skip("Existing cluster required")
			}

			addressName := "TEST-ADDRESS"
			queueName := "TEST-QUEUE"
			routingType := "anycast"

			By("deploy a broker cr")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.BrokerProperties = []string{
					"addressSettings.#.configDeleteAddresses=FORCE",
					"addressSettings.#.configDeleteQueues=FORCE",
				}
			})
			podName := namer.CrToSS(brokerCr.Name) + "-0"

			By("deploy an address cr")
			addressCr, createdAddressCr := DeployCustomAddress(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemisAddress) {
				candidate.Spec.AddressName = addressName
				candidate.Spec.QueueName = &queueName
				candidate.Spec.RoutingType = &routingType
				candidate.Spec.RemoveFromBrokerOnDelete = false
			})

			By("verify the queue exists")
			CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)
			CheckQueueAttribute(brokerCr.Name, podName, defaultNamespace, queueName, addressName, "anycast", "ConfigurationManaged", "true")

			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {
				By("adding broker properties")
				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
				createdBrokerCr.Spec.BrokerProperties = []string{
					"addressSettings.#.configDeleteAddresses=FORCE",
					"addressSettings.#.configDeleteQueues=FORCE",
					"addressConfigurations.TEST-ADDRESS.routingTypes=ANYCAST",
					"addressConfigurations.TEST-ADDRESS.queueConfigs.TEST-QUEUE.routingType=ANYCAST",
					"addressConfigurations.TEST-ADDRESS.queueConfigs.TEST-QUEUE.durable=true",
					"addressConfigurations.TEST-ADDRESS.queueConfigs.TEST-QUEUE.address=TEST-ADDRESS",
				}
				g.Expect(k8sClient.Update(ctx, createdBrokerCr)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				By("verify config applied")
				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
				condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			addressKey := types.NamespacedName{Name: createdAddressCr.Name, Namespace: createdAddressCr.Namespace}
			Eventually(func(g Gomega) {
				By("deleting address CR")
				g.Expect(k8sClient.Get(ctx, addressKey, createdAddressCr)).Should(Succeed())
				g.Expect(k8sClient.Delete(ctx, createdAddressCr)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				By("checking lscrs secret is gone")
				expectedSecuritySecret := &corev1.Secret{}
				expectedSecuritySecretKey := types.NamespacedName{Name: "secret-address-" + addressCr.Name, Namespace: defaultNamespace}
				g.Expect(k8sClient.Get(ctx, expectedSecuritySecretKey, expectedSecuritySecret)).ShouldNot(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verify the queue exists")
			CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)
			CheckQueueAttribute(brokerCr.Name, podName, defaultNamespace, queueName, addressName, "anycast", "ConfigurationManaged", "true")

			By("cleanup")
			CleanResource(createdBrokerCr, brokerCr.Name, defaultNamespace)
			CleanResource(createdAddressCr, addressCr.Name, defaultNamespace)
		})

		It("Migrate to broker properties when RemoveFromBrokerOnDelete is false", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				Skip("Existing cluster required")
			}

			addressName := "TEST-ADDRESS"
			queueName := "TEST-QUEUE"
			routingType := "anycast"

			By("deploy a broker cr")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, nil)
			podName := namer.CrToSS(brokerCr.Name) + "-0"

			By("deploy an address cr")
			addressCr, createdAddressCr := DeployCustomAddress(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemisAddress) {
				candidate.Spec.AddressName = addressName
				candidate.Spec.QueueName = &queueName
				candidate.Spec.RoutingType = &routingType
				candidate.Spec.RemoveFromBrokerOnDelete = false
			})

			By("verify the queue exists")
			CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)
			CheckQueueAttribute(brokerCr.Name, podName, defaultNamespace, queueName, addressName, "anycast", "ConfigurationManaged", "true")

			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {
				By("adding broker properties")
				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
				createdBrokerCr.Spec.BrokerProperties = []string{
					"addressConfigurations.TEST-ADDRESS.queueConfigs.TEST-QUEUE.routingType=ANYCAST",
					"addressConfigurations.TEST-ADDRESS.queueConfigs.TEST-QUEUE.durable=true",
				}
				g.Expect(k8sClient.Update(ctx, createdBrokerCr)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			addressKey := types.NamespacedName{Name: createdAddressCr.Name, Namespace: createdAddressCr.Namespace}
			Eventually(func(g Gomega) {
				By("deleting address CR")
				g.Expect(k8sClient.Get(ctx, addressKey, createdAddressCr)).Should(Succeed())
				g.Expect(k8sClient.Delete(ctx, createdAddressCr)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				By("checking lscrs secret is gone")
				expectedSecuritySecret := &corev1.Secret{}
				expectedSecuritySecretKey := types.NamespacedName{Name: "secret-address-" + addressCr.Name, Namespace: defaultNamespace}
				g.Expect(k8sClient.Get(ctx, expectedSecuritySecretKey, expectedSecuritySecret)).ShouldNot(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verify the queue exists")
			CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)
			CheckQueueAttribute(brokerCr.Name, podName, defaultNamespace, queueName, addressName, "anycast", "ConfigurationManaged", "true")

			By("cleanup")
			CleanResource(createdBrokerCr, brokerCr.Name, defaultNamespace)
			CleanResource(createdAddressCr, addressCr.Name, defaultNamespace)
		})

		It("Migrate to broker properties when RemoveFromBrokerOnDelete is true", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				Skip("Existing cluster required")
			}

			addressName := "TEST-ADDRESS"
			queueName := "TEST-QUEUE"
			routingType := "anycast"

			By("deploy a broker cr")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, nil)
			podName := namer.CrToSS(brokerCr.Name) + "-0"

			By("deploy an address cr")
			addressCr, createdAddressCr := DeployCustomAddress(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemisAddress) {
				candidate.Spec.AddressName = addressName
				candidate.Spec.QueueName = &queueName
				candidate.Spec.RoutingType = &routingType
				candidate.Spec.RemoveFromBrokerOnDelete = true
			})

			By("verify the queue exists")
			CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)
			CheckQueueAttribute(brokerCr.Name, podName, defaultNamespace, queueName, addressName, "anycast", "ConfigurationManaged", "true")

			addressKey := types.NamespacedName{Name: createdAddressCr.Name, Namespace: createdAddressCr.Namespace}
			Eventually(func(g Gomega) {
				By("disabling RemoveFromBrokerOnDelete")
				g.Expect(k8sClient.Get(ctx, addressKey, createdAddressCr)).Should(Succeed())
				createdAddressCr.Spec.RemoveFromBrokerOnDelete = false
				g.Expect(k8sClient.Update(ctx, createdAddressCr)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			brokerKey := types.NamespacedName{Name: createdBrokerCr.Name, Namespace: createdBrokerCr.Namespace}
			Eventually(func(g Gomega) {
				By("adding broker properties")
				g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
				createdBrokerCr.Spec.BrokerProperties = []string{
					"addressConfigurations.TEST-ADDRESS.queueConfigs.TEST-QUEUE.routingType=ANYCAST",
					"addressConfigurations.TEST-ADDRESS.queueConfigs.TEST-QUEUE.durable=true",
				}
				g.Expect(k8sClient.Update(ctx, createdBrokerCr)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				By("deleting address CR")
				g.Expect(k8sClient.Get(ctx, addressKey, createdAddressCr)).Should(Succeed())
				g.Expect(k8sClient.Delete(ctx, createdAddressCr)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				By("checking lscrs secret is gone")
				expectedSecuritySecret := &corev1.Secret{}
				expectedSecuritySecretKey := types.NamespacedName{Name: "secret-address-" + addressCr.Name, Namespace: defaultNamespace}
				g.Expect(k8sClient.Get(ctx, expectedSecuritySecretKey, expectedSecuritySecret)).ShouldNot(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verify the queue exists")
			CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)
			CheckQueueAttribute(brokerCr.Name, podName, defaultNamespace, queueName, addressName, "anycast", "ConfigurationManaged", "true")

			By("cleanup")
			CleanResource(createdBrokerCr, brokerCr.Name, defaultNamespace)
			CleanResource(createdAddressCr, addressCr.Name, defaultNamespace)
		})
	})
})

func CheckQueueExistInPod(brokerCrName string, podName string, queueName string, namespace string) {
	QueueStatAssertionInPod(brokerCrName, podName, namespace, func(g gomegaTypes.Gomega, content string) {
		g.Expect(content).Should(ContainSubstring(queueName))
	})
}

func CheckQueueNotExistInPod(brokerCrName string, podName string, queueName string, namespace string) {
	QueueStatAssertionInPod(brokerCrName, podName, namespace, func(g gomegaTypes.Gomega, content string) {
		g.Expect(content).ShouldNot(ContainSubstring(queueName))
	})
}

func QueueStatAssertionInPod(brokerCrName string, podName string, namespace string, customAssert func(g Gomega, content string)) {

	if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
		Fail("function should be called with an existing cluster")
	}

	podKey := types.NamespacedName{
		Name:      podName,
		Namespace: namespace,
	}
	pod := &corev1.Pod{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, podKey, pod)).Should(Succeed())
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	gvk := schema.GroupVersionKind{
		Group:   "",
		Version: "v1",
		Kind:    "Pod",
	}

	httpClient, err := rest.HTTPClientFor(restConfig)
	Expect(err).To(BeNil())
	restClient, err := apiutil.RESTClientForGVK(gvk, false, restConfig, serializer.NewCodecFactory(scheme.Scheme), httpClient)
	Expect(err).To(BeNil())

	Eventually(func(g Gomega) {
		execReq := restClient.
			Post().
			Namespace(namespace).
			Resource("pods").
			Name(podName).
			SubResource("exec").
			VersionedParams(&corev1.PodExecOptions{
				Container: brokerCrName + "-container",
				Command:   []string{"amq-broker/bin/artemis", "queue", "stat", "--silent", "--url", "tcp://" + podName + ":61616"},
				Stdin:     true,
				Stdout:    true,
				Stderr:    true,
			}, runtime.NewParameterCodec(scheme.Scheme))

		exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", execReq.URL())

		if err != nil {
			fmt.Printf("error while creating remote command executor: %v", err)
		}
		g.Expect(err).To(BeNil())
		var capturedOut bytes.Buffer

		err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:  os.Stdin,
			Stdout: &capturedOut,
			Stderr: os.Stderr,
			Tty:    false,
		})
		g.Expect(err).To(BeNil())

		By("Checking for output pod")
		g.Expect(capturedOut.Len() > 0)
		content := capturedOut.String()

		if verbose {
			fmt.Printf("OutputFromPod: \n%v\n", content)
		}

		customAssert(g, content)
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
}

func CheckQueueAttribute(brokerCrName string, podName string, namespace string, queueName string, addressName string, routingType string, attrName string, attrValueAsString string) {

	if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
		Fail("function should be called with an existing cluster")
	}

	Eventually(func(g Gomega) {
		key := types.NamespacedName{Name: namer.CrToSS(brokerCrName), Namespace: namespace}
		sfsFound := &appsv1.StatefulSet{}

		g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
		g.Expect(sfsFound.Status.ReadyReplicas).Should(BeEquivalentTo(1))
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	podKey := types.NamespacedName{
		Name:      podName,
		Namespace: namespace,
	}
	pod := &corev1.Pod{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, podKey, pod)).Should(Succeed())
		jolokia := jolokia.GetJolokia(k8sClient, pod.Status.PodIP, "8161", "/console/jolokia", "", "", "http")
		data, err := jolokia.Read("org.apache.activemq.artemis:broker=\"amq-broker\",address=\"" + addressName + "\",component=addresses,queue=\"" + queueName + "\",routing-type=\"" + routingType + "\",subcomponent=queues/" + attrName)
		g.Expect(err).To(BeNil())
		g.Expect(data.Value).Should(ContainSubstring(attrValueAsString), data.Value)

	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
}
