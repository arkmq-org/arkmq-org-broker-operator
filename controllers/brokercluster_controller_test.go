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
	"k8s.io/apimachinery/pkg/types"

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
