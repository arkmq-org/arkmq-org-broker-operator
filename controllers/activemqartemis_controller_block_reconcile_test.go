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
	"os"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
)

var _ = Describe("reconcile block with annotation", func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("test", Label("block-reconcile"), func() {
		It("deploy, annotate, verify", func() {

			deploycount := "DEPLOY_COUNT"
			By("deploy a broker cr")
			crd, _ := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Spec.Env = []corev1.EnvVar{
					{
						Name:  deploycount,
						Value: "1",
					},
				}
			})

			brokerKey := types.NamespacedName{
				Name:      crd.Name,
				Namespace: defaultNamespace,
			}

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, brokerKey, crd)).Should(Succeed())
					ready := meta.FindStatusCondition(crd.Status.Conditions, brokerv1beta1.ReadyConditionType)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}
			By("Checking SS env var")
			Eventually(func(g Gomega) {
				key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
				sfsFound := &appsv1.StatefulSet{}

				g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
				found := false
				for _, e := range sfsFound.Spec.Template.Spec.Containers[0].Env {
					By("checking env: " + e.Name)
					if e.Name == deploycount && e.Value == "1" {
						found = true
					}
				}
				g.Expect(found).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("annotating with block")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, crd)).Should(Succeed())
				crd.Annotations = map[string]string{common.BlockReconcileAnnotation: "true"}
				crd.Spec.Env[0].Value = "2"
				g.Expect(k8sClient.Update(ctx, crd)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Checking blocked condition")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, crd)).Should(Succeed())
				blocked := meta.FindStatusCondition(crd.Status.Conditions, brokerv1beta1.ReconcileBlockedType)
				g.Expect(blocked).NotTo(BeNil())
				g.Expect(blocked.Status).To(Equal(metav1.ConditionTrue))

				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
					ready := meta.FindStatusCondition(crd.Status.Conditions, brokerv1beta1.ReadyConditionType)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				}

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("release blocked annotation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, crd)).Should(Succeed())
				crd.Annotations = map[string]string{common.BlockReconcileAnnotation: "false"}
				g.Expect(k8sClient.Update(ctx, crd)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Checking blocked gone")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, crd)).Should(Succeed())

				blocked := meta.FindStatusCondition(crd.Status.Conditions, brokerv1beta1.ReconcileBlockedType)
				g.Expect(blocked).To(BeNil())

				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
					ready := meta.FindStatusCondition(crd.Status.Conditions, brokerv1beta1.ReadyConditionType)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Checking SS env var")
			Eventually(func(g Gomega) {
				key := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: defaultNamespace}
				sfsFound := &appsv1.StatefulSet{}

				g.Expect(k8sClient.Get(ctx, key, sfsFound)).Should(Succeed())
				found := false
				for _, e := range sfsFound.Spec.Template.Spec.Containers[0].Env {
					By("checking env: " + e.Name)
					if e.Name == deploycount && e.Value == "2" {
						found = true
					}
				}
				g.Expect(found).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(crd, crd.Name, defaultNamespace)
		})

		It("annotate, deploy, verify", func() {

			By("deploy a broker with blocked annotation")
			crd, _ := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
				candidate.Annotations = map[string]string{common.BlockReconcileAnnotation: "true"}
			})

			brokerKey := types.NamespacedName{
				Name:      crd.Name,
				Namespace: defaultNamespace,
			}

			By("checking status")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, crd)).Should(Succeed())

				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
					condition := meta.FindStatusCondition(crd.Status.Conditions, brokerv1beta1.ReadyConditionType)
					g.Expect(condition).NotTo(BeNil())
					g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				}
				blocked := meta.FindStatusCondition(crd.Status.Conditions, brokerv1beta1.ReconcileBlockedType)
				g.Expect(blocked).NotTo(BeNil())
				g.Expect(blocked.Status).To(Equal(metav1.ConditionTrue))

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("release blocked annotation")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, crd)).Should(Succeed())
				crd.Annotations = map[string]string{common.BlockReconcileAnnotation: "false"}
				g.Expect(k8sClient.Update(ctx, crd)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Checking ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, crd)).Should(Succeed())
				blocked := meta.FindStatusCondition(crd.Status.Conditions, brokerv1beta1.ReconcileBlockedType)
				g.Expect(blocked).To(BeNil())

				if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
					ready := meta.FindStatusCondition(crd.Status.Conditions, brokerv1beta1.ReadyConditionType)
					g.Expect(ready).NotTo(BeNil())
					g.Expect(ready.Status).To(Equal(metav1.ConditionTrue))
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(crd, crd.Name, defaultNamespace)
		})
	})
})
