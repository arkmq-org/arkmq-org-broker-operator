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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
)

var _ = Describe("brokercluster restricted", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("deprecated spec.restricted flag", Label("brokercluster-restricted"), func() {
		It("rejects a BrokerCluster with restricted=true as invalid", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				By("creating a BrokerCluster with spec.restricted=true")
				brokerCrd := generateBrokerSpec(defaultNamespace)
				brokerCrd.Spec.Restricted = ptr.To(true)
				brokerCrd.Spec.DeploymentPlan.Size = ptr.To(int32(1))

				Expect(k8sClient.Create(ctx, &brokerCrd)).Should(Succeed())

				brokerKey := types.NamespacedName{
					Name:      brokerCrd.Name,
					Namespace: defaultNamespace,
				}

				createdCrd := &v1beta2.BrokerCluster{}

				By("verifying Valid=False with deprecation reason")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					validCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, v1beta2.ValidConditionType)
					g.Expect(validCondition).NotTo(BeNil())
					g.Expect(validCondition.Status).Should(Equal(metav1.ConditionFalse))
					g.Expect(validCondition.Reason).Should(Equal(v1beta2.ValidConditionInvalidVersionReason))
					g.Expect(validCondition.Message).Should(ContainSubstring("spec.restricted is deprecated"))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying Ready=False")
				Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, v1beta2.ReadyConditionType)).Should(BeTrue())

				By("verifying no StatefulSet is created")
				ssKey := types.NamespacedName{
					Name:      namer.CrToSS(brokerCrd.Name),
					Namespace: defaultNamespace,
				}
				Consistently(func(g Gomega) {
					currentSS := &appsv1.StatefulSet{}
					err := k8sClient.Get(ctx, ssKey, currentSS)
					g.Expect(err).Should(HaveOccurred())
				}, "10s", "2s").Should(Succeed())

				By("cleaning up")
				CleanResource(createdCrd, brokerCrd.Name, defaultNamespace)
			}
		})
	})
})
