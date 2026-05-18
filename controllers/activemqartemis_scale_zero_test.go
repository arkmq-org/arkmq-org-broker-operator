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
	"os"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/scale"

	"k8s.io/client-go/discovery"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
)

var _ = Describe("subresource scale down", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("scale to zero via scale subresource", func() {
		It("deploy plan 1", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				ctx := context.Background()
				brokerCrd := generateArtemisSpec(defaultNamespace)

				// need to popluate scale json path
				brokerCrd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)

				Expect(k8sClient.Create(ctx, &brokerCrd)).Should(Succeed())

				createdBrokerCrd := &brokerv1beta1.ActiveMQArtemis{}
				createdBrokerCrdKey := types.NamespacedName{
					Name:      brokerCrd.Name,
					Namespace: defaultNamespace,
				}

				var crdResourceVersion string
				By("verifying started")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, createdBrokerCrdKey, createdBrokerCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					crdResourceVersion = createdBrokerCrd.GetResourceVersion()

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("scale to zero via scale sub resource")

				clientset, err := discovery.NewDiscoveryClientForConfig(k8Manager.GetConfig())
				Expect(err).To(BeNil())

				scaleClient := scale.New(
					clientset.RESTClient(), k8Manager.GetRESTMapper(),
					dynamic.LegacyAPIPathResolverFunc,
					scale.NewDiscoveryScaleKindResolver(clientset),
				)

				currentScales := scaleClient.Scales(defaultNamespace)

				By("by getting current val via scale sub resource")
				resource := schema.GroupResource{Group: artemisGvk.Group, Resource: artemisGvk.Kind}
				scaleVal, err := currentScales.Get(ctx, resource, createdBrokerCrd.Name, v1.GetOptions{})
				Expect(err).To(BeNil())

				Expect(scaleVal.Spec.Replicas).To(BeEquivalentTo(1))

				Eventually(func(g Gomega) {
					scaleVal, err = currentScales.Get(ctx, resource, createdBrokerCrd.Name, v1.GetOptions{})
					g.Expect(err).To(BeNil())

					By("by updating via via scale sub resource")
					scaleVal.Spec.Replicas = 0
					_, err = currentScales.Update(ctx, resource, scaleVal, v1.UpdateOptions{})
					g.Expect(err).To(BeNil())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying scaled down via status")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, createdBrokerCrdKey, createdBrokerCrd)).Should(Succeed())

					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeFalse())
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.DeployedConditionType)).Should(BeFalse())
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.ValidConditionType)).Should(BeTrue())

					g.Expect(createdBrokerCrd.Status.DeploymentPlanSize).Should(BeEquivalentTo(0))

					By("verify crd updated")
					g.Expect(crdResourceVersion).ShouldNot(Equal(createdBrokerCrd.GetResourceVersion()))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("scaling back up")
				Eventually(func(g Gomega) {
					scaleVal, err = currentScales.Get(ctx, resource, createdBrokerCrd.Name, v1.GetOptions{})
					g.Expect(err).To(BeNil())

					By("by updating via via scale sub resource")
					scaleVal.Spec.Replicas = 1
					_, err = currentScales.Update(ctx, resource, scaleVal, v1.UpdateOptions{})
					Expect(err).To(BeNil())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("verifying restarted")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, createdBrokerCrdKey, createdBrokerCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Expect(k8sClient.Delete(ctx, createdBrokerCrd)).Should(Succeed())
			}

		})

		It("deploy plan with label for autoscale", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				ctx := context.Background()
				brokerCrd := generateArtemisSpec(defaultNamespace)

				// need to popluate scale json path
				brokerCrd.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
				brokerCrd.Spec.DeploymentPlan.Labels = map[string]string{"A": "a"}

				Expect(k8sClient.Create(ctx, &brokerCrd)).Should(Succeed())

				createdBrokerCrd := &brokerv1beta1.ActiveMQArtemis{}
				createdBrokerCrdKey := types.NamespacedName{
					Name:      brokerCrd.Name,
					Namespace: defaultNamespace,
				}

				By("verifying started")
				Eventually(func(g Gomega) {

					g.Expect(k8sClient.Get(ctx, createdBrokerCrdKey, createdBrokerCrd)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(createdBrokerCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())

					g.Expect(createdBrokerCrd.Status.ScaleLabelSelector).Should(ContainSubstring("A=a"))
					g.Expect(createdBrokerCrd.Status.ScaleLabelSelector).Should(ContainSubstring(createdBrokerCrd.Name))
					g.Expect(createdBrokerCrd.Status.ScaleLabelSelector).Should(ContainSubstring(","))

					selector, err := labels.Parse(createdBrokerCrd.Status.ScaleLabelSelector)
					g.Expect(err).Should(BeNil())
					g.Expect(selector.Empty()).Should(BeFalse())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				Expect(k8sClient.Delete(ctx, createdBrokerCrd)).Should(Succeed())
			}
		})
	})
})
