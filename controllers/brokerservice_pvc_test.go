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
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
)

var _ = Describe("broker-service-pvc", func() {

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}

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

			By("installing operator cert")
			InstallCert(common.DefaultOperatorCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = common.DefaultOperatorCertSecretName
				candidate.Spec.CommonName = "arkmq-org-broker-operator"
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})
		}
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("persistent broker service", func() {

		It("creates a PVC and the PVC survives a pod restart", Label("slow"), func() {

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

			By("deploying BrokerService with persistent storage")
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
							corev1.ResourceMemory:         resource.MustParse("1Gi"),
							broker.ResourceJournalStorage: resource.MustParse("1Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &broker.BrokerService{}

			By("waiting for BrokerService to reach Ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				if verbose {
					fmt.Printf("Service STATUS: %v\n\n", createdCrd.Status.Conditions)
				}
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying child Broker CR has PersistenceEnabled and Storage")
			brokerCrd := &broker.Broker{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, brokerCrd)).Should(Succeed())
				g.Expect(brokerCrd.Spec.PersistenceEnabled).Should(BeTrue())
				g.Expect(brokerCrd.Spec.Storage.Size).Should(Equal("1Gi"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying StatefulSet has VolumeClaimTemplates")
			ssKey := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: crd.Namespace}
			ss := &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ssKey, ss)).Should(Succeed())
				g.Expect(ss.Spec.VolumeClaimTemplates).ShouldNot(BeEmpty())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying PVC is created and bound")
			// PVC name pattern: <cr-name>-<ss-name>-<ordinal>
			// e.g. mysvc-mysvc-ss-0
			pvcName := crd.Name + "-" + namer.CrToSS(crd.Name) + "-0"
			pvcKey := types.NamespacedName{Name: pvcName, Namespace: crd.Namespace}
			pvc := &corev1.PersistentVolumeClaim{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, pvcKey, pvc)).Should(Succeed())
				g.Expect(pvc.Status.Phase).Should(Equal(corev1.ClaimBound))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deleting the broker pod to trigger a restart")
			podList := &corev1.PodList{}
			Expect(k8sClient.List(ctx, podList,
				client.InNamespace(crd.Namespace),
				client.MatchingLabels{selectors.LabelBrokerService: crd.Name},
			)).Should(Succeed())
			Expect(podList.Items).ShouldNot(BeEmpty())
			Expect(k8sClient.Delete(ctx, &podList.Items[0])).Should(Succeed())

			By("waiting for replacement pod to be Ready")
			Eventually(func(g Gomega) {
				replacementPods := &corev1.PodList{}
				g.Expect(k8sClient.List(ctx, replacementPods,
					client.InNamespace(crd.Namespace),
					client.MatchingLabels{selectors.LabelBrokerService: crd.Name},
				)).Should(Succeed())
				g.Expect(replacementPods.Items).ShouldNot(BeEmpty())
				ready := false
				for _, c := range replacementPods.Items[0].Status.Conditions {
					if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
						ready = true
					}
				}
				g.Expect(ready).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying PVC remains Bound after pod restart")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, pvcKey, pvc)).Should(Succeed())
				g.Expect(pvc.Status.Phase).Should(Equal(corev1.ClaimBound))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("tidy up")
			CleanResource(createdCrd, createdCrd.Name, createdCrd.Namespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})

		It("propagates journalStorageClass to StatefulSet VolumeClaimTemplate", Label("slow"), func() {

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

			By("deploying BrokerService with journalStorageClass set")
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
							corev1.ResourceMemory:         resource.MustParse("1Gi"),
							broker.ResourceJournalStorage: resource.MustParse("1Gi"),
						},
					},
					JournalStorageClass: "standard",
				},
			}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			createdCrd := &broker.BrokerService{}

			By("waiting for BrokerService to reach Ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying child Broker CR has correct storageClassName from journalStorageClass")
			brokerCrd := &broker.Broker{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, brokerCrd)).Should(Succeed())
				g.Expect(brokerCrd.Spec.PersistenceEnabled).Should(BeTrue())
				g.Expect(brokerCrd.Spec.Storage.StorageClassName).Should(Equal("standard"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying StatefulSet VolumeClaimTemplate has correct storageClassName from journalStorageClass")
			ssKey := types.NamespacedName{Name: namer.CrToSS(crd.Name), Namespace: crd.Namespace}
			ss := &appsv1.StatefulSet{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, ssKey, ss)).Should(Succeed())
				g.Expect(ss.Spec.VolumeClaimTemplates).ShouldNot(BeEmpty())
				g.Expect(ss.Spec.VolumeClaimTemplates[0].Spec.StorageClassName).ShouldNot(BeNil())
				g.Expect(*ss.Spec.VolumeClaimTemplates[0].Spec.StorageClassName).Should(Equal("standard"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("tidy up")
			CleanResource(createdCrd, createdCrd.Name, createdCrd.Namespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})
	})
})
