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
	"context"
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
)

var _ = Describe("broker-service status conditions", func() {

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("status condition transitions", func() {

		It("should track Deployed and Ready conditions correctly", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &broker.BrokerService{}

			By("verifying initial Deployed condition is False (NotReady)")
			var initialDeployedTime metav1.Time
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				deployedCond := meta.FindStatusCondition(createdCrd.Status.Conditions, broker.DeployedConditionType)
				if deployedCond != nil {
					if verbose {
						fmt.Printf("Deployed condition: Status=%s, Reason=%s\n", deployedCond.Status, deployedCond.Reason)
					}
					g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionFalse))
					initialDeployedTime = deployedCond.LastTransitionTime
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("waiting for Deployed condition to become True")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				deployedCond := meta.FindStatusCondition(createdCrd.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCond).NotTo(BeNil())
				if verbose {
					fmt.Printf("Deployed condition: Status=%s, Reason=%s\n", deployedCond.Status, deployedCond.Reason)
				}
				g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionTrue))
				// Verify transition time changed
				g.Expect(deployedCond.LastTransitionTime.After(initialDeployedTime.Time)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying Ready condition is True")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleanup")
			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
		})

		It("should track AppsProvisioned condition correctly", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"test": "status"},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &broker.BrokerService{}

			By("waiting for service to be ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying AppsProvisioned starts as True with Synced reason (no apps)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				appsProvisionedCond := meta.FindStatusCondition(createdCrd.Status.Conditions, broker.AppsProvisionedConditionType)
				if appsProvisionedCond != nil {
					if verbose {
						fmt.Printf("AppsProvisioned condition (no apps): Status=%s, Reason=%s\n",
							appsProvisionedCond.Status, appsProvisionedCond.Reason)
					}
					g.Expect(appsProvisionedCond.Status).Should(Equal(metav1.ConditionTrue))
					g.Expect(appsProvisionedCond.Reason).Should(Equal(broker.AppsProvisionedConditionSyncedReason))
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("creating an app")
			appName := "status-test-app"
			app := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"test": "status"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "STATUS.QUEUE"}},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			By("waiting for app to be ready")
			appKey := types.NamespacedName{Name: appName, Namespace: defaultNamespace}
			createdApp := &broker.BrokerApp{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdApp.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying AppsProvisioned eventually becomes True after broker picks up config")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				appsProvisionedCond := meta.FindStatusCondition(createdCrd.Status.Conditions, broker.AppsProvisionedConditionType)
				if appsProvisionedCond != nil {
					if verbose {
						fmt.Printf("AppsProvisioned condition (with app): Status=%s, Reason=%s, Message=%s\n",
							appsProvisionedCond.Status, appsProvisionedCond.Reason, appsProvisionedCond.Message)
					}
					g.Expect(appsProvisionedCond.Status).Should(Equal(metav1.ConditionTrue))
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying ProvisionedApps status field is populated")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				if verbose {
					fmt.Printf("ProvisionedApps: %v\n", createdCrd.Status.ProvisionedApps)
				}
				g.Expect(createdCrd.Status.ProvisionedApps).Should(HaveLen(1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleanup")
			Expect(k8sClient.Delete(ctx, createdApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
		})
	})

	Context("configuration updates", func() {

		It("should handle app capability updates", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"config": "test"},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &broker.BrokerService{}

			By("waiting for service to be ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("creating app with initial capabilities")
			appName := "config-app"
			app := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"config": "test"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "INITIAL.QUEUE"}},
							ConsumerOf: []broker.AddressRef{{Address: "INITIAL.QUEUE"}},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			appKey := types.NamespacedName{Name: appName, Namespace: defaultNamespace}
			createdApp := &broker.BrokerApp{}

			By("waiting for app to be ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdApp.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("capturing initial config resource version")
			brokerKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
			brokerCrd := &broker.Broker{}
			var initialConfigRV string
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, brokerCrd)).Should(Succeed())
				for _, externalConfig := range brokerCrd.Status.ExternalConfigs {
					if externalConfig.Name == AppPropertiesSecretName(brokerCrd.Name) {
						initialConfigRV = externalConfig.ResourceVersion
					}
				}
				g.Expect(initialConfigRV).ShouldNot(BeEmpty())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("updating app capabilities")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				createdApp.Spec.Capabilities = append(createdApp.Spec.Capabilities, broker.AppCapabilityType{
					ProducerOf: []broker.AddressRef{
						{Address: "UPDATED.QUEUE"},
					},
					ConsumerOf: []broker.AddressRef{
						{Address: "UPDATED.QUEUE"},
					},
				})
				g.Expect(k8sClient.Update(ctx, createdApp)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying config resource version changed")
			var updatedConfigRV string
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, brokerCrd)).Should(Succeed())
				for _, externalConfig := range brokerCrd.Status.ExternalConfigs {
					if externalConfig.Name == AppPropertiesSecretName(brokerCrd.Name) {
						updatedConfigRV = externalConfig.ResourceVersion
					}
				}
				if verbose {
					fmt.Printf("Config RV changed: %s -> %s\n", initialConfigRV, updatedConfigRV)
				}
				g.Expect(updatedConfigRV).ShouldNot(BeEmpty())
				g.Expect(updatedConfigRV).ShouldNot(Equal(initialConfigRV))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying updated configuration is in secret")
			secretName := AppPropertiesSecretName(serviceName)
			secret := &corev1.Secret{}
			secretKey := types.NamespacedName{Name: secretName, Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, secretKey, secret)).Should(Succeed())
				// Verify both capabilities are present

				addressSettingsKey := AppIdentityPrefixed(createdApp, "capabilities.properties")
				data := string(secret.Data[addressSettingsKey])
				if verbose {
					fmt.Printf("Address settings for app: %s\n", data)
				}
				// Should contain configuration for both queues
				g.Expect(data).Should(ContainSubstring("INITIAL.QUEUE"))
				g.Expect(data).Should(ContainSubstring("UPDATED.QUEUE"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleanup")
			Expect(k8sClient.Delete(ctx, createdApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
		})
	})
})
