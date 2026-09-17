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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/ingresses"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	svc "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/services"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
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
									Subscriptions: []string{"my-client.mytopic"},

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

			acceptorService := svc.NewServiceDefinitionForCR(types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc"}, k8sClient, "acc-port", 61616, map[string]string{selectors.LabelBrokerKey: crd.Name}, nil, nil)
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
				bodyStr := scrapeMetrics(g, serverName, operatorClientCert)

				g.Expect(bodyStr).Should(MatchRegexp(`broker_queue_message_count.*queue="my-client\.mytopic"`), "should have MessageCount")

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

	Context("multi-tenant metrics isolation", func() {

		It("each app sees only its own queue metrics", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

			// One server identity for the whole service: both app acceptors
			// present this cert, so clients verify against it regardless of
			// which port they land on.
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
					Labels:    map[string]string{"forMQTTMultiTenant": "true"},
				},
				Spec: brokerv1beta2.BrokerServiceSpec{},
			}
			crd.Spec.Resources = corev1.ResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
				},
			}

			By("Deploying the BrokerService " + crd.Name)
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			alphaIngressHost := "alpha-" + serviceName + "-" + defaultNamespace + "." + defaultTestIngressDomain
			betaIngressHost := "beta-" + serviceName + "-" + defaultNamespace + "." + defaultTestIngressDomain

			newApp := func(name, topic, subscription string) brokerv1beta2.BrokerApp {
				return brokerv1beta2.BrokerApp{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ActiveMQArtemisApp",
						APIVersion: brokerv1beta2.GroupVersion.Identifier(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: defaultNamespace,
					},
					Spec: brokerv1beta2.BrokerAppSpec{
						ServiceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"forMQTTMultiTenant": "true"},
						},
						Capabilities: []brokerv1beta2.AppCapabilityType{{
							ProducerOf: []brokerv1beta2.AddressRef{{Address: topic}},
							ConsumerOf: []brokerv1beta2.AddressRef{{
								Address:       topic,
								Subscriptions: []string{subscription},
							}},
						}},
					},
				}
			}

			appAlpha := newApp("app-alpha", "alpha-topic", "alpha-client.alpha-topic")
			appBeta := newApp("app-beta", "beta-topic", "beta-client.beta-topic")

			By("installing per-app client certs")
			for _, appName := range []string{appAlpha.Name, appBeta.Name} {
				certName := appName + common.AppCertSecretSuffix
				InstallCert(certName, defaultNamespace, func(candidate *cmv1.Certificate) {
					candidate.Spec.SecretName = certName
					candidate.Spec.CommonName = appName
					candidate.Spec.Subject.Organizations = nil
					candidate.Spec.Subject.OrganizationalUnits = []string{defaultNamespace}
					candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
						Name: caIssuer.Name,
						Kind: "ClusterIssuer",
					}
				})
			}

			By("deploying app-alpha (produces/consumes on alpha-topic)")
			Expect(k8sClient.Create(ctx, &appAlpha)).Should(Succeed())

			By("deploying app-beta (produces/consumes on beta-topic)")
			Expect(k8sClient.Create(ctx, &appBeta)).Should(Succeed())

			By("waiting for both apps to be Ready")
			for _, name := range []string{appAlpha.Name, appBeta.Name} {
				key := types.NamespacedName{Name: name, Namespace: defaultNamespace}
				Eventually(func(g Gomega) {
					app := &brokerv1beta2.BrokerApp{}
					g.Expect(k8sClient.Get(ctx, key, app)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(app.Status.Conditions, brokerv1beta2.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			By("reading assigned ports from app status")
			alphaApp := &brokerv1beta2.BrokerApp{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appAlpha.Name, Namespace: defaultNamespace}, alphaApp)).Should(Succeed())
			alphaPort := alphaApp.Status.Service.AssignedPort
			fmt.Printf("app-alpha assigned port: %d\n", alphaPort)

			betaApp := &brokerv1beta2.BrokerApp{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appBeta.Name, Namespace: defaultNamespace}, betaApp)).Should(Succeed())
			betaPort := betaApp.Status.Service.AssignedPort
			fmt.Printf("app-beta assigned port: %d\n", betaPort)

			By("creating per-app acceptor services + ingresses")
			alphaAccSvc := svc.NewServiceDefinitionForCR(
				types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc-alpha"},
				k8sClient, "acc-port", alphaPort,
				map[string]string{selectors.LabelBrokerKey: crd.Name}, nil, nil)
			Expect(k8sClient.Create(ctx, alphaAccSvc)).Should(Succeed())

			alphaAccIng := ingresses.NewIngressForCRWithSSL(nil,
				types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc-alpha"},
				nil, serviceName+"-acc-alpha", fmt.Sprintf("%d", alphaPort), true,
				defaultTestIngressDomain, alphaIngressHost, isOpenshift)
			Expect(k8sClient.Create(ctx, alphaAccIng)).Should(Succeed())

			betaAccSvc := svc.NewServiceDefinitionForCR(
				types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc-beta"},
				k8sClient, "acc-port", betaPort,
				map[string]string{selectors.LabelBrokerKey: crd.Name}, nil, nil)
			Expect(k8sClient.Create(ctx, betaAccSvc)).Should(Succeed())

			betaAccIng := ingresses.NewIngressForCRWithSSL(nil,
				types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc-beta"},
				nil, serviceName+"-acc-beta", fmt.Sprintf("%d", betaPort), true,
				defaultTestIngressDomain, betaIngressHost, isOpenshift)
			Expect(k8sClient.Create(ctx, betaAccIng)).Should(Succeed())

			operandSecret, err := secrets.RetriveSecret(
				types.NamespacedName{Namespace: defaultNamespace, Name: sharedOperandCertName},
				make(map[string]string), k8sClient)
			Expect(err).Should(BeNil())
			certpool := x509.NewCertPool()
			certpool.AppendCertsFromPEM(operandSecret.Data["tls.crt"])

			// Retry rather than sleep: an app being Ready means the service wrote
			// the app-props secret, not that the broker has reloaded it. Connect
			// before that and the acceptor answers but the app is not yet in its
			// realm's cert_users, so the broker rejects the cert and the client
			// reports "not Authorized". There is no condition to wait on for the
			// reload, so retry until it accepts us -- a fixed delay is a fluke
			// away from failing on a slower machine, which is how this broke.
			connectMqtt := func(appName, clientID, ingressHost string) (mqtt.Client, *bool) {
				certSecret, err := secrets.RetriveSecret(
					types.NamespacedName{Namespace: defaultNamespace, Name: appName + common.AppCertSecretSuffix},
					make(map[string]string), k8sClient)
				Expect(err).Should(BeNil())
				keyPair, err := tls.X509KeyPair(certSecret.Data["tls.crt"], certSecret.Data["tls.key"])
				Expect(err).Should(BeNil())

				received := false
				var client mqtt.Client

				Eventually(func(g Gomega) {
					candidate := mqtt.NewClient(mqtt.NewClientOptions().
						AddBroker("ssl://" + clusterIngressHost + ":443").
						SetClientID(clientID).
						SetTLSConfig(&tls.Config{
							RootCAs:            certpool,
							Certificates:       []tls.Certificate{keyPair},
							ServerName:         ingressHost,
							InsecureSkipVerify: true,
						}).
						SetKeepAlive(30))

					token := candidate.Connect()
					g.Expect(token.WaitTimeout(10*time.Second)).Should(BeTrue(), "%s connect timed out", appName)
					g.Expect(token.Error()).Should(Succeed(), "%s connect failed", appName)
					client = candidate
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

				return client, &received
			}

			By("app-alpha: MQTT pub/sub on alpha-topic")
			alphaClient, alphaReceived := connectMqtt(appAlpha.Name, "alpha-client", alphaIngressHost)
			token := alphaClient.Subscribe("alpha-topic", 1, func(_ mqtt.Client, msg mqtt.Message) {
				*alphaReceived = true
				fmt.Printf("alpha received: '%s' on %s\n", msg.Payload(), msg.Topic())
			})
			if token.Wait() && token.Error() != nil {
				Fail(fmt.Sprintf("alpha subscribe failed: %v", token.Error()))
			}
			token = alphaClient.Publish("alpha-topic", 0, false, "hello from alpha")
			if token.Wait() && token.Error() != nil {
				Fail(fmt.Sprintf("alpha publish failed: %v", token.Error()))
			}
			Eventually(func() bool { return *alphaReceived }, existingClusterTimeout, existingClusterInterval).Should(BeTrue())

			By("app-beta: MQTT pub/sub on beta-topic")
			betaClient, betaReceived := connectMqtt(appBeta.Name, "beta-client", betaIngressHost)
			token = betaClient.Subscribe("beta-topic", 1, func(_ mqtt.Client, msg mqtt.Message) {
				*betaReceived = true
				fmt.Printf("beta received: '%s' on %s\n", msg.Payload(), msg.Topic())
			})
			if token.Wait() && token.Error() != nil {
				Fail(fmt.Sprintf("beta subscribe failed: %v", token.Error()))
			}
			token = betaClient.Publish("beta-topic", 0, false, "hello from beta")
			if token.Wait() && token.Error() != nil {
				Fail(fmt.Sprintf("beta publish failed: %v", token.Error()))
			}
			Eventually(func() bool { return *betaReceived }, existingClusterTimeout, existingClusterInterval).Should(BeTrue())

			serverName := common.OrdinalFQDNS(serviceName, defaultNamespace, 0)

			// Same reason the connect retries: the app's identity only reaches the
			// metrics endpoint once the broker has reloaded the override.
			By("app-alpha scrapes metrics with its own app cert")
			Eventually(func(g Gomega) {
				body := scrapeMetrics(g, serverName, appClientCert(g, defaultNamespace, appAlpha.Name))

				g.Expect(body).Should(MatchRegexp(`broker_queue_message_count.*queue="alpha-client\.alpha-topic"`),
					"alpha should see its own queue metrics")
				g.Expect(body).ShouldNot(MatchRegexp(`queue="beta-client\.beta-topic"`),
					"alpha must NOT see beta's queue metrics")
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("app-beta scrapes metrics with its own app cert")
			Eventually(func(g Gomega) {
				body := scrapeMetrics(g, serverName, appClientCert(g, defaultNamespace, appBeta.Name))

				g.Expect(body).Should(MatchRegexp(`broker_queue_message_count.*queue="beta-client\.beta-topic"`),
					"beta should see its own queue metrics")
				g.Expect(body).ShouldNot(MatchRegexp(`queue="alpha-client\.alpha-topic"`),
					"beta must NOT see alpha's queue metrics")
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("the operator still sees every app's queue metrics")
			Eventually(func(g Gomega) {
				body := scrapeMetrics(g, serverName, operatorClientCert)

				g.Expect(body).Should(MatchRegexp(`broker_queue_message_count.*queue="alpha-client\.alpha-topic"`))
				g.Expect(body).Should(MatchRegexp(`broker_queue_message_count.*queue="beta-client\.beta-topic"`))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			alphaClient.Disconnect(250)
			betaClient.Disconnect(250)

			By("removing acceptor ingresses")
			Expect(k8sClient.Delete(ctx, alphaAccIng)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, betaAccIng)).Should(Succeed())

			By("removing acceptor services")
			Expect(k8sClient.Delete(ctx, alphaAccSvc)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, betaAccSvc)).Should(Succeed())

			By("removing apps and waiting for finalizers")
			Expect(k8sClient.Delete(ctx, &appAlpha)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, &appBeta)).Should(Succeed())
			for _, key := range []types.NamespacedName{
				{Name: appAlpha.Name, Namespace: appAlpha.Namespace},
				{Name: appBeta.Name, Namespace: appBeta.Namespace},
			} {
				Eventually(func() bool {
					return errors.IsNotFound(k8sClient.Get(ctx, key, &brokerv1beta2.BrokerApp{}))
				}, existingClusterTimeout, existingClusterInterval).Should(BeTrue())
			}

			By("tidy up service and waiting for finalizer")
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name: crd.Name, Namespace: crd.Namespace,
				}, &brokerv1beta2.BrokerService{}))
			}, existingClusterTimeout, existingClusterInterval).Should(BeTrue())

			UninstallCert(appAlpha.Name+common.AppCertSecretSuffix, defaultNamespace)
			UninstallCert(appBeta.Name+common.AppCertSecretSuffix, defaultNamespace)
			UninstallCert(sharedOperandCertName, defaultNamespace)
		})
	})

})

// scrapeMetrics scrapes the broker's prometheus endpoint on :8888 presenting
// the client certificate supplied by getClientCert, and returns the body.
func scrapeMetrics(g Gomega, serverName string, getClientCert func(*tls.CertificateRequestInfo) (*tls.Certificate, error)) string {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	httpClient := http.Client{
		Transport: transport,
		Timeout:   time.Second * 5,
	}

	transport.TLSClientConfig = &tls.Config{
		ServerName:           serverName,
		InsecureSkipVerify:   false,
		GetClientCertificate: getClientCert,
	}
	if rootCas, err := common.GetRootCAs(k8sClient); err == nil {
		transport.TLSClientConfig.RootCAs = rootCas
	}

	resp, err := httpClient.Get("https://" + serverName + ":8888/metrics")
	g.Expect(err).Should(Succeed())
	g.Expect(resp).ShouldNot(BeNil())
	defer func() { _ = resp.Body.Close() }()

	fmt.Printf("Prometheus metrics scrape: status=%d\n", resp.StatusCode)
	g.Expect(resp.StatusCode).Should(Equal(200))

	body, err := io.ReadAll(resp.Body)
	g.Expect(err).Should(Succeed())

	bodyStr := string(body)
	if verbose {
		fmt.Printf("Metrics response (first 20000 chars):\n%s\n", bodyStr[:min(20000, len(bodyStr))])
	}
	return bodyStr
}

// operatorClientCert presents the operator's client certificate, which is a
// member of the broad "metrics" role and therefore sees every app's queues.
func operatorClientCert(cri *tls.CertificateRequestInfo) (*tls.Certificate, error) {
	return common.GetOperatorClientCertificate(k8sClient, cri)
}

// appClientCert presents an app's own <app>-app-cert, whose identity is scoped
// to that app's metrics role and therefore only sees that app's queues.
func appClientCert(g Gomega, namespace, appName string) func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
	certSecret, err := secrets.RetriveSecret(
		types.NamespacedName{Namespace: namespace, Name: appName + common.AppCertSecretSuffix},
		make(map[string]string), k8sClient)
	g.Expect(err).Should(BeNil())

	keyPair, err := tls.X509KeyPair(certSecret.Data["tls.crt"], certSecret.Data["tls.key"])
	g.Expect(err).Should(BeNil())

	return func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
		return &keyPair, nil
	}
}
