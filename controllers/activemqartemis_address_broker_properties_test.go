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
	"context"
	"fmt"
	"os"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("BrokerProperties Address tests", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("bp address queue config defaults", Label("queue-config-defaults"), func() {
		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			queueName := "myqueue"
			addressName := "myaddress"
			It("configurationManaged default should be true (without queue configuration)", func() {
				By("deploy a broker cr")
				brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(c *brokerv1beta1.ActiveMQArtemis) {
					c.Spec.BrokerProperties = []string{
						"addressConfigurations.myaddress.routingTypes=ANYCAST",
						"addressConfigurations.myaddress.queueConfigs.myqueue.address=myaddress",
						"addressConfigurations.myaddress.queueConfigs.myqueue.routingType=ANYCAST",
					}
				})

				By("verify the configurationManaged attribute of the queue is true")
				podOrdinal := strconv.FormatInt(0, 10)
				podName := namer.CrToSS(brokerCr.Name) + "-" + podOrdinal

				CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)

				CheckQueueAttribute(brokerCr.Name, podName, defaultNamespace, queueName, addressName, "anycast", "ConfigurationManaged", "true")

				//cleanup
				CleanResource(createdBrokerCr, brokerCr.Name, defaultNamespace)
			})
		} else {
			fmt.Println("Test skipped as it requires an existing cluster")
		}
	})

	Context("bp broker with queue", Label("broker-address-res"), func() {
		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			queueName := "myqueue"
			It("address after recreating broker cr", func() {
				By("deploy a broker cr")
				brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(c *brokerv1beta1.ActiveMQArtemis) {
					c.Spec.BrokerProperties = []string{
						"addressConfigurations.myqueue.queueConfigs.myqueue.routingType=ANYCAST",
					}
				})

				By("Waiting for all pods to be started and ready")
				Eventually(func(g Gomega) {

					getPersistedVersionedCrd(brokerCr.ObjectMeta.Name, defaultNamespace, createdBrokerCr)

					ConfigAppliedCondition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(ConfigAppliedCondition).NotTo(BeNil())
					g.Expect(ConfigAppliedCondition.Reason).To(Equal(brokerv1beta1.ConfigAppliedConditionSynchedReason))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verify the queue is created")
				podOrdinal := strconv.FormatInt(0, 10)
				podName := namer.CrToSS(brokerCr.Name) + "-" + podOrdinal

				CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)

				//cleanup
				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
			})
		} else {
			fmt.Println("Test skipped as it requires an existing cluster")
		}
	})

	Context("bp broker with duplicate queue", Label("broker-address-res"), func() {
		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			queueName := "myqueue"
			It("address after recreating broker cr", func() {
				By("deploy a broker cr")
				brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(c *brokerv1beta1.ActiveMQArtemis) {
					c.Spec.BrokerProperties = []string{
						"addressConfigurations.myqueue.routingTypes=ANYCAST",
						"addressConfigurations.myqueue.queueConfigs.myqueue.routingType=ANYCAST",
						"addressConfigurations.myqueuedup.routingTypes=ANYCAST,MULTICAST",
						"addressConfigurations.myqueuedup.queueConfigs.myqueue.routingType=ANYCAST",
						"addressConfigurations.myqueuedup.queueConfigs.myqueue.address=myqueuedup",
					}
				})

				By("Waiting for all pods to be started and ready")
				Eventually(func(g Gomega) {

					getPersistedVersionedCrd(brokerCr.ObjectMeta.Name, defaultNamespace, createdBrokerCr)

					ConfigAppliedCondition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
					g.Expect(ConfigAppliedCondition).NotTo(BeNil())
					g.Expect(ConfigAppliedCondition.Reason).To(Equal(brokerv1beta1.ConfigAppliedConditionSynchedReason))

					// status has no notion of broker warnings that are logged
					// it would be a nice broker enhancement to track that last N warnings in the status string.
					// maybe a metric ConditionBrokerHasWarnings?
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verify the queue is created")
				podOrdinal := strconv.FormatInt(0, 10)
				podName := namer.CrToSS(brokerCr.Name) + "-" + podOrdinal

				CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)

				By("verifying logging warn on duplicate queue")
				podWithOrdinal := namer.CrToSS(createdBrokerCr.Name) + "-0"

				Eventually(func(g Gomega) {
					stdOutContent := LogsOfPod(podWithOrdinal, createdBrokerCr.Name, defaultNamespace, g)
					if verbose {
						fmt.Printf("\nLOG of Pod:\n%s", stdOutContent)
					}
					// WARN  [org.apache.activemq.artemis.core.server.impl.ActiveMQServerImpl] AMQ229019: Queue myqueue already exists on address myqueue
					g.Expect(stdOutContent).Should(ContainSubstring("AMQ229019"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				//cleanup
				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
			})
		} else {
			fmt.Println("Test skipped as it requires an existing cluster")
		}
	})

	Context("bp address controller test with reconcile", func() {

		It("bp deploy CR with size 5 (pods)", func() {

			ctx := context.Background()

			brokerCrd := generateArtemisSpec(defaultNamespace)

			brokerCrd.Spec.DeploymentPlan.Size = common.Int32ToPtr(5)

			By("creating 5 queues")
			brokerCrd.Spec.BrokerProperties = make([]string, 15)
			ordinal := int32(0)
			for i := 0; i < 15; i += 3 {
				brokerCrd.Spec.BrokerProperties[i] = fmt.Sprintf("addressConfigurations.myaddress-%d.routingTypes=ANYCAST", ordinal)
				brokerCrd.Spec.BrokerProperties[i+1] = fmt.Sprintf("addressConfigurations.myaddress-%d.queueConfigs.myqueue-%d.address=myaddress-%d", ordinal, ordinal, ordinal)
				brokerCrd.Spec.BrokerProperties[i+2] = fmt.Sprintf("addressConfigurations.myaddress-%d.queueConfigs.myqueue-%d.routingType=ANYCAST", ordinal, ordinal)
				ordinal++
			}

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				Expect(k8sClient.Create(ctx, &brokerCrd)).Should(Succeed())
				createdBrokerCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("Waiting for all pods to be started and ready")
				Eventually(func(g Gomega) {

					getPersistedVersionedCrd(brokerCrd.ObjectMeta.Name, defaultNamespace, createdBrokerCrd)
					g.Expect(len(createdBrokerCrd.Status.PodStatus.Ready)).Should(BeEquivalentTo(*brokerCrd.Spec.DeploymentPlan.Size))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Checking all addresses are created on all pods")

				deploymentSize := *brokerCrd.Spec.DeploymentPlan.Size
				for ipod := deploymentSize - 1; ipod >= 0; ipod-- {
					podOrdinal := strconv.FormatInt(int64(ipod), 10)
					podName := namer.CrToSS(brokerCrd.Name) + "-" + podOrdinal
					queueName := "myqueue-" + podOrdinal

					CheckQueueExistInPod(brokerCrd.Name, podName, queueName, defaultNamespace)
				}

				//clean up all resources
				Expect(k8sClient.Delete(ctx, createdBrokerCrd)).Should(Succeed())
			}
		})
	})

	Context("bp address delete", func() {

		It("bp verify remove from shared secret after scale up", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(2)

			By("By deploying shared address secret in advance")

			secret := &corev1.Secret{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Secret",
					APIVersion: "k8s.io.api.core.v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "address-bp",
					Namespace: crd.ObjectMeta.Namespace,
				},
			}

			addressPropsKey := "address.properties"
			secret.StringData = map[string]string{
				addressPropsKey: `
				# addresses

				# allow delete from config reload
				addressSettings.A2.configDeleteAddresses=FORCE
				addressSettings.A2.configDeleteQueues=FORCE

				
				# an empty multicast address
				addressConfigurations.A2.routingTypes=MULTICAST
				`,
			}

			crd.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{secret.Name}

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("Deploying the -bp secret " + secret.ObjectMeta.Name)
				Expect(k8sClient.Create(ctx, secret)).Should(Succeed())

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
						g.Expect(stdOutContent).Should(ContainSubstring("A2"))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				By("Deleting address in the secret")
				createdSecret := &corev1.Secret{}
				secretName := types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace}
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, secretName, createdSecret)).Should(Succeed())
					createdSecret.Data[addressPropsKey] = []byte(`
					# addresses

					# allow delete from config reload
					addressSettings.A2.configDeleteAddresses=FORCE
					addressSettings.A2.configDeleteQueues=FORCE
					
					# an empty multicast address
					# deleted by commenting out
					# addressConfigurations.A2.routingTypes=MULTICAST
					`)
					g.Expect(k8sClient.Update(ctx, createdSecret)).Should(Succeed())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Verifying address gone from both brokers")
				for _, ordinal := range ordinals {
					podWithOrdinal := namer.CrToSS(crd.Name) + "-" + ordinal
					command := []string{"amq-broker/bin/artemis", "address", "show", "--url", "tcp://" + podWithOrdinal + ":61616"}

					Eventually(func(g Gomega) {
						stdOutContent := ExecOnPod(podWithOrdinal, crd.Name, defaultNamespace, command, g)
						g.Expect(stdOutContent).ShouldNot(ContainSubstring("A2"))
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				k8sClient.Delete(ctx, createdSecret)
			}

			// cleanup
			k8sClient.Delete(ctx, &crd)

		})
	})

	Context("bp address creation test", Label("address-creation-test"), func() {

		It("bp create a queue, direct", func() {

			ctx := context.Background()
			crd := generateArtemisSpec(defaultNamespace)
			crd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
			dla1 := "DLQ.XxxxxxXXxxXdata"
			dlqPrefix := "DLQ"
			addressName := "myAddress0"
			queueName := "myQueue0"

			crd.Spec.BrokerProperties = []string{
				"# some defaults",

				"addressSettings.#.enableMetrics=true",
				"addressSettings.#.autoCreateExpiryResources=false",
				"addressSettings.#.deadLetterAddress=DLQ",
				"addressSettings.#.autoCreateDeadLetterResources=true",
				"addressSettings.#.autoCreateAddresses=false",
				"addressSettings.#.deadLetterQueuePrefix=" + dlqPrefix,
				"addressSettings.#.defaultMaxConsumers=10",
				"addressSettings.#.maxDeliveryAttempts=10",

				"# some specifics",
				"addressSettings.XxxxxxXXxxXdata#.deadLetterAddress=" + dla1,
				"addressSettings.XxxxxxXXxxXdata#.autoCreateExpiryResources=true",
				"addressSettings.XxxxxxXXxxXdata#.defaultMaxConsumers=10",

				"# addresses and queues",
				"addressConfigurations.myAddress0.routingTypes=ANYCAST",
				"addressConfigurations.myAddress0.queueConfigs.myQueue0.rouingType=ANYCAST",
				"addressConfigurations.myAddress0.queueConfigs.myQueue0.address=myAddress0",
			}
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

				By("Verifying queue is created")
				podWithOrdinal := namer.CrToSS(crd.Name) + "-0"
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
				Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
			} else {
				fmt.Println("Test skipped as it requires existing cluster with operator installed")
			}
		})
	})
})
