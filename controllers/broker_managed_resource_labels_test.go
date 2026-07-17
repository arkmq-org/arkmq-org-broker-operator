/*
Copyright 2026.

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
	"k8s.io/apimachinery/pkg/types"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
)

const (
	brokerTrackingLabelKey          = "Broker"
	activeMQArtemisTrackingLabelKey = "ActiveMQArtemis"
)

func assertManagedResourceTrackingLabels(g Gomega, labels map[string]string, crName string, expectedKey string, forbiddenKey string) {
	g.Expect(labels).To(HaveKeyWithValue(expectedKey, crName))
	g.Expect(labels).To(HaveKeyWithValue(selectors.LabelAppKey, crName+"-app"))
	_, hasForbidden := labels[forbiddenKey]
	g.Expect(hasForbidden).To(BeFalse(), "must not use tracking label key %q", forbiddenKey)
}

var _ = Describe("broker managed resource labels", Label("broker-label-test"), func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("Broker CR", func() {
		It("tags managed resources with the Broker tracking label", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			By("deploying a Broker CR")
			brokerCr, createdBrokerCr := DeployCustomBrokerCR(defaultNamespace, nil)

			By("waiting for the broker pod to be running")
			WaitForPod(brokerCr.Name)

			ssKey := types.NamespacedName{
				Name:      namer.CrToSS(brokerCr.Name),
				Namespace: defaultNamespace,
			}
			headlessSvcKey := types.NamespacedName{
				Name:      brokerCr.Name + "-hdls-svc",
				Namespace: defaultNamespace,
			}
			propsSecretKey := types.NamespacedName{
				Name:      brokerCr.Name + "-props",
				Namespace: defaultNamespace,
			}

			By("verifying StatefulSet labels")
			Eventually(func(g Gomega) {
				currentSS := &appsv1.StatefulSet{}
				g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, currentSS.Labels, brokerCr.Name, brokerTrackingLabelKey, activeMQArtemisTrackingLabelKey)
				assertManagedResourceTrackingLabels(g, currentSS.Spec.Template.Labels, brokerCr.Name, brokerTrackingLabelKey, activeMQArtemisTrackingLabelKey)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying headless Service labels and selector")
			Eventually(func(g Gomega) {
				headlessSvc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, headlessSvcKey, headlessSvc)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, headlessSvc.Labels, brokerCr.Name, brokerTrackingLabelKey, activeMQArtemisTrackingLabelKey)
				assertManagedResourceTrackingLabels(g, headlessSvc.Spec.Selector, brokerCr.Name, brokerTrackingLabelKey, activeMQArtemisTrackingLabelKey)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying broker properties Secret labels")
			Eventually(func(g Gomega) {
				propsSecret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, propsSecretKey, propsSecret)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, propsSecret.Labels, brokerCr.Name, brokerTrackingLabelKey, activeMQArtemisTrackingLabelKey)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying scale label selector uses Broker tracking label")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: brokerCr.Name, Namespace: defaultNamespace}, createdBrokerCr)).Should(Succeed())
				g.Expect(createdBrokerCr.Status.ScaleLabelSelector).To(ContainSubstring(brokerTrackingLabelKey + "=" + brokerCr.Name))
				g.Expect(createdBrokerCr.Status.ScaleLabelSelector).NotTo(ContainSubstring(activeMQArtemisTrackingLabelKey + "="))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
		})
	})

	Context("ActiveMQArtemis CR", func() {
		It("continues to tag managed resources with the ActiveMQArtemis tracking label", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			By("deploying an ActiveMQArtemis CR")
			brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, nil)

			By("waiting for the broker pod to be running")
			WaitForPod(brokerCr.Name)

			ssKey := types.NamespacedName{
				Name:      namer.CrToSS(brokerCr.Name),
				Namespace: defaultNamespace,
			}
			headlessSvcKey := types.NamespacedName{
				Name:      brokerCr.Name + "-hdls-svc",
				Namespace: defaultNamespace,
			}
			propsSecretKey := types.NamespacedName{
				Name:      brokerCr.Name + "-props",
				Namespace: defaultNamespace,
			}

			By("verifying StatefulSet labels")
			Eventually(func(g Gomega) {
				currentSS := &appsv1.StatefulSet{}
				g.Expect(k8sClient.Get(ctx, ssKey, currentSS)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, currentSS.Labels, brokerCr.Name, activeMQArtemisTrackingLabelKey, brokerTrackingLabelKey)
				assertManagedResourceTrackingLabels(g, currentSS.Spec.Template.Labels, brokerCr.Name, activeMQArtemisTrackingLabelKey, brokerTrackingLabelKey)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying headless Service labels and selector")
			Eventually(func(g Gomega) {
				headlessSvc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, headlessSvcKey, headlessSvc)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, headlessSvc.Labels, brokerCr.Name, activeMQArtemisTrackingLabelKey, brokerTrackingLabelKey)
				assertManagedResourceTrackingLabels(g, headlessSvc.Spec.Selector, brokerCr.Name, activeMQArtemisTrackingLabelKey, brokerTrackingLabelKey)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying broker properties Secret labels")
			Eventually(func(g Gomega) {
				propsSecret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, propsSecretKey, propsSecret)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, propsSecret.Labels, brokerCr.Name, activeMQArtemisTrackingLabelKey, brokerTrackingLabelKey)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying scale label selector uses ActiveMQArtemis tracking label")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: brokerCr.Name, Namespace: defaultNamespace}, createdBrokerCr)).Should(Succeed())
				g.Expect(createdBrokerCr.Status.ScaleLabelSelector).To(ContainSubstring(activeMQArtemisTrackingLabelKey + "=" + brokerCr.Name))
				g.Expect(createdBrokerCr.Status.ScaleLabelSelector).NotTo(ContainSubstring(brokerTrackingLabelKey + "="))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
		})
	})
})
