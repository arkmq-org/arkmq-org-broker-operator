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
	"strings"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
)

func assertManagedResourceTrackingLabels(g Gomega, labels map[string]string, crName string, expectedKey string, forbiddenKey string) {
	g.Expect(labels).To(HaveKeyWithValue(expectedKey, crName))
	g.Expect(labels).To(HaveKeyWithValue(selectors.LabelAppKey, crName+"-app"))
	g.Expect(labels).To(HaveKeyWithValue(selectors.LabelPartOfKey, selectors.LabelPartOfValue))
	_, hasForbidden := labels[forbiddenKey]
	g.Expect(hasForbidden).To(BeFalse(), "must not use tracking label key %q", forbiddenKey)
}

func installRestrictedBrokerCerts(brokerName string) {
	By("installing operator cert")
	InstallCert(common.DefaultOperatorCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
		candidate.Spec.SecretName = common.DefaultOperatorCertSecretName
		candidate.Spec.CommonName = common.OperatorName
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
				assertManagedResourceTrackingLabels(g, currentSS.Labels, brokerCr.Name, selectors.LabelBrokerKey, selectors.LabelActiveMQArtemisKey)
				assertManagedResourceTrackingLabels(g, currentSS.Spec.Template.Labels, brokerCr.Name, selectors.LabelBrokerKey, selectors.LabelActiveMQArtemisKey)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying headless Service labels and selector")
			Eventually(func(g Gomega) {
				headlessSvc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, headlessSvcKey, headlessSvc)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, headlessSvc.Labels, brokerCr.Name, selectors.LabelBrokerKey, selectors.LabelActiveMQArtemisKey)
				assertManagedResourceTrackingLabels(g, headlessSvc.Spec.Selector, brokerCr.Name, selectors.LabelBrokerKey, selectors.LabelActiveMQArtemisKey)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying broker properties Secret labels")
			Eventually(func(g Gomega) {
				propsSecret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, propsSecretKey, propsSecret)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, propsSecret.Labels, brokerCr.Name, selectors.LabelBrokerKey, selectors.LabelActiveMQArtemisKey)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			CleanResource(&createdBrokerCr, createdBrokerCr.Name, defaultNamespace)
		})

		It("broker pod has no unnecessary AMQ env vars", func() {
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

			By("verifying the broker container has no unnecessary environment variables")
			podKey := types.NamespacedName{
				Name:      namer.CrToSSOrdinal(brokerCr.Name, 0),
				Namespace: defaultNamespace,
			}

			pod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, podKey, pod)).Should(Succeed())

			var brokerContainer *corev1.Container
			for i := range pod.Spec.Containers {
				if pod.Spec.Containers[i].Name == brokerCr.Name+"-container" {
					brokerContainer = &pod.Spec.Containers[i]
					break
				}
			}

			Expect(brokerContainer).NotTo(BeNil(), "broker container should exist")

			for _, env := range brokerContainer.Env {
				Expect(strings.HasPrefix(env.Name, "AMQ_")).To(
					BeFalse(),
					"broker container must not have AMQ_* env vars, found: %s", env.Name,
				)
				Expect(env.Name).NotTo(Equal("CONFIG_BROKER"),
					"broker container must not have CONFIG_BROKER (shell-script var)")
				Expect(env.Name).NotTo(Equal("CONFIG_INSTANCE_DIR"),
					"broker container must not have CONFIG_INSTANCE_DIR (shell-script var)")
			}

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
				assertManagedResourceTrackingLabels(g, currentSS.Labels, brokerCr.Name, selectors.LabelActiveMQArtemisKey, selectors.LabelBrokerKey)
				assertManagedResourceTrackingLabels(g, currentSS.Spec.Template.Labels, brokerCr.Name, selectors.LabelActiveMQArtemisKey, selectors.LabelBrokerKey)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying headless Service labels and selector")
			Eventually(func(g Gomega) {
				headlessSvc := &corev1.Service{}
				g.Expect(k8sClient.Get(ctx, headlessSvcKey, headlessSvc)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, headlessSvc.Labels, brokerCr.Name, selectors.LabelActiveMQArtemisKey, selectors.LabelBrokerKey)
				assertManagedResourceTrackingLabels(g, headlessSvc.Spec.Selector, brokerCr.Name, selectors.LabelActiveMQArtemisKey, selectors.LabelBrokerKey)
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying broker properties Secret labels")
			Eventually(func(g Gomega) {
				propsSecret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, propsSecretKey, propsSecret)).Should(Succeed())
				assertManagedResourceTrackingLabels(g, propsSecret.Labels, brokerCr.Name, selectors.LabelActiveMQArtemisKey, selectors.LabelBrokerKey)
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

	Context("Pod watch cache filter", func() {
		BeforeEach(func() {
			BeforeEachSpec()
		})

		AfterEach(func() {
			AfterEachSpec()
		})

		// Verifies mgrOptions.Cache.ByObject Pod Label selector: only pods with
		// app.kubernetes.io/part-of=broker.arkmq.org appear in the manager cache.
		It("keeps unrelated pods out of the operator Pod cache and allows only part-of labeled pods", func() {
			By("creating a pod without the part-of label")
			unrelated := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-cache-filter-unrelated",
					Namespace: defaultNamespace,
					Labels: map[string]string{
						"app": "not-operator-managed",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "pause",
						Image:   "busybox",
						Command: []string{"sleep", "3600"},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, unrelated)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, unrelated)
			})

			unrelatedKey := types.NamespacedName{Name: unrelated.Name, Namespace: unrelated.Namespace}
			By("confirming the uncached API client can Get the unrelated pod")
			Eventually(func(g Gomega) {
				found := &corev1.Pod{}
				g.Expect(k8sClient.Get(ctx, unrelatedKey, found)).To(Succeed())
			}, timeout, interval).Should(Succeed())

			By("confirming the manager Pod cache excludes that pod (Get returns NotFound)")
			cachedClient := k8Manager.GetClient()
			Consistently(func(g Gomega) {
				found := &corev1.Pod{}
				err := cachedClient.Get(ctx, unrelatedKey, found)
				g.Expect(apierrors.IsNotFound(err)).To(BeTrue(),
					"unrelated pod %s should not be in the operator Pod cache, expected NotFound, got: %v",
					unrelatedKey, err)
			}, 3*time.Second, interval).Should(Succeed())

			By("creating a pod labeled app.kubernetes.io/part-of=broker.arkmq.org (operator-managed)")
			managed := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod-cache-filter-managed",
					Namespace: defaultNamespace,
					Labels: map[string]string{
						selectors.LabelPartOfKey: selectors.LabelPartOfValue,
						selectors.LabelAppKey:    "pod-cache-filter-managed-app",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "pause",
						Image:   "busybox",
						Command: []string{"sleep", "3600"},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, managed)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, managed)
			})

			managedKey := types.NamespacedName{Name: managed.Name, Namespace: managed.Namespace}
			By("confirming the manager Pod cache includes the part-of labeled pod")
			Eventually(func(g Gomega) {
				found := &corev1.Pod{}
				g.Expect(cachedClient.Get(ctx, managedKey, found)).To(Succeed(),
					"part-of labeled pod %s should be in the operator Pod cache", managedKey)
				g.Expect(found.Labels).To(HaveKeyWithValue(selectors.LabelPartOfKey, selectors.LabelPartOfValue))
			}, timeout, interval).Should(Succeed())
		})
	})
})
