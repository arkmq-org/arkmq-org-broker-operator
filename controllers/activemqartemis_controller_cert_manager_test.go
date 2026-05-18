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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"os"

	"github.com/Azure/go-amqp"
	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/certutil"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/namer"
	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	tm "github.com/cert-manager/trust-manager/pkg/apis/trust/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

const (
	brokerCrNameBase = "broker-cert-mgr"

	rootIssuerName      = "root-issuer"
	rootCertName        = "root-cert"
	rootCertNamespce    = "cert-manager"
	rootCertSecretName  = "artemis-root-cert-secret"
	caIssuerName        = "broker-ca-issuer"
	caPemTrustStoreName = "ca.pem"
)

var (
	serverCert = "server-cert"
	rootIssuer = &cmv1.ClusterIssuer{}
	rootCert   = &cmv1.Certificate{}
	caIssuer   = &cmv1.ClusterIssuer{}
)

type ConnectorConfig struct {
	Name             string
	FactoryClassName string
	Params           map[string]string
}

var _ = Describe("artemis controller with cert manager test", Label("controller-cert-mgr-test"), func() {
	var installedCertManager bool = false

	BeforeEach(func() {
		BeforeEachSpec()

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			//if cert manager/trust manager is not installed, install it
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
		}
	})

	AfterEach(func() {
		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			UnInstallCaBundle(common.DefaultOperatorCASecretName)
			UninstallClusteredIssuer(caIssuerName)
			UninstallCert(rootCert.Name, rootCert.Namespace)
			UninstallClusteredIssuer(rootIssuerName)

			if installedCertManager {
				Expect(UninstallCertManager()).To(Succeed())
				installedCertManager = false
			}
		}

		AfterEachSpec()
	})

	Context("cert-manager cert with java store", Label("cert-mgr-cert-as-java-store"), func() {
		var cert *cmv1.Certificate
		var passwdSec *corev1.Secret
		var trustSec *corev1.Secret
		var certSecret = corev1.Secret{}

		BeforeEach(func() {
			var err error
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				By("creating a password secret")
				_, passwdSec = DeploySecret(defaultNamespace, func(candidate *corev1.Secret) {
					candidate.StringData = make(map[string]string)
					candidate.StringData["pkcs12-password"] = "password"
				})

				By("creating tls secret as pkcs12 truststore")
				trustSec, err = CreateTlsSecret("ca-trust-secret", defaultNamespace, "password", []string{"core-client-0"})
				Expect(err).To(BeNil())
				Expect(k8sClient.Create(ctx, trustSec)).Should(Succeed())

				By("installing the cert with pkcs12 option")
				cert = InstallCert(serverCert, defaultNamespace, func(candidate *cmv1.Certificate) {
					candidate.Spec.DNSNames = []string{brokerCrNameBase + "0-ss-0"}
					candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
						Name: caIssuer.Name,
						Kind: "ClusterIssuer",
					}
					candidate.Spec.SecretName = "tls-legacy-secret"
					candidate.Spec.Keystores = &cmv1.CertificateKeystores{
						PKCS12: &cmv1.PKCS12Keystore{
							Create: true,
							PasswordSecretRef: cmmetav1.SecretKeySelector{
								Key: "pkcs12-password",
								LocalObjectReference: cmmetav1.LocalObjectReference{
									Name: passwdSec.Name,
								},
							},
						},
					}
				})

				By("updating the tls secret with default legacy secret contents")

				certSecretKey := types.NamespacedName{Name: cert.Spec.SecretName, Namespace: defaultNamespace}
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, certSecretKey, &certSecret)).Should(Succeed())

					certSecret.Data["keyStorePassword"] = []byte("password")
					certSecret.Data["trustStorePassword"] = []byte("password")
					certSecret.Data["trustStorePath"] = []byte("/amq/extra/secrets/" + trustSec.Name + "/client.ts")
					certSecret.Data["keyStorePath"] = []byte("/etc/" + certSecret.Name + "-volume/keystore.p12")

					g.Expect(k8sClient.Update(ctx, &certSecret)).Should(Succeed())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("checking cert secret get updated")
				Eventually(func(g Gomega) {
					g.Expect(k8sClient.Get(ctx, certSecretKey, &certSecret)).Should(Succeed())
					g.Expect(certSecret.Data["keyStorePassword"]).To(Equal([]byte("password")))
					g.Expect(certSecret.Data["trustStorePassword"]).To(Equal([]byte("password")))
					g.Expect(certSecret.Data["trustStorePath"]).To(Equal([]byte("/amq/extra/secrets/" + trustSec.Name + "/client.ts")))
					g.Expect(certSecret.Data["keyStorePath"]).To(Equal([]byte("/etc/" + certSecret.Name + "-volume/keystore.p12")))
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}
		})

		AfterEach(func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				UninstallCert(cert.Name, cert.Namespace)
				CleanResource(&certSecret, certSecret.Name, certSecret.Namespace)
				CleanResource(trustSec, trustSec.Name, trustSec.Namespace)
				CleanResource(passwdSec, passwdSec.Name, passwdSec.Namespace)
			}
		})

		It("test configured with cert secret as legacy one", func() {
			if isFIPSEnabled {
				Skip("Legacy PKCS12 key stores with PBEWithSHA1AndRC2_40 are not supported when FIPS mode is enabled")
			}

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				By("deploying the broker")
				_, brokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {
					candidate.Name = brokerCrNameBase + "0"
					candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{
						{
							Name:             "amqps",
							EnabledProtocols: "TLSv1.2,TLSv1.3",
							Expose:           false,
							Port:             5671,
							Protocols:        "amqp,core",
							SSLEnabled:       true,
							SSLSecret:        cert.Spec.SecretName,
							NeedClientAuth:   true,
						},
					}
					candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
					candidate.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{
						trustSec.Name,
					}
				})

				By("verify pod is up and acceptor is working")
				WaitForPod(brokerCr.Name)
				podName := namer.CrToSSOrdinal(brokerCr.Name, 0)
				Eventually(func(g Gomega) {
					CheckAcceptorStarted(podName, brokerCr.Name, "amqps", g)
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				By("check messaging should work")
				keyStorePath := "/amq/extra/secrets/" + trustSec.Name + "/broker.ks"
				trustStorePath := "/etc/" + certSecret.Name + "-volume/truststore.p12"
				password := "password"
				Eventually(func(g Gomega) {
					checkMessagingInPodWithJavaStore(podName, brokerCr.Name, "5671", trustStorePath, password, &keyStorePath, &password, g)
				}, timeout, interval).Should(Succeed())

				By("clean up")
				CleanResource(brokerCr, brokerCr.Name, brokerCr.Namespace)
			}
		})
	})

	Context("tls exposure with cert manager", func() {
		BeforeEach(func() {

			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				InstallCert(serverCert, defaultNamespace, func(candidate *cmv1.Certificate) {
					candidate.Spec.DNSNames = []string{brokerCrNameBase + "0-ss-0", brokerCrNameBase + "1-ss-0", brokerCrNameBase + "2-ss-0",
						// verifyhost in presence of operator cert and bundle requires a valid cert hostname
						common.OrdinalFQDNS(brokerCrNameBase+"0", defaultNamespace, 0), common.OrdinalFQDNS(brokerCrNameBase+"1", defaultNamespace, 0)}
					candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
						Name: caIssuer.Name,
						Kind: "ClusterIssuer",
					}
				})
			}
		})
		AfterEach(func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				UninstallCert(serverCert, defaultNamespace)
			}
		})
		It("test configured with cert and ca bundle", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				testConfiguredWithCertAndBundle(serverCert+"-secret", common.DefaultOperatorCASecretName)
			}
		})
		It("test console cert broker status access", Label("console-tls-broker-status-access"), func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				testConsoleAccessWithCert(serverCert + "-secret")
			}
		})
		It("test ssl args with keystore secrets only", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
				certKey := types.NamespacedName{Name: serverCert + "-secret", Namespace: defaultNamespace}
				certSecret := corev1.Secret{}
				Expect(resources.Retrieve(certKey, k8sClient, &certSecret)).To(Succeed())
				sslArgs, err := certutil.GetSslArgumentsFromSecret(&certSecret, "any", nil, false)
				Expect(err).To(Succeed())
				sslFlags := sslArgs.ToFlags()
				Expect(sslFlags).To(Not(ContainSubstring("trust")))
				sslArgs, err = certutil.GetSslArgumentsFromSecret(&certSecret, "any", nil, true)

				Expect(err).To(Succeed())
				sslFlags = sslArgs.ToFlags()
				Expect(sslFlags).To(Not(ContainSubstring("trust")))
				sslProps := sslArgs.ToSystemProperties()
				Expect(sslProps).To(Not(ContainSubstring("trust")))
				Expect(sslProps).To(Not(ContainSubstring("trust")))
			}
		})
	})
	Context("certutil functions", Label("check-cert-secret"), func() {
		It("certutil - is secret from cert", func() {
			secret := corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "mysecret",
				},
				Data: map[string][]byte{
					"tls.crt": []byte("some cert"),
				},
			}
			ok, valid := certutil.IsSecretFromCert(&secret)
			Expect(ok).To(BeFalse())
			Expect(valid).To(BeFalse())

			secret.ObjectMeta.Annotations = map[string]string{
				certutil.Cert_annotation_key: "caissuer",
			}
			ok, valid = certutil.IsSecretFromCert(&secret)
			Expect(ok).To(BeTrue())
			Expect(valid).To(BeFalse())

			secret.Data["tls.key"] = []byte("somekey")
			ok, valid = certutil.IsSecretFromCert(&secret)
			Expect(ok).To(BeTrue())
			Expect(valid).To(BeTrue())
		})
	})

	Context("Certificate from annotations", Label("certificate"), func() {
		It("ingress certificate annotations", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				Skip("Existing cluster required")
			}

			if isOpenshift {
				Skip("Passthrough ingress resources with spec.tls are not supported on OpenShift")
			}

			activeMQArtemis := generateArtemisSpec(defaultNamespace)

			rootIssuerName := activeMQArtemis.Name + "-root-issuer"
			By("Creating root issuer: " + rootIssuerName)
			rootIssuer := cmv1.Issuer{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Issuer"},
				ObjectMeta: metav1.ObjectMeta{Name: rootIssuerName, Namespace: defaultNamespace},
				Spec: cmv1.IssuerSpec{
					IssuerConfig: cmv1.IssuerConfig{
						SelfSigned: &cmv1.SelfSignedIssuer{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &rootIssuer)).Should(Succeed())

			issuerCertName := activeMQArtemis.Name + "-issuer-cert"
			issuerCertSecretName := issuerCertName + "-secret"
			By("Creating issuer certificate: " + issuerCertName)
			issuerCert := cmv1.Certificate{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Certificate"},
				ObjectMeta: metav1.ObjectMeta{Name: issuerCertName, Namespace: defaultNamespace},
				Spec: cmv1.CertificateSpec{
					IsCA:       true,
					SecretName: issuerCertSecretName,
					IssuerRef:  cmmetav1.ObjectReference{Name: rootIssuerName, Kind: "Issuer"},
					CommonName: "arkmq-org Issuer",
					DNSNames:   []string{"issuer.arkmq-org.io"},
					Subject:    &cmv1.X509Subject{Organizations: []string{"arkmq-org"}},
				},
			}
			Expect(k8sClient.Create(ctx, &issuerCert)).Should(Succeed())

			issuerName := activeMQArtemis.Name + "-issuer"
			By("Creating issuer: " + issuerName)
			issuer := cmv1.Issuer{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Issuer"},
				ObjectMeta: metav1.ObjectMeta{Name: issuerName, Namespace: defaultNamespace},
				Spec: cmv1.IssuerSpec{
					IssuerConfig: cmv1.IssuerConfig{
						CA: &cmv1.CAIssuer{SecretName: issuerCertSecretName},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &issuer)).Should(Succeed())

			ingressHost := activeMQArtemis.Name + "." + defaultTestIngressDomain
			acceptorName := "tls"
			acceptorIngressName := activeMQArtemis.Name + "-" + acceptorName + "-0-svc-ing"
			certSecretName := acceptorIngressName + "-ptls"

			By("Creating ActiveMQArtemis: " + activeMQArtemis.Name)
			activeMQArtemis.Spec.Acceptors = []brokerv1beta1.AcceptorType{
				{
					Name:        acceptorName,
					Port:        61617,
					SSLEnabled:  true,
					SSLSecret:   certSecretName,
					Expose:      true,
					ExposeMode:  &brokerv1beta1.ExposeModes.Ingress,
					IngressHost: ingressHost,
				},
			}
			activeMQArtemis.Spec.ResourceTemplates = []brokerv1beta1.ResourceTemplate{
				{
					Selector: &brokerv1beta1.ResourceSelector{
						Kind: ptr.To("Ingress"),
						Name: ptr.To(acceptorIngressName),
					},
					Annotations: map[string]string{
						"cert-manager.io/issuer": issuerName,
					},
					Patch: FromUnstructuredToRawExtension(&unstructured.Unstructured{
						Object: map[string]interface{}{
							"kind": "Ingress",
							"spec": map[string]interface{}{
								"tls": []interface{}{
									map[string]interface{}{
										"hosts":      []string{ingressHost},
										"secretName": certSecretName,
									},
								},
							},
						},
					}),
				},
			}

			activeMQArtemis.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{issuerCertSecretName}

			Expect(k8sClient.Create(ctx, &activeMQArtemis)).Should(Succeed())

			By("Checking tls acceptor")
			podName := activeMQArtemis.Name + "-ss-0"
			trustStorePath := "/amq/extra/secrets/" + issuerCertSecretName + "/tls.crt"
			checkCommandBeforeUpdating := []string{"/home/jboss/amq-broker/bin/artemis", "check", "node", "--up", "--url",
				"tcp://" + podName + ":61617?sslEnabled=true&forceSSLParameters=true&sniHost=" + ingressHost + "&trustStoreType=PEM&trustStorePath=" + trustStorePath}
			Eventually(func(g Gomega) {
				stdOutContent := ExecOnPod(podName, activeMQArtemis.Name, defaultNamespace, checkCommandBeforeUpdating, g)
				g.Expect(stdOutContent).Should(ContainSubstring("Checks run: 1"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			if isIngressSSLPassthroughEnabled {
				By("loading issuer cert secret")
				issuerCertSecret := &corev1.Secret{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: issuerCertSecretName, Namespace: defaultNamespace}, issuerCertSecret)).Should(Succeed())

				roots := x509.NewCertPool()
				Expect(roots.AppendCertsFromPEM([]byte(issuerCertSecret.Data["tls.crt"]))).Should(BeTrue())

				By("check acceptor is reachable")
				Eventually(func(g Gomega) {
					url := "amqps://" + clusterIngressHost + ":443"
					connTLSConfig := amqp.ConnTLSConfig(&tls.Config{ServerName: ingressHost, RootCAs: roots})
					client, err := amqp.Dial(url, amqp.ConnSASLPlain("dummy-user", "dummy-pass"), amqp.ConnTLS(true), connTLSConfig)
					g.Expect(err).Should(BeNil())
					g.Expect(client).ShouldNot(BeNil())
					defer client.Close()
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			CleanResource(&activeMQArtemis, activeMQArtemis.Name, defaultNamespace)
			CleanResource(&issuer, issuer.Name, defaultNamespace)
			CleanResource(&issuerCert, issuerCert.Name, defaultNamespace)
			CleanResource(&rootIssuer, rootIssuer.Name, defaultNamespace)

			certSecret := &corev1.Secret{}
			// by default, cert-manager does not delete the Secret resource containing the signed certificate
			// when the corresponding Certificate resource is deleted
			if k8sClient.Get(ctx, types.NamespacedName{Name: certSecretName, Namespace: defaultNamespace}, certSecret) == nil {
				CleanResource(certSecret, certSecretName, defaultNamespace)
			}
		})
	})

	Context("certificate rotation", Label("certificate"), func() {
		It("broker certificate rotation", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				Skip("Existing cluster required")
			}

			activeMQArtemis := generateArtemisSpec(defaultNamespace)

			rootIssuerName := activeMQArtemis.Name + "-root-issuer"
			By("Creating root issuer: " + rootIssuerName)
			rootIssuer := cmv1.Issuer{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Issuer"},
				ObjectMeta: metav1.ObjectMeta{Name: rootIssuerName, Namespace: defaultNamespace},
				Spec: cmv1.IssuerSpec{
					IssuerConfig: cmv1.IssuerConfig{
						SelfSigned: &cmv1.SelfSignedIssuer{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &rootIssuer)).Should(Succeed())

			issuerCertName := activeMQArtemis.Name + "-issuer-cert"
			issuerCertSecretName := issuerCertName + "-secret"
			By("Creating issuer certificate: " + issuerCertName)
			issuerCert := cmv1.Certificate{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Certificate"},
				ObjectMeta: metav1.ObjectMeta{Name: issuerCertName, Namespace: defaultNamespace},
				Spec: cmv1.CertificateSpec{
					IsCA:       true,
					SecretName: issuerCertSecretName,
					IssuerRef:  cmmetav1.ObjectReference{Name: rootIssuerName, Kind: "Issuer"},
					CommonName: "arkmq-org Issuer",
					DNSNames:   []string{"issuer.arkmq-org.io"},
					Subject:    &cmv1.X509Subject{Organizations: []string{"arkmq-org"}},
				},
			}
			Expect(k8sClient.Create(ctx, &issuerCert)).Should(Succeed())

			issuerName := activeMQArtemis.Name + "-issuer"
			By("Creating issuer: " + issuerName)
			issuer := cmv1.Issuer{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Issuer"},
				ObjectMeta: metav1.ObjectMeta{Name: issuerName, Namespace: defaultNamespace},
				Spec: cmv1.IssuerSpec{
					IssuerConfig: cmv1.IssuerConfig{
						CA: &cmv1.CAIssuer{SecretName: issuerCertSecretName},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &issuer)).Should(Succeed())

			certName := activeMQArtemis.Name + "-cert"
			certSecretName := certName + "-secret"
			By("Creating certificate: " + certName)
			cert := cmv1.Certificate{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Certificate"},
				ObjectMeta: metav1.ObjectMeta{Name: certName, Namespace: defaultNamespace},
				Spec: cmv1.CertificateSpec{
					SecretName: certSecretName,
					IssuerRef:  cmmetav1.ObjectReference{Name: issuerName, Kind: "Issuer"},
					CommonName: "arkmq-org Broker",
					DNSNames:   []string{"before.arkmq-org.io"},
					Subject:    &cmv1.X509Subject{Organizations: []string{"arkmq-org"}},
				},
			}
			Expect(k8sClient.Create(ctx, &cert)).Should(Succeed())

			By("Creating ActiveMQArtemis: " + activeMQArtemis.Name)
			activeMQArtemis.Spec.Acceptors = []brokerv1beta1.AcceptorType{
				{
					Name:       "tls-acceptor",
					Port:       61617,
					SSLEnabled: true,
					SSLSecret:  certSecretName,
				},
			}
			activeMQArtemis.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{issuerCertSecretName}
			// uncomment the following line to enable javax net debug
			//activeMQArtemis.Spec.Env = []corev1.EnvVar{{Name: "JAVA_ARGS_APPEND", Value: "-Djavax.net.debug=all"}}
			activeMQArtemis.Spec.BrokerProperties = []string{
				"acceptorConfigurations.tls-acceptor.params.sslAutoReload=true",
			}

			Expect(k8sClient.Create(ctx, &activeMQArtemis)).Should(Succeed())

			podName := activeMQArtemis.Name + "-ss-0"
			trustStorePath := "/amq/extra/secrets/" + issuerCertSecretName + "/tls.crt"
			certDumpCommand := []string{"cat", "/etc/" + certSecretName + "-volume/tls.crt"}

			By("Checking tls-acceptor before updating")
			checkCommandBeforeUpdating := []string{"/home/jboss/amq-broker/bin/artemis", "check", "node", "--up", "--url",
				"tcp://" + podName + ":61617?sslEnabled=true&forceSSLParameters=true&sniHost=before.arkmq-org.io&trustStoreType=PEM&trustStorePath=" + trustStorePath}
			Eventually(func(g Gomega) {
				stdOutContent := ExecOnPod(podName, activeMQArtemis.Name, defaultNamespace, checkCommandBeforeUpdating, g)
				g.Expect(stdOutContent).Should(ContainSubstring("Checks run: 1"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			certDumpBeforeUpdating := ""
			By("Dumping certificate before updating")
			Eventually(func(g Gomega) {
				certDumpBeforeUpdating = ExecOnPod(podName, activeMQArtemis.Name, defaultNamespace, certDumpCommand, g)
				g.Expect(certDumpBeforeUpdating).Should(ContainSubstring("CERTIFICATE"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Updating certificate: " + certName)
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: certName, Namespace: defaultNamespace}, &cert)).Should(Succeed())
				cert.Spec.DNSNames = []string{"after.arkmq-org.io"}
				g.Expect(k8sClient.Update(ctx, &cert)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			certDumpAfterUpdating := ""
			By("Dumping certificate after updating")
			Eventually(func(g Gomega) {
				certDumpAfterUpdating = ExecOnPod(podName, activeMQArtemis.Name, defaultNamespace, certDumpCommand, g)
				g.Expect(certDumpAfterUpdating).Should(ContainSubstring("CERTIFICATE"))
				g.Expect(certDumpAfterUpdating).ShouldNot(BeEquivalentTo(certDumpBeforeUpdating))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Checking tls-acceptor after updating")
			checkCommandAfterUpdating := []string{"/home/jboss/amq-broker/bin/artemis", "check", "node", "--up", "--url",
				"tcp://" + podName + ":61617?sslEnabled=true&forceSSLParameters=true&sniHost=after.arkmq-org.io&trustStoreType=PEM&trustStorePath=" + trustStorePath}
			Eventually(func(g Gomega) {
				stdOutContent := ExecOnPod(podName, activeMQArtemis.Name, defaultNamespace, checkCommandAfterUpdating, g)
				g.Expect(stdOutContent).Should(ContainSubstring("Checks run: 1"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(&activeMQArtemis, activeMQArtemis.Name, defaultNamespace)
			CleanResource(&cert, cert.Name, defaultNamespace)
			CleanResource(&issuer, issuer.Name, defaultNamespace)
			CleanResource(&issuerCert, issuerCert.Name, defaultNamespace)
			CleanResource(&rootIssuer, rootIssuer.Name, defaultNamespace)

			certSecret := &corev1.Secret{}
			// by default, cert-manager does not delete the Secret resource containing the signed certificate
			// when the corresponding Certificate resource is deleted
			if k8sClient.Get(ctx, types.NamespacedName{Name: certSecretName, Namespace: defaultNamespace}, certSecret) == nil {
				CleanResource(certSecret, certSecretName, defaultNamespace)
			}
		})

		It("broker issuer certificate rotation", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				Skip("Existing cluster required")
			}

			activeMQArtemis := generateArtemisSpec(defaultNamespace)

			rootIssuerName := activeMQArtemis.Name + "-root-issuer"
			By("Creating root issuer: " + rootIssuerName)
			rootIssuer := cmv1.ClusterIssuer{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: rootIssuerName}, &rootIssuer) == nil {
				CleanResource(&rootIssuer, rootIssuerName, "")
			}
			rootIssuer = cmv1.ClusterIssuer{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ClusterIssuer"},
				ObjectMeta: metav1.ObjectMeta{Name: rootIssuerName},
				Spec: cmv1.IssuerSpec{
					IssuerConfig: cmv1.IssuerConfig{
						SelfSigned: &cmv1.SelfSignedIssuer{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &rootIssuer)).Should(Succeed())

			beforeIssuerCertName := activeMQArtemis.Name + "-before-issuer-cert"
			beforeIssuerCertSecretName := beforeIssuerCertName + "-secret"
			By("Creating before issuer certificate: " + beforeIssuerCertName)
			beforeIssuerCert := cmv1.Certificate{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: beforeIssuerCertName, Namespace: rootCertNamespce}, &beforeIssuerCert) == nil {
				CleanResource(&beforeIssuerCert, beforeIssuerCertName, rootCertNamespce)
			}
			beforeIssuerCert = cmv1.Certificate{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Certificate"},
				ObjectMeta: metav1.ObjectMeta{Name: beforeIssuerCertName, Namespace: rootCertNamespce},
				Spec: cmv1.CertificateSpec{
					IsCA:       true,
					SecretName: beforeIssuerCertSecretName,
					IssuerRef:  cmmetav1.ObjectReference{Name: rootIssuerName, Kind: "ClusterIssuer"},
					CommonName: "arkmq-org Before Issuer",
					DNSNames:   []string{"issuer.arkmq-org.io"},
					Subject:    &cmv1.X509Subject{Organizations: []string{"arkmq-org"}},
				},
			}
			Expect(k8sClient.Create(ctx, &beforeIssuerCert)).Should(Succeed())

			afterIssuerCertName := activeMQArtemis.Name + "-after-issuer-cert"
			afterIssuerCertSecretName := afterIssuerCertName + "-secret"
			By("Creating after issuer certificate: " + afterIssuerCertName)
			afterIssuerCert := cmv1.Certificate{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: afterIssuerCertName, Namespace: rootCertNamespce}, &afterIssuerCert) == nil {
				CleanResource(&afterIssuerCert, afterIssuerCertName, rootCertNamespce)
			}
			afterIssuerCert = cmv1.Certificate{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Certificate"},
				ObjectMeta: metav1.ObjectMeta{Name: afterIssuerCertName, Namespace: rootCertNamespce},
				Spec: cmv1.CertificateSpec{
					IsCA:       true,
					SecretName: afterIssuerCertSecretName,
					IssuerRef:  cmmetav1.ObjectReference{Name: rootIssuerName, Kind: "ClusterIssuer"},
					CommonName: "arkmq-org After Issuer",
					DNSNames:   []string{"issuer.arkmq-org.io"},
					Subject:    &cmv1.X509Subject{Organizations: []string{"arkmq-org"}},
				},
			}
			Expect(k8sClient.Create(ctx, &afterIssuerCert)).Should(Succeed())

			beforeIssuerName := activeMQArtemis.Name + "-before-issuer"
			By("Creating before issuer: " + beforeIssuerName)
			beforeIssuer := cmv1.ClusterIssuer{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: beforeIssuerName}, &beforeIssuer) == nil {
				CleanResource(&beforeIssuer, beforeIssuerName, "")
			}
			beforeIssuer = cmv1.ClusterIssuer{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ClusterIssuer"},
				ObjectMeta: metav1.ObjectMeta{Name: beforeIssuerName},
				Spec: cmv1.IssuerSpec{
					IssuerConfig: cmv1.IssuerConfig{
						CA: &cmv1.CAIssuer{SecretName: beforeIssuerCertSecretName},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &beforeIssuer)).Should(Succeed())

			afterIssuerName := activeMQArtemis.Name + "-after-issuer"
			By("Creating after issuer: " + afterIssuerName)
			afterIssuer := cmv1.ClusterIssuer{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: afterIssuerName}, &afterIssuer) == nil {
				CleanResource(&afterIssuer, afterIssuerName, "")
			}
			afterIssuer = cmv1.ClusterIssuer{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ClusterIssuer"},
				ObjectMeta: metav1.ObjectMeta{Name: afterIssuerName},
				Spec: cmv1.IssuerSpec{
					IssuerConfig: cmv1.IssuerConfig{
						CA: &cmv1.CAIssuer{SecretName: afterIssuerCertSecretName},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &afterIssuer)).Should(Succeed())

			certName := activeMQArtemis.Name + "-cert"
			certSecretName := certName + "-secret"
			By("Creating certificate: " + certName)
			cert := cmv1.Certificate{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Certificate"},
				ObjectMeta: metav1.ObjectMeta{Name: certName, Namespace: defaultNamespace},
				Spec: cmv1.CertificateSpec{
					SecretName: certSecretName,
					IssuerRef:  cmmetav1.ObjectReference{Name: beforeIssuerName, Kind: "ClusterIssuer"},
					CommonName: "arkmq-org Broker",
					DNSNames:   []string{"broker.arkmq-org.io"},
					Subject:    &cmv1.X509Subject{Organizations: []string{"arkmq-org"}},
				},
			}
			Expect(k8sClient.Create(ctx, &cert)).Should(Succeed())

			bundleName := activeMQArtemis.Name + "-bundle"
			By("Creating bundle: " + bundleName)
			bundle := tm.Bundle{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: bundleName}, &bundle) == nil {
				CleanResource(&bundle, bundleName, "")
			}
			bundle = tm.Bundle{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1alpha1", Kind: "Bundle"},
				ObjectMeta: metav1.ObjectMeta{Name: bundleName},
				Spec: tm.BundleSpec{
					Sources: []tm.BundleSource{
						{Secret: &tm.SourceObjectKeySelector{Name: beforeIssuerCertSecretName, KeySelector: tm.KeySelector{Key: "tls.crt"}}},
						{Secret: &tm.SourceObjectKeySelector{Name: afterIssuerCertSecretName, KeySelector: tm.KeySelector{Key: "tls.crt"}}},
					},
					Target: tm.BundleTarget{Secret: &tm.KeySelector{Key: "root-certs.pem"}},
				},
			}
			Expect(k8sClient.Create(ctx, &bundle)).Should(Succeed())

			By("Creating ActiveMQArtemis: " + activeMQArtemis.Name)
			activeMQArtemis.Spec.Acceptors = []brokerv1beta1.AcceptorType{
				{
					Name:       "tls-acceptor",
					Port:       61617,
					SSLEnabled: true,
					SSLSecret:  certSecretName,
				},
			}
			activeMQArtemis.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{bundleName}
			// uncomment the following line to enable javax net debug
			//activeMQArtemis.Spec.Env = []corev1.EnvVar{{Name: "JAVA_ARGS_APPEND", Value: "-Djavax.net.debug=all"}}
			activeMQArtemis.Spec.BrokerProperties = []string{
				"acceptorConfigurations.tls-acceptor.params.sslAutoReload=true",
			}

			Expect(k8sClient.Create(ctx, &activeMQArtemis)).Should(Succeed())

			podName := activeMQArtemis.Name + "-ss-0"
			trustStorePath := "/amq/extra/secrets/" + bundleName + "/root-certs.pem"
			certDumpCommand := []string{"cat", "/etc/" + certSecretName + "-volume/tls.crt"}
			checkCommand := []string{"/home/jboss/amq-broker/bin/artemis", "check", "node", "--up", "--url",
				"tcp://" + podName + ":61617?sslEnabled=true&forceSSLParameters=true&sniHost=broker.arkmq-org.io&trustStoreType=PEMCA&trustStorePath=" + trustStorePath}

			By("Checking tls-acceptor before updating")
			Eventually(func(g Gomega) {
				stdOutContent := ExecOnPod(podName, activeMQArtemis.Name, defaultNamespace, checkCommand, g)
				g.Expect(stdOutContent).Should(ContainSubstring("Checks run: 1"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			certDumpBeforeUpdating := ""
			By("Dumping certificate before updating")
			Eventually(func(g Gomega) {
				certDumpBeforeUpdating = ExecOnPod(podName, activeMQArtemis.Name, defaultNamespace, certDumpCommand, g)
				g.Expect(certDumpBeforeUpdating).Should(ContainSubstring("CERTIFICATE"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Updating issuer certificate: " + certName)
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: certName, Namespace: defaultNamespace}, &cert)).Should(Succeed())
				cert.Spec.IssuerRef = cmmetav1.ObjectReference{Name: afterIssuerName, Kind: "ClusterIssuer"}
				g.Expect(k8sClient.Update(ctx, &cert)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			certDumpAfterUpdating := ""
			By("Dumping certificate after updating")
			Eventually(func(g Gomega) {
				certDumpAfterUpdating = ExecOnPod(podName, activeMQArtemis.Name, defaultNamespace, certDumpCommand, g)
				g.Expect(certDumpAfterUpdating).Should(ContainSubstring("CERTIFICATE"))
				g.Expect(certDumpAfterUpdating).ShouldNot(BeEquivalentTo(certDumpBeforeUpdating))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Checking tls-acceptor after updating")
			Eventually(func(g Gomega) {
				stdOutContent := ExecOnPod(podName, activeMQArtemis.Name, defaultNamespace, checkCommand, g)
				g.Expect(stdOutContent).Should(ContainSubstring("Checks run: 1"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(&activeMQArtemis, activeMQArtemis.Name, defaultNamespace)
			CleanResource(&cert, cert.Name, defaultNamespace)
			CleanResource(&beforeIssuer, beforeIssuer.Name, defaultNamespace)
			CleanResource(&afterIssuer, afterIssuer.Name, defaultNamespace)
			CleanResource(&bundle, bundle.Name, defaultNamespace)
			CleanResource(&beforeIssuerCert, beforeIssuerCert.Name, defaultNamespace)
			CleanResource(&afterIssuerCert, afterIssuerCert.Name, defaultNamespace)
			CleanResource(&rootIssuer, rootIssuer.Name, defaultNamespace)

			certSecret := &corev1.Secret{}
			// by default, cert-manager does not delete the Secret resource containing the signed certificate
			// when the corresponding Certificate resource is deleted
			if k8sClient.Get(ctx, types.NamespacedName{Name: certSecretName, Namespace: defaultNamespace}, certSecret) == nil {
				CleanResource(certSecret, certSecretName, defaultNamespace)
			}
		})
	})

	Context("certificate bundle", Label("certificate"), func() {
		It("mutual authentication", func() {
			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				Skip("Existing cluster required")
			}

			activeMQArtemis := generateArtemisSpec(defaultNamespace)

			selfsignedIssuerName := activeMQArtemis.Name + "-selfsigned-issuer"
			By("Creating root issuer: " + selfsignedIssuerName)
			selfsignedIssuer := cmv1.ClusterIssuer{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: selfsignedIssuerName}, &selfsignedIssuer) == nil {
				CleanResource(&selfsignedIssuer, selfsignedIssuerName, "")
			}
			selfsignedIssuer = cmv1.ClusterIssuer{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ClusterIssuer"},
				ObjectMeta: metav1.ObjectMeta{Name: selfsignedIssuerName},
				Spec: cmv1.IssuerSpec{
					IssuerConfig: cmv1.IssuerConfig{
						SelfSigned: &cmv1.SelfSignedIssuer{},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &selfsignedIssuer)).Should(Succeed())

			brokerCertName := activeMQArtemis.Name + "-broker-cert"
			brokerCertSecretName := brokerCertName + "-secret"
			By("Creating broker certificate: " + brokerCertName)
			brokerCert := cmv1.Certificate{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: brokerCertName, Namespace: defaultNamespace}, &brokerCert) == nil {
				CleanResource(&brokerCert, brokerCertName, defaultNamespace)
			}
			brokerCert = cmv1.Certificate{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Certificate"},
				ObjectMeta: metav1.ObjectMeta{Name: brokerCertName, Namespace: defaultNamespace},
				Spec: cmv1.CertificateSpec{
					IsCA:       true,
					SecretName: brokerCertSecretName,
					IssuerRef:  cmmetav1.ObjectReference{Name: selfsignedIssuerName, Kind: "ClusterIssuer"},
					CommonName: "arkmq-org Broker",
					DNSNames:   []string{"broker.arkmq-org.io"},
					Subject:    &cmv1.X509Subject{Organizations: []string{"arkmq-org"}},
				},
			}
			Expect(k8sClient.Create(ctx, &brokerCert)).Should(Succeed())

			clientFooIssuerCertName := activeMQArtemis.Name + "-client-foo-issuer-cert"
			clientFooIssuerCertSecretName := clientFooIssuerCertName + "-secret"
			By("Creating client foo issuer certificate: " + clientFooIssuerCertName)
			clientFooIssuerCert := cmv1.Certificate{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: clientFooIssuerCertName, Namespace: rootCertNamespce}, &clientFooIssuerCert) == nil {
				CleanResource(&clientFooIssuerCert, clientFooIssuerCertName, rootCertNamespce)
			}
			clientFooIssuerCert = cmv1.Certificate{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Certificate"},
				ObjectMeta: metav1.ObjectMeta{Name: clientFooIssuerCertName, Namespace: rootCertNamespce},
				Spec: cmv1.CertificateSpec{
					IsCA:       true,
					SecretName: clientFooIssuerCertSecretName,
					IssuerRef:  cmmetav1.ObjectReference{Name: rootIssuerName, Kind: "ClusterIssuer"},
					CommonName: "arkmq-org Client Foo Issuer",
					DNSNames:   []string{"client-foo.issuer.arkmq-org.io"},
					Subject:    &cmv1.X509Subject{Organizations: []string{"arkmq-org"}},
				},
			}
			Expect(k8sClient.Create(ctx, &clientFooIssuerCert)).Should(Succeed())

			clientFooIssuerName := activeMQArtemis.Name + "-client-foo-issuer"
			By("Creating client foo issuer: " + clientFooIssuerName)
			clientFooIssuer := cmv1.ClusterIssuer{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: clientFooIssuerName}, &clientFooIssuer) == nil {
				CleanResource(&clientFooIssuer, clientFooIssuerName, "")
			}
			clientFooIssuer = cmv1.ClusterIssuer{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ClusterIssuer"},
				ObjectMeta: metav1.ObjectMeta{Name: clientFooIssuerName},
				Spec: cmv1.IssuerSpec{
					IssuerConfig: cmv1.IssuerConfig{
						CA: &cmv1.CAIssuer{SecretName: clientFooIssuerCertSecretName},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &clientFooIssuer)).Should(Succeed())

			clientBarIssuerCertName := activeMQArtemis.Name + "-client-bar-issuer-cert"
			clientBarIssuerCertSecretName := clientBarIssuerCertName + "-secret"
			By("Creating client bar issuer certificate: " + clientBarIssuerCertName)
			clientBarIssuerCert := cmv1.Certificate{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: clientBarIssuerCertName, Namespace: rootCertNamespce}, &clientBarIssuerCert) == nil {
				CleanResource(&clientBarIssuerCert, clientBarIssuerCertName, rootCertNamespce)
			}
			clientBarIssuerCert = cmv1.Certificate{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Certificate"},
				ObjectMeta: metav1.ObjectMeta{Name: clientBarIssuerCertName, Namespace: rootCertNamespce},
				Spec: cmv1.CertificateSpec{
					IsCA:       true,
					SecretName: clientBarIssuerCertSecretName,
					IssuerRef:  cmmetav1.ObjectReference{Name: rootIssuerName, Kind: "ClusterIssuer"},
					CommonName: "arkmq-org Client Bar Issuer",
					DNSNames:   []string{"client-bar.issuer.arkmq-org.io"},
					Subject:    &cmv1.X509Subject{Organizations: []string{"arkmq-org"}},
				},
			}
			Expect(k8sClient.Create(ctx, &clientBarIssuerCert)).Should(Succeed())

			clientBarIssuerName := activeMQArtemis.Name + "-client-bar-issuer"
			By("Creating client bar issuer: " + clientBarIssuerName)
			clientBarIssuer := cmv1.ClusterIssuer{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: clientBarIssuerName}, &clientBarIssuer) == nil {
				CleanResource(&clientBarIssuer, clientBarIssuerName, "")
			}
			clientBarIssuer = cmv1.ClusterIssuer{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ClusterIssuer"},
				ObjectMeta: metav1.ObjectMeta{Name: clientBarIssuerName},
				Spec: cmv1.IssuerSpec{
					IssuerConfig: cmv1.IssuerConfig{
						CA: &cmv1.CAIssuer{SecretName: clientBarIssuerCertSecretName},
					},
				},
			}
			Expect(k8sClient.Create(ctx, &clientBarIssuer)).Should(Succeed())

			clientFooCertName := activeMQArtemis.Name + "-client-foo-cert"
			clientFooCertSecretName := clientFooCertName + "-secret"
			By("Creating client foo certificate: " + clientFooCertName)
			clientFooCert := cmv1.Certificate{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: clientFooCertName, Namespace: defaultNamespace}, &clientFooCert) == nil {
				CleanResource(&clientFooCert, clientFooCertName, defaultNamespace)
			}
			clientFooCert = cmv1.Certificate{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Certificate"},
				ObjectMeta: metav1.ObjectMeta{Name: clientFooCertName, Namespace: defaultNamespace},
				Spec: cmv1.CertificateSpec{
					IsCA:       true,
					SecretName: clientFooCertSecretName,
					IssuerRef:  cmmetav1.ObjectReference{Name: clientFooIssuerName, Kind: "ClusterIssuer"},
					CommonName: "arkmq-org Client Foo",
					DNSNames:   []string{"client-foo.arkmq-org.io"},
					Subject:    &cmv1.X509Subject{Organizations: []string{"arkmq-org"}},
				},
			}
			Expect(k8sClient.Create(ctx, &clientFooCert)).Should(Succeed())

			clientBarCertName := activeMQArtemis.Name + "-client-bar-cert"
			clientBarCertSecretName := clientBarCertName + "-secret"
			By("Creating client bar certificate: " + clientBarCertName)
			clientBarCert := cmv1.Certificate{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: clientBarCertName, Namespace: defaultNamespace}, &clientBarCert) == nil {
				CleanResource(&clientBarCert, clientBarCertName, defaultNamespace)
			}
			clientBarCert = cmv1.Certificate{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Certificate"},
				ObjectMeta: metav1.ObjectMeta{Name: clientBarCertName, Namespace: defaultNamespace},
				Spec: cmv1.CertificateSpec{
					IsCA:       true,
					SecretName: clientBarCertSecretName,
					IssuerRef:  cmmetav1.ObjectReference{Name: clientBarIssuerName, Kind: "ClusterIssuer"},
					CommonName: "arkmq-org Client Bar",
					DNSNames:   []string{"client-bar.arkmq-org.io"},
					Subject:    &cmv1.X509Subject{Organizations: []string{"arkmq-org"}},
				},
			}
			Expect(k8sClient.Create(ctx, &clientBarCert)).Should(Succeed())

			bundleName := activeMQArtemis.Name + "-bundle"
			By("Creating bundle: " + bundleName)
			bundle := tm.Bundle{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: bundleName}, &bundle) == nil {
				CleanResource(&bundle, bundleName, "")
			}
			bundle = tm.Bundle{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1alpha1", Kind: "Bundle"},
				ObjectMeta: metav1.ObjectMeta{Name: bundleName},
				Spec: tm.BundleSpec{
					Sources: []tm.BundleSource{
						{Secret: &tm.SourceObjectKeySelector{Name: clientFooIssuerCertSecretName, KeySelector: tm.KeySelector{Key: "tls.crt"}}},
						{Secret: &tm.SourceObjectKeySelector{Name: clientBarIssuerCertSecretName, KeySelector: tm.KeySelector{Key: "tls.crt"}}},
					},
					Target: tm.BundleTarget{Secret: &tm.KeySelector{Key: "root-certs.crt"}},
				},
			}
			Expect(k8sClient.Create(ctx, &bundle)).Should(Succeed())

			clientKeyStoreSecretName := activeMQArtemis.Name + "-client-keystore-secret"
			By("Creating client keystore secret: " + bundleName)
			clientKeyStoreSecret := corev1.Secret{}
			if k8sClient.Get(ctx, types.NamespacedName{Name: clientKeyStoreSecretName, Namespace: defaultNamespace}, &clientKeyStoreSecret) == nil {
				CleanResource(&clientKeyStoreSecret, clientKeyStoreSecretName, defaultNamespace)
			}
			clientKeyStoreSecret = corev1.Secret{
				TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
				ObjectMeta: metav1.ObjectMeta{Name: clientKeyStoreSecretName, Namespace: defaultNamespace},
				StringData: map[string]string{
					"client-foo.pemcfg": "source.key=/amq/extra/secrets/" + clientFooCertSecretName + "/tls.key\nsource.cert=/amq/extra/secrets/" + clientFooCertSecretName + "/tls.crt\n",
					"client-bar.pemcfg": "source.key=/amq/extra/secrets/" + clientBarCertSecretName + "/tls.key\nsource.cert=/amq/extra/secrets/" + clientBarCertSecretName + "/tls.crt\n",
				},
			}
			Expect(k8sClient.Create(ctx, &clientKeyStoreSecret)).Should(Succeed())

			By("Creating ActiveMQArtemis: " + activeMQArtemis.Name)
			activeMQArtemis.Spec.Acceptors = []brokerv1beta1.AcceptorType{
				{
					Name:           "tls-acceptor",
					Port:           61617,
					NeedClientAuth: true,
					SSLEnabled:     true,
					SSLSecret:      brokerCertSecretName,
					TrustSecret:    &bundleName,
				},
			}
			activeMQArtemis.Spec.DeploymentPlan.ExtraMounts.Secrets = []string{clientFooCertSecretName, clientBarCertSecretName, clientKeyStoreSecretName}
			// uncomment the following line to enable javax net debug
			activeMQArtemis.Spec.Env = []corev1.EnvVar{{Name: "JAVA_ARGS_APPEND", Value: "-Djavax.net.debug=all"}}
			Expect(k8sClient.Create(ctx, &activeMQArtemis)).Should(Succeed())

			By("checking deployed condition")
			Eventually(func(g Gomega) {
				activeMQArtemisKey := types.NamespacedName{Name: activeMQArtemis.Name, Namespace: activeMQArtemis.Namespace}
				g.Expect(k8sClient.Get(ctx, activeMQArtemisKey, &activeMQArtemis)).Should(Succeed())

				condition := meta.FindStatusCondition(activeMQArtemis.Status.Conditions, brokerv1beta1.DeployedConditionType)
				g.Expect(condition).NotTo(BeNil())
				g.Expect(condition.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(condition.Reason).To(Equal(brokerv1beta1.DeployedConditionCrudKindErrorReason))
				g.Expect(condition.Message).To(ContainSubstring(bundleName))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("updating bundle: " + bundleName)
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: bundleName}, &bundle)).Should(Succeed())
				bundle.Spec.Target.Secret.Key = "root-certs.pem"
				g.Expect(k8sClient.Update(ctx, &bundle)).Should(Succeed())
			}, timeout, interval).Should(Succeed())

			podName := activeMQArtemis.Name + "-ss-0"
			trustStorePath := "/etc/" + brokerCertSecretName + "-volume/tls.crt"

			By("Checking tls-acceptor with client foo")
			Eventually(func(g Gomega) {
				keyStorePath := "/amq/extra/secrets/" + clientKeyStoreSecretName + "/client-foo.pemcfg"
				checkCommand := []string{"/home/jboss/amq-broker/bin/artemis", "check", "node", "--up", "--url",
					"tcp://" + podName + ":61617?sslEnabled=true&forceSSLParameters=true&sniHost=broker.arkmq-org.io&keyStoreType=PEMCFG&keyStorePath=" + keyStorePath + "&trustStoreType=PEM&trustStorePath=" + trustStorePath}

				stdOutContent := ExecOnPod(podName, activeMQArtemis.Name, defaultNamespace, checkCommand, g)
				g.Expect(stdOutContent).Should(ContainSubstring("Checks run: 1"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("Checking tls-acceptor with client bar")
			Eventually(func(g Gomega) {
				keyStorePath := "/amq/extra/secrets/" + clientKeyStoreSecretName + "/client-bar.pemcfg"
				checkCommand := []string{"/home/jboss/amq-broker/bin/artemis", "check", "node", "--up", "--url",
					"tcp://" + podName + ":61617?sslEnabled=true&forceSSLParameters=true&sniHost=broker.arkmq-org.io&keyStoreType=PEMCFG&keyStorePath=" + keyStorePath + "&trustStoreType=PEM&trustStorePath=" + trustStorePath}

				stdOutContent := ExecOnPod(podName, activeMQArtemis.Name, defaultNamespace, checkCommand, g)
				g.Expect(stdOutContent).Should(ContainSubstring("Checks run: 1"))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			CleanResource(&activeMQArtemis, activeMQArtemis.Name, defaultNamespace)
			CleanResource(&clientKeyStoreSecret, clientKeyStoreSecret.Name, defaultNamespace)
			CleanResource(&clientBarCert, clientBarCert.Name, defaultNamespace)
			CleanResource(&clientFooCert, clientFooCert.Name, defaultNamespace)
			CleanResource(&bundle, bundle.Name, defaultNamespace)
			CleanResource(&clientBarIssuer, clientBarIssuer.Name, defaultNamespace)
			CleanResource(&clientBarIssuerCert, clientBarIssuerCert.Name, defaultNamespace)
			CleanResource(&clientFooIssuer, clientFooIssuer.Name, defaultNamespace)
			CleanResource(&clientFooIssuerCert, clientFooIssuerCert.Name, defaultNamespace)
			CleanResource(&brokerCert, brokerCert.Name, defaultNamespace)
			CleanResource(&selfsignedIssuer, selfsignedIssuer.Name, defaultNamespace)

			certSecret := &corev1.Secret{}
			// by default, cert-manager does not delete the Secret resource containing the signed certificate
			// when the corresponding Certificate resource is deleted
			if k8sClient.Get(ctx, types.NamespacedName{Name: brokerCertSecretName, Namespace: defaultNamespace}, certSecret) == nil {
				CleanResource(certSecret, brokerCertSecretName, defaultNamespace)
			}
			if k8sClient.Get(ctx, types.NamespacedName{Name: clientFooIssuerCertSecretName, Namespace: defaultNamespace}, certSecret) == nil {
				CleanResource(certSecret, clientFooIssuerCertSecretName, defaultNamespace)
			}
			if k8sClient.Get(ctx, types.NamespacedName{Name: clientBarIssuerCertSecretName, Namespace: defaultNamespace}, certSecret) == nil {
				CleanResource(certSecret, clientBarIssuerCertSecretName, defaultNamespace)
			}
		})
	})
})

func getConnectorConfig(podName string, crName string, connectorName string, g Gomega) map[string]string {
	curlUrl := "http://" + podName + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=%22amq-broker%22/ConnectorsAsJSON"
	command := []string{"curl", "-k", "-s", "-u", "testuser:testpassword", curlUrl}

	result := ExecOnPod(podName, crName, defaultNamespace, command, g)

	var rootMap map[string]any
	g.Expect(json.Unmarshal([]byte(result), &rootMap)).To(Succeed())

	rootMapValue := rootMap["value"]
	g.Expect(rootMapValue).ShouldNot(BeNil())
	connectors := rootMapValue.(string)

	var listOfConnectors []ConnectorConfig
	g.Expect(json.Unmarshal([]byte(connectors), &listOfConnectors))

	for _, v := range listOfConnectors {
		if v.Name == connectorName {
			return v.Params
		}
	}
	return nil
}

func CheckAcceptorStarted(podName string, crName string, acceptorName string, g Gomega) {
	curlUrl := "http://" + podName + ":8161/console/jolokia/read/org.apache.activemq.artemis:broker=%22amq-broker%22,component=acceptors,name=%22" + acceptorName + "%22/Started"
	command := []string{"curl", "-k", "-s", "-u", "testuser:testpassword", curlUrl}

	result := ExecOnPod(podName, crName, defaultNamespace, command, g)

	var rootMap map[string]any
	g.Expect(json.Unmarshal([]byte(result), &rootMap)).To(Succeed())

	rootMapValue := rootMap["value"]
	g.Expect(rootMapValue).Should(BeTrue())
}

func checkMessagingInPodWithJavaStore(podName string, crName string, portNumber string, trustStoreLoc string, trustStorePassword string, keyStoreLoc *string, keyStorePassword *string, g Gomega) {
	tcpUrl := "tcp://" + podName + ":" + portNumber + "?sslEnabled=true&forceSSLParameters=true&trustStorePath=" + trustStoreLoc + "&trustStorePassword=" + trustStorePassword
	if keyStoreLoc != nil {
		tcpUrl += "&keyStorePath=" + *keyStoreLoc + "&keyStorePassword=" + *keyStorePassword
	}
	sendCommand := []string{"amq-broker/bin/artemis", "producer", "--user", "testuser", "--password", "testpassword", "--url", tcpUrl, "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}
	result := ExecOnPod(podName, crName, defaultNamespace, sendCommand, g)
	g.Expect(result).To(ContainSubstring("Produced: 1 messages"))
	receiveCommand := []string{"amq-broker/bin/artemis", "consumer", "--user", "testuser", "--password", "testpassword", "--url", tcpUrl, "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}
	result = ExecOnPod(podName, crName, defaultNamespace, receiveCommand, g)
	g.Expect(result).To(ContainSubstring("Consumed: 1 messages"))
}

func checkMessagingInPod(podName string, crName string, portNumber string, trustStoreLoc string, g Gomega) {
	tcpUrl := "tcp://" + podName + ":" + portNumber + "?sslEnabled=true&forceSSLParameters=true&trustStorePath=" + trustStoreLoc + "&trustStoreType=PEM"
	sendCommand := []string{"amq-broker/bin/artemis", "producer", "--user", "testuser", "--password", "testpassword", "--url", tcpUrl, "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}
	result := ExecOnPod(podName, crName, defaultNamespace, sendCommand, g)
	g.Expect(result).To(ContainSubstring("Produced: 1 messages"))
	receiveCommand := []string{"amq-broker/bin/artemis", "consumer", "--user", "testuser", "--password", "testpassword", "--url", tcpUrl, "--message-count", "1", "--destination", "queue://DLQ", "--verbose"}
	result = ExecOnPod(podName, crName, defaultNamespace, receiveCommand, g)
	g.Expect(result).To(ContainSubstring("Consumed: 1 messages"))
}

func testConfiguredWithCertAndBundle(certSecret string, caSecret string) {
	// UseClientAuth needs an operator-cert
	operatorCert := InstallCert(common.DefaultOperatorCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
		candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
			Name: caIssuer.Name,
			Kind: "ClusterIssuer",
		}
		candidate.Spec.SecretName = common.DefaultOperatorCertSecretName
	})
	Expect(operatorCert).ShouldNot(BeNil())

	// it should use PEM store type
	By("Deploying the broker cr")
	brokerCrName := brokerCrNameBase + "0"
	brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {

		candidate.Name = brokerCrName

		candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
		candidate.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 1,
			PeriodSeconds:       1,
			TimeoutSeconds:      5,
		}
		candidate.Spec.Console.Expose = true
		candidate.Spec.Console.SSLEnabled = true
		candidate.Spec.Console.UseClientAuth = true
		candidate.Spec.Console.SSLSecret = certSecret
		candidate.Spec.Console.TrustSecret = &caSecret
		candidate.Spec.IngressDomain = defaultTestIngressDomain
	})
	pod0Name := createdBrokerCr.Name + "-ss-0"
	By("Checking the broker status reflect the truth")
	Eventually(func(g Gomega) {
		crdRef := types.NamespacedName{
			Namespace: brokerCr.Namespace,
			Name:      brokerCr.Name,
		}
		g.Expect(k8sClient.Get(ctx, crdRef, createdBrokerCr)).Should(Succeed())

		condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.DeployedConditionType)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Status).Should(Equal(metav1.ConditionTrue))

		condition = meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Status).Should(Equal(metav1.ConditionTrue))

	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	UninstallCert(common.DefaultOperatorCertSecretName, defaultNamespace)
	CleanResource(createdBrokerCr, brokerCr.Name, createdBrokerCr.Namespace)

	By("Deploying the broker cr exposing acceptor ssl and connector ssl")
	brokerCrName = brokerCrNameBase + "1"
	pod0Name = brokerCrName + "-ss-0"
	brokerCr, createdBrokerCr = DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {

		candidate.Name = brokerCrName
		candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
		candidate.Spec.IngressDomain = defaultTestIngressDomain
		candidate.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 1,
			PeriodSeconds:       1,
			TimeoutSeconds:      5,
		}
		candidate.Spec.Acceptors = []brokerv1beta1.AcceptorType{{
			Name:        "new-acceptor",
			Port:        62666,
			Protocols:   "all",
			Expose:      true,
			SSLEnabled:  true,
			SSLSecret:   certSecret,
			TrustSecret: &caSecret,
		}}
		candidate.Spec.Connectors = []brokerv1beta1.ConnectorType{{
			Name:        "new-connector",
			Host:        pod0Name,
			Port:        62666,
			Expose:      true,
			SSLEnabled:  true,
			SSLSecret:   certSecret,
			TrustSecret: &caSecret,
		}}
	})

	crdRef := types.NamespacedName{
		Namespace: brokerCr.Namespace,
		Name:      brokerCr.Name,
	}

	By("checking the broker status reflect the truth")

	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, crdRef, createdBrokerCr)).Should(Succeed())

		condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.DeployedConditionType)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Status).Should(Equal(metav1.ConditionTrue))
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	By("checking the broker message send and receive")
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, crdRef, createdBrokerCr)).Should(Succeed())
		checkMessagingInPod(pod0Name, createdBrokerCr.Name, "62666", "/etc/"+common.DefaultOperatorCASecretName+"-volume/"+caPemTrustStoreName, g)
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	By("checking connector parameters")
	Eventually(func(g Gomega) {
		connectorCfg := getConnectorConfig(pod0Name, createdBrokerCr.Name, "new-connector", g)
		g.Expect(connectorCfg).NotTo(BeNil())
		g.Expect(connectorCfg["keyStoreType"]).To(Equal("PEMCFG"))
		g.Expect(connectorCfg["port"]).To(Equal("62666"))
		g.Expect(connectorCfg["sslEnabled"]).To(Equal("true"))
		g.Expect(connectorCfg["host"]).To(Equal(pod0Name))
		g.Expect(connectorCfg["trustStorePath"]).To(Equal("/etc/" + common.DefaultOperatorCASecretName + "-volume/" + caPemTrustStoreName))
		g.Expect(connectorCfg["trustStoreType"]).To(Equal("PEMCA"))
		g.Expect(connectorCfg["keyStorePath"]).To(Equal("/etc/secret-server-cert-secret-pemcfg/" + certSecret + ".pemcfg"))
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	CleanResource(createdBrokerCr, brokerCr.Name, createdBrokerCr.Namespace)
}

func testConsoleAccessWithCert(certSecret string) {
	By("Deploying the broker cr")
	brokerCrName := brokerCrNameBase + "0"
	brokerCr, createdBrokerCr := DeployCustomBroker(defaultNamespace, func(candidate *brokerv1beta1.ActiveMQArtemis) {

		candidate.Name = brokerCrName

		candidate.Spec.DeploymentPlan.Size = common.Int32ToPtr(1)
		candidate.Spec.DeploymentPlan.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 1,
			PeriodSeconds:       1,
			TimeoutSeconds:      5,
		}
		candidate.Spec.Console.Expose = true
		candidate.Spec.Console.SSLEnabled = true
		candidate.Spec.Console.SSLSecret = certSecret
		candidate.Spec.IngressDomain = defaultTestIngressDomain
	})

	By("Checking the broker status reflect the truth")
	Eventually(func(g Gomega) {
		crdRef := types.NamespacedName{
			Namespace: brokerCr.Namespace,
			Name:      brokerCr.Name,
		}
		g.Expect(k8sClient.Get(ctx, crdRef, createdBrokerCr)).Should(Succeed())

		condition := meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.BrokerVersionAlignedConditionType)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Status).Should(Equal(metav1.ConditionTrue))

		condition = meta.FindStatusCondition(createdBrokerCr.Status.Conditions, brokerv1beta1.ConfigAppliedConditionType)
		g.Expect(condition).NotTo(BeNil())
		g.Expect(condition.Status).Should(Equal(metav1.ConditionTrue))
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	CleanResource(createdBrokerCr, brokerCr.Name, createdBrokerCr.Namespace)
}
