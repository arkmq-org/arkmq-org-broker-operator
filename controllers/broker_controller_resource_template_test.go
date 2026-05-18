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
	"fmt"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
)

var _ = Describe("templates", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("dud template", func() {
		It("missing kind field applies with error", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				brokerCrd := generateBrokerSpec(defaultNamespace)

				brokerCrd.Spec.ResourceTemplates = []v1beta2.ResourceTemplate{
					v1beta2.ResourceTemplate{
						// match all kinds with nill selector
						Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{
							Object: map[string]interface{}{
								"name": "does not match, should error",
							},
						}),
					},
				}

				Expect(k8sClient.Create(ctx, &brokerCrd)).Should(Succeed())

				brokerKey := types.NamespacedName{
					Name:      brokerCrd.Name,
					Namespace: defaultNamespace,
				}

				createdCrd := &v1beta2.Broker{}
				By("Checking status")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					if verbose {
						fmt.Printf("STATUS: \n%v\n", createdCrd.Status)
					}
					g.Expect(meta.IsStatusConditionFalse(createdCrd.Status.Conditions, v1beta2.ReadyConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, v1beta2.ValidConditionType)).Should(BeTrue())

					deployedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, v1beta2.DeployedConditionType)
					g.Expect(deployedCondition).NotTo(BeNil())
					g.Expect(deployedCondition.Reason).Should(Equal(v1beta2.DeployedConditionCrudKindErrorReason))
					g.Expect(deployedCondition.Message).Should(ContainSubstring("error applying strategic merge patch"))

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("cleaning up")
				CleanResource(createdCrd, brokerCrd.Name, defaultNamespace)
			}
		})

		It("missing kind succeed", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				brokerCrd := generateBrokerSpec(defaultNamespace)

				brokerCrd.Spec.ResourceTemplates = []v1beta2.ResourceTemplate{
					{
						Selector: &v1beta2.ResourceSelector{Kind: ptr.To("Service")},
						Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{
							Object: map[string]interface{}{
								"spec": map[string]interface{}{
									"publishNotReadyAddresses": false,
								},
							},
						}),
					},
				}
				Expect(k8sClient.Create(ctx, &brokerCrd)).Should(Succeed())

				brokerKey := types.NamespacedName{
					Name:      brokerCrd.Name,
					Namespace: defaultNamespace,
				}

				createdCrd := &v1beta2.Broker{}
				By("Checking status")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					if verbose {
						fmt.Printf("STATUS: \n%v\n", createdCrd.Status)
					}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, v1beta2.ValidConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, v1beta2.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Checking service template attribute")

				serviceKey := types.NamespacedName{
					Name:      brokerCrd.Name + "-hdls-svc",
					Namespace: defaultNamespace,
				}
				service := &corev1.Service{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, serviceKey, service)).Should(Succeed())
					g.Expect(service.Spec.PublishNotReadyAddresses).To(BeFalse())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("cleaning up")
				CleanResource(createdCrd, brokerCrd.Name, defaultNamespace)
			}
		})

		It("backward compatibility: patch with kind field still works", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {

				brokerCrd := generateBrokerSpec(defaultNamespace)

				brokerCrd.Spec.ResourceTemplates = []v1beta2.ResourceTemplate{
					{
						Selector: &v1beta2.ResourceSelector{Kind: ptr.To("Service")},
						Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{
							Object: map[string]interface{}{
								"kind": "Service", // Including kind for backward compatibility test
								"spec": map[string]interface{}{
									"publishNotReadyAddresses": false,
								},
							},
						}),
					},
				}
				Expect(k8sClient.Create(ctx, &brokerCrd)).Should(Succeed())

				brokerKey := types.NamespacedName{
					Name:      brokerCrd.Name,
					Namespace: defaultNamespace,
				}

				createdCrd := &v1beta2.Broker{}
				By("Checking status - should succeed even with kind field present")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					if verbose {
						fmt.Printf("STATUS: \n%v\n", createdCrd.Status)
					}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, v1beta2.ValidConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, v1beta2.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Checking service template attribute was applied")

				serviceKey := types.NamespacedName{
					Name:      brokerCrd.Name + "-hdls-svc",
					Namespace: defaultNamespace,
				}
				service := &corev1.Service{}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, serviceKey, service)).Should(Succeed())
					g.Expect(service.Spec.PublishNotReadyAddresses).To(BeFalse())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("cleaning up")
				CleanResource(createdCrd, brokerCrd.Name, defaultNamespace)
			}
		})
	})
})
