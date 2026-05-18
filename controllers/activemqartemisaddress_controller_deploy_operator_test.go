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
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// To run this test using the following command
// export OPERATOR_IMAGE="<the test operator image>";export DEPLOY_OPERATOR="true";export TEST_ARGS="-ginkgo.focus \"Address controller\" -ginkgo.v"; make -e test-mk
// if OPERATOR_IMAGE is not defined the test will use the latest dev tag
// Makefile target: test-mk-do
// The Deployeyed Operator (DO) watches a single NameSpace called "default"
var _ = Describe("Address controller DO", Label("do"), func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("Address test", func() {

		It("Deploy CR with size 5 (pods)", Label("slow"), func() {

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
					g.Expect(len(createdBrokerCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(5))

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
					g.Expect(len(createdBrokerCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(5))

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

				for ipod := 4; ipod >= 0; ipod-- {
					podOrdinal := strconv.FormatInt(int64(ipod), 10)
					podName := namer.CrToSS(brokerCrd.Name) + "-" + podOrdinal

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

						By("Checking for output pod")
						g.Expect(capturedOut.Len() > 0)
						content := capturedOut.String()
						g.Expect(content).Should(ContainSubstring("myQueue0"))
						g.Expect(content).Should(ContainSubstring("myQueue1"))
						g.Expect(content).Should(ContainSubstring("myQueue2"))
						g.Expect(content).Should(ContainSubstring("myQueue3"))
						g.Expect(content).Should(ContainSubstring("myQueue4"))
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

		It("Scale down, verify RemoveFromBrokerOnDelete", Label("slow"), func() {

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

		It("address creation with multiple ApplyToCrNames", Label("slow"), func() {

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
			}

			// cleanup
			k8sClient.Delete(ctx, crd0)
			k8sClient.Delete(ctx, crd1)
			k8sClient.Delete(ctx, crd2)
		})
	})
})
