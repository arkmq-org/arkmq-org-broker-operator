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

package controllers

import (
	"context"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
)

var _ = Describe("broker name", func() {

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("set non default value", func() {

		It("non restricted", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()

			crd := brokerv1beta1.ActiveMQArtemis{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemis",
					APIVersion: brokerv1beta1.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      NextSpecResourceName(),
					Namespace: defaultNamespace,
				},
			}

			crd.Spec.Env = []corev1.EnvVar{
				{Name: "AMQ_NAME", Value: "joe"},
			}

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &brokerv1beta1.ActiveMQArtemis{}

			By("Checking ready, operator can access broker status via jmx")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

				if verbose {
					fmt.Printf("STATUS: %v\n\n", createdCrd.Status.Conditions)
				}
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
		})

		Context("restricted", func() {

			BeforeEach(func() {
				if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
					Skip("MUST be run with USE_EXISTING_CLUSTER=true")
				}
			})

			It("in same namespace as operator", func() {
				ctx := context.Background()

				crd := brokerv1beta1.ActiveMQArtemis{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ActiveMQArtemis",
						APIVersion: brokerv1beta1.GroupVersion.Identifier(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      NextSpecResourceName(),
						Namespace: defaultNamespace,
					},
				}

				crd.Spec.Restricted = common.NewTrue()
				crd.Spec.Env = []corev1.EnvVar{
					{Name: "AMQ_NAME", Value: "broker-same-ns"},
				}

				By("Deploying the CRD " + crd.ObjectMeta.Name + " in " + defaultNamespace)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("Checking broker is ready")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					if verbose {
						fmt.Printf("STATUS: %v\n\n", createdCrd.Status.Conditions)
					}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Cleaning up")
				Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
			})

			It("in different namespace from operator", func() {
				ctx := context.Background()
				brokerNamespace := "other-test"

				By("creating broker namespace " + brokerNamespace + " with restricted security policy")
				restrictedSecurityPolicy := "restricted"
				Expect(createNamespace(brokerNamespace, &restrictedSecurityPolicy)).To(Succeed())

				crd := brokerv1beta1.ActiveMQArtemis{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ActiveMQArtemis",
						APIVersion: brokerv1beta1.GroupVersion.Identifier(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      NextSpecResourceName(),
						Namespace: brokerNamespace,
					},
				}

				crd.Spec.Restricted = common.NewTrue()
				crd.Spec.Env = []corev1.EnvVar{
					{Name: "AMQ_NAME", Value: "broker-other-ns"},
				}

				By("Deploying the CRD " + crd.ObjectMeta.Name + " in " + brokerNamespace)
				Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

				brokerKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
				createdCrd := &brokerv1beta1.ActiveMQArtemis{}

				By("Checking broker is ready")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, createdCrd)).Should(Succeed())

					if verbose {
						fmt.Printf("STATUS: %v\n\n", createdCrd.Status.Conditions)
					}
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ReadyConditionType)).Should(BeTrue())
					g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)).Should(BeTrue())

				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("Cleaning up")
				Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())

				By("deleting broker namespace " + brokerNamespace)
				deleteNamespace(brokerNamespace, true, Default)
			})
		})
	})
})
