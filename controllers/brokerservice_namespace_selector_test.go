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

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/appselector"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
)

var _ = Describe("broker-service namespace-based CEL selection", func() {

	var installedCertManager bool = false

	BeforeEach(func() {
		// Enable namespace permission for tests
		appselector.SetNamespacePermission(true)

		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			if !CertManagerInstalled() {
				Expect(InstallCertManager()).To(Succeed())
				installedCertManager = true
			}

			rootIssuer = InstallClusteredIssuer(rootIssuerName, nil)

			rootCert = InstallCert(rootCertName, rootCertNamespce, func(candidate *cmv1.Certificate) {
				candidate.Spec.IsCA = true
				candidate.Spec.CommonName = "artemis.root.ca"
				candidate.Spec.SecretName = rootCertSecretName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: rootIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			caIssuer = InstallClusteredIssuer(caIssuerName, func(candidate *cmv1.ClusterIssuer) {
				candidate.Spec.SelfSigned = nil
				candidate.Spec.CA = &cmv1.CAIssuer{
					SecretName: rootCertSecretName,
				}
			})
			InstallCaBundle(common.DefaultOperatorCASecretName, rootCertSecretName, caPemTrustStoreName)

			By("installing operator cert")
			InstallCert(common.DefaultOperatorCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = common.DefaultOperatorCertSecretName
				candidate.Spec.CommonName = "activemq-artemis-operator"
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})
		}
	})

	AfterEach(func() {
		appselector.SetNamespacePermission(false)

		if false && os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			UnInstallCaBundle(common.DefaultOperatorCASecretName)
			UninstallClusteredIssuer(caIssuerName)
			UninstallCert(rootCert.Name, rootCert.Namespace)
			UninstallCert(common.DefaultOperatorCertSecretName, defaultNamespace)
			UninstallClusteredIssuer(rootIssuerName)

			if installedCertManager {
				Expect(UninstallCertManager()).To(Succeed())
				installedCertManager = false
			}
		}
		AfterEachSpec()
	})

	Context("namespace label-based authorization", func() {

		It("should allow apps from authorized namespaces only", Label("verySlow"), func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			// Create test namespaces with different labels
			prodNamespace := "test-" + serviceName + "-prod"
			devNamespace := "test-" + serviceName + "-dev"
			qaNamespace := "test-" + serviceName + "-qa"

			By("creating namespace for production with environment=production label")
			prodNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: prodNamespace,
					Labels: map[string]string{
						"environment": "production",
						"team":        "payments",
					},
				},
			}
			Expect(k8sClient.Create(ctx, prodNs)).Should(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, prodNs)
			}()

			By("creating namespace for dev with environment=development label")
			devNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: devNamespace,
					Labels: map[string]string{
						"environment": "development",
						"team":        "payments",
					},
				},
			}
			Expect(k8sClient.Create(ctx, devNs)).Should(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, devNs)
			}()

			By("creating namespace for qa without environment label")
			qaNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: qaNamespace,
					Labels: map[string]string{
						"team": "qa-team",
					},
				},
			}
			Expect(k8sClient.Create(ctx, qaNs)).Should(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, qaNs)
			}()

			// Install certificates for apps in each namespace
			for _, ns := range []string{prodNamespace, devNamespace, qaNamespace} {
				appCertName := "app-cert"
				By(fmt.Sprintf("installing app cert in namespace %s", ns))
				InstallCert(appCertName, ns, func(candidate *cmv1.Certificate) {
					candidate.Spec.SecretName = appCertName
					candidate.Spec.CommonName = fmt.Sprintf("app-%s", ns)
					candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
						Name: caIssuer.Name,
						Kind: "ClusterIssuer",
					}
				})
			}

			sharedOperandCertName := serviceName + "-" + common.DefaultOperandCertSecretName
			By("installing broker cert")
			InstallCert(sharedOperandCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = sharedOperandCertName
				candidate.Spec.CommonName = serviceName
				candidate.Spec.DNSNames = []string{serviceName, fmt.Sprintf("%s.%s", serviceName, defaultNamespace), fmt.Sprintf("%s.%s.svc.%s", serviceName, defaultNamespace, common.GetClusterDomain()), common.ClusterDNSWildCard(serviceName, defaultNamespace)}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			prometheusCertName := common.DefaultPrometheusCertSecretName
			By("installing prometheus cert")
			InstallCert(prometheusCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = prometheusCertName
				candidate.Spec.CommonName = "prometheus"
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("creating BrokerService with namespace CEL selector - production only")
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
					// Only allow apps from namespaces labeled environment=production
					AppSelectorExpression: `has(appNamespace.metadata.labels) && appNamespace.metadata.labels["environment"] == "production"`,
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

			// Create app in production namespace (should be accepted)
			By("creating app in production namespace - should be ACCEPTED")
			prodApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prod-app",
					Namespace: prodNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{}, // Match any service
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "PROD.QUEUE"}},
							ConsumerOf: []broker.AddressRef{{Address: "PROD.QUEUE"}},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &prodApp)).Should(Succeed())

			prodAppKey := types.NamespacedName{Name: prodApp.Name, Namespace: prodApp.Namespace}
			createdProdApp := &broker.BrokerApp{}

			By("verifying production app gets provisioned")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, prodAppKey, createdProdApp)).Should(Succeed())

				if verbose {
					fmt.Printf("Status: %v\n", createdProdApp.Status)
				}

				g.Expect(meta.IsStatusConditionTrue(createdProdApp.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())

				// Verify it's bound to our service
				g.Expect(createdProdApp.Status.Service).ShouldNot(BeNil())
				g.Expect(fmt.Sprintf("%s:%s", createdProdApp.Status.Service.Namespace, createdProdApp.Status.Service.Name)).Should(Equal(fmt.Sprintf("%s:%s", defaultNamespace, serviceName)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying service shows the production app as provisioned")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(createdCrd.Status.ProvisionedApps).Should(ContainElement(ContainSubstring("prod-app")))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// Create app in dev namespace (should be rejected)
			By("creating app in development namespace - should be REJECTED")
			devApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dev-app",
					Namespace: devNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{}, // Match any service
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "DEV.QUEUE"}},
							ConsumerOf: []broker.AddressRef{{Address: "DEV.QUEUE"}},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &devApp)).Should(Succeed())

			devAppKey := types.NamespacedName{Name: devApp.Name, Namespace: devApp.Namespace}
			createdDevApp := &broker.BrokerApp{}

			By("verifying development app is REJECTED (doesn't match namespace selector)")
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, devAppKey, createdDevApp)).Should(Succeed())
				// Should have Deployed condition = False with DoesNotMatch reason

				if verbose {
					fmt.Printf("Status: %v\n", createdDevApp.Status)
				}

				deployedCondition := meta.FindStatusCondition(createdDevApp.Status.Conditions, broker.DeployedConditionType)
				if deployedCondition != nil {
					g.Expect(deployedCondition.Status).Should(Equal(metav1.ConditionFalse))
					g.Expect(deployedCondition.Reason).Should(Equal(broker.DeployedConditionNoMatchingServiceReason))
				}
			}, existingClusterConsistentlyTimeout, existingClusterInterval).Should(Succeed())

			By("verifying service does NOT provision the development app")
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(createdCrd.Status.ProvisionedApps).ShouldNot(ContainElement(ContainSubstring("dev-app")))
			}, existingClusterConsistentlyTimeout, existingClusterInterval).Should(Succeed())

			// Create app in QA namespace (should be rejected - no environment label)
			By("creating app in QA namespace - should be REJECTED (missing label)")
			qaApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "qa-app",
					Namespace: qaNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{}, // Match any service
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "QA.QUEUE"}},
							ConsumerOf: []broker.AddressRef{{Address: "QA.QUEUE"}},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &qaApp)).Should(Succeed())

			qaAppKey := types.NamespacedName{Name: qaApp.Name, Namespace: qaApp.Namespace}
			createdQaApp := &broker.BrokerApp{}

			By("verifying QA app is REJECTED (namespace missing environment label)")
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, qaAppKey, createdQaApp)).Should(Succeed())

				if verbose {
					fmt.Printf("Status: %v\n", createdQaApp.Status)
				}

				deployedCondition := meta.FindStatusCondition(createdQaApp.Status.Conditions, broker.DeployedConditionType)
				if deployedCondition != nil {
					g.Expect(deployedCondition.Status).Should(Equal(metav1.ConditionFalse))
					g.Expect(deployedCondition.Reason).Should(Equal(broker.DeployedConditionSelectorEvaluationError))
				}
			}, existingClusterConsistentlyTimeout, existingClusterInterval).Should(Succeed())

			// Clean up
			By("cleaning up apps")
			Expect(k8sClient.Delete(ctx, &prodApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &devApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &qaApp)).Should(Succeed())

			By("cleaning up service")
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
		})

		It("should support team-based multi-tenancy with namespace labels", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			// Create namespaces for different teams
			paymentsNamespace := serviceName + "-payments"
			ordersNamespace := serviceName + "-orders"

			By("creating payments team namespace")
			paymentsNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: paymentsNamespace,
					Labels: map[string]string{
						"team": "payments",
						"tier": "premium",
					},
				},
			}
			Expect(k8sClient.Create(ctx, paymentsNs)).Should(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, paymentsNs)
			}()

			By("creating orders team namespace")
			ordersNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ordersNamespace,
					Labels: map[string]string{
						"team": "orders",
						"tier": "standard",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ordersNs)).Should(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, ordersNs)
			}()

			// Install certificates
			for _, ns := range []string{paymentsNamespace, ordersNamespace} {
				appCertName := "app-cert"
				InstallCert(appCertName, ns, func(candidate *cmv1.Certificate) {
					candidate.Spec.SecretName = appCertName
					candidate.Spec.CommonName = fmt.Sprintf("app-%s", ns)
					candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
						Name: caIssuer.Name,
						Kind: "ClusterIssuer",
					}
				})
			}

			sharedOperandCertName := serviceName + "-" + common.DefaultOperandCertSecretName
			InstallCert(sharedOperandCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = sharedOperandCertName
				candidate.Spec.CommonName = serviceName
				candidate.Spec.DNSNames = []string{serviceName, fmt.Sprintf("%s.%s", serviceName, defaultNamespace), fmt.Sprintf("%s.%s.svc.%s", serviceName, defaultNamespace, common.GetClusterDomain()), common.ClusterDNSWildCard(serviceName, defaultNamespace)}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			prometheusCertName := common.DefaultPrometheusCertSecretName
			InstallCert(prometheusCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = prometheusCertName
				candidate.Spec.CommonName = "prometheus"
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("creating BrokerService for premium tier teams only")
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
					// Only allow apps from premium tier namespaces
					AppSelectorExpression: `has(appNamespace.metadata.labels) && appNamespace.metadata.labels["tier"] == "premium"`,
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

			// Create app in payments namespace (premium tier - should be accepted)
			By("creating app in payments namespace (premium tier) - should be ACCEPTED")
			paymentsApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "payments-app",
					Namespace: paymentsNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "PAYMENTS.QUEUE"}},
							ConsumerOf: []broker.AddressRef{{Address: "PAYMENTS.QUEUE"}},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &paymentsApp)).Should(Succeed())

			paymentsAppKey := types.NamespacedName{Name: paymentsApp.Name, Namespace: paymentsApp.Namespace}
			createdPaymentsApp := &broker.BrokerApp{}

			By("verifying payments app gets provisioned")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, paymentsAppKey, createdPaymentsApp)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdPaymentsApp.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// Create app in orders namespace (standard tier - should be rejected)
			By("creating app in orders namespace (standard tier) - should be REJECTED")
			ordersApp := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "orders-app",
					Namespace: ordersNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "ORDERS.QUEUE"}},
							ConsumerOf: []broker.AddressRef{{Address: "ORDERS.QUEUE"}},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("100Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &ordersApp)).Should(Succeed())

			ordersAppKey := types.NamespacedName{Name: ordersApp.Name, Namespace: ordersApp.Namespace}
			createdOrdersApp := &broker.BrokerApp{}

			By("verifying orders app is REJECTED (not premium tier)")
			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ordersAppKey, createdOrdersApp)).Should(Succeed())
				deployedCondition := meta.FindStatusCondition(createdOrdersApp.Status.Conditions, broker.DeployedConditionType)
				if deployedCondition != nil {
					g.Expect(deployedCondition.Status).Should(Equal(metav1.ConditionFalse))
				}
			}, existingClusterConsistentlyTimeout, existingClusterInterval).Should(Succeed())

			// Verify only payments app is provisioned
			By("verifying only payments app is provisioned on service")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(createdCrd.Status.ProvisionedApps).Should(ContainElement(ContainSubstring("payments-app")))
				g.Expect(createdCrd.Status.ProvisionedApps).ShouldNot(ContainElement(ContainSubstring("orders-app")))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// Clean up
			Expect(k8sClient.Delete(ctx, &paymentsApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ordersApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
		})
	})
})
