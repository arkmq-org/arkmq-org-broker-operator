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

package controllers

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
)

var _ = Describe("broker controller", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("basic broker deployment", Label("broker-deploy"), func() {
		It("deploys, verifies and updates a single broker", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("deploying the Broker CR")
				brokerCr, createdBrokerCr := DeployCustomBrokerV1(defaultNamespace, nil)

				By("verifying the broker pod is running")
				WaitForPod(brokerCr.Name)

				ssKey := types.NamespacedName{
					Name:      namer.CrToSS(brokerCr.Name),
					Namespace: defaultNamespace,
				}
				currentSS := &appsv1.StatefulSet{}

				By("verifying the StatefulSet is ready")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
					g.Expect(currentSS.Status.ReadyReplicas).Should(BeEquivalentTo(1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				brokerKey := types.NamespacedName{
					Name:      brokerCr.Name,
					Namespace: defaultNamespace,
				}

				By("updating the Broker CR with an annotation")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdBrokerCr)).Should(Succeed())
					if createdBrokerCr.Spec.DeploymentPlan.Annotations == nil {
						createdBrokerCr.Spec.DeploymentPlan.Annotations = make(map[string]string)
					}
					createdBrokerCr.Spec.DeploymentPlan.Annotations["test-key"] = "test-value"
					g.Expect(k8sClient.Update(ctx, createdBrokerCr)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying the annotation appears on the StatefulSet pod template")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
					g.Expect(currentSS.Spec.Template.Annotations).To(HaveKeyWithValue("test-key", "test-value"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("cleaning up")
				CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
			}
		})
	})
})

var _ = Describe("brokercluster configmap bp", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("configmap with -bp suffix", func() {
		It("round-trip: broker picks up properties from ConfigMap and reloads on update", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			queueName := "cm-bp-queue"
			cmName := NextSpecResourceName() + common.BrokerPropsSuffix

			By("creating a ConfigMap with -bp suffix containing broker properties")
			configMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: defaultNamespace,
				},
				Data: map[string]string{
					"address.properties": "addressConfigurations." + queueName + ".queueConfigs." + queueName + ".routingType=ANYCAST\n" +
						"addressConfigurations." + queueName + ".routingTypes=ANYCAST\n",
				},
			}
			Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())

			By("deploying a BrokerCluster CR referencing the ConfigMap via extraMounts")
			brokerCr, createdBrokerCr := DeployCustomBrokerV1(defaultNamespace, func(c *brokerv1beta2.BrokerCluster) {
				c.Spec.DeploymentPlan.ExtraMounts.ConfigMaps = []string{cmName}
			})

			By("waiting for the broker pod to be ready")
			WaitForPod(brokerCr.Name)

			ssKey := types.NamespacedName{
				Name:      namer.CrToSS(brokerCr.Name),
				Namespace: defaultNamespace,
			}
			currentSS := &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
				g.Expect(currentSS.Status.ReadyReplicas).Should(BeEquivalentTo(1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying the queue from ConfigMap properties exists in the broker")
			podName := namer.CrToSS(brokerCr.Name) + "-0"
			CheckQueueExistInPod(brokerCr.Name, podName, queueName, defaultNamespace)

			By("updating the ConfigMap with a new queue property")
			updatedQueueName := "cm-bp-queue-updated"
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: defaultNamespace}, configMap)).Should(Succeed())
				configMap.Data["address.properties"] = "addressConfigurations." + updatedQueueName + ".queueConfigs." + updatedQueueName + ".routingType=ANYCAST\n" +
					"addressConfigurations." + updatedQueueName + ".routingTypes=ANYCAST\n"
				g.Expect(k8sClient.Update(ctx, configMap)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying the updated queue appears in the broker after ConfigMap reload")
			command := []string{"amq-broker/bin/artemis", "queue", "stat", "--url", "tcp://" + podName + ":61616"}
			Eventually(func(g Gomega) {
				stdOutContent := ExecOnPod(podName, brokerCr.Name, defaultNamespace, command, g)
				g.Expect(stdOutContent).Should(ContainSubstring(updatedQueueName))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
			CleanResource(configMap, cmName, defaultNamespace)
		})
	})
})
