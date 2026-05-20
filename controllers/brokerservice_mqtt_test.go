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
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/resources/ingresses"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/resources/secrets"
	svc "github.com/arkmq-org/arkmq-org-broker-operator/pkg/resources/services"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
)

var _ = Describe("broker-service", func() {

	var installedCertManager bool = false

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}

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

	Context("mqtt round trip simple", func() {

		It("non persistent", func() {

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
				candidate.Spec.DNSNames = []string{serviceName, common.ClusterDNSWildCard(serviceName, defaultNamespace)}
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			crd := brokerv1beta2.BrokerService{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemisService",
					APIVersion: brokerv1beta2.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      serviceName,
					Namespace: defaultNamespace,
					Labels:    map[string]string{"forMQTT": "true"},
				},
				Spec: brokerv1beta2.BrokerServiceSpec{},
			}

			crd.Spec.Resources = corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			}

			By("Deploying the CRD " + crd.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			By("deploying app")
			appName := "mqtt-app"
			app := brokerv1beta2.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemisApp",
					APIVersion: brokerv1beta2.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: defaultNamespace,
				},
				Spec: brokerv1beta2.BrokerAppSpec{

					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"forMQTT": "true",
						}},

					Capabilities: []brokerv1beta2.AppCapabilityType{
						{
							ProducerOf: []brokerv1beta2.AddressRef{{Address: "mytopic"}, {Address: "mytopic/A"}, {Address: "mytopic/B"}},

							ConsumerOf: []brokerv1beta2.AddressRef{
								{
									Address:       "mytopic",
									Subscriptions: &[]string{"my-client.mytopic"},

									// no support in the broker for liternal matches yet in security settings
									// {Address: "mytopic.*::my-client.mytopic.*"},
								},
							},
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

			By("Deploying the App " + app.ObjectMeta.Name)
			Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

			By("verify app status")
			appKey := types.NamespacedName{Name: app.Name, Namespace: crd.Namespace}
			createdApp := &brokerv1beta2.BrokerApp{}

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())

				if verbose {
					fmt.Printf("App STATUS: %v\n\n", createdApp.Status.Conditions)
				}
				g.Expect(meta.IsStatusConditionTrue(createdApp.Status.Conditions, brokerv1beta2.ReadyConditionType)).Should(BeTrue())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			acceptorService := svc.NewServiceDefinitionForCR(types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc"}, k8sClient, "acc-port", 61616, map[string]string{"ActiveMQArtemis": crd.Name}, nil, nil)
			Expect(k8sClient.Create(ctx, acceptorService)).Should(Succeed())
			acceptorIngressHost := serviceName + "-" + defaultNamespace + "." + defaultTestIngressDomain
			acceptorIngress := ingresses.NewIngressForCRWithSSL(nil, types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc"}, nil, serviceName+"-acc", "61616", true, defaultTestIngressDomain, acceptorIngressHost, isOpenshift)
			Expect(k8sClient.Create(ctx, acceptorIngress)).Should(Succeed())

			sharedOperandCertNameSecret, err := secrets.RetriveSecret(types.NamespacedName{Namespace: defaultNamespace, Name: sharedOperandCertName}, make(map[string]string), k8sClient)
			Expect(err).Should(BeNil())

			certpool := x509.NewCertPool()
			certpool.AppendCertsFromPEM(sharedOperandCertNameSecret.Data["tls.crt"])

			appCertNameSecret, err := secrets.RetriveSecret(types.NamespacedName{Namespace: defaultNamespace, Name: appCertName}, make(map[string]string), k8sClient)
			Expect(err).Should(BeNil())

			clientKeyPair, err := tls.X509KeyPair(appCertNameSecret.Data["tls.crt"], appCertNameSecret.Data["tls.key"])
			Expect(err).Should(BeNil())

			time.Sleep(20 * time.Second)

			tlsConfig := &tls.Config{RootCAs: certpool, Certificates: []tls.Certificate{clientKeyPair}, ServerName: acceptorIngressHost, InsecureSkipVerify: true}

			opts := mqtt.NewClientOptions()
			opts.AddBroker("ssl://" + clusterIngressHost + ":443")
			opts.SetClientID("my-client")
			opts.SetTLSConfig(tlsConfig)
			opts.SetKeepAlive(30)

			// Define the onConnect handler
			opts.OnConnect = func(c mqtt.Client) {
				fmt.Println("Successfully connected to the broker!")
			}

			messageReceived := false
			messageHandler := func(client mqtt.Client, msg mqtt.Message) {
				messageReceived = true
				fmt.Printf("Received message: '%s' from topic: %s\n", msg.Payload(), msg.Topic())
			}

			// Create and connect the client
			client := mqtt.NewClient(opts)

			log.Printf("mqtt client: %v", client)

			if token := client.Connect(); token.Wait() && token.Error() != nil {
				log.Printf("mqtt token: %v", token)

				log.Fatalf("Failed to connect to broker: %v", token.Error())
			}

			if token := client.Subscribe("mytopic", 1, messageHandler); token.Wait() && token.Error() != nil {
				log.Fatalf("Failed to subscribe to topic: %v", token.Error())
			}

			text := "Hello MQTT from Go!"
			if token := client.Publish("mytopic", 0, false, text); token.Wait() && token.Error() != nil {
				log.Fatalf("Failed to publish to topic: %v", token.Error())
			}

			Eventually(func(g Gomega) {
				g.Expect(messageReceived).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("scraping prometheus metrics")
			serverName := common.OrdinalFQDNS(serviceName, defaultNamespace, 0)

			Eventually(func(g Gomega) {
				transport := http.DefaultTransport.(*http.Transport).Clone()
				httpClient := http.Client{
					Transport: transport,
					Timeout:   time.Second * 5,
				}

				httpClientTransport := httpClient.Transport.(*http.Transport)
				httpClientTransport.TLSClientConfig = &tls.Config{
					ServerName:         serverName,
					InsecureSkipVerify: false,
				}
				httpClientTransport.TLSClientConfig.GetClientCertificate =
					func(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
						return common.GetOperatorClientCertificate(k8sClient, cri)
					}

				if rootCas, err := common.GetRootCAs(k8sClient); err == nil {
					httpClientTransport.TLSClientConfig.RootCAs = rootCas
				}

				resp, err := httpClient.Get("https://" + serverName + ":8888/metrics")
				g.Expect(err).Should(Succeed())

				if resp != nil {
					fmt.Printf("Prometheus metrics scrape: status=%d\n", resp.StatusCode)
					g.Expect(resp.StatusCode).Should(Equal(200))

					defer resp.Body.Close()
					body, err := io.ReadAll(resp.Body)
					g.Expect(err).Should(Succeed())

					bodyStr := string(body)
					if verbose {
						fmt.Printf("Metrics response (first 20000 chars):\n%s\n", bodyStr[:min(20000, len(bodyStr))])
					}

					g.Expect(bodyStr).Should(MatchRegexp(`broker_queue_message_count.*queue="my-client\.mytopic"`), "should have MessageCount")

				}

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// Disconnect
			client.Disconnect(250)

			By("removing acceptor ingress")
			Expect(k8sClient.Delete(ctx, acceptorIngress)).Should(Succeed())

			By("removing acceptor service")
			Expect(k8sClient.Delete(ctx, acceptorService)).Should(Succeed())

			By("removing app")
			Expect(k8sClient.Delete(ctx, createdApp)).Should(Succeed())

			By("tidy up")
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())

			UninstallCert(appCertName, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})
	})

})
