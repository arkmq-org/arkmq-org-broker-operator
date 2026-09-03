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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/appselector"
	cot "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/chain-of-trust"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/version"
)

var _ = Describe("chain-of-trust PKI", Label("chain-of-trust"), func() {

	var installedCertManager = false

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			// Remove stale global PKI secrets that would interfere with
			// per-service chain-of-trust CA resolution in the Jolokia client.
			for _, name := range []string{
				common.DefaultOperatorCASecretName,
				common.DefaultOperatorCertSecretName,
			} {
				stale := &corev1.Secret{}
				key := types.NamespacedName{Name: name, Namespace: defaultNamespace}
				if err := k8sClient.Get(context.Background(), key, stale); err == nil {
					_ = k8sClient.Delete(context.Background(), stale)
				}
			}
			common.ResetOperatorCertCache()

			if !CertManagerInstalled() {
				Expect(InstallCertManager()).To(Succeed())
				installedCertManager = true
			}
		}
	})

	AfterEach(func() {
		if false && os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			if installedCertManager {
				Expect(UninstallCertManager()).To(Succeed())
				installedCertManager = false
			}
		}
		AfterEachSpec()
	})

	Context("PKI lifecycle", func() {

		It("BrokerService creation auto-provisions all PKI resources", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService without any manual cert setup")
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"env": "pki-test"},
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

			serviceKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
			createdCrd := &broker.BrokerService{}

			By("verifying self-signed Issuer is created")
			issuerKey := types.NamespacedName{
				Name:      cot.RootIssuerName(serviceName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				issuer := &cmv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, issuerKey, issuer)).Should(Succeed())
				g.Expect(issuer.Spec.SelfSigned).ShouldNot(BeNil())
				g.Expect(hasOwnerRef(issuer, serviceName, "BrokerService")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying root CA Certificate is created")
			rootCertKey := types.NamespacedName{
				Name:      cot.RootCertName(serviceName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				cert := &cmv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, rootCertKey, cert)).Should(Succeed())
				g.Expect(cert.Spec.IsCA).Should(BeTrue())
				g.Expect(cert.Spec.IssuerRef.Name).Should(Equal(cot.RootIssuerName(serviceName)))
				g.Expect(cert.Spec.IssuerRef.Kind).Should(Equal("Issuer"))
				g.Expect(hasOwnerRef(cert, serviceName, "BrokerService")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying CA Issuer is created")
			caIssuerKey := types.NamespacedName{
				Name:      cot.CAIssuerName(serviceName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				issuer := &cmv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, caIssuerKey, issuer)).Should(Succeed())
				g.Expect(issuer.Spec.CA).ShouldNot(BeNil())
				g.Expect(issuer.Spec.CA.SecretName).Should(Equal(cot.RootCertSecretName(serviceName)))
				g.Expect(hasOwnerRef(issuer, serviceName, "BrokerService")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying broker cert is created")
			brokerCertKey := types.NamespacedName{
				Name:      cot.BrokerCertName(serviceName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				cert := &cmv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, brokerCertKey, cert)).Should(Succeed())
				g.Expect(cert.Spec.IssuerRef.Name).Should(Equal(cot.CAIssuerName(serviceName)))
				g.Expect(cert.Spec.IssuerRef.Kind).Should(Equal("Issuer"))
				g.Expect(hasOwnerRef(cert, serviceName, "BrokerService")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying operator cert is created")
			operatorCertKey := types.NamespacedName{
				Name:      cot.OperatorCertName(serviceName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				cert := &cmv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, operatorCertKey, cert)).Should(Succeed())
				g.Expect(cert.Spec.IssuerRef.Name).Should(Equal(cot.CAIssuerName(serviceName)))
				g.Expect(hasOwnerRef(cert, serviceName, "BrokerService")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying metrics self-signed issuer is created")
			metricsIssuerKey := types.NamespacedName{
				Name:      cot.MetricsIssuerName(serviceName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				issuer := &cmv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, metricsIssuerKey, issuer)).Should(Succeed())
				g.Expect(issuer.Spec.SelfSigned).ShouldNot(BeNil())
				g.Expect(hasOwnerRef(issuer, serviceName, "BrokerService")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying metrics root CA is created")
			metricsRootCertKey := types.NamespacedName{
				Name:      cot.MetricsRootCertName(serviceName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				cert := &cmv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, metricsRootCertKey, cert)).Should(Succeed())
				g.Expect(cert.Spec.IsCA).Should(BeTrue())
				g.Expect(cert.Spec.IssuerRef.Name).Should(Equal(cot.MetricsIssuerName(serviceName)))
				g.Expect(hasOwnerRef(cert, serviceName, "BrokerService")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying metrics CA issuer is created")
			metricsCAIssuerKey := types.NamespacedName{
				Name:      cot.MetricsCAIssuerName(serviceName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				issuer := &cmv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, metricsCAIssuerKey, issuer)).Should(Succeed())
				g.Expect(issuer.Spec.CA).ShouldNot(BeNil())
				g.Expect(issuer.Spec.CA.SecretName).Should(Equal(cot.MetricsRootCertSecretName(serviceName)))
				g.Expect(hasOwnerRef(issuer, serviceName, "BrokerService")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying prometheus cert is created under metrics CA")
			promCertKey := types.NamespacedName{
				Name:      cot.PrometheusCertName(serviceName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				cert := &cmv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, promCertKey, cert)).Should(Succeed())
				g.Expect(cert.Spec.IssuerRef.Name).Should(Equal(cot.MetricsCAIssuerName(serviceName)))
				g.Expect(hasOwnerRef(cert, serviceName, "BrokerService")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying BrokerService reaches Ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying all cert secrets exist (cert-manager issued them)")
			for _, secretName := range []string{
				cot.RootCertSecretName(serviceName),
				cot.BrokerCertName(serviceName),
				cot.OperatorCertName(serviceName),
				cot.MetricsRootCertSecretName(serviceName),
				cot.PrometheusCertName(serviceName),
			} {
				secretKey := types.NamespacedName{Name: secretName, Namespace: defaultNamespace}
				Eventually(func(g Gomega) {
					secret := &corev1.Secret{}
					g.Expect(k8sClient.Get(ctx, secretKey, secret)).Should(Succeed())
					g.Expect(secret.Data["tls.crt"]).ShouldNot(BeEmpty())
					g.Expect(secret.Data["tls.key"]).ShouldNot(BeEmpty())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			By("deleting BrokerService")
			cascade := metav1.DeletePropagationForeground
			Expect(k8sClient.Delete(ctx, &crd, &client.DeleteOptions{PropagationPolicy: &cascade})).Should(Succeed())

			By("verifying all PKI resources are garbage-collected (operator + metrics planes)")
			Eventually(func(g Gomega) {
				// Operator plane
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, issuerKey, &cmv1.Issuer{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, caIssuerKey, &cmv1.Issuer{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, rootCertKey, &cmv1.Certificate{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, brokerCertKey, &cmv1.Certificate{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, operatorCertKey, &cmv1.Certificate{}))).Should(BeTrue())
				// Metrics plane
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, metricsIssuerKey, &cmv1.Issuer{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, metricsCAIssuerKey, &cmv1.Issuer{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, metricsRootCertKey, &cmv1.Certificate{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, promCertKey, &cmv1.Certificate{}))).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		})
	})

	Context("same-namespace app cert provisioning", func() {

		It("BrokerApp in same namespace gets app-cert and CA trust", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()
			appName := "same-ns-app"

			By("creating BrokerService (no manual certs)")
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"env": "pki-same-ns"},
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

			serviceKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
			createdCrd := &broker.BrokerService{}

			By("waiting for service to be ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("creating BrokerApp in the same namespace")
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
						MatchLabels: map[string]string{"env": "pki-same-ns"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "PKI.TEST.QUEUE"}},
							ConsumerOf: []broker.AddressRef{{Address: "PKI.TEST.QUEUE"}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			By("verifying app is bound to service")
			appKey := types.NamespacedName{Name: appName, Namespace: defaultNamespace}
			createdApp := &broker.BrokerApp{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				g.Expect(createdApp.Status.Service).ShouldNot(BeNil())
				g.Expect(createdApp.Status.Service.Name).Should(Equal(serviceName))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying app PKI resources exist (app plane, owned by BrokerApp)")
			appIssuerKey := types.NamespacedName{
				Name:      cot.AppIssuerName(appName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				issuer := &cmv1.Issuer{}
				g.Expect(k8sClient.Get(ctx, appIssuerKey, issuer)).Should(Succeed())
				g.Expect(issuer.Spec.SelfSigned).ShouldNot(BeNil())
				g.Expect(hasOwnerRef(issuer, appName, "BrokerApp")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			appCACertKey := types.NamespacedName{
				Name:      cot.AppRootCertName(appName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				cert := &cmv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, appCACertKey, cert)).Should(Succeed())
				g.Expect(cert.Spec.IsCA).Should(BeTrue())
				g.Expect(hasOwnerRef(cert, appName, "BrokerApp")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying app client cert is created under app CA, owned by BrokerApp")
			appCertKey := types.NamespacedName{
				Name:      cot.AppCertName(appName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				cert := &cmv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, appCertKey, cert)).Should(Succeed())
				g.Expect(cert.Spec.IssuerRef.Name).Should(Equal(cot.AppCAIssuerName(appName)))
				g.Expect(cert.Spec.IssuerRef.Kind).Should(Equal("Issuer"))
				g.Expect(hasOwnerRef(cert, appName, "BrokerApp")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying app-cert TLS secret is issued")
			appSecretKey := types.NamespacedName{
				Name:      cot.AppCertName(appName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, appSecretKey, secret)).Should(Succeed())
				g.Expect(secret.Data["tls.crt"]).ShouldNot(BeEmpty())
				g.Expect(secret.Data["tls.key"]).ShouldNot(BeEmpty())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying app CA trust secret exists, owned by BrokerApp")
			appCATrustKey := types.NamespacedName{
				Name:      cot.AppCATrustSecretName(appName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, appCATrustKey, secret)).Should(Succeed())
				g.Expect(secret.Data["tls.crt"]).ShouldNot(BeEmpty())
				g.Expect(secret.Data).ShouldNot(HaveKey("tls.key"))
				g.Expect(hasOwnerRef(secret, appName, "BrokerApp")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deleting BrokerApp")
			Expect(k8sClient.Delete(ctx, &app)).Should(Succeed())

			By("verifying app PKI resources are garbage-collected (owned by BrokerApp)")
			Eventually(func(g Gomega) {
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, appIssuerKey, &cmv1.Issuer{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, appCACertKey, &cmv1.Certificate{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, appCertKey, &cmv1.Certificate{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, appCATrustKey, &corev1.Secret{}))).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying service PKI is untouched after app deletion")
			issuerKey := types.NamespacedName{
				Name:      cot.RootIssuerName(serviceName),
				Namespace: defaultNamespace,
			}
			Expect(k8sClient.Get(ctx, issuerKey, &cmv1.Issuer{})).Should(Succeed())

			By("cleaning up BrokerService")
			cascade := metav1.DeletePropagationForeground
			Expect(k8sClient.Delete(ctx, &crd, &client.DeleteOptions{PropagationPolicy: &cascade})).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, serviceKey, &broker.BrokerService{}))).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		})
	})

	Context("cross-namespace app cert provisioning", func() {

		It("BrokerApp in different namespace gets cert copied + finalizer cleanup", Label("verySlow"), func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			appselector.SetNamespacePermission(true)
			defer appselector.SetNamespacePermission(false)

			ctx := context.Background()
			serviceName := NextSpecResourceName()
			appName := "cross-ns-app"
			appNamespace := "test-cot-" + serviceName

			By("creating app namespace")
			appNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: appNamespace},
			}
			Expect(k8sClient.Create(ctx, appNs)).Should(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, appNs)
			}()

			By("creating BrokerService with cross-namespace selector")
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"env": "pki-cross-ns"},
				},
				Spec: broker.BrokerServiceSpec{
					AppSelectorExpression: fmt.Sprintf(`app.metadata.namespace in ["%s", "%s"]`, appNamespace, defaultNamespace),
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
			createdCrd := &broker.BrokerService{}

			By("waiting for service to be ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("creating BrokerApp in a DIFFERENT namespace")
			app := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: appNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "pki-cross-ns"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "CROSS.NS.QUEUE"}},
							ConsumerOf: []broker.AddressRef{{Address: "CROSS.NS.QUEUE"}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			By("verifying app is bound to service")
			appKey := types.NamespacedName{Name: appName, Namespace: appNamespace}
			createdApp := &broker.BrokerApp{}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				g.Expect(createdApp.Status.Service).ShouldNot(BeNil())
				g.Expect(createdApp.Status.Service.Name).Should(Equal(serviceName))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying app server cert is created in app namespace (app CA)")
			appServerCertKey := types.NamespacedName{
				Name:      cot.AppServerCertName(appName),
				Namespace: appNamespace,
			}
			Eventually(func(g Gomega) {
				cert := &cmv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, appServerCertKey, cert)).Should(Succeed())
				g.Expect(cert.Spec.IssuerRef.Name).Should(Equal(cot.AppCAIssuerName(appName)))
				g.Expect(cert.Spec.IssuerRef.Kind).Should(Equal("Issuer"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying server cert is copied to service namespace")
			serverCertInSvcNs := types.NamespacedName{
				Name:      cot.AppServerCertName(appName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, serverCertInSvcNs, secret)).Should(Succeed())
				g.Expect(secret.Data["tls.crt"]).ShouldNot(BeEmpty())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying app CA trust is copied to service namespace")
			appCATrustInSvcNs := types.NamespacedName{
				Name:      cot.AppCATrustSecretName(appName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, appCATrustInSvcNs, secret)).Should(Succeed())
				g.Expect(secret.Data["tls.crt"]).ShouldNot(BeEmpty())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying metrics cert is created in service namespace (metrics CA)")
			metricsCertKey := types.NamespacedName{
				Name:      cot.MetricsCertName(appName),
				Namespace: defaultNamespace,
			}
			Eventually(func(g Gomega) {
				cert := &cmv1.Certificate{}
				g.Expect(k8sClient.Get(ctx, metricsCertKey, cert)).Should(Succeed())
				g.Expect(cert.Spec.IssuerRef.Name).Should(Equal(cot.MetricsCAIssuerName(serviceName)))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying metrics cert secret is copied to app namespace")
			metricsCertInAppNs := types.NamespacedName{
				Name:      cot.MetricsCertName(appName),
				Namespace: appNamespace,
			}
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, metricsCertInAppNs, secret)).Should(Succeed())
				g.Expect(secret.Data["tls.crt"]).ShouldNot(BeEmpty())
				g.Expect(secret.Data["tls.key"]).ShouldNot(BeEmpty())
				g.Expect(hasOwnerRef(secret, appName, "BrokerApp")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying metrics CA trust is copied to app namespace")
			metricsCATrustInAppNs := types.NamespacedName{
				Name:      cot.MetricsCATrustSecretName(serviceName),
				Namespace: appNamespace,
			}
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, metricsCATrustInAppNs, secret)).Should(Succeed())
				g.Expect(secret.Data["tls.crt"]).ShouldNot(BeEmpty())
				g.Expect(secret.Data).ShouldNot(HaveKey("tls.key"))
				g.Expect(hasOwnerRef(secret, appName, "BrokerApp")).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying BrokerApp has chain-of-trust finalizer")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				g.Expect(createdApp.Finalizers).Should(ContainElement(cot.FinalizerName))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deleting BrokerApp — finalizer should clean up cross-ns Certificate")
			Expect(k8sClient.Delete(ctx, createdApp)).Should(Succeed())

			By("verifying cross-ns cert CRs in service namespace are cleaned up by finalizer")
			Eventually(func(g Gomega) {
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: cot.AppServerCertName(appName), Namespace: defaultNamespace}, &cmv1.Certificate{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, metricsCertKey, &cmv1.Certificate{}))).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying provisioning secrets in both namespaces are cleaned up")
			Eventually(func(g Gomega) {
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, serverCertInSvcNs, &corev1.Secret{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, appCATrustInSvcNs, &corev1.Secret{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, metricsCertInAppNs, &corev1.Secret{}))).Should(BeTrue())
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, metricsCATrustInAppNs, &corev1.Secret{}))).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying BrokerApp deletion completed (finalizer removed)")
			Eventually(func(g Gomega) {
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, appKey, &broker.BrokerApp{}))).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up BrokerService")
			cascade := metav1.DeletePropagationForeground
			Expect(k8sClient.Delete(ctx, &crd, &client.DeleteOptions{PropagationPolicy: &cascade})).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, serviceKey, &broker.BrokerService{}))).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		})
	})

	Context("cert renewal propagation", func() {

		It("renewed cert secret propagates to app namespace", Label("verySlow"), func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			appselector.SetNamespacePermission(true)
			defer appselector.SetNamespacePermission(false)

			ctx := context.Background()
			serviceName := NextSpecResourceName()
			appName := "renewal-app"
			appNamespace := "test-renew-" + serviceName

			By("creating app namespace")
			appNs := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: appNamespace},
			}
			Expect(k8sClient.Create(ctx, appNs)).Should(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, appNs)
			}()

			By("creating BrokerService")
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"env": "pki-renewal"},
				},
				Spec: broker.BrokerServiceSpec{
					AppSelectorExpression: fmt.Sprintf(`app.metadata.namespace in ["%s", "%s"]`, appNamespace, defaultNamespace),
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
			createdCrd := &broker.BrokerService{}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("creating cross-namespace BrokerApp")
			app := broker.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerApp",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: appNamespace,
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "pki-renewal"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "RENEW.QUEUE"}},
							ConsumerOf: []broker.AddressRef{{Address: "RENEW.QUEUE"}},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			By("waiting for metrics cert to be copied to app namespace")
			metricsCertKey := types.NamespacedName{
				Name:      cot.MetricsCertName(appName),
				Namespace: appNamespace,
			}
			var originalCertData []byte
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, metricsCertKey, secret)).Should(Succeed())
				g.Expect(secret.Data["tls.crt"]).ShouldNot(BeEmpty())
				originalCertData = secret.Data["tls.crt"]
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("simulating cert renewal by deleting the metrics cert source secret")
			sourceSecretKey := types.NamespacedName{
				Name:      cot.MetricsCertName(appName),
				Namespace: defaultNamespace,
			}
			sourceSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, sourceSecretKey, sourceSecret)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, sourceSecret)).Should(Succeed())

			By("waiting for cert-manager to re-issue the secret")
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, sourceSecretKey, secret)).Should(Succeed())
				g.Expect(secret.Data["tls.crt"]).ShouldNot(BeEmpty())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying the renewed cert is propagated to app namespace")
			Eventually(func(g Gomega) {
				secret := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, metricsCertKey, secret)).Should(Succeed())
				g.Expect(secret.Data["tls.crt"]).ShouldNot(BeEmpty())
				g.Expect(secret.Data["tls.crt"]).ShouldNot(Equal(originalCertData))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleaning up")
			Expect(k8sClient.Delete(ctx, &app)).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: appNamespace}, &broker.BrokerApp{}))).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			cascade := metav1.DeletePropagationForeground
			Expect(k8sClient.Delete(ctx, &crd, &client.DeleteOptions{PropagationPolicy: &cascade})).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, serviceKey, &broker.BrokerService{}))).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		})
	})

	Context("metrics scraping with auto-provisioned certs", func() {

		It("prometheus metrics are scrapable using chain-of-trust certs", Label("verySlow"), func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			By("creating BrokerService")
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
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			serviceKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
			createdCrd := &broker.BrokerService{}

			By("waiting for BrokerService Ready")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, serviceKey, createdCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(createdCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("waiting for Broker Ready + ConfigApplied")
			brokerKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				brokerCrd := &broker.Broker{}
				g.Expect(k8sClient.Get(ctx, brokerKey, brokerCrd)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(brokerCrd.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionTrue(brokerCrd.Status.Conditions, broker.ConfigAppliedConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("loading chain-of-trust operator cert + CA for mTLS scrape")
			operatorCertSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: cot.OperatorCertName(serviceName), Namespace: defaultNamespace,
			}, operatorCertSecret)).Should(Succeed())

			caSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: cot.RootCertSecretName(serviceName), Namespace: defaultNamespace,
			}, caSecret)).Should(Succeed())

			operatorCert, err := tls.X509KeyPair(operatorCertSecret.Data["tls.crt"], operatorCertSecret.Data["tls.key"])
			Expect(err).Should(Succeed())

			caPool := x509.NewCertPool()
			Expect(caPool.AppendCertsFromPEM(caSecret.Data["tls.crt"])).Should(BeTrue())

			serverName := common.OrdinalFQDNS(serviceName, defaultNamespace, 0)

			By("scraping :8888/metrics with operator cert from chain-of-trust")
			Eventually(func(g Gomega) {
				transport := http.DefaultTransport.(*http.Transport).Clone()
				transport.TLSClientConfig = &tls.Config{
					ServerName:   serverName,
					Certificates: []tls.Certificate{operatorCert},
					RootCAs:      caPool,
				}
				httpClient := &http.Client{Transport: transport, Timeout: 5 * time.Second}

				resp, err := httpClient.Get("https://" + serverName + ":8888/metrics")
				g.Expect(err).Should(Succeed())
				g.Expect(resp).ShouldNot(BeNil())
				defer func() { _ = resp.Body.Close() }()

				g.Expect(resp.StatusCode).Should(Equal(http.StatusOK))

				body, err := io.ReadAll(resp.Body)
				g.Expect(err).Should(Succeed())

				bodyStr := string(body)
				g.Expect(bodyStr).Should(ContainSubstring("jvm_memory_committed_bytes"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("cleanup")
			cascade := metav1.DeletePropagationForeground
			Expect(k8sClient.Delete(ctx, &crd, &client.DeleteOptions{PropagationPolicy: &cascade})).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, serviceKey, &broker.BrokerService{}))).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		})
	})

	Context("messaging survives natural cert renewal", func() {

		It("produce/consume works before and after cert-manager auto-renews short-lived certs", Label("verySlow"), func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			// cert-manager minimum duration is 1h by default.
			// Use 1h duration + 59m renewBefore so renewal fires ~1 minute after issuance.
			certDuration := time.Hour
			certRenewBefore := 59 * time.Minute

			By("creating BrokerService with short-lived leaf certs")
			crd := broker.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "BrokerService",
					APIVersion: broker.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"env": "cert-renewal-msg"},
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

			serviceKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}

			By("waiting for BrokerService Ready")
			Eventually(func(g Gomega) {
				svc := &broker.BrokerService{}
				g.Expect(k8sClient.Get(ctx, serviceKey, svc)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(svc.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("waiting for Broker Ready + ConfigApplied")
			brokerKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				b := &broker.Broker{}
				g.Expect(k8sClient.Get(ctx, brokerKey, b)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(b.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionTrue(b.Status.Conditions, broker.ConfigAppliedConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("verifying operator-plane broker cert uses hardcoded defaults (not customizable)")
			brokerCertKey := types.NamespacedName{
				Name:      cot.BrokerCertName(serviceName),
				Namespace: defaultNamespace,
			}
			brokerCertCR := &cmv1.Certificate{}
			Expect(k8sClient.Get(ctx, brokerCertKey, brokerCertCR)).Should(Succeed())
			Expect(brokerCertCR.Spec.Duration.Duration).Should(Equal(cot.DefaultCertDuration))
			Expect(brokerCertCR.Spec.RenewBefore.Duration).Should(Equal(cot.DefaultCertRenewBefore))

			By("deploying BrokerApp with matching short-lived cert")
			appName := "renewal-msg-app"
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
						MatchLabels: map[string]string{"env": "cert-renewal-msg"},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{{Address: "RENEWAL.TEST"}},
							ConsumerOf: []broker.AddressRef{{Address: "RENEWAL.TEST"}},
						},
					},
					PKI: &broker.AppPKISpec{
						Leaf: &broker.CertificateTemplate{
							Duration:    &metav1.Duration{Duration: certDuration},
							RenewBefore: &metav1.Duration{Duration: certRenewBefore},
							PrivateKey: &broker.CertificatePrivateKey{
								RotationPolicy: "Always",
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			appKey := types.NamespacedName{Name: appName, Namespace: defaultNamespace}
			By("waiting for BrokerApp Ready")
			var bindingSecretName string
			Eventually(func(g Gomega) {
				a := &broker.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, appKey, a)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(a.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				g.Expect(a.Status.Service).ShouldNot(BeNil())
				bindingSecretName = a.Status.Service.Secret
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("waiting for Broker ConfigApplied after app add")
			Eventually(func(g Gomega) {
				b := &broker.Broker{}
				g.Expect(k8sClient.Get(ctx, brokerKey, b)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(b.Status.Conditions, broker.ConfigAppliedConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// --- messaging helpers ---
			appCertName := cot.AppCertName(appName)
			caTrustName := cot.AppCATrustSecretName(appName)
			brokerImage := version.LatestKubeImage

			pemcfgSecretName := "cert-pemcfg-renewal"
			pemcfgKey := types.NamespacedName{Name: pemcfgSecretName, Namespace: defaultNamespace}
			pemcfgSecret := secrets.NewSecret(pemcfgKey, map[string][]byte{
				"tls.pemcfg":    []byte("source.key=/app/tls/client/tls.key\nsource.cert=/app/tls/client/tls.crt"),
				"java.security": []byte("security.provider.6=de.dentrassi.crypto.pem.PemKeyStoreProvider"),
			}, nil)
			Expect(k8sClient.Create(ctx, pemcfgSecret, &client.CreateOptions{})).Should(Succeed())

			serviceHostEnvVar := "BROKER_SERVICE_HOST"
			servicePortEnvVar := "BROKER_SERVICE_PORT"

			buf := &bytes.Buffer{}
			fmt.Fprintf(buf, "amqps://${%s}:${%s}", serviceHostEnvVar, servicePortEnvVar)
			fmt.Fprintf(buf, "?transport.trustStoreType=PEMCA\\&transport.trustStoreLocation=/app/tls/ca/tls.crt")
			fmt.Fprintf(buf, "\\&transport.keyStoreType=PEMCFG\\&transport.keyStoreLocation=/app/tls/pem/tls.pemcfg")
			serviceUrl := buf.String()

			boolFalse := false
			jobCounter := 0
			runMessagingRoundTrip := func(label string) {
				jobCounter++
				producerName := fmt.Sprintf("producer-%d", jobCounter)
				consumerName := fmt.Sprintf("consumer-%d", jobCounter)

				appLabels := map[string]string{"app": producerName}
				jobTemplate := func(name string, cmd string) batchv1.Job {
					return batchv1.Job{
						TypeMeta:   metav1.TypeMeta{Kind: "Job", APIVersion: "batch/v1"},
						ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: defaultNamespace, Labels: appLabels},
						Spec: batchv1.JobSpec{
							Template: corev1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{Labels: appLabels},
								Spec: corev1.PodSpec{
									Containers: []corev1.Container{{
										Name:    name,
										Image:   brokerImage,
										Command: []string{"/bin/sh", "-c", cmd},
										Env: []corev1.EnvVar{
											{Name: "JDK_JAVA_OPTIONS", Value: "-Djava.security.properties=/app/tls/pem/java.security"},
											{Name: serviceHostEnvVar, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: bindingSecretName}, Key: "host", Optional: &boolFalse}}},
											{Name: servicePortEnvVar, ValueFrom: &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: bindingSecretName}, Key: "port", Optional: &boolFalse}}},
										},
										VolumeMounts: []corev1.VolumeMount{
											{Name: "trust", MountPath: "/app/tls/ca"},
											{Name: "cert", MountPath: "/app/tls/client"},
											{Name: "pem", MountPath: "/app/tls/pem"},
										},
									}},
									Volumes: []corev1.Volume{
										{Name: "trust", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: caTrustName}}},
										{Name: "cert", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: appCertName}}},
										{Name: "pem", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: pemcfgSecretName}}},
									},
									RestartPolicy: corev1.RestartPolicyOnFailure,
								},
							},
						},
					}
				}

				By(fmt.Sprintf("[%s] producing 1 message", label))
				producer := jobTemplate(producerName, "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis producer --protocol=AMQP --url "+serviceUrl+" --message-count 1 --destination queue://RENEWAL.TEST;")
				Expect(k8sClient.Create(ctx, &producer)).Should(Succeed())

				Eventually(func(g Gomega) {
					j := &batchv1.Job{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: producerName, Namespace: defaultNamespace}, j)).Should(Succeed())
					g.Expect(j.Status.Succeeded).Should(BeNumerically("==", 1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By(fmt.Sprintf("[%s] consuming 1 message", label))
				consumer := jobTemplate(consumerName, "exec java -classpath /opt/amq/lib/*:/opt/amq/lib/extra/* org.apache.activemq.artemis.cli.Artemis consumer --protocol=AMQP --url "+serviceUrl+" --message-count 1 --receive-timeout 30000 --destination queue://RENEWAL.TEST;")
				Expect(k8sClient.Create(ctx, &consumer)).Should(Succeed())

				Eventually(func(g Gomega) {
					j := &batchv1.Job{}
					g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: consumerName, Namespace: defaultNamespace}, j)).Should(Succeed())
					g.Expect(j.Status.Succeeded).Should(BeNumerically("==", 1))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			By("=== Round 1: baseline messaging with short-lived certs ===")
			runMessagingRoundTrip("pre-renewal")

			By("recording original broker cert serial number")
			brokerCertSecretKey := types.NamespacedName{
				Name:      cot.BrokerCertName(serviceName),
				Namespace: defaultNamespace,
			}
			originalBrokerCertSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, brokerCertSecretKey, originalBrokerCertSecret)).Should(Succeed())
			originalCertData := make([]byte, len(originalBrokerCertSecret.Data["tls.crt"]))
			copy(originalCertData, originalBrokerCertSecret.Data["tls.crt"])

			By("waiting for cert-manager to auto-renew the broker cert (renewBefore=59m of 1h duration)")
			// cert-manager should trigger renewal ~1 minute after issuance.
			// Allow up to 5 minutes for cert-manager to detect and process renewal.
			Eventually(func(g Gomega) {
				s := &corev1.Secret{}
				g.Expect(k8sClient.Get(ctx, brokerCertSecretKey, s)).Should(Succeed())
				g.Expect(s.Data["tls.crt"]).ShouldNot(BeEmpty())
				g.Expect(s.Data["tls.crt"]).ShouldNot(Equal(originalCertData),
					"tls.crt should change after auto-renewal")
			}, 5*time.Minute, 5*time.Second).Should(Succeed())

			By("waiting for Broker to reconcile with renewed cert: Ready + ConfigApplied")
			Eventually(func(g Gomega) {
				b := &broker.Broker{}
				g.Expect(k8sClient.Get(ctx, brokerKey, b)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(b.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
				g.Expect(meta.IsStatusConditionTrue(b.Status.Conditions, broker.ConfigAppliedConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("=== Round 2: messaging after natural cert renewal ===")
			runMessagingRoundTrip("post-renewal")

			By("cleanup")
			Expect(k8sClient.Delete(ctx, &app)).Should(Succeed())
			cascade := metav1.DeletePropagationForeground
			Expect(k8sClient.Delete(ctx, &crd, &client.DeleteOptions{PropagationPolicy: &cascade})).Should(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8serrors.IsNotFound(k8sClient.Get(ctx, serviceKey, &broker.BrokerService{}))).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		})
	})
})

func hasOwnerRef(obj metav1.Object, ownerName string, ownerKind string) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.Name == ownerName && ref.Kind == ownerKind {
			return true
		}
	}
	return false
}
