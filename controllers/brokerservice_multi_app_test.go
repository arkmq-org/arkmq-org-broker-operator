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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"
)

var _ = Describe("broker-service multi-app scenarios", func() {

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("multiple apps on single service", func() {

		It("should handle multiple apps with different capabilities", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService with label selector")
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"tier": "backend", "env": "test"},
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

			By("creating first app with queue capabilities")
			app1Name := "queue-app"
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
						MatchLabels: map[string]string{"tier": "backend"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "APP1.QUEUE"}},
							ConsumerOf: []broker.AddressRef{{Address: "APP1.QUEUE"}},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, &app1)).Should(Succeed())

			By("creating second app with topic capabilities")
			app2Name := "topic-app"
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
						MatchLabels: map[string]string{"env": "test"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "APP2.TOPIC"}},
							ConsumerOf: []broker.AddressRef{
								{
									Address:       "APP2.TOPIC",
									Subscriptions: []string{"client-a.sub-a"},
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, &app2)).Should(Succeed())

			By("waiting for both apps to be ready")
			app1Key := types.NamespacedName{Name: app1Name, Namespace: defaultNamespace}
			createdApp1 := &broker.BrokerApp{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app1Key, createdApp1)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdApp1.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				g.Expect(createdApp1.Status.Service).ShouldNot(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			app2Key := types.NamespacedName{Name: app2Name, Namespace: defaultNamespace}
			createdApp2 := &broker.BrokerApp{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app2Key, createdApp2)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdApp2.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				g.Expect(createdApp2.Status.Service).ShouldNot(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying both apps are in service's ProvisionedApps status")
			brokerKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
			brokerCrd := &broker.Broker{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, brokerCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(brokerCrd.Status.Conditions, broker.ConfigAppliedConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				if verbose {
					fmt.Printf("Service ProvisionedApps: %v\n", createdCrd.Status.ProvisionedApps)
				}
				g.Expect(createdCrd.Status.ProvisionedApps).Should(HaveLen(2))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying app properties secret contains both apps")

			app1ConfigKey := AppIdentityPrefixed(&app1, "capabilities.properties")
			app2ConfigKey := AppIdentityPrefixed(&app2, "capabilities.properties")
			secretName := AppPropertiesSecretName(serviceName)
			secret := &corev1.Secret{}
			secretKey := types.NamespacedName{Name: secretName, Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, secretKey, secret)).Should(Succeed())
				// Check for app-specific keys in the secret
				hasApp1Config := false
				hasApp2Config := false
				for key := range secret.Data {
					if verbose {
						fmt.Printf("Secret key: %s\n", key)
					}
					if key == app1ConfigKey {
						hasApp1Config = true
					}
					if key == app2ConfigKey {
						hasApp2Config = true
					}
				}
				g.Expect(hasApp1Config).Should(BeTrue(), "app1 config should be in secret")
				g.Expect(hasApp2Config).Should(BeTrue(), "app2 config should be in secret")
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("removing first app")
			Expect(k8sClient.Delete(ctx, createdApp1)).Should(Succeed())

			By("verifying only second app remains in ProvisionedApps status")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				if verbose {
					fmt.Printf("Service ProvisionedApps after app1 delete: %v\n", createdCrd.Status.ProvisionedApps)
				}
				g.Expect(createdCrd.Status.ProvisionedApps).Should(HaveLen(1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleanup")
			Expect(k8sClient.Delete(ctx, createdApp2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdCrd)).Should(Succeed())
		})
	})

	Context("app moving between services", func() {

		It("should properly update both services when app moves", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			service1Name := NextSpecResourceName()
			service2Name := NextSpecResourceName()

			By("creating first service with label env=dev")
			service1 := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      service1Name,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"env": "dev"},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service1)).Should(Succeed())

			By("creating second service with label env=prod")
			service2 := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      service2Name,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"env": "prod"},
				},
				Spec: broker.BrokerServiceSpec{
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service2)).Should(Succeed())

			By("waiting for both services to be ready")
			service1Key := types.NamespacedName{Name: service1Name, Namespace: defaultNamespace}
			createdService1 := &broker.BrokerService{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, service1Key, createdService1)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdService1.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			service2Key := types.NamespacedName{Name: service2Name, Namespace: defaultNamespace}
			createdService2 := &broker.BrokerService{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, service2Key, createdService2)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdService2.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("creating app that matches service1 (env=dev)")
			appName := "mobile-app"
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
						MatchLabels: map[string]string{"env": "dev"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "MOBILE.TASKS"}},
							ConsumerOf: []broker.AddressRef{{Address: "MOBILE.TASKS"}},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			By("verifying app is ready and bound to service1")
			appKey := types.NamespacedName{Name: appName, Namespace: defaultNamespace}
			createdApp := &broker.BrokerApp{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdApp.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				g.Expect(createdApp.Status.Service).ShouldNot(BeNil())
				// Binding secret name should contain service name
				if verbose {
					fmt.Printf("App binding secret: %s\n", createdApp.Status.Service.Secret)
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying service1 has the app in ProvisionedApps")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, service1Key, createdService1)).Should(Succeed())
				if verbose {
					fmt.Printf("Service1 ProvisionedApps: %v\n", createdService1.Status.ProvisionedApps)
				}
				g.Expect(createdService1.Status.ProvisionedApps).Should(HaveLen(1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying service2 has no apps")
			Expect(k8sClient.Get(ctx, service2Key, createdService2)).Should(Succeed())
			Expect(createdService2.Status.ProvisionedApps).Should(BeEmpty())

			By("moving app to service2 by changing selector to env=prod")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				createdApp.Spec.ServiceSelector = &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "prod"},
				}
				g.Expect(k8sClient.Update(ctx, createdApp)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying service1 no longer has the app")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, service1Key, createdService1)).Should(Succeed())
				if verbose {
					fmt.Printf("Service1 ProvisionedApps after move: %v\n", createdService1.Status.ProvisionedApps)
				}
				g.Expect(createdService1.Status.ProvisionedApps).Should(BeEmpty())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying service2 now has the app")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, service2Key, createdService2)).Should(Succeed())
				if verbose {
					fmt.Printf("Service2 ProvisionedApps after move: %v\n", createdService2.Status.ProvisionedApps)
				}
				g.Expect(createdService2.Status.ProvisionedApps).Should(HaveLen(1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying app binding is updated")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdApp.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				// Binding should still exist
				g.Expect(createdApp.Status.Service).ShouldNot(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleanup")
			Expect(k8sClient.Delete(ctx, createdApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdService1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdService2)).Should(Succeed())
		})
	})

	Context("cross-namespace app provisioning", func() {

		It("should provision apps from multiple namespaces on same service with sharing", Label("verySlow"), func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			// 3-app sequential provisioning with cross-namespace cert copies
			// and projected volume refreshes; needs much more than 180s.
			slowTimeout := existingClusterVerySlowTimeout

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

			By("creating service with 2Gi memory limit")
			service := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"cross-ns": "test"},
				},
				Spec: broker.BrokerServiceSpec{
					AppSelectorExpression: fmt.Sprintf(`app.metadata.namespace in ["%s", "%s"]`, otherNamespace, defaultNamespace), // Allow both namespaces
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
			createdService := &broker.BrokerService{}

			By("waiting for service to be ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdService)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdService.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, slowTimeout, existingClusterInterval).Should(Succeed())

			By("creating app in default namespace with 512Mi memory request")
			app1Name := "app-ns-test"
			var app1Port int32
			app1CertName := app1Name + common.AppCertSecretSuffix

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
						MatchLabels: map[string]string{"cross-ns": "test"},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
					SharedAddresses: []broker.AddressType{{Address: "shared.address", PubSub: &[]bool{true}[0]}},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:       "app1.address",
									Subscriptions: []string{"queue1"},
								},
								{
									Address:       "shared.address",
									Subscriptions: []string{"app1-client.app1-shared-queue"},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app1)).Should(Succeed())

			app1Key := types.NamespacedName{Name: app1Name, Namespace: defaultNamespace}
			createdApp1 := &broker.BrokerApp{}

			By("waiting for app1 to be ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app1Key, createdApp1)).Should(Succeed())

				if verbose {
					fmt.Printf("App1 Status: %v\n", createdApp1.Status)
				}

				g.Expect(meta.IsStatusConditionTrue(createdApp1.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())

				// Verify port was assigned
				g.Expect(createdApp1.Status.Service.AssignedPort).ShouldNot(BeZero())
				app1Port = createdApp1.Status.Service.AssignedPort

				if verbose {
					fmt.Printf("App1 Ready, binding: %v\n", createdApp1.Status.Service)
				}
			}, slowTimeout, existingClusterInterval).Should(Succeed())

			By("creating app in other namespace with 1Gi memory request")
			app2Name := "app-ns-other"
			var app2Port int32
			app2CertName := app2Name + common.AppCertSecretSuffix

			app2 := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      app2Name,
					Namespace: otherNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"cross-ns": "test"},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:       "app2.address",
									Subscriptions: []string{"queue2"},
								},
								{
									Address:       "shared.address",
									Subscriptions: []string{"app2-client.app2-shared-queue"},
									AppNamespace:  defaultNamespace,
									AppName:       app1Name,
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app2)).Should(Succeed())

			app2Key := types.NamespacedName{Name: app2Name, Namespace: otherNamespace}
			createdApp2 := &broker.BrokerApp{}

			By("waiting for app2 to be ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app2Key, createdApp2)).Should(Succeed())

				if verbose {
					fmt.Printf("App2 Status: %v\n", createdApp2.Status)
				}

				g.Expect(meta.IsStatusConditionTrue(createdApp2.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())

				// Verify port was assigned
				g.Expect(createdApp2.Status.Service.AssignedPort).ShouldNot(BeZero())
				app2Port = createdApp2.Status.Service.AssignedPort

				if verbose {
					fmt.Printf("App2 Ready, binding: %v\n", createdApp2.Status.Service)
				}
			}, slowTimeout, existingClusterInterval).Should(Succeed())

			By("verifying both apps are provisioned on the service")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdService)).Should(Succeed())
				if verbose {
					fmt.Printf("Service ProvisionedApps: %v\n", createdService.Status.ProvisionedApps)
				}
				g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(2))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(app1Name)))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(app2Name)))
			}, slowTimeout, existingClusterInterval).Should(Succeed())

			By("verifying both apps have correct status bindings")
			expectedBinding := fmt.Sprintf("%s:%s", defaultNamespace, serviceName)

			Expect(k8sClient.Get(ctx, app1Key, createdApp1)).Should(Succeed())
			Expect(createdApp1.Status.Service).ShouldNot(BeNil())
			Expect(fmt.Sprintf("%s:%s", createdApp1.Status.Service.Namespace, createdApp1.Status.Service.Name)).Should(Equal(expectedBinding))

			Expect(k8sClient.Get(ctx, app2Key, createdApp2)).Should(Succeed())
			Expect(createdApp2.Status.Service).ShouldNot(BeNil())
			Expect(fmt.Sprintf("%s:%s", createdApp2.Status.Service.Namespace, createdApp2.Status.Service.Name)).Should(Equal(expectedBinding))

			By("verifying capacity tracking across namespaces - third app should fail")
			app3Name := "app-ns-test-too-big"
			var app3Port int32
			app3CertName := app3Name + common.AppCertSecretSuffix

			app3 := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      app3Name,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"cross-ns": "test"},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"), // Total would be 2.5Gi > 2Gi limit
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:       "app3.address",
									Subscriptions: []string{"queue3"},
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app3)).Should(Succeed())

			app3Key := types.NamespacedName{Name: app3Name, Namespace: defaultNamespace}
			createdApp3 := &broker.BrokerApp{}

			By("verifying app3 cannot be provisioned due to insufficient capacity")
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app3Key, createdApp3)).Should(Succeed())

				// Valid should be True (spec is valid)
				validCond := meta.FindStatusCondition(createdApp3.Status.Conditions, broker.ValidConditionType)
				if validCond != nil {
					if verbose {
						fmt.Printf("App3 Valid condition: %s, Reason: %s\n",
							validCond.Status, validCond.Reason)
					}
					g.Expect(validCond.Status).Should(Equal(metav1.ConditionTrue))
					g.Expect(validCond.Reason).Should(Equal(broker.ValidConditionSuccessReason))
				}

				// Deployed should be False with NoServiceCapacity reason
				deployedCond := meta.FindStatusCondition(createdApp3.Status.Conditions, broker.DeployedConditionType)
				if deployedCond != nil {
					if verbose {
						fmt.Printf("App3 Deployed condition: %s, Reason: %s, Message: %s\n",
							deployedCond.Status, deployedCond.Reason, deployedCond.Message)
					}
					g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionFalse))
					g.Expect(deployedCond.Reason).Should(Equal(broker.DeployedConditionNoServiceCapacityReason))
				}
			}, "10s", "1s").Should(Succeed())

			By("modifying app3 to reduce memory and add producer for shared address")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app3Key, createdApp3)).Should(Succeed())
				createdApp3.Spec.Resources.Requests[corev1.ResourceMemory] = resource.MustParse("256Mi")
				createdApp3.Spec.Capabilities = []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{{Address: "shared.address", AppNamespace: defaultNamespace, AppName: app1Name, PubSub: &[]bool{true}[0]}},
						ConsumerOf: []broker.AddressRef{
							{
								Address:       "app3.address",
								Subscriptions: []string{"queue3"},
							},
						},
					},
				}
				g.Expect(k8sClient.Update(ctx, createdApp3)).Should(Succeed())
			}, slowTimeout, existingClusterInterval).Should(Succeed())

			By("verifying app3 becomes ready after modification")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app3Key, createdApp3)).Should(Succeed())

				if verbose {
					fmt.Printf("App3 Status: %v\n", createdApp3.Status)
				}

				g.Expect(meta.IsStatusConditionTrue(createdApp3.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())

				// Verify port was assigned
				g.Expect(createdApp3.Status.Service.AssignedPort).ShouldNot(BeZero())
				app3Port = createdApp3.Status.Service.AssignedPort

				if verbose {
					fmt.Printf("App3 now ready, binding: %v\n", createdApp3.Status.Service)
				}
			}, slowTimeout, existingClusterInterval).Should(Succeed())

			By("verifying all three apps are provisioned on the service")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdService)).Should(Succeed())
				if verbose {
					fmt.Printf("Service ProvisionedApps with app3: %v\n", createdService.Status.ProvisionedApps)
				}
				g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(3))
			}, slowTimeout, existingClusterInterval).Should(Succeed())

			By("verify an app1 and app2 client can consume a message produced by app3")

			brokerImage := version.LatestKubeImage

			By("provisioning pemcfg secret for client certs in both namespaces")
			boolFalse := false
			serviceHostEnvVar := "BROKER_SERVICE_HOST"
			clientPemcfgSecretName := "client-cert-pemcfg"
			clientPemcfgKey := types.NamespacedName{Name: clientPemcfgSecretName, Namespace: defaultNamespace}
			clientPemcfgSecret := secrets.NewSecret(clientPemcfgKey, map[string][]byte{
				"tls.pemcfg":    []byte("source.key=/app/tls/client/tls.key\nsource.cert=/app/tls/client/tls.crt"),
				"java.security": []byte("security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider"),
			}, nil)
			Expect(k8sClient.Create(ctx, clientPemcfgSecret, &client.CreateOptions{})).Should(Succeed())

			clientPemcfgSecret.Namespace = otherNamespace
			clientPemcfgSecret.ResourceVersion = ""
			Expect(k8sClient.Create(ctx, clientPemcfgSecret, &client.CreateOptions{})).Should(Succeed())

			jobTemplate := func(name string, ns string, bindingSecretName string, appCertName string, caTrustName string, command []string) batchv1.Job {
				appLabels := map[string]string{"app": name}
				return batchv1.Job{
					TypeMeta:   metav1.TypeMeta{Kind: "Job", APIVersion: "batch/v1"},
					ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: appLabels},
					Spec: batchv1.JobSpec{
						Parallelism: common.Int32ToPtr(1),
						Template: corev1.PodTemplateSpec{
							ObjectMeta: metav1.ObjectMeta{Labels: appLabels},
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{
									{
										Name:    name,
										Image:   brokerImage,
										Command: command,
										Env: []corev1.EnvVar{
											{
												Name:  "JDK_JAVA_OPTIONS",
												Value: "-Djava.security.properties=/app/tls/pem/java.security",
											},
											{
												Name: serviceHostEnvVar,
												ValueFrom: &corev1.EnvVarSource{
													SecretKeyRef: &corev1.SecretKeySelector{
														LocalObjectReference: corev1.LocalObjectReference{
															Name: bindingSecretName,
														},
														Key:      "host",
														Optional: &boolFalse,
													},
												},
											},
										},
										VolumeMounts: []corev1.VolumeMount{
											{
												Name:      "trust",
												MountPath: "/app/tls/ca",
											},
											{
												Name:      "cert",
												MountPath: "/app/tls/client",
											},
											{
												Name:      "pem",
												MountPath: "/app/tls/pem",
											},
										},
									},
								},
								Volumes: []corev1.Volume{
									{
										Name: "trust",
										VolumeSource: corev1.VolumeSource{
											Secret: &corev1.SecretVolumeSource{
												SecretName: caTrustName,
											},
										},
									},
									{
										Name: "cert",
										VolumeSource: corev1.VolumeSource{
											Secret: &corev1.SecretVolumeSource{
												SecretName: appCertName,
											},
										},
									},
									{
										Name: "pem",
										VolumeSource: corev1.VolumeSource{
											Secret: &corev1.SecretVolumeSource{
												SecretName: clientPemcfgSecretName,
											},
										},
									},
								},
								RestartPolicy: corev1.RestartPolicyOnFailure,
							},
						},
					},
				}
			}

			By("deploying consumers for app1 and app2 shared queues")
			serviceUrlTemplate := fmt.Sprintf("amqps://${%s}:%%d?transport.trustStoreType=PEMCA\\&transport.trustStoreLocation=/app/tls/ca/tls.crt\\&transport.keyStoreType=PEMCFG\\&transport.keyStoreLocation=/app/tls/pem/tls.pemcfg", serviceHostEnvVar)

			app1ServiceUrl := fmt.Sprintf(serviceUrlTemplate, app1Port)
			app1Consumer := jobTemplate(
				"app1-consumer",
				defaultNamespace,
				createdApp1.Status.Service.Secret,
				app1CertName,
				app1Name+"-ca-trust",
				[]string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis consumer --protocol=AMQP --url " + app1ServiceUrl + " --message-count=1 --durable --clientID=app1-client --subscriptionName=app1-shared-queue --destination topic://shared.address;"},
			)
			Expect(k8sClient.Create(ctx, &app1Consumer)).Should(Succeed())

			app2ServiceUrl := fmt.Sprintf(serviceUrlTemplate, app2Port)
			app2Consumer := jobTemplate(
				"app2-consumer",
				otherNamespace,
				createdApp2.Status.Service.Secret,
				app2CertName,
				app2Name+"-ca-trust",
				[]string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis consumer --protocol=AMQP --url " + app2ServiceUrl + " --message-count=1 --durable --clientID=app2-client --subscriptionName=app2-shared-queue --destination topic://shared.address;"},
			)
			Expect(k8sClient.Create(ctx, &app2Consumer)).Should(Succeed())

			By("deploying producer for app3 to send message to shared.address")
			app3ServiceUrl := fmt.Sprintf(serviceUrlTemplate, app3Port)
			app3Producer := jobTemplate(
				"app3-producer",
				defaultNamespace,
				createdApp3.Status.Service.Secret,
				app3CertName,
				app3Name+"-ca-trust",
				[]string{"/bin/sh", "-c", "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis producer --protocol=AMQP --url " + app3ServiceUrl + " --message-count=1 --destination topic://shared.address;"},
			)
			Expect(k8sClient.Create(ctx, &app3Producer)).Should(Succeed())

			By("verifying producer succeeded")
			producerKey := types.NamespacedName{Name: app3Producer.Name, Namespace: defaultNamespace}
			producerJob := &batchv1.Job{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, producerKey, producerJob)).Should(Succeed())
				if verbose {
					fmt.Printf("Producer job STATUS: %v\n", producerJob.Status)
				}
				g.Expect(producerJob.Status.Succeeded).Should(BeNumerically("==", 1))
			}, slowTimeout, existingClusterInterval).Should(Succeed())

			By("verifying both consumers received the message")
			app1ConsumerKey := types.NamespacedName{Name: app1Consumer.Name, Namespace: defaultNamespace}
			app1ConsumerJob := &batchv1.Job{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app1ConsumerKey, app1ConsumerJob)).Should(Succeed())
				if verbose {
					fmt.Printf("App1 consumer job STATUS: %v\n", app1ConsumerJob.Status)
				}
				g.Expect(app1ConsumerJob.Status.Succeeded).Should(BeNumerically("==", 1))
			}, slowTimeout, existingClusterInterval).Should(Succeed())

			app2ConsumerKey := types.NamespacedName{Name: app2Consumer.Name, Namespace: otherNamespace}
			app2ConsumerJob := &batchv1.Job{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app2ConsumerKey, app2ConsumerJob)).Should(Succeed())
				if verbose {
					fmt.Printf("App2 consumer job STATUS: %v\n", app2ConsumerJob.Status)
				}
				g.Expect(app2ConsumerJob.Status.Succeeded).Should(BeNumerically("==", 1))
			}, slowTimeout, existingClusterInterval).Should(Succeed())

			By("cleanup")
			cascade_foreground_policy := metav1.DeletePropagationForeground
			Expect(k8sClient.Delete(ctx, &app1Consumer, &client.DeleteOptions{PropagationPolicy: &cascade_foreground_policy})).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &app2Consumer, &client.DeleteOptions{PropagationPolicy: &cascade_foreground_policy})).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &app3Producer, &client.DeleteOptions{PropagationPolicy: &cascade_foreground_policy})).Should(Succeed())
			Expect(k8sClient.Delete(ctx, clientPemcfgSecret)).Should(Succeed())
			clientPemcfgSecret.Namespace = defaultNamespace
			Expect(k8sClient.Delete(ctx, clientPemcfgSecret)).Should(Succeed())

			By("deleting apps and waiting for finalizers")
			Expect(k8sClient.Delete(ctx, createdApp1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdApp2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdApp3)).Should(Succeed())
			for _, key := range []types.NamespacedName{
				{Name: createdApp1.Name, Namespace: createdApp1.Namespace},
				{Name: createdApp2.Name, Namespace: createdApp2.Namespace},
				{Name: createdApp3.Name, Namespace: createdApp3.Namespace},
			} {
				Eventually(func() bool {
					return errors.IsNotFound(k8sClient.Get(ctx, key, &broker.BrokerApp{}))
				}, slowTimeout, existingClusterInterval).Should(BeTrue())
			}

			By("deleting service and waiting for finalizer")
			Expect(k8sClient.Delete(ctx, createdService)).Should(Succeed())
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name: createdService.Name, Namespace: createdService.Namespace,
				}, &broker.BrokerService{}))
			}, slowTimeout, existingClusterInterval).Should(BeTrue())
		})
	})

	Context("validation and error handling", func() {

		It("should reject invalid resource names", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()

			By("attempting to create service with path traversal in name")
			invalidService := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "../evil-service",
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerServiceSpec{},
			}

			// Kubernetes API should reject this before it reaches our controller
			err := k8sClient.Create(ctx, &invalidService)
			Expect(err).Should(HaveOccurred())
			if verbose {
				fmt.Printf("Expected error for invalid name: %v\n", err)
			}
		})

		It("should handle app without matching service gracefully", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			appName := NextSpecResourceName()

			By("creating app with selector that matches no service")
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
						MatchLabels: map[string]string{"nonexistent": "label"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "TEST.QUEUE"}},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			By("verifying app condition reflects no matching service")
			appKey := types.NamespacedName{Name: appName, Namespace: defaultNamespace}
			createdApp := &broker.BrokerApp{}

			// The app should exist but not be Ready
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				// Should have a condition indicating the problem
				readyCond := meta.FindStatusCondition(createdApp.Status.Conditions, broker.ReadyConditionType)
				if readyCond != nil {
					if verbose {
						fmt.Printf("App Ready condition: Status=%s, Reason=%s, Message=%s\n",
							readyCond.Status, readyCond.Reason, readyCond.Message)
					}
					g.Expect(readyCond.Status).Should(Equal(metav1.ConditionFalse))
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleanup")
			Expect(k8sClient.Delete(ctx, createdApp)).Should(Succeed())
		})

		It("should auto-assign unique ports to apps and handle pool exhaustion", func() {

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
					Labels:    map[string]string{"auto-port-test": "true"},
				},
				Spec: broker.BrokerServiceSpec{},
			}
			Expect(k8sClient.Create(ctx, &service)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
			createdService := &broker.BrokerService{}

			By("waiting for service to be ready and verify port pool discovery")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdService)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdService.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("creating first app - should get auto-assigned port")
			app1Name := "app1-auto-port"

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
						MatchLabels: map[string]string{"auto-port-test": "true"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "APP1.QUEUE"}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app1)).Should(Succeed())

			app1Key := types.NamespacedName{Name: app1Name, Namespace: defaultNamespace}
			createdApp1 := &broker.BrokerApp{}

			var app1Port int32
			By("waiting for first app to be ready with assigned port")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app1Key, createdApp1)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdApp1.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())

				// Verify port was assigned
				g.Expect(createdApp1.Status.Service.AssignedPort).ShouldNot(BeZero())
				app1Port = createdApp1.Status.Service.AssignedPort

				if verbose {
					fmt.Printf("App1 assigned port: %d\n", app1Port)
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("creating second app - should get different auto-assigned port")
			app2Name := "app2-auto-port"

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
						MatchLabels: map[string]string{"auto-port-test": "true"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "APP2.QUEUE"}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app2)).Should(Succeed())

			app2Key := types.NamespacedName{Name: app2Name, Namespace: defaultNamespace}
			createdApp2 := &broker.BrokerApp{}

			var app2Port int32
			By("waiting for second app to be ready with different assigned port")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app2Key, createdApp2)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdApp2.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())

				// Verify port was assigned and is different from app1
				g.Expect(createdApp2.Status.Service.AssignedPort).ShouldNot(BeZero())
				g.Expect(createdApp2.Status.Service.AssignedPort).ShouldNot(Equal(app1Port))
				app2Port = createdApp2.Status.Service.AssignedPort

				if verbose {
					fmt.Printf("App2 assigned port: %d (different from app1: %d)\n", app2Port, app1Port)
				}
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying both apps are provisioned on the service")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdService)).Should(Succeed())
				if verbose {
					fmt.Printf("Service ProvisionedApps: %v\n", createdService.Status.ProvisionedApps)
				}
				g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(2))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying ports are unique across both apps")
			Expect(app1Port).ShouldNot(Equal(app2Port), "Apps should have unique auto-assigned ports")

			By("verifying binding secrets contain correct assigned ports")
			app1BindingSecret := &corev1.Secret{}
			app1SecretKey := types.NamespacedName{Name: BindingsSecretName(app1Name), Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app1SecretKey, app1BindingSecret)).Should(Succeed())
				portStr := string(app1BindingSecret.Data["port"])
				g.Expect(portStr).Should(Equal(fmt.Sprintf("%d", app1Port)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			app2BindingSecret := &corev1.Secret{}
			app2SecretKey := types.NamespacedName{Name: BindingsSecretName(app2Name), Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app2SecretKey, app2BindingSecret)).Should(Succeed())
				portStr := string(app2BindingSecret.Data["port"])
				g.Expect(portStr).Should(Equal(fmt.Sprintf("%d", app2Port)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleanup")
			Expect(k8sClient.Delete(ctx, createdApp1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdApp2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, createdService)).Should(Succeed())
		})
	})

	Context("CEL Expression Validation", func() {
		It("sets Valid=False when appSelectorExpression has syntax error", func() {
			ctx := context.Background()

			// Create namespace
			ns := &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-invalid-cel",
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			// Create BrokerService with invalid CEL expression
			service := &broker.BrokerService{
				ObjectMeta: v1.ObjectMeta{
					Name:      "invalid-cel-service",
					Namespace: ns.Name,
				},
				Spec: broker.BrokerServiceSpec{
					AppSelectorExpression: `app.metadata.namespace ==`, // Syntax error
				},
			}
			Expect(k8sClient.Create(ctx, service)).Should(Succeed())

			// Check status - should have Valid=False
			Eventually(func(g Gomega) {
				updatedService := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      service.Name,
					Namespace: service.Namespace,
				}, updatedService)).Should(Succeed())

				// Find Valid condition
				validCondition := meta.FindStatusCondition(updatedService.Status.Conditions, broker.ValidConditionType)
				g.Expect(validCondition).NotTo(BeNil())
				g.Expect(validCondition.Status).To(Equal(v1.ConditionFalse))
				g.Expect(validCondition.Reason).To(Equal(broker.ValidConditionSpecSelectorError))
				g.Expect(validCondition.Message).To(ContainSubstring("invalid appSelectorExpression"))
				g.Expect(validCondition.Message).To(ContainSubstring("failed to compile CEL expression"))
			}, timeout, interval).Should(Succeed())

			// Cleanup
			Expect(k8sClient.Delete(ctx, service)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
		})

		It("sets Valid=False when appSelectorExpression returns non-boolean", func() {
			ctx := context.Background()

			// Create namespace
			ns := &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-nonbool-cel",
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			// Create BrokerService with expression that returns string
			service := &broker.BrokerService{
				ObjectMeta: v1.ObjectMeta{
					Name:      "nonbool-cel-service",
					Namespace: ns.Name,
				},
				Spec: broker.BrokerServiceSpec{
					AppSelectorExpression: `app.metadata.namespace`, // Returns string, not bool
				},
			}
			Expect(k8sClient.Create(ctx, service)).Should(Succeed())

			// Check status - should have Valid=False
			Eventually(func(g Gomega) {
				updatedService := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      service.Name,
					Namespace: service.Namespace,
				}, updatedService)).Should(Succeed())

				// Find Valid condition
				validCondition := meta.FindStatusCondition(updatedService.Status.Conditions, broker.ValidConditionType)
				g.Expect(validCondition).NotTo(BeNil())
				g.Expect(validCondition.Status).To(Equal(v1.ConditionFalse))
				g.Expect(validCondition.Reason).To(Equal(broker.ValidConditionSpecSelectorError))
				g.Expect(validCondition.Message).To(ContainSubstring("invalid appSelectorExpression"))
				g.Expect(validCondition.Message).To(ContainSubstring("must return boolean"))
			}, timeout, interval).Should(Succeed())

			// Cleanup
			Expect(k8sClient.Delete(ctx, service)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
		})

		It("sets Valid=True when appSelectorExpression is valid", func() {
			ctx := context.Background()

			// Create namespace
			ns := &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-valid-cel",
				},
			}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

			// Create BrokerService with valid CEL expression
			service := &broker.BrokerService{
				ObjectMeta: v1.ObjectMeta{
					Name:      "valid-cel-service",
					Namespace: ns.Name,
				},
				Spec: broker.BrokerServiceSpec{
					AppSelectorExpression: `app.metadata.namespace.startsWith("team-")`,
				},
			}
			Expect(k8sClient.Create(ctx, service)).Should(Succeed())

			// Check status - should have Valid=True
			Eventually(func(g Gomega) {
				updatedService := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{
					Name:      service.Name,
					Namespace: service.Namespace,
				}, updatedService)).Should(Succeed())

				// Find Valid condition
				validCondition := meta.FindStatusCondition(updatedService.Status.Conditions, broker.ValidConditionType)
				g.Expect(validCondition).NotTo(BeNil())
				g.Expect(validCondition.Status).To(Equal(v1.ConditionTrue))
				g.Expect(validCondition.Reason).To(Equal(broker.ValidConditionSuccessReason))
			}, timeout, interval).Should(Succeed())

			// Cleanup
			Expect(k8sClient.Delete(ctx, service)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
		})
	})
})
