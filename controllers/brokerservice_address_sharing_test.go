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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
)

var _ = Describe("broker-service address sharing scenarios", func() {

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("Phase 2: Same-Namespace Sharing", func() {

		It("Scenario 1: should allow apps in same namespace to share via addressRef", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
			service := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels: map[string]string{
						"test": "address-sharing",
					},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			createdService := &broker.BrokerService{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, createdService)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			ownerAppName := NextSpecResourceName()

			By("creating owner app that declares and uses 'orders' address")
			ownerApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      ownerAppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "address-sharing",
						},
					},
					SharedAddresses: []broker.AddressType{{Address: "orders"}},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{Address: "orders"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &ownerApp)).Should(Succeed())

			consumerAppName := NextSpecResourceName()

			By("creating consumer app that references 'orders' from owner")
			consumerApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      consumerAppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "address-sharing",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:      "orders",
									AppNamespace: defaultNamespace,
									AppName:      ownerAppName,
								},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &consumerApp)).Should(Succeed())

			By("verifying both apps are provisioned")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, createdService)).Should(Succeed())
				if verbose {
					fmt.Printf("Service ProvisionedApps: %v\n", createdService.Status.ProvisionedApps)
				}
				g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(2))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(ownerAppName)))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(consumerAppName)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
		})

		It("Scenario 8: should reject addressRef to non-existent app", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
			service := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels: map[string]string{
						"test": "address-sharing",
					},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			appName := NextSpecResourceName()

			By("creating app that references non-existent owner")
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
						MatchLabels: map[string]string{
							"test": "address-sharing",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:      "orders",
									AppNamespace: defaultNamespace,
									AppName:      "does-not-exist",
								},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			createdApp := &broker.BrokerApp{}
			By("verifying app is Valid but not Deployed (dependency not satisfied)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: defaultNamespace}, createdApp)).Should(Succeed())
				if verbose {
					fmt.Printf("App conditions: %v\n", createdApp.Status.Conditions)
				}
				// Spec is well-formed, so Valid=True
				validCondition := meta.FindStatusCondition(createdApp.Status.Conditions, broker.ValidConditionType)
				g.Expect(validCondition).ShouldNot(BeNil())
				g.Expect(validCondition.Status).Should(Equal(metav1.ConditionTrue))

				// But cannot be deployed due to missing dependency
				deployedCondition := meta.FindStatusCondition(createdApp.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCondition).ShouldNot(BeNil())
				g.Expect(deployedCondition.Status).Should(Equal(metav1.ConditionFalse))
				// No service binding since dependency not satisfied
				g.Expect(createdApp.Status.Service).Should(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &app)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
		})

		It("Scenario 5: should allow app to declare addresses without capabilities, lifecycle", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
			service := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels: map[string]string{
						"test": "address-sharing",
					},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			registryAppName := NextSpecResourceName()

			By("creating address registry app (no capabilities, just declares addresses)")
			registryApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      registryAppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "address-sharing",
						},
					},
					SharedAddresses: []broker.AddressType{{Address: "events"}, {Address: "commands"}, {Address: "queries"}},
					// No capabilities - just owns the addresses
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &registryApp)).Should(Succeed())

			consumerAppName := NextSpecResourceName()

			By("creating consumer app that references 'events' from registry")
			consumerApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      consumerAppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "address-sharing",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:      "events",
									AppNamespace: defaultNamespace,
									AppName:      registryAppName,
								},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &consumerApp)).Should(Succeed())

			createdService := &broker.BrokerService{}
			By("verifying both apps are provisioned")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, createdService)).Should(Succeed())
				if verbose {
					fmt.Printf("Service ProvisionedApps: %v\n", createdService.Status.ProvisionedApps)
				}
				g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(2))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(registryAppName)))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(consumerAppName)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &registryApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
		})

		It("Scenario 9: should reject addressRef when target app doesn't declare that address", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
			service := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels: map[string]string{
						"test": "address-sharing",
					},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			ownerAppName := NextSpecResourceName()

			By("creating owner app that only declares 'events'")
			ownerApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      ownerAppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "address-sharing",
						},
					},
					Addresses: []broker.AddressType{{Address: "events"}},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &ownerApp)).Should(Succeed())

			consumerAppName := NextSpecResourceName()

			By("creating consumer app that tries to reference 'orders' (not declared)")
			consumerApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      consumerAppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "address-sharing",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:      "orders", // Not in owner's spec.addresses
									AppNamespace: defaultNamespace,
									AppName:      ownerAppName,
								},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &consumerApp)).Should(Succeed())

			createdConsumer := &broker.BrokerApp{}
			By("verifying consumer app is Valid but not Deployed (referenced address not declared)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: consumerAppName, Namespace: defaultNamespace}, createdConsumer)).Should(Succeed())
				if verbose {
					fmt.Printf("Consumer app conditions: %v\n", createdConsumer.Status.Conditions)
				}
				// Spec is well-formed, so Valid=True
				validCondition := meta.FindStatusCondition(createdConsumer.Status.Conditions, broker.ValidConditionType)
				g.Expect(validCondition).ShouldNot(BeNil())
				g.Expect(validCondition.Status).Should(Equal(metav1.ConditionTrue))

				// But cannot be deployed due to address not being declared by owner
				deployedCondition := meta.FindStatusCondition(createdConsumer.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCondition).ShouldNot(BeNil())
				g.Expect(deployedCondition.Status).Should(Equal(metav1.ConditionFalse))
				// No service binding since dependency not satisfied
				g.Expect(createdConsumer.Status.Service).Should(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
		})
	})

	Context("Phase 3: Clash Detection", func() {

		It("Scenario 4: should reject second app that declares same direct address", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
			service := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels: map[string]string{
						"test": "address-sharing",
					},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			app1Name := NextSpecResourceName()

			By("creating app1 that declares 'orders' directly")
			app1 := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      app1Name,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "address-sharing",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{Address: "orders"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app1)).Should(Succeed())

			createdService := &broker.BrokerService{}
			By("verifying app1 is provisioned (first wins)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, createdService)).Should(Succeed())
				if verbose {
					fmt.Printf("Service ProvisionedApps: %v\n", createdService.Status.ProvisionedApps)
				}
				g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(1))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(app1Name)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			app2Name := NextSpecResourceName()

			By("creating app2 that also declares 'orders' directly (clash!)")
			app2 := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      app2Name,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "address-sharing",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{Address: "orders"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app2)).Should(Succeed())

			createdApp2 := &broker.BrokerApp{}
			By("verifying app2 is Valid but cannot be Deployed due to address clash")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app2Name, Namespace: defaultNamespace}, createdApp2)).Should(Succeed())
				if verbose {
					fmt.Printf("App2 conditions: %v\n", createdApp2.Status.Conditions)
				}

				// Spec is well-formed, so Valid=True
				validCondition := meta.FindStatusCondition(createdApp2.Status.Conditions, broker.ValidConditionType)
				g.Expect(validCondition).ShouldNot(BeNil())
				g.Expect(validCondition.Status).Should(Equal(metav1.ConditionTrue))

				// But cannot be deployed due to address clash
				deployedCondition := meta.FindStatusCondition(createdApp2.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCondition).ShouldNot(BeNil())
				g.Expect(deployedCondition.Status).Should(Equal(metav1.ConditionFalse))
				g.Expect(deployedCondition.Message).Should(ContainSubstring("already declared"))
				g.Expect(deployedCondition.Message).Should(ContainSubstring("orders"))
				g.Expect(deployedCondition.Message).Should(ContainSubstring(app1Name))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying app2 does not bind to service")
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app2Name, Namespace: defaultNamespace}, createdApp2)).Should(Succeed())
				g.Expect(createdApp2.Status.Service).Should(BeNil())
			}, duration, interval).Should(Succeed())

			By("verifying only app1 is in ProvisionedApps")
			g := NewWithT(GinkgoT())
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, createdService)).Should(Succeed())
			g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(1))
			g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(app1Name)))

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &app2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &app1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
		})
	})

	Context("Phase 4: Cross-Namespace Support", func() {

		It("Scenario 2: should allow cross-namespace sharing when CEL permits both namespaces", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("ensuring other namespace exists")
			otherNs := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: otherNamespace,
				},
			}
			err := k8sClient.Create(ctx, &otherNs)
			if err != nil && !errors.IsAlreadyExists(err) {
				Fail(fmt.Sprintf("Failed to create other namespace: %v", err))
			}

			By("creating BrokerService with CEL allowing both namespaces")
			service := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels: map[string]string{
						"test": "cross-namespace",
					},
				},
				Spec: broker.BrokerServiceSpec{
					AppSelectorExpression: fmt.Sprintf(`app.metadata.namespace in ["%s", "%s"]`, defaultNamespace, otherNamespace),
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			ownerAppName := NextSpecResourceName()

			By("creating owner app in default namespace")
			ownerApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      ownerAppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "cross-namespace",
						},
					},
					SharedAddresses: []broker.AddressType{{Address: "shared-events"}},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{Address: "shared-events"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &ownerApp)).Should(Succeed())

			consumerAppName := NextSpecResourceName()

			By("creating consumer app in other namespace that references owner")
			consumerApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      consumerAppName,
					Namespace: otherNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "cross-namespace",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:      "shared-events",
									AppNamespace: defaultNamespace,
									AppName:      ownerAppName,
								},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &consumerApp)).Should(Succeed())

			createdService := &broker.BrokerService{}
			By("verifying both apps are provisioned")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, createdService)).Should(Succeed())
				if verbose {
					fmt.Printf("Service ProvisionedApps: %v\n", createdService.Status.ProvisionedApps)
				}
				g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(2))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(ownerAppName)))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(consumerAppName)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
		})
	})

	Context("Phase 6: SharedAddresses Validation", func() {

		It("Scenario 10: should reject reference to private address (in Addresses, not SharedAddresses)", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
			service := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels: map[string]string{
						"test": "private-address",
					},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			ownerAppName := NextSpecResourceName()

			By("creating owner app with private address (Addresses only, not in SharedAddresses)")
			ownerApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      ownerAppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "private-address",
						},
					},
					Addresses: []broker.AddressType{{Address: "private-data"}},
					// Note: NOT in SharedAddresses - this is a private address
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{Address: "private-data"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &ownerApp)).Should(Succeed())

			consumerAppName := NextSpecResourceName()

			By("creating consumer app that tries to reference private address")
			consumerApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      consumerAppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "private-address",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:      "private-data",
									AppNamespace: defaultNamespace,
									AppName:      ownerAppName,
								},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &consumerApp)).Should(Succeed())

			createdConsumer := &broker.BrokerApp{}
			By("verifying consumer app is rejected (address not in SharedAddresses)")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: consumerAppName, Namespace: defaultNamespace}, createdConsumer)).Should(Succeed())
				if verbose {
					fmt.Printf("Consumer app conditions: %v\n", createdConsumer.Status.Conditions)
				}
				validCondition := meta.FindStatusCondition(createdConsumer.Status.Conditions, broker.ValidConditionType)
				g.Expect(validCondition).ShouldNot(BeNil())
				g.Expect(validCondition.Status).Should(Equal(metav1.ConditionTrue))

				deployedCondition := meta.FindStatusCondition(createdConsumer.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCondition).ShouldNot(BeNil())
				g.Expect(deployedCondition.Status).Should(Equal(metav1.ConditionFalse))
				g.Expect(deployedCondition.Message).Should(ContainSubstring("does not share address"))
				g.Expect(createdConsumer.Status.Service).Should(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
		})

		It("Scenario 11: should allow app with SharedAddresses only (no Addresses)", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
			service := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels: map[string]string{
						"test": "shared-only",
					},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			ownerAppName := NextSpecResourceName()

			By("creating owner app with SharedAddresses only (no Addresses)")
			ownerApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      ownerAppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "shared-only",
						},
					},
					// Note: no Addresses field, only SharedAddresses
					SharedAddresses: []broker.AddressType{{Address: "public-api"}},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{Address: "public-api"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &ownerApp)).Should(Succeed())

			consumerAppName := NextSpecResourceName()

			By("creating consumer app that references SharedAddresses-only owner")
			consumerApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      consumerAppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "shared-only",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:      "public-api",
									AppNamespace: defaultNamespace,
									AppName:      ownerAppName,
								},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &consumerApp)).Should(Succeed())

			createdService := &broker.BrokerService{}
			By("verifying both apps are provisioned")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, createdService)).Should(Succeed())
				if verbose {
					fmt.Printf("Service ProvisionedApps: %v\n", createdService.Status.ProvisionedApps)
				}
				g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(2))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(ownerAppName)))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(consumerAppName)))
			}, existingClusterVerySlowTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
		})

		It("Scenario 12: should reject clash between two SharedAddresses", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
			service := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels: map[string]string{
						"test": "shared-clash",
					},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			app1Name := NextSpecResourceName()

			By("creating app1 with SharedAddresses")
			app1 := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      app1Name,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "shared-clash",
						},
					},
					SharedAddresses: []broker.AddressType{{Address: "api"}},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{Address: "api"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app1)).Should(Succeed())

			createdService := &broker.BrokerService{}
			By("verifying app1 is provisioned")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, createdService)).Should(Succeed())
				g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(1))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(app1Name)))
			}, existingClusterVerySlowTimeout, existingClusterInterval).Should(Succeed())

			app2Name := NextSpecResourceName()

			By("creating app2 that also declares 'api' in SharedAddresses (clash!)")
			app2 := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      app2Name,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "shared-clash",
						},
					},
					SharedAddresses: []broker.AddressType{{Address: "api"}},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{Address: "api"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app2)).Should(Succeed())

			createdApp2 := &broker.BrokerApp{}
			By("verifying app2 is rejected due to address clash")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app2Name, Namespace: defaultNamespace}, createdApp2)).Should(Succeed())
				if verbose {
					fmt.Printf("App2 conditions: %v\n", createdApp2.Status.Conditions)
				}

				validCondition := meta.FindStatusCondition(createdApp2.Status.Conditions, broker.ValidConditionType)
				g.Expect(validCondition).ShouldNot(BeNil())
				g.Expect(validCondition.Status).Should(Equal(metav1.ConditionTrue))

				deployedCondition := meta.FindStatusCondition(createdApp2.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCondition).ShouldNot(BeNil())
				g.Expect(deployedCondition.Status).Should(Equal(metav1.ConditionFalse))
				g.Expect(deployedCondition.Message).Should(ContainSubstring("already declared"))
				g.Expect(deployedCondition.Message).Should(ContainSubstring("api"))
				g.Expect(createdApp2.Status.Service).Should(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &app2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &app1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
		})

		It("Scenario 13: should reject clash between Addresses and SharedAddresses", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
			service := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels: map[string]string{
						"test": "mixed-clash",
					},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			app1Name := NextSpecResourceName()

			By("creating app1 with address in Addresses (private)")
			app1 := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      app1Name,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "mixed-clash",
						},
					},
					Addresses: []broker.AddressType{{Address: "data"}},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{Address: "data"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app1)).Should(Succeed())

			createdService := &broker.BrokerService{}
			By("verifying app1 is provisioned")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, createdService)).Should(Succeed())
				g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(1))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(app1Name)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			app2Name := NextSpecResourceName()

			By("creating app2 that declares same address in SharedAddresses (clash!)")
			app2 := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      app2Name,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "mixed-clash",
						},
					},
					SharedAddresses: []broker.AddressType{{Address: "data"}},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{Address: "data"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app2)).Should(Succeed())

			createdApp2 := &broker.BrokerApp{}
			By("verifying app2 is rejected due to address clash")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app2Name, Namespace: defaultNamespace}, createdApp2)).Should(Succeed())
				if verbose {
					fmt.Printf("App2 conditions: %v\n", createdApp2.Status.Conditions)
				}

				validCondition := meta.FindStatusCondition(createdApp2.Status.Conditions, broker.ValidConditionType)
				g.Expect(validCondition).ShouldNot(BeNil())
				g.Expect(validCondition.Status).Should(Equal(metav1.ConditionTrue))

				deployedCondition := meta.FindStatusCondition(createdApp2.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCondition).ShouldNot(BeNil())
				g.Expect(deployedCondition.Status).Should(Equal(metav1.ConditionFalse))
				g.Expect(deployedCondition.Message).Should(ContainSubstring("already declared"))
				g.Expect(deployedCondition.Message).Should(ContainSubstring("data"))
				g.Expect(createdApp2.Status.Service).Should(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &app2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &app1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
		})
	})
})
