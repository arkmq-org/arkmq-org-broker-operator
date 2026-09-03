package controllers

import (
	"context"
	"os"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	cot "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/chain-of-trust"
	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("chain-of-trust deployed operator", Label("do"), func() {

	BeforeEach(func() {
		BeforeEachSpec()
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	It("BrokerService + BrokerApp PKI provisioning with deployed operator", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" || os.Getenv("DEPLOY_OPERATOR") != "true" {
			Skip("requires deployed operator on a real cluster")
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()

		svcLabel := "do-cot-" + serviceName

		By("creating a BrokerService")
		svc := broker.BrokerService{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerService",
				APIVersion: broker.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: defaultNamespace,
				Labels:    map[string]string{"env": svcLabel},
			},
			Spec: broker.BrokerServiceSpec{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &svc)).Should(Succeed())

		defer func() {
			_ = k8sClient.Delete(ctx, &svc)
		}()

		By("waiting for BrokerService to become Ready")
		svcKey := types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}
		Eventually(func(g Gomega) {
			s := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, svcKey, s)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(s.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("verifying operator-plane PKI resources")
		rootIssuer := &cmv1.Issuer{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.RootIssuerName(serviceName), Namespace: defaultNamespace,
		}, rootIssuer)).Should(Succeed())

		rootCert := &cmv1.Certificate{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.RootCertName(serviceName), Namespace: defaultNamespace,
		}, rootCert)).Should(Succeed())
		Expect(rootCert.Spec.IsCA).Should(BeTrue())

		operatorCert := &cmv1.Certificate{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.OperatorCertName(serviceName), Namespace: defaultNamespace,
		}, operatorCert)).Should(Succeed())

		brokerCert := &cmv1.Certificate{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.BrokerCertName(serviceName), Namespace: defaultNamespace,
		}, brokerCert)).Should(Succeed())

		By("verifying metrics-plane PKI resources")
		metricsRootCert := &cmv1.Certificate{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.MetricsRootCertName(serviceName), Namespace: defaultNamespace,
		}, metricsRootCert)).Should(Succeed())
		Expect(metricsRootCert.Spec.IsCA).Should(BeTrue())

		promCert := &cmv1.Certificate{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.PrometheusCertName(serviceName), Namespace: defaultNamespace,
		}, promCert)).Should(Succeed())

		By("creating a BrokerApp in the same namespace")
		appName := serviceName + "-app"
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
					MatchLabels: map[string]string{"env": svcLabel},
				},
				Capabilities: []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{{Address: appName + ".queue"}},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

		defer func() {
			_ = k8sClient.Delete(ctx, &app)
		}()

		By("waiting for BrokerApp to become Ready")
		appKey := types.NamespacedName{Name: appName, Namespace: defaultNamespace}
		Eventually(func(g Gomega) {
			a := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, appKey, a)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(a.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("verifying app-plane PKI resources")
		appCACert := &cmv1.Certificate{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.AppRootCertName(appName), Namespace: defaultNamespace,
		}, appCACert)).Should(Succeed())
		Expect(appCACert.Spec.IsCA).Should(BeTrue())

		appClientCert := &cmv1.Certificate{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.AppCertName(appName), Namespace: defaultNamespace,
		}, appClientCert)).Should(Succeed())

		By("verifying operator cert secret exists")
		oprSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: cot.OperatorCertName(serviceName), Namespace: defaultNamespace,
		}, oprSecret)).Should(Succeed())
		Expect(oprSecret.Data).Should(HaveKey("tls.crt"))
		Expect(oprSecret.Data).Should(HaveKey("tls.key"))

		By("verifying certs secret exists with app data")
		certsSecret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: serviceName + "-certs", Namespace: defaultNamespace,
		}, certsSecret)).Should(Succeed())

		GinkgoWriter.Printf("[do] chain-of-trust deployed operator smoke test passed for service=%s app=%s\n",
			serviceName, appName)
	})
})
