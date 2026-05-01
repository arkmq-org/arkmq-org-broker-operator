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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
)

var _ = Describe("broker-service address sharing scenarios", func() {

	var installedCertManager bool = false

	BeforeEach(func() {
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

	Context("Phase 2: Same-Namespace Sharing", func() {

		It("Scenario 1: should allow apps in same namespace to share via addressRef", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

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
			ownerCertName := ownerAppName + "-" + common.DefaultOperandCertSecretName
			By("installing owner app cert")
			InstallCert(ownerCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = ownerCertName
				candidate.Spec.CommonName = ownerAppName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

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
					Addresses: []broker.AddressType{{Address: "orders"}},
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
			consumerCertName := consumerAppName + "-" + common.DefaultOperandCertSecretName
			By("installing consumer app cert")
			InstallCert(consumerCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = consumerCertName
				candidate.Spec.CommonName = consumerAppName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

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
			}, timeout, interval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
			UninstallCert(consumerCertName, defaultNamespace)
			UninstallCert(ownerCertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})

		It("Scenario 8: should reject addressRef to non-existent app", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

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
			appCertName := appName + "-" + common.DefaultOperandCertSecretName
			By("installing app cert")
			InstallCert(appCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = appCertName
				candidate.Spec.CommonName = appName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

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
			}, timeout, interval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &app)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
			UninstallCert(appCertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})

		It("Scenario 5: should allow app to declare addresses without capabilities", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

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
			registryCertName := registryAppName + "-" + common.DefaultOperandCertSecretName
			By("installing registry app cert")
			InstallCert(registryCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = registryCertName
				candidate.Spec.CommonName = registryAppName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

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
					Addresses: []broker.AddressType{{Address: "events"}, {Address: "commands"}, {Address: "queries"}},
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
			consumerCertName := consumerAppName + "-" + common.DefaultOperandCertSecretName
			By("installing consumer app cert")
			InstallCert(consumerCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = consumerCertName
				candidate.Spec.CommonName = consumerAppName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

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
			}, timeout, interval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &registryApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
			UninstallCert(consumerCertName, defaultNamespace)
			UninstallCert(registryCertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})

		It("Scenario 9: should reject addressRef when target app doesn't declare that address", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

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
			ownerCertName := ownerAppName + "-" + common.DefaultOperandCertSecretName
			By("installing owner app cert")
			InstallCert(ownerCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = ownerCertName
				candidate.Spec.CommonName = ownerAppName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

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
			consumerCertName := consumerAppName + "-" + common.DefaultOperandCertSecretName
			By("installing consumer app cert")
			InstallCert(consumerCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = consumerCertName
				candidate.Spec.CommonName = consumerAppName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

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
			}, timeout, interval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
			UninstallCert(consumerCertName, defaultNamespace)
			UninstallCert(ownerCertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})
	})

	Context("Phase 3: Clash Detection", func() {

		It("Scenario 4: should reject second app that declares same direct address", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

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
			app1CertName := app1Name + "-" + common.DefaultOperandCertSecretName
			By("installing app1 cert")
			InstallCert(app1CertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = app1CertName
				candidate.Spec.CommonName = app1Name
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

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
			}, timeout, interval).Should(Succeed())

			app2Name := NextSpecResourceName()
			app2CertName := app2Name + "-" + common.DefaultOperandCertSecretName
			By("installing app2 cert")
			InstallCert(app2CertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = app2CertName
				candidate.Spec.CommonName = app2Name
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

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
			}, timeout, interval).Should(Succeed())

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
			UninstallCert(app2CertName, defaultNamespace)
			UninstallCert(app1CertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
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
			ownerCertName := ownerAppName + "-cert"
			By("installing owner app cert in default namespace")
			InstallCert(ownerCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = ownerCertName
				candidate.Spec.CommonName = ownerAppName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

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
					Addresses: []broker.AddressType{{Address: "shared-events"}},
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
			consumerCertName := consumerAppName + "-cert"
			By("installing consumer app cert in other namespace")
			InstallCert(consumerCertName, otherNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = consumerCertName
				candidate.Spec.CommonName = consumerAppName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

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
			}, timeout, interval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
			UninstallCert(consumerCertName, otherNamespace)
			UninstallCert(ownerCertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})
	})

	Context("Phase 5: FQQN and Mixed Usage", func() {

		It("Scenario 6: should support FQQN in addressRef for topic subscriptions", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

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
						"test": "fqqn-sharing",
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
			ownerCertName := ownerAppName + "-cert"
			By("installing owner app cert")
			InstallCert(ownerCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = ownerCertName
				candidate.Spec.CommonName = ownerAppName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("creating owner app that declares 'events' topic")
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
							"test": "fqqn-sharing",
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

			sub1AppName := NextSpecResourceName()
			sub1CertName := sub1AppName + "-cert"
			By("installing subscriber1 cert")
			InstallCert(sub1CertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = sub1CertName
				candidate.Spec.CommonName = sub1AppName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("creating subscriber1 with FQQN addressRef")
			sub1App := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      sub1AppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "fqqn-sharing",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							SubscriberOf: []broker.AddressRef{
								{
									Address:      "events::sub1-queue",
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
			Expect(k8sClient.Create(ctx, &sub1App)).Should(Succeed())

			sub2AppName := NextSpecResourceName()
			sub2CertName := sub2AppName + "-cert"
			By("installing subscriber2 cert")
			InstallCert(sub2CertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = sub2CertName
				candidate.Spec.CommonName = sub2AppName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("creating subscriber2 with different FQQN queue")
			sub2App := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      sub2AppName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "fqqn-sharing",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							SubscriberOf: []broker.AddressRef{
								{
									Address:      "events::sub2-queue",
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
			Expect(k8sClient.Create(ctx, &sub2App)).Should(Succeed())

			createdService := &broker.BrokerService{}
			By("verifying all three apps are provisioned")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, createdService)).Should(Succeed())
				if verbose {
					fmt.Printf("Service ProvisionedApps: %v\n", createdService.Status.ProvisionedApps)
				}
				g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(3))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(ownerAppName)))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(sub1AppName)))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(sub2AppName)))
			}, timeout, interval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &sub2App)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &sub1App)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
			UninstallCert(sub2CertName, defaultNamespace)
			UninstallCert(sub1CertName, defaultNamespace)
			UninstallCert(ownerCertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})

		It("Scenario 7: should allow mixed ANYCAST and MULTICAST on same base address", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

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
						"test": "mixed-routing",
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
			ownerCertName := ownerAppName + "-cert"
			By("installing owner app cert")
			InstallCert(ownerCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = ownerCertName
				candidate.Spec.CommonName = ownerAppName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("creating owner app that declares 'events' and produces to it (ANYCAST)")
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
							"test": "mixed-routing",
						},
					},
					Addresses: []broker.AddressType{{Address: "events"}},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{Address: "events"}, // Direct reference, ANYCAST
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &ownerApp)).Should(Succeed())

			queueConsumerName := NextSpecResourceName()
			queueCertName := queueConsumerName + "-cert"
			By("installing queue consumer cert")
			InstallCert(queueCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = queueCertName
				candidate.Spec.CommonName = queueConsumerName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("creating queue consumer that consumes from 'events' (ANYCAST)")
			queueConsumer := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      queueConsumerName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "mixed-routing",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:      "events",
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
			Expect(k8sClient.Create(ctx, &queueConsumer)).Should(Succeed())

			topicSubName := NextSpecResourceName()
			topicCertName := topicSubName + "-cert"
			By("installing topic subscriber cert")
			InstallCert(topicCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = topicCertName
				candidate.Spec.CommonName = topicSubName
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("creating topic subscriber that subscribes to 'events::subscription' (MULTICAST)")
			topicSubscriber := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      topicSubName,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "mixed-routing",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							SubscriberOf: []broker.AddressRef{
								{
									Address:      "events::subscription",
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
			Expect(k8sClient.Create(ctx, &topicSubscriber)).Should(Succeed())

			createdService := &broker.BrokerService{}
			By("verifying all three apps are provisioned")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, createdService)).Should(Succeed())
				if verbose {
					fmt.Printf("Service ProvisionedApps: %v\n", createdService.Status.ProvisionedApps)
				}
				g.Expect(createdService.Status.ProvisionedApps).Should(HaveLen(3))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(ownerAppName)))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(queueConsumerName)))
				g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(topicSubName)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &topicSubscriber)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &queueConsumer)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
			UninstallCert(topicCertName, defaultNamespace)
			UninstallCert(queueCertName, defaultNamespace)
			UninstallCert(ownerCertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})
	})
})
