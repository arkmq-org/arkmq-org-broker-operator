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
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
)

var _ = Describe("NetworkPolicy port discovery integration", func() {

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

	Context("NetworkPolicy port discovery", func() {

		It("should discover ports from NetworkPolicy and assign to apps", func() {

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
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"tier": "backend", "env": "test", "networkpolicy-test": "true"},
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

			By("waiting for service to be ready and port discovery to complete")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())

				// Service should be deployed
				deployedCondition := meta.FindStatusCondition(createdCrd.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCondition).NotTo(BeNil())
				g.Expect(deployedCondition.Status).To(Equal(metav1.ConditionTrue))

				// Should have discovered default port pool (no NetworkPolicy yet)
				g.Expect(createdCrd.Status.AvailablePorts).NotTo(BeNil())
				g.Expect(createdCrd.Status.AvailablePorts.Source).To(Equal("Default"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("creating NetworkPolicy with specific allowed ports")
			// Get the StatefulSet to find pod labels for NetworkPolicy selector
			brokerCR := &broker.Broker{}
			brokerKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, brokerKey, brokerCR)).Should(Succeed())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// Use the standard labels that the broker pods will have
			podLabels := map[string]string{
				common.LabelAppKubernetesInstance:  serviceName,
				common.LabelAppKubernetesComponent: "broker-service",
				common.LabelAppKubernetesManagedBy: "arkmq-org-broker-operator",
				common.LabelBrokerService:          serviceName,
				common.LabelBrokerPeerIndex:        "0",
			}

			allowedPorts := []int32{61716, 61717, 61718}
			networkPolicy := &networkingv1.NetworkPolicy{
				TypeMeta: metav1.TypeMeta{
					Kind:       "NetworkPolicy",
					APIVersion: "networking.k8s.io/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName + "-ingress-policy",
					Namespace: defaultNamespace,
				},
				Spec: networkingv1.NetworkPolicySpec{
					PodSelector: metav1.LabelSelector{
						MatchLabels: podLabels,
					},
					PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeIngress},
					Ingress: []networkingv1.NetworkPolicyIngressRule{
						{
							Ports: []networkingv1.NetworkPolicyPort{
								{
									Protocol: func() *corev1.Protocol { p := corev1.ProtocolTCP; return &p }(),
									Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: allowedPorts[0]},
								},
								{
									Protocol: func() *corev1.Protocol { p := corev1.ProtocolTCP; return &p }(),
									Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: allowedPorts[1]},
								},
								{
									Protocol: func() *corev1.Protocol { p := corev1.ProtocolTCP; return &p }(),
									Port:     &intstr.IntOrString{Type: intstr.Int, IntVal: allowedPorts[2]},
								},
							},
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, networkPolicy)).Should(Succeed())

			By("waiting for service to discover NetworkPolicy ports")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())

				// Should have switched to NetworkPolicy source
				g.Expect(createdCrd.Status.AvailablePorts).NotTo(BeNil())
				g.Expect(createdCrd.Status.AvailablePorts.Source).To(Equal("NetworkPolicy"))

				// Should have discovered the specific ports
				g.Expect(createdCrd.Status.AvailablePorts.Ports).To(HaveLen(3))
				g.Expect(createdCrd.Status.AvailablePorts.Ports).To(ConsistOf(allowedPorts))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("creating first app - should get first available port")
			app1Name := "netpol-app1"
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

			app1CertName := app1.Name + common.AppCertSecretSuffix
			InstallCert(app1CertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = app1CertName
				candidate.Spec.CommonName = app1.Name
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			Expect(k8sClient.Create(ctx, &app1)).Should(Succeed())

			app1Key := types.NamespacedName{Name: app1.Name, Namespace: app1.Namespace}
			createdApp1 := &broker.BrokerApp{}

			By("waiting for app1 to be assigned a port from NetworkPolicy pool")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app1Key, createdApp1)).Should(Succeed())

				// Should be bound to service
				g.Expect(createdApp1.Status.Service).NotTo(BeNil())
				g.Expect(createdApp1.Status.Service.Name).To(Equal(serviceName))

				// Should have assigned port from NetworkPolicy pool
				g.Expect(createdApp1.Status.Service.AssignedPort).NotTo(Equal(int32(UnassignedPort)))
				g.Expect(allowedPorts).To(ContainElement(createdApp1.Status.Service.AssignedPort))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			app1AssignedPort := createdApp1.Status.Service.AssignedPort
			By(fmt.Sprintf("app1 assigned port %d from NetworkPolicy pool", app1AssignedPort))

			By("creating second app - should get different port")
			app2Name := "netpol-app2"
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
						MatchLabels: map[string]string{"tier": "backend"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "APP2.TOPIC"}},
						},
					},
				},
			}

			app2CertName := app2.Name + common.AppCertSecretSuffix
			InstallCert(app2CertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = app2CertName
				candidate.Spec.CommonName = app2.Name
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			Expect(k8sClient.Create(ctx, &app2)).Should(Succeed())

			app2Key := types.NamespacedName{Name: app2.Name, Namespace: app2.Namespace}
			createdApp2 := &broker.BrokerApp{}

			By("waiting for app2 to be assigned a different port")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app2Key, createdApp2)).Should(Succeed())

				g.Expect(createdApp2.Status.Service).NotTo(BeNil())
				g.Expect(createdApp2.Status.Service.AssignedPort).NotTo(Equal(int32(UnassignedPort)))
				g.Expect(allowedPorts).To(ContainElement(createdApp2.Status.Service.AssignedPort))

				// Should be different from app1's port
				g.Expect(createdApp2.Status.Service.AssignedPort).NotTo(Equal(app1AssignedPort))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			app2AssignedPort := createdApp2.Status.Service.AssignedPort
			By(fmt.Sprintf("app2 assigned port %d (different from app1)", app2AssignedPort))

			By("verifying both apps are provisioned on the service")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())

				// Both apps should be in ProvisionedApps list
				g.Expect(createdCrd.Status.ProvisionedApps).To(HaveLen(2))

				app1Identity := fmt.Sprintf("%s-%s", app1.Namespace, app1.Name)
				app2Identity := fmt.Sprintf("%s-%s", app2.Namespace, app2.Name)
				g.Expect(createdCrd.Status.ProvisionedApps).To(ContainElement(app1Identity))
				g.Expect(createdCrd.Status.ProvisionedApps).To(ContainElement(app2Identity))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("creating third app - should get the last available port")
			app3Name := "netpol-app3"
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
						MatchLabels: map[string]string{"tier": "backend"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{{Address: "APP3.QUEUE"}},
						},
					},
				},
			}

			app3CertName := app3.Name + common.AppCertSecretSuffix
			InstallCert(app3CertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = app3CertName
				candidate.Spec.CommonName = app3.Name
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			Expect(k8sClient.Create(ctx, &app3)).Should(Succeed())

			app3Key := types.NamespacedName{Name: app3.Name, Namespace: app3.Namespace}
			createdApp3 := &broker.BrokerApp{}

			By("waiting for app3 to be assigned the last port")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app3Key, createdApp3)).Should(Succeed())

				g.Expect(createdApp3.Status.Service).NotTo(BeNil())
				g.Expect(createdApp3.Status.Service.AssignedPort).NotTo(Equal(int32(UnassignedPort)))
				g.Expect(allowedPorts).To(ContainElement(createdApp3.Status.Service.AssignedPort))

				// Should be different from both app1 and app2
				g.Expect(createdApp3.Status.Service.AssignedPort).NotTo(Equal(app1AssignedPort))
				g.Expect(createdApp3.Status.Service.AssignedPort).NotTo(Equal(app2AssignedPort))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying all three NetworkPolicy ports are now allocated")
			usedPorts := []int32{app1AssignedPort, app2AssignedPort, createdApp3.Status.Service.AssignedPort}
			Expect(usedPorts).To(ConsistOf(allowedPorts))

			By("creating fourth app - should fail due to port pool exhaustion")
			app4Name := "netpol-app4"
			app4 := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      app4Name,
					Namespace: defaultNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"tier": "backend"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "APP4.QUEUE"}},
						},
					},
				},
			}

			app4CertName := app4.Name + common.AppCertSecretSuffix
			InstallCert(app4CertName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = app4CertName
				candidate.Spec.CommonName = app4.Name
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			Expect(k8sClient.Create(ctx, &app4)).Should(Succeed())

			app4Key := types.NamespacedName{Name: app4.Name, Namespace: app4.Namespace}
			createdApp4 := &broker.BrokerApp{}

			By("waiting for app4 to fail with PortPoolExhausted condition")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, app4Key, createdApp4)).Should(Succeed())

				// Should not be bound to service (no ports available)
				deployedCondition := meta.FindStatusCondition(createdApp4.Status.Conditions, broker.DeployedConditionType)
				g.Expect(deployedCondition).NotTo(BeNil())
				g.Expect(deployedCondition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(deployedCondition.Reason).To(Equal(broker.DeployedConditionNoServiceCapacityReason))
				g.Expect(deployedCondition.Message).To(ContainSubstring("port"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("test completed successfully - NetworkPolicy port discovery and allocation working")

			// Cleanup
			By("cleaning up test resources")
			Expect(k8sClient.Delete(ctx, &app4)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &app3)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &app2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &app1)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, networkPolicy)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
		})
	})
})
