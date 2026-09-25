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

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
)

func assertManagedResourceTrackingLabels(g Gomega, labels map[string]string, crName string, expectedKey string, forbiddenKey string, expectedAppValue string) {
	g.Expect(labels).To(HaveKeyWithValue(expectedKey, crName))
	g.Expect(labels).To(HaveKeyWithValue(selectors.LabelAppKey, expectedAppValue))
	_, hasForbidden := labels[forbiddenKey]
	g.Expect(hasForbidden).To(BeFalse(), "must not use tracking label key %q", forbiddenKey)
}

func installRestrictedBrokerCerts(brokerName string) {
	By("installing operator cert")
	InstallCert(common.DefaultOperatorCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
		candidate.Spec.SecretName = common.DefaultOperatorCertSecretName
		candidate.Spec.CommonName = "arkmq-org-broker-operator"
		candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
			Name: caIssuer.Name,
			Kind: "ClusterIssuer",
		}
	})

	By("installing restricted mtls broker cert")
	InstallCert(common.DefaultOperandCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
		candidate.Spec.SecretName = common.DefaultOperandCertSecretName
		candidate.Spec.CommonName = "arkmq-org-broker-operand"
		candidate.Spec.DNSNames = []string{common.OrdinalFQDNS(brokerName, defaultNamespace, 0)}
		candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
			Name: caIssuer.Name,
			Kind: "ClusterIssuer",
		}
	})

	By("installing prometheus cert")
	InstallCert(common.DefaultPrometheusCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
		candidate.Spec.SecretName = common.DefaultPrometheusCertSecretName
		candidate.Spec.CommonName = "prometheus"
		candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
			Name: caIssuer.Name,
			Kind: "ClusterIssuer",
		}
	})
}

var _ = Describe("broker managed resource labels", Label("broker-label-test"), func() {

	Context("Broker CR", func() {
		BeforeEach(func() {
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
			}
		})

		AfterEach(func() {
			AfterEachSpec()
		})

		It("tags managed resources with the Broker tracking label", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			brokerCr := generateBrokerCRSpec(defaultNamespace)
			installRestrictedBrokerCerts(brokerCr.Name)

			By("deploying a Broker CR")
			Expect(k8sClient.Create(ctx, &brokerCr)).Should(Succeed())

			createdBrokerCr := v1beta2.Broker{}
			Eventually(func() bool {
				return getPersistedVersionedCrd(brokerCr.Name, defaultNamespace, &createdBrokerCr)
			}, timeout, interval).Should(BeTrue())

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
				assertManagedResourceTrackingLabels(g, currentSS.Labels, brokerCr.Name, selectors.LabelBrokerKey, selectors.LabelActiveMQArtemisKey, selectors.LabelAppValueBroker)
				assertManagedResourceTrackingLabels(g, currentSS.Spec.Template.Labels, brokerCr.Name, selectors.LabelBrokerKey, selectors.LabelActiveMQArtemisKey, selectors.LabelAppValueBroker)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying headless Service labels and selector")
			Eventually(func(g Gomega) {
				headlessSvc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, headlessSvcKey, headlessSvc)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, headlessSvc.Labels, brokerCr.Name, selectors.LabelBrokerKey, selectors.LabelActiveMQArtemisKey, selectors.LabelAppValueBroker)
				assertManagedResourceTrackingLabels(g, headlessSvc.Spec.Selector, brokerCr.Name, selectors.LabelBrokerKey, selectors.LabelActiveMQArtemisKey, selectors.LabelAppValueBroker)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying broker properties Secret labels")
			Eventually(func(g Gomega) {
				propsSecret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, propsSecretKey, propsSecret)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, propsSecret.Labels, brokerCr.Name, selectors.LabelBrokerKey, selectors.LabelActiveMQArtemisKey, selectors.LabelAppValueBroker)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying scale label selector uses Broker tracking label")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: brokerCr.Name, Namespace: defaultNamespace}, &createdBrokerCr)).Should(Succeed())
				g.Expect(createdBrokerCr.Status.ScaleLabelSelector).To(ContainSubstring(selectors.LabelBrokerKey + "=" + brokerCr.Name))
				g.Expect(createdBrokerCr.Status.ScaleLabelSelector).NotTo(ContainSubstring(selectors.LabelActiveMQArtemisKey + "="))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			CleanResource(&createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
		})
	})

	Context("ActiveMQArtemis CR", func() {
		BeforeEach(func() {
			BeforeEachSpec()
		})

		AfterEach(func() {
			AfterEachSpec()
		})

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
				assertManagedResourceTrackingLabels(g, currentSS.Labels, brokerCr.Name, selectors.LabelActiveMQArtemisKey, selectors.LabelBrokerKey, selectors.LabelAppValueArtemis)
				assertManagedResourceTrackingLabels(g, currentSS.Spec.Template.Labels, brokerCr.Name, selectors.LabelActiveMQArtemisKey, selectors.LabelBrokerKey, selectors.LabelAppValueArtemis)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying headless Service labels and selector")
			Eventually(func(g Gomega) {
				headlessSvc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, headlessSvcKey, headlessSvc)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, headlessSvc.Labels, brokerCr.Name, selectors.LabelActiveMQArtemisKey, selectors.LabelBrokerKey, selectors.LabelAppValueArtemis)
				assertManagedResourceTrackingLabels(g, headlessSvc.Spec.Selector, brokerCr.Name, selectors.LabelActiveMQArtemisKey, selectors.LabelBrokerKey, selectors.LabelAppValueArtemis)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying broker properties Secret labels")
			Eventually(func(g Gomega) {
				propsSecret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, propsSecretKey, propsSecret)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, propsSecret.Labels, brokerCr.Name, selectors.LabelActiveMQArtemisKey, selectors.LabelBrokerKey, selectors.LabelAppValueArtemis)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying scale label selector uses ActiveMQArtemis tracking label")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: brokerCr.Name, Namespace: defaultNamespace}, createdBrokerCr)).Should(Succeed())
				g.Expect(createdBrokerCr.Status.ScaleLabelSelector).To(ContainSubstring(selectors.LabelActiveMQArtemisKey + "=" + brokerCr.Name))
				g.Expect(createdBrokerCr.Status.ScaleLabelSelector).NotTo(ContainSubstring(selectors.LabelBrokerKey + "="))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			CleanResource(createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
		})
	})
})
