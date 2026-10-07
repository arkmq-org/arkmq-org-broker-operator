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
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"

	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/monitoring"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/ingresses"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	svc "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/services"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
)

var _ = Describe("broker-service mqtt", func() {

	BeforeEach(func() {
		BeforeEachSpec()

		if verbose {
			fmt.Println("Time with MicroSeconds: ", time.Now().Format("2006-01-02 15:04:05.000000"), " test:", CurrentSpecReport())
		}

		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			//if cert manager/trust manager is not installed, install it
			if !CertManagerInstalled() {
				Expect(InstallCertManager()).To(Succeed())
			}

			// required, not optional: the operator generates scrape wiring for every
			// service and app, so the E2E cluster has to be able to run it
			if !PrometheusStackInstalled() {
				Expect(InstallPrometheusStack()).To(Succeed())
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
				candidate.Spec.CommonName = common.OperatorName
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

	Context("mqtt round trip simple", func() {

		It("consumer then producer", func() {

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
							// lets add a ProducerOf via an update
							//ProducerOf: []brokerv1beta2.AddressRef{{Address: "mytopic"}, {Address: "mytopic/A"}, {Address: "mytopic/B"}},

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

			acceptorService := svc.NewServiceDefinitionForCR(types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc"}, k8sClient, "acc-port", 61616, map[string]string{selectors.LabelAppKubernetesInstance: crd.Name}, nil, nil)
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

			serverURL, err := url.Parse("ssl://" + clusterIngressHost + ":443")
			Expect(err).Should(BeNil())

			By("setting up subscriber connection")
			messageReceived := false
			subRouter := paho.NewStandardRouter()
			subRouter.RegisterHandler("mytopic", func(p *paho.Publish) {
				messageReceived = true
				fmt.Printf("Received message: '%s' from topic: %s\n", p.Payload, p.Topic)
			})

			subscriber, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
				ServerUrls: []*url.URL{serverURL},
				TlsCfg:     tlsConfig,
				KeepAlive:  30,
				ClientConfig: paho.ClientConfig{
					ClientID: "my-client",
					Router:   subRouter,
				},
			})
			Expect(err).Should(BeNil())

			subConnectCtx, subConnectCancel := context.WithTimeout(ctx, existingClusterTimeout)
			defer subConnectCancel()
			Expect(subscriber.AwaitConnection(subConnectCtx)).Should(Succeed())

			_, err = subscriber.Subscribe(ctx, &paho.Subscribe{
				Subscriptions: []paho.SubscribeOptions{
					{Topic: "mytopic", QoS: 1},
				},
			})
			Expect(err).Should(BeNil())

			By("setting up publisher connection - expect publish to fail")
			publisher, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
				ServerUrls: []*url.URL{serverURL},
				TlsCfg:     tlsConfig,
				KeepAlive:  30,
				ClientConfig: paho.ClientConfig{
					ClientID: "my-client-pub",
				},
			})
			Expect(err).Should(BeNil())

			pubConnectCtx, pubConnectCancel := context.WithTimeout(ctx, existingClusterTimeout)
			defer pubConnectCancel()
			Expect(publisher.AwaitConnection(pubConnectCtx)).Should(Succeed())

			// MQTT v5 returns a reason code in PUBACK for unauthorized access
			text := "Hello MQTT from Go!"
			_, err = publisher.Publish(ctx, &paho.Publish{
				Topic:   "mytopic",
				QoS:     1,
				Payload: []byte(text),
			})
			Expect(err).ShouldNot(BeNil())
			fmt.Printf("Publish correctly rejected: %v\n", err)

			Expect(publisher.Disconnect(ctx)).Should(Succeed())

			By("updating app to add producer capability")
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())
				createdApp.Spec.Capabilities = append(createdApp.Spec.Capabilities, brokerv1beta2.AppCapabilityType{
					ProducerOf: []brokerv1beta2.AddressRef{{Address: "mytopic"}, {Address: "mytopic/A"}, {Address: "mytopic/B"}},
				})
				g.Expect(k8sClient.Update(ctx, createdApp)).Should(Succeed())

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, appKey, createdApp)).Should(Succeed())

				if verbose {
					fmt.Printf("App STATUS: %v\n\n", createdApp.Status.Conditions)
				}
				readyCond := meta.FindStatusCondition(createdApp.Status.Conditions, brokerv1beta2.ReadyConditionType)
				g.Expect(readyCond).ShouldNot(BeNil())
				g.Expect(readyCond.Status).Should(Equal(metav1.ConditionTrue))
				g.Expect(readyCond.ObservedGeneration).Should(Equal(createdApp.Generation))

			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// publish should now succeed on a new connection,
			// that gets newly authenticated and authorized and can see the permissions from the ProducerOf capability
			publisher2, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
				ServerUrls: []*url.URL{serverURL},
				TlsCfg:     tlsConfig,
				KeepAlive:  30,
				ClientConfig: paho.ClientConfig{
					ClientID: "my-client-pub",
				},
			})
			Expect(err).Should(BeNil())

			pubConnectCtx2, pubConnectCancel2 := context.WithTimeout(ctx, existingClusterTimeout)
			defer pubConnectCancel2()
			Expect(publisher2.AwaitConnection(pubConnectCtx2)).Should(Succeed())

			_, err = publisher2.Publish(ctx, &paho.Publish{
				Topic:   "mytopic",
				QoS:     1,
				Payload: []byte(text),
			})
			Expect(err).Should(BeNil())

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
			Expect(publisher2.Disconnect(ctx)).Should(Succeed())
			Expect(subscriber.Disconnect(ctx)).Should(Succeed())

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

			// app-beta lives in a namespace of its own, away from the service, the
			// way a tenant would, so its scrape wiring is exercised across
			// namespaces.
			By("ensuring other namespace exists")
			otherNs := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: otherNamespace}}
			if err := k8sClient.Create(ctx, &otherNs); err != nil && !errors.IsAlreadyExists(err) {
				Fail(fmt.Sprintf("Failed to create other namespace: %v", err))
			}

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
				Spec: brokerv1beta2.BrokerServiceSpec{
					AppSelectorExpression: fmt.Sprintf(`app.metadata.namespace in ["%s", "%s"]`, defaultNamespace, otherNamespace),
				},
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

			newApp := func(name, namespace, topic, subscription string) brokerv1beta2.BrokerApp {
				return brokerv1beta2.BrokerApp{
					TypeMeta: metav1.TypeMeta{
						Kind:       "ActiveMQArtemisApp",
						APIVersion: brokerv1beta2.GroupVersion.Identifier(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      name,
						Namespace: namespace,
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

			appAlpha := newApp("app-alpha", defaultNamespace, "alpha-topic", "alpha-client.alpha-topic")
			appBeta := newApp("app-beta", otherNamespace, "beta-topic", "beta-client.beta-topic")

			// alpha owns a queue beta consumes from, across namespaces: its
			// series belong to alpha alone, beta reading them needs access to
			// alpha's namespace
			const sharedQueue = "SHARED.Q"
			appAlpha.Spec.SharedAddresses = []brokerv1beta2.AddressType{{Address: sharedQueue}}
			appAlpha.Spec.Capabilities[0].ProducerOf = append(appAlpha.Spec.Capabilities[0].ProducerOf,
				brokerv1beta2.AddressRef{Address: sharedQueue})
			appBeta.Spec.Capabilities[0].ConsumerOf = append(appBeta.Spec.Capabilities[0].ConsumerOf,
				brokerv1beta2.AddressRef{Address: sharedQueue, AppName: appAlpha.Name, AppNamespace: appAlpha.Namespace})

			By("installing per-app client certs, each in its app's namespace")
			for _, app := range []*brokerv1beta2.BrokerApp{&appAlpha, &appBeta} {
				certName := app.Name + common.AppCertSecretSuffix
				InstallCert(certName, app.Namespace, func(candidate *cmv1.Certificate) {
					candidate.Spec.SecretName = certName
					candidate.Spec.CommonName = app.Name
					candidate.Spec.Subject.Organizations = nil
					candidate.Spec.Subject.OrganizationalUnits = []string{app.Namespace}
					candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
						Name: caIssuer.Name,
						Kind: "ClusterIssuer",
					}
				})
			}

			By("installing prometheus cert")
			InstallCert(common.DefaultPrometheusCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
				candidate.Spec.SecretName = common.DefaultPrometheusCertSecretName
				candidate.Spec.CommonName = "prometheus"
				candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
					Name: caIssuer.Name,
					Kind: "ClusterIssuer",
				}
			})

			By("deploying app-alpha and waiting for Ready before app-beta (serializes port assignment)")
			Expect(k8sClient.Create(ctx, &appAlpha)).Should(Succeed())
			Eventually(func(g Gomega) {
				app := &brokerv1beta2.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appAlpha.Name, Namespace: defaultNamespace}, app)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(app.Status.Conditions, brokerv1beta2.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("deploying app-beta (produces/consumes on beta-topic)")
			Expect(k8sClient.Create(ctx, &appBeta)).Should(Succeed())
			Eventually(func(g Gomega) {
				app := &brokerv1beta2.BrokerApp{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appBeta.Name, Namespace: appBeta.Namespace}, app)).Should(Succeed())
				g.Expect(meta.IsStatusConditionTrue(app.Status.Conditions, brokerv1beta2.ReadyConditionType)).Should(BeTrue())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("the service listing both apps by namespace and name, the one across namespaces included")
			Eventually(func(g Gomega) {
				service := &brokerv1beta2.BrokerService{}
				g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, service)).Should(Succeed())
				g.Expect(service.Status.ProvisionedApps).Should(ConsistOf(
					HavePrefix(appAlpha.Namespace+"/"+appAlpha.Name+"@"),
					HavePrefix(appBeta.Namespace+"/"+appBeta.Name+"@"),
				))
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("reading assigned ports from app status")
			alphaApp := &brokerv1beta2.BrokerApp{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appAlpha.Name, Namespace: defaultNamespace}, alphaApp)).Should(Succeed())
			alphaPort := alphaApp.Status.Service.AssignedPort
			fmt.Printf("app-alpha assigned port: %d\n", alphaPort)

			betaApp := &brokerv1beta2.BrokerApp{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appBeta.Name, Namespace: appBeta.Namespace}, betaApp)).Should(Succeed())
			betaPort := betaApp.Status.Service.AssignedPort
			fmt.Printf("app-beta assigned port: %d\n", betaPort)

			Expect(alphaPort).ShouldNot(Equal(betaPort), "port collision: both apps got the same port")

			By("creating per-app acceptor services + ingresses")
			alphaAccSvc := svc.NewServiceDefinitionForCR(
				types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc-alpha"},
				k8sClient, "acc-port", alphaPort,
				map[string]string{selectors.LabelAppKubernetesInstance: crd.Name}, nil, nil)
			Expect(k8sClient.Create(ctx, alphaAccSvc)).Should(Succeed())

			alphaAccIng := ingresses.NewIngressForCRWithSSL(nil,
				types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc-alpha"},
				nil, serviceName+"-acc-alpha", fmt.Sprintf("%d", alphaPort), true,
				defaultTestIngressDomain, alphaIngressHost, isOpenshift)
			Expect(k8sClient.Create(ctx, alphaAccIng)).Should(Succeed())

			betaAccSvc := svc.NewServiceDefinitionForCR(
				types.NamespacedName{Namespace: defaultNamespace, Name: serviceName + "-acc-beta"},
				k8sClient, "acc-port", betaPort,
				map[string]string{selectors.LabelAppKubernetesInstance: crd.Name}, nil, nil)
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
			connectMqtt := func(app *brokerv1beta2.BrokerApp, clientID, ingressHost, topic string) (*autopaho.ConnectionManager, *bool) {
				appName := app.Name
				certSecret, err := secrets.RetriveSecret(
					types.NamespacedName{Namespace: app.Namespace, Name: appName + common.AppCertSecretSuffix},
					make(map[string]string), k8sClient)
				Expect(err).Should(BeNil())
				keyPair, err := tls.X509KeyPair(certSecret.Data["tls.crt"], certSecret.Data["tls.key"])
				Expect(err).Should(BeNil())

				received := false
				router := paho.NewStandardRouter()
				router.RegisterHandler(topic, func(p *paho.Publish) {
					received = true
					fmt.Printf("%s received: '%s' on %s\n", clientID, p.Payload, p.Topic)
				})

				serverURL, err := url.Parse("ssl://" + clusterIngressHost + ":443")
				Expect(err).Should(BeNil())

				cm, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
					ServerUrls: []*url.URL{serverURL},
					TlsCfg: &tls.Config{
						RootCAs:            certpool,
						Certificates:       []tls.Certificate{keyPair},
						ServerName:         ingressHost,
						InsecureSkipVerify: true,
					},
					KeepAlive: 30,
					ClientConfig: paho.ClientConfig{
						ClientID: clientID,
						Router:   router,
					},
				})
				Expect(err).Should(BeNil())

				connectCtx, connectCancel := context.WithTimeout(ctx, existingClusterTimeout)
				defer connectCancel()
				Expect(cm.AwaitConnection(connectCtx)).Should(Succeed())

				_, err = cm.Subscribe(ctx, &paho.Subscribe{
					Subscriptions: []paho.SubscribeOptions{
						{Topic: topic, QoS: 1},
					},
				})
				Expect(err).Should(BeNil())

				return cm, &received
			}

			By("app-alpha: MQTT pub/sub on alpha-topic")
			alphaClient, alphaReceived := connectMqtt(&appAlpha, "alpha-client", alphaIngressHost, "alpha-topic")
			_, err = alphaClient.Publish(ctx, &paho.Publish{
				Topic: "alpha-topic", QoS: 1, Payload: []byte("hello from alpha"),
			})
			Expect(err).Should(BeNil())
			Eventually(func() bool { return *alphaReceived }, existingClusterTimeout, existingClusterInterval).Should(BeTrue())

			By("app-beta: MQTT pub/sub on beta-topic")
			betaClient, betaReceived := connectMqtt(&appBeta, "beta-client", betaIngressHost, "beta-topic")
			_, err = betaClient.Publish(ctx, &paho.Publish{
				Topic: "beta-topic", QoS: 1, Payload: []byte("hello from beta"),
			})
			Expect(err).Should(BeNil())
			Eventually(func() bool { return *betaReceived }, existingClusterTimeout, existingClusterInterval).Should(BeTrue())

			serverName := common.OrdinalFQDNS(serviceName, defaultNamespace, 0)

			// Same reason the connect retries: the owner labels only reach the
			// metrics endpoint once the broker has reloaded its exporter config.
			By("the broker labelling each app's queue with the app that owns it")
			Eventually(func(g Gomega) {
				body := scrapeMetrics(g, serverName, operatorClientCert)

				alpha := queueSeriesLabels(body, "alpha-client.alpha-topic")
				g.Expect(alpha).Should(HaveKeyWithValue("namespace", defaultNamespace))
				g.Expect(alpha).Should(HaveKeyWithValue("brokerapp", appAlpha.Name))

				beta := queueSeriesLabels(body, "beta-client.beta-topic")
				g.Expect(beta).Should(HaveKeyWithValue("namespace", otherNamespace),
					"beta's queue belongs in beta's namespace, away from the service")
				g.Expect(beta).Should(HaveKeyWithValue("brokerapp", appBeta.Name))

				g.Expect(body).ShouldNot(MatchRegexp(`(?m)^jvm_\w*\{[^}]*namespace=`),
					"broker-wide series belong to no tenant")
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// An app's own certificate reads the queues it is authorised on,
			// whoever owns them: the way to its data where metrics are read without
			// a namespace enforcing query path, or across a shared queue.
			By("app-alpha scraping with its own app cert, seeing only its queues")
			Eventually(func(g Gomega) {
				body := scrapeMetrics(g, serverName, appClientCert(g, defaultNamespace, appAlpha.Name))

				g.Expect(queueSeriesLabels(body, "alpha-client.alpha-topic")).ShouldNot(BeNil(),
					"alpha should see its own queue metrics")
				g.Expect(queueSeriesLabels(body, "beta-client.beta-topic")).Should(BeNil(),
					"alpha must NOT see beta's queue metrics")
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			By("app-beta scraping with its own app cert, seeing only its queues")
			Eventually(func(g Gomega) {
				body := scrapeMetrics(g, serverName, appClientCert(g, appBeta.Namespace, appBeta.Name))

				g.Expect(queueSeriesLabels(body, "beta-client.beta-topic")).ShouldNot(BeNil(),
					"beta should see its own queue metrics")
				g.Expect(queueSeriesLabels(body, "alpha-client.alpha-topic")).Should(BeNil(),
					"beta must NOT see alpha's queue metrics")
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// The scraper side of the same story: the operator generates the wiring a
			// Prometheus needs, and that wiring reproduces the placement asserted
			// above without anyone hand writing a target, a serverName or a cert ref.
			By("the operator generating a ServiceMonitor for the service that keeps those labels")
			serviceMonitor := &monitoringv1.ServiceMonitor{}
			wiringKey := types.NamespacedName{Name: serviceName + monitoring.WiringSuffix, Namespace: defaultNamespace}
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, wiringKey, serviceMonitor)).Should(Succeed())
				g.Expect(serviceMonitor.Labels).Should(HaveKeyWithValue(selectors.LabelMonitoring, "true"))
				g.Expect(serviceMonitor.Spec.Endpoints).Should(HaveLen(1))

				endpoint := serviceMonitor.Spec.Endpoints[0]
				g.Expect(endpoint.HonorLabels).Should(BeTrue(),
					"without it the namespace the broker sets is renamed exported_namespace")
				g.Expect(endpoint.TLSConfig).ShouldNot(BeNil())
				g.Expect(endpoint.TLSConfig.ServerName).Should(HaveValue(Equal(serverName)))
				g.Expect(endpoint.TLSConfig.Cert.Secret).ShouldNot(BeNil())
				g.Expect(endpoint.TLSConfig.Cert.Secret.Name).Should(
					Equal(common.DefaultPrometheusCertSecretName),
					"the service scrapes as prometheus, which the broker grants the broad metrics role")
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// Driven purely from the generated spec: a wrong serverName or cert
			// reference fails here instead of producing a target stuck in "down".
			By("the scrape the ServiceMonitor describes seeing every app's queues")
			Eventually(func(g Gomega) {
				endpoint := serviceMonitor.Spec.Endpoints[0]
				body := scrapeUrl(g, fmt.Sprintf("https://%s:%d%s", serverName, monitoring.Port, endpoint.Path),
					tlsConfigFromSpec(g, &endpoint.TLSConfig.SafeTLSConfig, serviceMonitor.Namespace))

				g.Expect(queueSeriesLabels(body, "alpha-client.alpha-topic")).ShouldNot(BeNil())
				g.Expect(queueSeriesLabels(body, "beta-client.beta-topic")).ShouldNot(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

			// The scrapes above prove the generated object is right; this proves a
			// Prometheus accepts it and files each app's queue with the app, which
			// is what tenancy rests on: a series is readable by whoever may read
			// metrics in its namespace.
			if isOpenshift {
				By("not asking Prometheus for its series: the platform Prometheus is only reachable with a token")
			} else {
				By("Prometheus filing each queue in its owner's namespace and nowhere else")
				job := serviceMonitor.Name
				for _, owned := range []struct{ queue, namespace string }{
					{"alpha-client.alpha-topic", defaultNamespace},
					{"beta-client.beta-topic", otherNamespace},
					// beta consumes from it, but alpha owns it
					{sharedQueue, defaultNamespace},
				} {
					Eventually(func(g Gomega) {
						g.Expect(prometheusSeriesLabel(g, fmt.Sprintf(`count by (namespace) (broker_queue_message_count{job=%q, queue=%q})`, job, owned.queue), "namespace")).
							Should(ConsistOf(owned.namespace), "namespaces holding queue %s", owned.queue)
					}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
				}

				By("Prometheus filing no broker-wide series in an app's namespace")
				Expect(prometheusSeriesLabel(Default, fmt.Sprintf(`count by (__name__) ({namespace=%q, __name__=~"jvm_.*|process_.*"})`, otherNamespace), "__name__")).
					Should(BeEmpty())
			}

			By("the generated objects not being rewritten on every reconcile")
			wiringVersion := serviceMonitor.ResourceVersion
			Consistently(func(g Gomega) {
				current := &monitoringv1.ServiceMonitor{}
				g.Expect(k8sClient.Get(ctx, wiringKey, current)).Should(Succeed())
				g.Expect(current.ResourceVersion).Should(Equal(wiringVersion))
			}, time.Second*30, time.Second*5).Should(Succeed())

			Expect(alphaClient.Disconnect(ctx)).Should(Succeed())
			Expect(betaClient.Disconnect(ctx)).Should(Succeed())

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
			UninstallCert(appBeta.Name+common.AppCertSecretSuffix, appBeta.Namespace)
			UninstallCert(common.DefaultPrometheusCertSecretName, defaultNamespace)
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

// queueSeriesLabels returns the labels of a queue's message count series in an
// exposition body, or nil when the queue has none.
func queueSeriesLabels(body, queue string) map[string]string {
	series := regexp.MustCompile(`(?m)^broker_queue_message_count\{([^}]*)\}`)
	label := regexp.MustCompile(`(\w+)="([^"]*)"`)

	for _, match := range series.FindAllStringSubmatch(body, -1) {
		labels := map[string]string{}
		for _, pair := range label.FindAllStringSubmatch(match[1], -1) {
			labels[pair[1]] = pair[2]
		}
		if labels["queue"] == queue {
			return labels
		}
	}
	return nil
}

// tlsConfigFromSpec resolves a generated tlsConfig the way prometheus-operator
// would: every secret reference is read from the namespace of the object that
// declared it.
func tlsConfigFromSpec(g Gomega, tlsSpec *monitoringv1.SafeTLSConfig, namespace string) *tls.Config {
	g.Expect(tlsSpec).ShouldNot(BeNil())

	readKey := func(secretName, key string) []byte {
		secret, err := secrets.RetriveSecret(
			types.NamespacedName{Namespace: namespace, Name: secretName},
			make(map[string]string), k8sClient)
		g.Expect(err).Should(BeNil(), "secret %s referenced in namespace %s", secretName, namespace)
		g.Expect(secret.Data).Should(HaveKey(key))
		return secret.Data[key]
	}

	tlsConfig := &tls.Config{
		ServerName: ptr.Deref(tlsSpec.ServerName, ""),
	}

	g.Expect(tlsSpec.CA.Secret).ShouldNot(BeNil(), "generated tlsConfig must reference a CA secret")
	caPool := x509.NewCertPool()
	g.Expect(caPool.AppendCertsFromPEM(readKey(tlsSpec.CA.Secret.Name, tlsSpec.CA.Secret.Key))).Should(BeTrue())
	tlsConfig.RootCAs = caPool

	g.Expect(tlsSpec.Cert.Secret).ShouldNot(BeNil(), "generated tlsConfig must reference a client cert")
	g.Expect(tlsSpec.KeySecret).ShouldNot(BeNil(), "generated tlsConfig must reference a client key")
	keyPair, err := tls.X509KeyPair(
		readKey(tlsSpec.Cert.Secret.Name, tlsSpec.Cert.Secret.Key),
		readKey(tlsSpec.KeySecret.Name, tlsSpec.KeySecret.Key))
	g.Expect(err).Should(BeNil())
	tlsConfig.Certificates = []tls.Certificate{keyPair}

	return tlsConfig
}

// prometheusSeriesLabel runs an instant query against the Prometheus the suite
// installed and returns, for each resulting series, the values of the given
// labels joined by a slash.
func prometheusSeriesLabel(g Gomega, query string, labels ...string) []string {
	clientset, err := kubernetes.NewForConfig(restConfig)
	g.Expect(err).Should(Succeed())

	body, err := clientset.CoreV1().Services(prometheusNamespace).
		ProxyGet("http", "kube-prometheus-stack-prometheus", "http-web", "/api/v1/query", map[string]string{"query": query}).
		DoRaw(ctx)
	g.Expect(err).Should(Succeed())

	var result struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
			} `json:"result"`
		} `json:"data"`
	}
	g.Expect(json.Unmarshal(body, &result)).Should(Succeed())

	values := make([]string, 0, len(result.Data.Result))
	for _, series := range result.Data.Result {
		value := make([]string, 0, len(labels))
		for _, label := range labels {
			value = append(value, series.Metric[label])
		}
		values = append(values, strings.Join(value, "/"))
	}
	return values
}

func scrapeUrl(g Gomega, url string, tlsConfig *tls.Config) string {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig

	httpClient := http.Client{Transport: transport, Timeout: time.Second * 5}

	resp, err := httpClient.Get(url)
	g.Expect(err).Should(Succeed())
	g.Expect(resp).ShouldNot(BeNil())
	defer func() { _ = resp.Body.Close() }()
	g.Expect(resp.StatusCode).Should(Equal(200))

	body, err := io.ReadAll(resp.Body)
	g.Expect(err).Should(Succeed())
	return string(body)
}
