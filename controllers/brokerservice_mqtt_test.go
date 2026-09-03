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
	cot "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/chain-of-trust"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/ingresses"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	svc "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/services"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
)

var _ = Describe("broker-service", func() {

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}
	})

	AfterEach(func() {
		AfterEachSpec()
	})

	Context("mqtt round trip simple", func() {

		It("non persistent", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()

			serviceName := NextSpecResourceName()

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

			By("waiting for service to be ready")
			serviceKey := types.NamespacedName{Name: crd.Name, Namespace: crd.Namespace}
			Eventually(func(g Gomega) {
				svc := &brokerv1beta2.BrokerService{}
				g.Expect(k8sClient.Get(ctx, serviceKey, svc)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(svc.Status.Conditions, brokerv1beta2.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deploying app")
			appName := "mqtt-app"
			acceptorIngressHost := serviceName + "-" + defaultNamespace + "." + defaultTestIngressDomain
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

					PKI: &brokerv1beta2.AppPKISpec{
						Leaf: &brokerv1beta2.CertificateTemplate{
							AdditionalDNSNames: []string{acceptorIngressHost},
						},
					},

					Capabilities: []brokerv1beta2.AppCapabilityType{
						{
							ProducerOf: []brokerv1beta2.AddressRef{{Address: "mytopic"}, {Address: "mytopic/A"}, {Address: "mytopic/B"}},

							ConsumerOf: []brokerv1beta2.AddressRef{
								{
									Address:       "mytopic",
									Subscriptions: []string{"my-client.mytopic"},
								},
							},
						},
					},
				},
			}

			appCertName := app.Name + common.AppCertSecretSuffix

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
			acceptorIngress := ingresses.NewIngressForCRWithSSL(nil, types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc"}, nil, serviceName+"-acc", "61616", true, defaultTestIngressDomain, acceptorIngressHost, isOpenshift)
			Expect(k8sClient.Create(ctx, acceptorIngress)).Should(Succeed())

			appCATrustSecret, err := secrets.RetriveSecret(types.NamespacedName{Namespace: defaultNamespace, Name: appName + "-ca-trust"}, make(map[string]string), k8sClient)
			Expect(err).Should(BeNil())

			certpool := x509.NewCertPool()
			certpool.AppendCertsFromPEM(appCATrustSecret.Data["tls.crt"])

			appCertNameSecret, err := secrets.RetriveSecret(types.NamespacedName{Namespace: defaultNamespace, Name: appCertName}, make(map[string]string), k8sClient)
			Expect(err).Should(BeNil())

			clientKeyPair, err := tls.X509KeyPair(appCertNameSecret.Data["tls.crt"], appCertNameSecret.Data["tls.key"])
			Expect(err).Should(BeNil())

			time.Sleep(20 * time.Second)

			tlsConfig := &tls.Config{RootCAs: certpool, Certificates: []tls.Certificate{clientKeyPair}, ServerName: acceptorIngressHost}

			opts := mqtt.NewClientOptions()
			opts.AddBroker("ssl://" + clusterIngressHost + ":443")
			opts.SetClientID("my-client")
			opts.SetTLSConfig(tlsConfig)
			opts.SetKeepAlive(30)

			opts.OnConnect = func(c mqtt.Client) {
				fmt.Println("Successfully connected to the broker!")
			}

			messageReceived := false
			messageHandler := func(client mqtt.Client, msg mqtt.Message) {
				messageReceived = true
				fmt.Printf("Received message: '%s' from topic: %s\n", msg.Payload(), msg.Topic())
			}

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
				body := scrapeMetrics(g, serviceName, serverName, defaultNamespace)
				g.Expect(body).Should(MatchRegexp(`broker_queue_message_count.*queue="my-client\.mytopic"`), "should have MessageCount")
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			client.Disconnect(250)

			By("removing acceptor ingress")
			Expect(k8sClient.Delete(ctx, acceptorIngress)).Should(Succeed())

			By("removing acceptor service")
			Expect(k8sClient.Delete(ctx, acceptorService)).Should(Succeed())

			By("removing app and waiting for finalizer")
			Expect(k8sClient.Delete(ctx, createdApp)).Should(Succeed())
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name: createdApp.Name, Namespace: createdApp.Namespace,
				}, &brokerv1beta2.BrokerApp{}))
			}, existingClusterTimeout, existingClusterInterval).Should(BeTrue())

			By("tidy up service and waiting for finalizer")
			Expect(k8sClient.Delete(ctx, &crd)).Should(Succeed())
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name: crd.Name, Namespace: crd.Namespace,
				}, &brokerv1beta2.BrokerService{}))
			}, existingClusterTimeout, existingClusterInterval).Should(BeTrue())

		})
	})

	Context("multi-tenant metrics isolation", func() {

		It("each app sees only its own queue metrics", func() {

			if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
				return
			}

			ctx := context.Background()
			serviceName := NextSpecResourceName()

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

			By("Deploying the BrokerService")
			Expect(k8sClient.Create(ctx, &crd)).Should(Succeed())

			alphaIngressHost := "alpha-" + serviceName + "-" + defaultNamespace + "." + defaultTestIngressDomain
			betaIngressHost := "beta-" + serviceName + "-" + defaultNamespace + "." + defaultTestIngressDomain

			By("deploying app-alpha (produces/consumes on alpha-topic)")
			appAlpha := brokerv1beta2.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemisApp",
					APIVersion: brokerv1beta2.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-alpha",
					Namespace: defaultNamespace,
				},
				Spec: brokerv1beta2.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"forMQTTMultiTenant": "true"},
					},
					PKI: &brokerv1beta2.AppPKISpec{
						Leaf: &brokerv1beta2.CertificateTemplate{
							AdditionalDNSNames: []string{alphaIngressHost, betaIngressHost},
						},
					},
					Capabilities: []brokerv1beta2.AppCapabilityType{{
						ProducerOf: []brokerv1beta2.AddressRef{{Address: "alpha-topic"}},
						ConsumerOf: []brokerv1beta2.AddressRef{{
							Address:       "alpha-topic",
							Subscriptions: []string{"alpha-client.alpha-topic"},
						}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, &appAlpha)).Should(Succeed())

			By("deploying app-beta (produces/consumes on beta-topic)")
			appBeta := brokerv1beta2.BrokerApp{
				TypeMeta: metav1.TypeMeta{
					Kind:       "ActiveMQArtemisApp",
					APIVersion: brokerv1beta2.GroupVersion.Identifier(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "app-beta",
					Namespace: defaultNamespace,
				},
				Spec: brokerv1beta2.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"forMQTTMultiTenant": "true"},
					},
					PKI: &brokerv1beta2.AppPKISpec{
						Leaf: &brokerv1beta2.CertificateTemplate{
							AdditionalDNSNames: []string{alphaIngressHost, betaIngressHost},
						},
					},
					Capabilities: []brokerv1beta2.AppCapabilityType{{
						ProducerOf: []brokerv1beta2.AddressRef{{Address: "beta-topic"}},
						ConsumerOf: []brokerv1beta2.AddressRef{{
							Address:       "beta-topic",
							Subscriptions: []string{"beta-client.beta-topic"},
						}},
					}},
				},
			}
			Expect(k8sClient.Create(ctx, &appBeta)).Should(Succeed())

			By("waiting for both apps to be Ready")
			for _, name := range []string{"app-alpha", "app-beta"} {
				key := types.NamespacedName{Name: name, Namespace: defaultNamespace}
				Eventually(func(g Gomega) {
					app := &brokerv1beta2.BrokerApp{}
					g.Expect(k8sClient.Get(ctx, key, app)).Should(Succeed())
					g.Expect(meta.IsStatusConditionTrue(app.Status.Conditions, brokerv1beta2.ReadyConditionType)).Should(BeTrue())
				}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			}

			By("reading assigned ports from app status")
			alphaApp := &brokerv1beta2.BrokerApp{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "app-alpha", Namespace: defaultNamespace}, alphaApp)).Should(Succeed())
			alphaPort := alphaApp.Status.Service.AssignedPort
			fmt.Printf("app-alpha assigned port: %d\n", alphaPort)

			betaApp := &brokerv1beta2.BrokerApp{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "app-beta", Namespace: defaultNamespace}, betaApp)).Should(Succeed())
			betaPort := betaApp.Status.Service.AssignedPort
			fmt.Printf("app-beta assigned port: %d\n", betaPort)

			By("creating per-app acceptor services + ingresses")
			alphaAccSvc := svc.NewServiceDefinitionForCR(
				types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc-alpha"},
				k8sClient, "acc-port", alphaPort,
				map[string]string{"ActiveMQArtemis": crd.Name}, nil, nil)
			Expect(k8sClient.Create(ctx, alphaAccSvc)).Should(Succeed())

			alphaAccIng := ingresses.NewIngressForCRWithSSL(nil,
				types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc-alpha"},
				nil, serviceName+"-acc-alpha", fmt.Sprintf("%d", alphaPort), true,
				defaultTestIngressDomain, alphaIngressHost, isOpenshift)
			Expect(k8sClient.Create(ctx, alphaAccIng)).Should(Succeed())

			betaAccSvc := svc.NewServiceDefinitionForCR(
				types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc-beta"},
				k8sClient, "acc-port", betaPort,
				map[string]string{"ActiveMQArtemis": crd.Name}, nil, nil)
			Expect(k8sClient.Create(ctx, betaAccSvc)).Should(Succeed())

			betaAccIng := ingresses.NewIngressForCRWithSSL(nil,
				types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc-beta"},
				nil, serviceName+"-acc-beta", fmt.Sprintf("%d", betaPort), true,
				defaultTestIngressDomain, betaIngressHost, isOpenshift)
			Expect(k8sClient.Create(ctx, betaAccIng)).Should(Succeed())

			alphaCATrust, err := secrets.RetriveSecret(
				types.NamespacedName{Namespace: defaultNamespace, Name: "app-alpha-ca-trust"},
				make(map[string]string), k8sClient)
			Expect(err).Should(BeNil())

			betaCATrust, err := secrets.RetriveSecret(
				types.NamespacedName{Namespace: defaultNamespace, Name: "app-beta-ca-trust"},
				make(map[string]string), k8sClient)
			Expect(err).Should(BeNil())

			// Both apps may share the same acceptor port, so the broker serves
			// one server cert signed by one app CA. Each client must trust both
			// CAs to verify the server regardless of which cert is presented.
			sharedPool := x509.NewCertPool()
			sharedPool.AppendCertsFromPEM(alphaCATrust.Data["tls.crt"])
			sharedPool.AppendCertsFromPEM(betaCATrust.Data["tls.crt"])

			time.Sleep(20 * time.Second)

			By("app-alpha: MQTT pub/sub on alpha-topic")
			alphaCertSecret, err := secrets.RetriveSecret(
				types.NamespacedName{Namespace: defaultNamespace, Name: "app-alpha" + common.AppCertSecretSuffix},
				make(map[string]string), k8sClient)
			Expect(err).Should(BeNil())
			alphaKeyPair, err := tls.X509KeyPair(alphaCertSecret.Data["tls.crt"], alphaCertSecret.Data["tls.key"])
			Expect(err).Should(BeNil())

			alphaReceived := false
			alphaClient := mqtt.NewClient(mqtt.NewClientOptions().
				AddBroker("ssl://" + clusterIngressHost + ":443").
				SetClientID("alpha-client").
				SetTLSConfig(&tls.Config{
					RootCAs:      sharedPool,
					Certificates: []tls.Certificate{alphaKeyPair},
					ServerName:   alphaIngressHost,
				}).
				SetKeepAlive(30))

			if token := alphaClient.Connect(); token.Wait() && token.Error() != nil {
				Fail(fmt.Sprintf("alpha connect failed: %v", token.Error()))
			}

			token := alphaClient.Subscribe("alpha-topic", 1, func(_ mqtt.Client, msg mqtt.Message) {
				alphaReceived = true
				fmt.Printf("alpha received: '%s' on %s\n", msg.Payload(), msg.Topic())
			})
			if token.Wait() && token.Error() != nil {
				Fail(fmt.Sprintf("alpha subscribe failed: %v", token.Error()))
			}

			token = alphaClient.Publish("alpha-topic", 0, false, "hello from alpha")
			if token.Wait() && token.Error() != nil {
				Fail(fmt.Sprintf("alpha publish failed: %v", token.Error()))
			}

			Eventually(func() bool { return alphaReceived }, existingClusterTimeout, existingClusterInterval).Should(BeTrue())

			By("app-beta: MQTT pub/sub on beta-topic")
			betaCertSecret, err := secrets.RetriveSecret(
				types.NamespacedName{Namespace: defaultNamespace, Name: "app-beta" + common.AppCertSecretSuffix},
				make(map[string]string), k8sClient)
			Expect(err).Should(BeNil())
			betaKeyPair, err := tls.X509KeyPair(betaCertSecret.Data["tls.crt"], betaCertSecret.Data["tls.key"])
			Expect(err).Should(BeNil())

			betaReceived := false
			betaClient := mqtt.NewClient(mqtt.NewClientOptions().
				AddBroker("ssl://" + clusterIngressHost + ":443").
				SetClientID("beta-client").
				SetTLSConfig(&tls.Config{
					RootCAs:      sharedPool,
					Certificates: []tls.Certificate{betaKeyPair},
					ServerName:   betaIngressHost,
				}).
				SetKeepAlive(30))

			if token := betaClient.Connect(); token.Wait() && token.Error() != nil {
				Fail(fmt.Sprintf("beta connect failed: %v", token.Error()))
			}

			token = betaClient.Subscribe("beta-topic", 1, func(_ mqtt.Client, msg mqtt.Message) {
				betaReceived = true
				fmt.Printf("beta received: '%s' on %s\n", msg.Payload(), msg.Topic())
			})
			if token.Wait() && token.Error() != nil {
				Fail(fmt.Sprintf("beta subscribe failed: %v", token.Error()))
			}

			token = betaClient.Publish("beta-topic", 0, false, "hello from beta")
			if token.Wait() && token.Error() != nil {
				Fail(fmt.Sprintf("beta publish failed: %v", token.Error()))
			}

			Eventually(func() bool { return betaReceived }, existingClusterTimeout, existingClusterInterval).Should(BeTrue())

			serverName := common.OrdinalFQDNS(serviceName, defaultNamespace, 0)

			By("app-alpha scrapes metrics with its own metrics cert")
			Eventually(func(g Gomega) {
				body := scrapeMetricsWithAppCert(g, serviceName, serverName, defaultNamespace, "app-alpha")

				g.Expect(body).Should(MatchRegexp(`broker_queue_message_count.*queue="alpha-client\.alpha-topic"`),
					"alpha should see its own queue metrics")
				g.Expect(body).ShouldNot(MatchRegexp(`queue="beta-client\.beta-topic"`),
					"alpha must NOT see beta's queue metrics")
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("app-beta scrapes metrics with its own metrics cert")
			Eventually(func(g Gomega) {
				body := scrapeMetricsWithAppCert(g, serviceName, serverName, defaultNamespace, "app-beta")

				g.Expect(body).Should(MatchRegexp(`broker_queue_message_count.*queue="beta-client\.beta-topic"`),
					"beta should see its own queue metrics")
				g.Expect(body).ShouldNot(MatchRegexp(`queue="alpha-client\.alpha-topic"`),
					"beta must NOT see alpha's queue metrics")
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
		})
	})

})

// scrapeMetrics scrapes the prometheus endpoint using the service-level prometheus cert.
func scrapeMetrics(g Gomega, serviceName, serverName, namespace string) string {
	return scrapeMetricsWithCert(g, serviceName, serverName, namespace,
		cot.PrometheusCertName(serviceName))
}

// scrapeMetricsWithAppCert scrapes the prometheus endpoint using the per-app metrics cert.
func scrapeMetricsWithAppCert(g Gomega, serviceName, serverName, namespace, appName string) string {
	return scrapeMetricsWithCert(g, serviceName, serverName, namespace,
		cot.MetricsCertName(appName))
}

func scrapeMetricsWithCert(g Gomega, serviceName, serverName, namespace, clientCertSecretName string) string {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	httpClient := http.Client{
		Transport: transport,
		Timeout:   time.Second * 5,
	}

	metricsRootSecret, err := secrets.RetriveSecret(
		types.NamespacedName{Namespace: namespace, Name: cot.MetricsRootCertSecretName(serviceName)},
		make(map[string]string), k8sClient)
	g.Expect(err).Should(BeNil())
	metricsPool := x509.NewCertPool()
	metricsPool.AppendCertsFromPEM(metricsRootSecret.Data["tls.crt"])

	clientCertSecret, err := secrets.RetriveSecret(
		types.NamespacedName{Namespace: namespace, Name: clientCertSecretName},
		make(map[string]string), k8sClient)
	g.Expect(err).Should(BeNil())
	clientKeyPair, err := tls.X509KeyPair(clientCertSecret.Data["tls.crt"], clientCertSecret.Data["tls.key"])
	g.Expect(err).Should(BeNil())

	transport.TLSClientConfig = &tls.Config{
		ServerName:   serverName,
		RootCAs:      metricsPool,
		Certificates: []tls.Certificate{clientKeyPair},
	}

	resp, err := httpClient.Get("https://" + serverName + ":8888/metrics")
	g.Expect(err).Should(Succeed())
	g.Expect(resp).ShouldNot(BeNil())

	fmt.Printf("Prometheus metrics scrape (%s): status=%d\n", clientCertSecretName, resp.StatusCode)
	g.Expect(resp.StatusCode).Should(Equal(200))

	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	g.Expect(err).Should(Succeed())

	bodyStr := string(body)
	if verbose {
		fmt.Printf("Metrics response (%s, first 5000 chars):\n%s\n",
			clientCertSecretName, bodyStr[:min(5000, len(bodyStr))])
	}
	return bodyStr
}
