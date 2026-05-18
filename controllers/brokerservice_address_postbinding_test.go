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

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
)

var _ = Describe("broker-service address post-binding validation", func() {

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

	Context("Post-Binding Address Validation", func() {

		It("Scenario 1: should detect when referenced app is deleted", func() {

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
						"test": "post-binding",
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

			By("creating owner app that shares 'orders' address")
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
							"test": "post-binding",
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

			By("creating consumer app that references owner's 'orders'")
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
							"test": "post-binding",
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

			By("waiting for both apps to be deployed")
			Eventually(func(g Gomega) {
				createdOwner := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ownerAppName, Namespace: defaultNamespace}, createdOwner)).Should(Succeed())
				deployedCond := meta.FindStatusCondition(createdOwner.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCond).ShouldNot(BeNil())
				g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionTrue))
				g.Expect(createdOwner.Status.Service).ShouldNot(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				createdConsumer := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: consumerAppName, Namespace: defaultNamespace}, createdConsumer)).Should(Succeed())
				deployedCond := meta.FindStatusCondition(createdConsumer.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCond).ShouldNot(BeNil())
				g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionTrue))
				g.Expect(createdConsumer.Status.Service).ShouldNot(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying both apps are provisioned on the service")
			Eventually(func(g Gomega) {
				updatedService := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, updatedService)).Should(Succeed())
				g.Expect(updatedService.Status.ProvisionedApps).Should(HaveLen(2))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deleting the owner app")
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())

			By("waiting for owner app to be deleted")
			Eventually(func(g Gomega) {
				deletedOwner := &broker.BrokerApp{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: ownerAppName, Namespace: defaultNamespace}, deletedOwner)
				g.Expect(err).Should(HaveOccurred())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("EXPECTED: consumer app should reconcile and unbind")
			Eventually(func(g Gomega) {
				updatedConsumer := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: consumerAppName, Namespace: defaultNamespace}, updatedConsumer)).Should(Succeed())

				// App should see it's rejected and clear its binding
				g.Expect(updatedConsumer.Status.Service).Should(BeNil())

				// Deployed condition should be false
				deployedCond := meta.FindStatusCondition(updatedConsumer.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCond).ShouldNot(BeNil())
				g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionFalse))
				g.Expect(deployedCond.Message).Should(ContainSubstring("addressRef"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("EXPECTED: consumer app should not be provisioned")
			Eventually(func(g Gomega) {
				updatedService := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, updatedService)).Should(Succeed())

				// Consumer should not be in ProvisionedApps
				g.Expect(updatedService.Status.ProvisionedApps).ShouldNot(ContainElement(ContainSubstring(consumerAppName)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
			UninstallCert(consumerCertName, defaultNamespace)
			UninstallCert(ownerCertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})

		It("Scenario 2: should detect when sharedAddresses is modified to remove referenced address", func() {

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
						"test": "post-binding",
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

			ownerAppName := NextSpecResourceName() + "-owner"
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

			By("creating owner app that shares 'orders' and 'shipments'")
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
							"test": "post-binding",
						},
					},
					SharedAddresses: []broker.AddressType{
						{Address: "orders"},
						{Address: "shipments"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{Address: "orders"},
								{Address: "shipments"},
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

			consumerAppName := NextSpecResourceName() + "-ref"
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

			By("creating consumer app that references owner's 'orders'")
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
							"test": "post-binding",
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

			By("waiting for both apps to be deployed")
			Eventually(func(g Gomega) {
				createdOwner := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ownerAppName, Namespace: defaultNamespace}, createdOwner)).Should(Succeed())
				deployedCond := meta.FindStatusCondition(createdOwner.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCond).ShouldNot(BeNil())
				g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionTrue))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				createdConsumer := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: consumerAppName, Namespace: defaultNamespace}, createdConsumer)).Should(Succeed())
				deployedCond := meta.FindStatusCondition(createdConsumer.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCond).ShouldNot(BeNil())
				g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionTrue))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying both apps are provisioned")
			Eventually(func(g Gomega) {
				updatedService := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, updatedService)).Should(Succeed())
				if verbose {
					fmt.Printf("service STATUS: %v\n", updatedService.Status)
				}
				g.Expect(updatedService.Status.ProvisionedApps).Should(HaveLen(2))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("updating owner app to remove 'orders' from sharedAddresses")
			Eventually(func(g Gomega) {
				updatedOwner := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ownerAppName, Namespace: defaultNamespace}, updatedOwner)).Should(Succeed())

				// Remove 'orders' from sharedAddresses, keep only 'shipments'
				updatedOwner.Spec.SharedAddresses = []broker.AddressType{
					{Address: "shipments"},
				}
				// Still produce to orders, but don't share it
				updatedOwner.Spec.Addresses = []broker.AddressType{
					{Address: "orders"},
				}

				g.Expect(k8sClient.Update(ctx, updatedOwner)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("EXPECTED: consumer app should reconcile and unbind (non-shared address)")
			Eventually(func(g Gomega) {
				updatedService := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, updatedService)).Should(Succeed())

				// Consumer should not be in ProvisionedApps
				g.Expect(updatedService.Status.ProvisionedApps).ShouldNot(ContainElement(ContainSubstring(consumerAppName)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
			UninstallCert(consumerCertName, defaultNamespace)
			UninstallCert(ownerCertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})

		It("Scenario 3: should detect routing type conflict after binding", func() {

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
						"test": "post-binding",
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

			ownerAppName := NextSpecResourceName() + "-ow"
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

			By("creating owner app sharing 'events' as anycast (no subscriptions)")
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
							"test": "post-binding",
						},
					},
					SharedAddresses: []broker.AddressType{
						{Address: "events"}, // anycast - no subscriptions
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{Address: "events"}, // anycast
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

			consumerAppName := NextSpecResourceName() + "-ref"
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

			By("creating consumer app that uses 'events' as anycast")
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
							"test": "post-binding",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:      "events",
									AppNamespace: defaultNamespace,
									AppName:      ownerAppName,
									// Match owner: anycast (no subscriptions)
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

			By("waiting for both apps to be deployed")
			Eventually(func(g Gomega) {
				createdOwner := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ownerAppName, Namespace: defaultNamespace}, createdOwner)).Should(Succeed())

				if verbose {
					fmt.Printf("owner STATUS: %v\n", createdOwner.Status)
				}
				deployedCond := meta.FindStatusCondition(createdOwner.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCond).ShouldNot(BeNil())
				g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionTrue))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				createdConsumer := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: consumerAppName, Namespace: defaultNamespace}, createdConsumer)).Should(Succeed())
				if verbose {
					fmt.Printf("consumer STATUS: %v\n", createdConsumer.Status)
				}

				deployedCond := meta.FindStatusCondition(createdConsumer.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCond).ShouldNot(BeNil())
				g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionTrue))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying both apps are provisioned")
			Eventually(func(g Gomega) {
				updatedService := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, updatedService)).Should(Succeed())

				if verbose {
					fmt.Printf("service STATUS: %v\n", updatedService.Status)
				}

				g.Expect(updatedService.Status.ProvisionedApps).Should(HaveLen(2))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("updating owner app to change shared 'events' to multicast (add subscriptions)")
			Eventually(func(g Gomega) {
				updatedOwner := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ownerAppName, Namespace: defaultNamespace}, updatedOwner)).Should(Succeed())

				// Change existing address to multicast by adding subscriptions (implicit pubSub inference)
				updatedOwner.Spec.SharedAddresses[0].Subscriptions = []string{"sub1"}

				// ProducerOf is still with an anycast reference, should be invalid

				g.Expect(k8sClient.Update(ctx, updatedOwner)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("owner app is now invalid, mixed usage, stays deployed")
			Eventually(func(g Gomega) {
				createdOwner := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ownerAppName, Namespace: defaultNamespace}, createdOwner)).Should(Succeed())

				if verbose {
					fmt.Printf("invalid? owner STATUS: %v\n", createdOwner.Status)
				}

				deployedCond := meta.FindStatusCondition(createdOwner.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCond).ShouldNot(BeNil())
				g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionTrue))
				g.Expect(deployedCond.ObservedGeneration).To(BeNumerically(">", 0))
				g.Expect(deployedCond.ObservedGeneration).To(BeNumerically("<", createdOwner.Generation))

				validCond := meta.FindStatusCondition(createdOwner.Status.Conditions, broker.ValidConditionType)
				g.Expect(validCond).ShouldNot(BeNil())
				g.Expect(validCond.Status).Should(Equal(metav1.ConditionFalse))
				g.Expect(validCond.Reason).Should(Equal(broker.ValidConditionAddressTypeError))
				g.Expect(validCond.ObservedGeneration).Should(Equal(createdOwner.Generation))
				g.Expect(createdOwner.Status.ObservedGeneration).Should(Equal(createdOwner.Generation))

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying both apps are still provisioned")
			Eventually(func(g Gomega) {
				updatedService := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, updatedService)).Should(Succeed())

				if verbose {
					fmt.Printf("service STATUS: %v\n", updatedService.Status)
				}

				g.Expect(updatedService.Status.ProvisionedApps).Should(HaveLen(2))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("updating owner app to change 'events' to multicast (add subscriptions) and fix  ")
			Eventually(func(g Gomega) {
				updatedOwner := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ownerAppName, Namespace: defaultNamespace}, updatedOwner)).Should(Succeed())

				// change to valid producer - multicast with no queue names (producers declare intent, don't create queues)
				pubSubTrue := true
				updatedOwner.Spec.Capabilities[0].ProducerOf = []broker.AddressRef{
					{Address: "events", PubSub: &pubSubTrue}, // multicast producer (no queue names)
				}

				g.Expect(k8sClient.Update(ctx, updatedOwner)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("verify owner app is now updated and ok")
			Eventually(func(g Gomega) {
				createdOwner := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ownerAppName, Namespace: defaultNamespace}, createdOwner)).Should(Succeed())

				if verbose {
					fmt.Printf("owner STATUS: %v\n", createdOwner.Status)
				}
				readyCond := meta.FindStatusCondition(createdOwner.Status.Conditions, broker.ReadyConditionType)
				g.Expect(readyCond).ShouldNot(BeNil())
				g.Expect(readyCond.Status).Should(Equal(metav1.ConditionTrue))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("EXPECTED: consumer app should reconcile and unbind")
			Eventually(func(g Gomega) {

				updatedConsumer := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: consumerAppName, Namespace: defaultNamespace}, updatedConsumer)).Should(Succeed())

				if verbose {
					fmt.Printf("consumer STATUS: %v\n", updatedConsumer.Status)
				}

				// Deployed condition should be false
				deployedCond := meta.FindStatusCondition(updatedConsumer.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCond).ShouldNot(BeNil())
				g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionFalse))
				g.Expect(deployedCond.Message).Should(ContainSubstring("addressRef"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying ref app is unbound")
			Eventually(func(g Gomega) {
				updatedService := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, updatedService)).Should(Succeed())

				if verbose {
					fmt.Printf("service STATUS: %v\n", updatedService.Status)
				}

				g.Expect(updatedService.Status.ProvisionedApps).Should(HaveLen(1))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &consumerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &ownerApp)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
			UninstallCert(consumerCertName, defaultNamespace)
			UninstallCert(ownerCertName, defaultNamespace)
			UninstallCert(prometheusCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})

		It("Scenario 4: should detect new address clash introduced post-binding", func() {

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
						"test": "post-binding",
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

			By("creating app1 with address 'orders'")
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
							"test": "post-binding",
						},
					},
					Addresses: []broker.AddressType{
						{Address: "orders"},
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

			By("creating app2 with address 'shipments' (no clash yet)")
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
							"test": "post-binding",
						},
					},
					Addresses: []broker.AddressType{
						{Address: "shipments"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{Address: "shipments"},
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

			By("waiting for both apps to be deployed")
			Eventually(func(g Gomega) {
				createdApp1 := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app1Name, Namespace: defaultNamespace}, createdApp1)).Should(Succeed())
				deployedCond := meta.FindStatusCondition(createdApp1.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCond).ShouldNot(BeNil())
				g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionTrue))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				createdApp2 := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app2Name, Namespace: defaultNamespace}, createdApp2)).Should(Succeed())
				deployedCond := meta.FindStatusCondition(createdApp2.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCond).ShouldNot(BeNil())
				g.Expect(deployedCond.Status).Should(Equal(metav1.ConditionTrue))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying both apps are provisioned")
			Eventually(func(g Gomega) {
				updatedService := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, updatedService)).Should(Succeed())

				if verbose {
					fmt.Printf("service STATUS: %v\n", updatedService.Status)
				}

				g.Expect(updatedService.Status.ProvisionedApps).Should(HaveLen(2))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("updating app2 to add 'orders' to sharedAddresses (introducing clash)")
			Eventually(func(g Gomega) {
				updatedApp2 := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app2Name, Namespace: defaultNamespace}, updatedApp2)).Should(Succeed())

				// Add 'orders' to shared addresses - this clashes with app1's private 'orders'
				updatedApp2.Spec.SharedAddresses = []broker.AddressType{
					{Address: "orders"}, // CLASH!
				}

				g.Expect(k8sClient.Update(ctx, updatedApp2)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			By("EXPECTED: app2 should reconcile and unbind - clash. harsh for an update but we can't have a partially valid deployed app")
			Eventually(func(g Gomega) {
				updatedService := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, updatedService)).Should(Succeed())

				// app2 should not be in ProvisionedApps
				g.Expect(updatedService.Status.ProvisionedApps).ShouldNot(ContainElement(ContainSubstring(app2Name)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

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
})
