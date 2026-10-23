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

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
)

var _ = Describe("BrokerService NetworkPolicy for child broker", Label("network-policy"), func() {

	BeforeEach(func() {
		Expect(os.Setenv("BROKER_NETWORK_POLICY", "true")).To(Succeed())
		BeforeEachSpec()

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			if !CertManagerInstalled() {
				Expect(InstallCertManager()).To(Succeed())
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
		Expect(os.Unsetenv("BROKER_NETWORK_POLICY")).To(Succeed())
		AfterEachSpec()
	})

	It("creates a NetworkPolicy with restricted ports for the child broker", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" || !isOpenshift {
			Skip("requires OpenShift cluster with NP-enforcing CNI")
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()

		sharedOperandCertName := serviceName + "-" + common.DefaultOperandCertSecretName
		By("installing broker cert")
		InstallCert(sharedOperandCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
			candidate.Spec.SecretName = sharedOperandCertName
			candidate.Spec.CommonName = serviceName
			candidate.Spec.DNSNames = []string{
				serviceName,
				fmt.Sprintf("%s.%s", serviceName, defaultNamespace),
				fmt.Sprintf("%s.%s.svc.%s", serviceName, defaultNamespace, common.GetClusterDomain()),
				common.ClusterDNSWildCard(serviceName, defaultNamespace),
			}
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

		By("deploying a BrokerService")
		svc := brokerv1beta2.BrokerService{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerService",
				APIVersion: brokerv1beta2.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: defaultNamespace,
				Labels:    map[string]string{"env": "netpol-test"},
			},
			Spec: brokerv1beta2.BrokerServiceSpec{},
		}
		Expect(k8sClient.Create(ctx, &svc)).Should(Succeed())

		serviceKey := types.NamespacedName{Name: svc.Name, Namespace: svc.Namespace}
		createdSvc := &brokerv1beta2.BrokerService{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, serviceKey, createdSvc)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(createdSvc.Status.Conditions, brokerv1beta2.ReadyConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("verifying the NetworkPolicy exists with only restricted ports before any app is bound")
		netpolKey := types.NamespacedName{
			Name:      serviceName + "-netpol",
			Namespace: defaultNamespace,
		}
		netpolObject := netv1.NetworkPolicy{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, netpolKey, &netpolObject)).Should(Succeed())
			ports := collectIngressPorts(&netpolObject)
			g.Expect(ports).To(ContainElement(int32(8778)), "Jolokia port")
			g.Expect(ports).To(ContainElement(int32(8888)), "Prometheus port")
			g.Expect(ports).To(HaveLen(2), "only restricted ports before app binding")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("deploying a BrokerApp that binds to the service")
		app := brokerv1beta2.BrokerApp{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerApp",
				APIVersion: brokerv1beta2.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "netpol-app",
				Namespace: defaultNamespace,
			},
			Spec: brokerv1beta2.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"env": "netpol-test"},
				},
				Capabilities: []brokerv1beta2.AppCapabilityType{
					{
						ProducerOf: []brokerv1beta2.AddressRef{{Address: "TEST.QUEUE"}},
						ConsumerOf: []brokerv1beta2.AddressRef{{Address: "TEST.QUEUE"}},
					},
				},
			},
		}

		appCertName := app.Name + common.AppCertSecretSuffix
		By("installing app client cert")
		InstallCert(appCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
			candidate.Spec.SecretName = appCertName
			candidate.Spec.CommonName = app.Name
			candidate.Spec.Subject.Organizations = nil
			candidate.Spec.Subject.OrganizationalUnits = []string{defaultNamespace}
			candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
				Name: caIssuer.Name,
				Kind: "ClusterIssuer",
			}
		})

		Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

		appKey := types.NamespacedName{Name: app.Name, Namespace: app.Namespace}
		createdApp := &brokerv1beta2.BrokerApp{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(createdApp.Status.Conditions, brokerv1beta2.ReadyConditionType)).Should(BeTrue())
			g.Expect(createdApp.Status.Service).NotTo(BeNil())
			g.Expect(createdApp.Status.Service.AssignedPort).NotTo(BeZero())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		appPort := createdApp.Status.Service.AssignedPort

		By("verifying the NetworkPolicy now includes the BrokerApp assigned port via ResourceTemplate")
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, netpolKey, &netpolObject)).Should(Succeed())
			ports := collectIngressPorts(&netpolObject)
			g.Expect(ports).To(ContainElement(int32(8778)), "Jolokia port")
			g.Expect(ports).To(ContainElement(int32(8888)), "Prometheus port")
			g.Expect(ports).To(ContainElement(appPort), "BrokerApp assigned port")
			g.Expect(ports).To(HaveLen(3), "restricted ports + app port")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("deleting the BrokerApp and verifying the port is removed from the NetworkPolicy")
		Expect(k8sClient.Delete(ctx, &app)).Should(Succeed())

		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, netpolKey, &netpolObject)).Should(Succeed())
			ports := collectIngressPorts(&netpolObject)
			g.Expect(ports).NotTo(ContainElement(appPort), "app port removed after unbind")
			g.Expect(ports).To(HaveLen(2), "only restricted ports after app removal")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		CleanResource(&svc, svc.Name, defaultNamespace)
	})
})
