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
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"time"

	"github.com/Azure/go-amqp"
	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/ingresses"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	svc "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/services"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
)

// Each spec asserts a guarantee the drain finalizer is meant to give
var _ = Describe("broker-service drain failure modes", Label("verySlow", "failure-modes"), func() {

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

	// helper: create a BrokerService with certs and wait for it to exist
	setupService := func(ctx context.Context, serviceName string, selectorLabels map[string]string) broker.BrokerService {
		sharedOperandCertName := serviceName + "-" + common.DefaultOperandCertSecretName
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

		prometheusCertName := common.DefaultPrometheusCertSecretName
		InstallCert(prometheusCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
			candidate.Spec.SecretName = prometheusCertName
			candidate.Spec.CommonName = "prometheus"
			candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
				Name: caIssuer.Name,
				Kind: "ClusterIssuer",
			}
		})

		service := broker.BrokerService{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerService",
				APIVersion: broker.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: defaultNamespace,
				Labels:    selectorLabels,
			},
			Spec: broker.BrokerServiceSpec{
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &service)).Should(Succeed())
		return service
	}

	// helper: create a BrokerApp with certs
	setupApp := func(ctx context.Context, appName string, selectorLabels map[string]string, spec broker.BrokerAppSpec) broker.BrokerApp {
		InstallCert(appName+"-"+common.DefaultOperandCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
			candidate.Spec.SecretName = appName + "-" + common.DefaultOperandCertSecretName
			candidate.Spec.CommonName = appName
			candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
				Name: caIssuer.Name,
				Kind: "ClusterIssuer",
			}
		})

		spec.ServiceSelector = &metav1.LabelSelector{
			MatchLabels: selectorLabels,
		}
		if spec.Resources.Requests == nil {
			spec.Resources = corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("256Mi"),
				},
			}
		}

		app := broker.BrokerApp{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerApp",
				APIVersion: broker.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: defaultNamespace,
			},
			Spec: spec,
		}
		InstallAppCert(&app)
		Expect(k8sClient.Create(ctx, &app)).Should(Succeed())

		createdApp := &broker.BrokerApp{}
		Eventually(func(g Gomega) {

			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app.Name, Namespace: defaultNamespace}, createdApp)).Should(Succeed())

			if verbose {
				fmt.Printf("\nCREATED APP: %v\n", createdApp.Status)
			}

			readyCond := meta.FindStatusCondition(createdApp.Status.Conditions, broker.ReadyConditionType)

			g.Expect(readyCond).ToNot(BeNil())
			g.Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))

			if verbose {
				fmt.Printf("\nAPP Finalizers: %v\n", createdApp.ObjectMeta.Finalizers)
			}

		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		return app
	}

	// helper: verify app is provisioned on the service
	verifyProvisioned := func(ctx context.Context, serviceName, appName string) {
		createdService := &broker.BrokerService{}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, createdService)).Should(Succeed())

			if verbose {
				fmt.Printf("\nPROVISIONED STATUS: %v\n", createdService.Status)
				fmt.Printf("\nPROVISIONED Finalizers: %v\n", createdService.ObjectMeta.Finalizers)
			}

			g.Expect(createdService.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(appName)))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	}

	type mqttTestEndpoint struct {
		tlsConfig *tls.Config
		serverURL *url.URL
		accSvc    *corev1.Service
		accIng    *netv1.Ingress
	}

	// helper: set up service, ingress, and TLS for MQTT access to an app's acceptor
	setupMqttEndpoint := func(ctx context.Context, serviceName, appName string) mqttTestEndpoint {
		createdApp := &broker.BrokerApp{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: defaultNamespace}, createdApp)).Should(Succeed())
		Expect(createdApp.Status.Service).ShouldNot(BeNil())
		appPort := createdApp.Status.Service.AssignedPort
		Expect(appPort).ShouldNot(BeZero())

		accServiceName := appName + "-acc-drain"
		accService := svc.NewServiceDefinitionForCR(
			types.NamespacedName{Namespace: defaultNamespace, Name: accServiceName},
			k8sClient, "acc-port", appPort,
			map[string]string{selectors.LabelAppKubernetesInstance: serviceName}, nil, nil)
		Expect(k8sClient.Create(ctx, accService)).Should(Succeed())

		acceptorIngressHost := accServiceName + "-" + defaultNamespace + "." + defaultTestIngressDomain
		accIngress := ingresses.NewIngressForCRWithSSL(nil,
			types.NamespacedName{Namespace: defaultNamespace, Name: accServiceName},
			nil, accServiceName, fmt.Sprintf("%d", appPort), true,
			defaultTestIngressDomain, acceptorIngressHost, isOpenshift)
		Expect(k8sClient.Create(ctx, accIngress)).Should(Succeed())

		operandCertName := serviceName + "-" + common.DefaultOperandCertSecretName
		operandSecret, err := secrets.RetriveSecret(
			types.NamespacedName{Namespace: defaultNamespace, Name: operandCertName},
			make(map[string]string), k8sClient)
		Expect(err).Should(BeNil())
		certpool := x509.NewCertPool()
		certpool.AppendCertsFromPEM(operandSecret.Data["tls.crt"])

		appCertName := appName + common.AppCertSecretSuffix
		appCertSecret, err := secrets.RetriveSecret(
			types.NamespacedName{Namespace: defaultNamespace, Name: appCertName},
			make(map[string]string), k8sClient)
		Expect(err).Should(BeNil())
		clientKeyPair, err := tls.X509KeyPair(appCertSecret.Data["tls.crt"], appCertSecret.Data["tls.key"])
		Expect(err).Should(BeNil())

		serverURL, err := url.Parse("ssl://" + clusterIngressHost + ":443")
		Expect(err).Should(BeNil())

		return mqttTestEndpoint{
			tlsConfig: &tls.Config{
				RootCAs:            certpool,
				Certificates:       []tls.Certificate{clientKeyPair},
				ServerName:         acceptorIngressHost,
				InsecureSkipVerify: true,
			},
			serverURL: serverURL,
			accSvc:    accService,
			accIng:    accIngress,
		}
	}

	// helper: establish a durable MQTT subscription then disconnect
	mqttSubscribe := func(ctx context.Context, ep mqttTestEndpoint, clientID, topic string) {
		Eventually(func(g Gomega) {
			conn, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
				ServerUrls: []*url.URL{ep.serverURL},
				TlsCfg:     ep.tlsConfig,
				KeepAlive:  30,
				ClientConfig: paho.ClientConfig{
					ClientID: clientID,
				},
			})
			g.Expect(err).Should(BeNil())

			connCtx, connCancel := context.WithTimeout(ctx, 30*time.Second)
			defer connCancel()
			g.Expect(conn.AwaitConnection(connCtx)).Should(Succeed())

			_, err = conn.Subscribe(ctx, &paho.Subscribe{
				Subscriptions: []paho.SubscribeOptions{
					{Topic: topic, QoS: 1},
				},
			})
			g.Expect(err).Should(BeNil())

			g.Expect(conn.Disconnect(ctx)).Should(Succeed())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	}

	// helper: publish messages via MQTT, retries until the broker has reloaded config
	mqttProduce := func(ctx context.Context, ep mqttTestEndpoint, topic string, count int) {
		Eventually(func(g Gomega) {
			conn, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
				ServerUrls: []*url.URL{ep.serverURL},
				TlsCfg:     ep.tlsConfig,
				KeepAlive:  30,
				ClientConfig: paho.ClientConfig{
					ClientID: "drain-test-producer",
				},
			})
			g.Expect(err).Should(BeNil())

			connCtx, connCancel := context.WithTimeout(ctx, 30*time.Second)
			defer connCancel()
			g.Expect(conn.AwaitConnection(connCtx)).Should(Succeed())

			for i := 0; i < count; i++ {
				_, err = conn.Publish(ctx, &paho.Publish{
					Topic:   topic,
					QoS:     1,
					Payload: []byte(fmt.Sprintf("drain-test-%s-%d", topic, i)),
				})
				g.Expect(err).Should(BeNil())
			}

			g.Expect(conn.Disconnect(ctx)).Should(Succeed())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	}

	// helper: consume messages via MQTT durable subscription
	mqttConsume := func(ctx context.Context, ep mqttTestEndpoint, clientID, topic string, expectedMin int) {
		var consumed int32
		Eventually(func(g Gomega) {
			atomic.StoreInt32(&consumed, 0)
			router := paho.NewStandardRouter()
			router.RegisterHandler(topic, func(p *paho.Publish) {
				atomic.AddInt32(&consumed, 1)
				if verbose {
					fmt.Printf("Consumed message: '%s'\n", p.Payload)
				}
			})

			conn, err := autopaho.NewConnection(ctx, autopaho.ClientConfig{
				ServerUrls: []*url.URL{ep.serverURL},
				TlsCfg:     ep.tlsConfig,
				KeepAlive:  30,
				ClientConfig: paho.ClientConfig{
					ClientID: clientID,
					Router:   router,
				},
			})
			g.Expect(err).Should(BeNil())

			connCtx, connCancel := context.WithTimeout(ctx, 30*time.Second)
			defer connCancel()
			g.Expect(conn.AwaitConnection(connCtx)).Should(Succeed())

			_, err = conn.Subscribe(ctx, &paho.Subscribe{
				Subscriptions: []paho.SubscribeOptions{
					{Topic: topic, QoS: 1},
				},
			})
			g.Expect(err).Should(BeNil())

			time.Sleep(10 * time.Second)

			g.Expect(conn.Disconnect(ctx)).Should(Succeed())
			g.Expect(atomic.LoadInt32(&consumed)).Should(BeNumerically(">=", int32(expectedMin)),
				"should have consumed all produced messages")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	}

	// helper: clean up MQTT endpoint resources
	cleanupMqttEndpoint := func(ep mqttTestEndpoint) {
		Expect(k8sClient.Delete(ctx, ep.accIng)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, ep.accSvc)).Should(Succeed())
	}

	amqpDialSASLExternal := func(g Gomega, ctx context.Context, ep mqttTestEndpoint, containerID string) *amqp.Conn {

		url := "amqps://" + clusterIngressHost + ":443"
		conn, err := amqp.Dial(ctx, url, &amqp.ConnOptions{
			ContainerID: containerID,
			SASLType:    amqp.SASLTypeExternal(""),
			TLSConfig:   ep.tlsConfig,
		})
		g.Expect(err).ShouldNot(HaveOccurred())
		return conn
	}

	// helper: produce messages to an anycast address via AMQP
	amqpProduce := func(ctx context.Context, ep mqttTestEndpoint, address string, count int) {
		Eventually(func(g Gomega) {
			conn := amqpDialSASLExternal(g, ctx, ep, "drain-amqp-producer")
			defer func() {
				_ = conn.Close()
			}()
			session, err := conn.NewSession(ctx, nil)
			g.Expect(err).ShouldNot(HaveOccurred())

			sender, err := session.NewSender(ctx, address, nil)
			g.Expect(err).ShouldNot(HaveOccurred())

			sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			defer func() {
				_ = sender.Close(sendCtx)
			}()

			for i := 0; i < count; i++ {
				err = sender.Send(sendCtx, amqp.NewMessage([]byte(fmt.Sprintf("drain-test-%s-%d", address, i))), nil)
				g.Expect(err).ShouldNot(HaveOccurred())
			}
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	}

	// helper: consume messages from an anycast address via AMQP
	amqpConsume := func(ctx context.Context, ep mqttTestEndpoint, address string, expectedCount int) {
		Eventually(func(g Gomega) {
			conn := amqpDialSASLExternal(g, ctx, ep, "drain-amqp-consumer")
			defer func() {
				_ = conn.Close()
			}()
			session, err := conn.NewSession(ctx, nil)
			g.Expect(err).ShouldNot(HaveOccurred())

			receiver, err := session.NewReceiver(ctx, address, &amqp.ReceiverOptions{
				Credit: int32(expectedCount + 1),
			})
			g.Expect(err).ShouldNot(HaveOccurred())

			recvCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			defer func() {
				_ = receiver.Close(recvCtx)
			}()
			for i := 0; i < expectedCount; i++ {
				msg, err := receiver.Receive(recvCtx, nil)
				g.Expect(err).ShouldNot(HaveOccurred())
				g.Expect(receiver.AcceptMessage(recvCtx, msg)).Should(Succeed())
			}
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	}

	jobsSpec := func() broker.BrokerAppSpec {
		return broker.BrokerAppSpec{
			Addresses: []broker.AddressType{{Address: "jobs"}},
			Capabilities: []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{{Address: "jobs"}},
				ConsumerOf: []broker.AddressRef{{Address: "jobs"}},
			}},
		}
	}

	// stand in for hack/clear-finalizers.sh on one resource
	stripFinalizers := func(ctx context.Context, obj client.Object) {
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj)
			if errors.IsNotFound(err) {
				return
			}
			g.Expect(err).ShouldNot(HaveOccurred())
			obj.SetFinalizers(nil)
			err = k8sClient.Update(ctx, obj)
			g.Expect(err == nil || errors.IsNotFound(err)).Should(BeTrue(), "%v", err)
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	}

	forceRemove := func(ctx context.Context, obj client.Object) {
		stripFinalizers(ctx, obj)
		_ = k8sClient.Delete(ctx, obj)
		Eventually(func() bool {
			return errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj))
		}, existingClusterTimeout, existingClusterInterval).Should(BeTrue())
	}

	setAppSelectorExpression := func(ctx context.Context, service *broker.BrokerService, expression string) {
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(service), service)).Should(Succeed())
			service.Spec.AppSelectorExpression = expression
			g.Expect(k8sClient.Update(ctx, service)).Should(Succeed())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	}

	It("accepts no message from the app's producers once the app is released", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()
		labels := map[string]string{"test": "fm-f1"}
		appName := NextSpecResourceName()
		DeferCleanup(CleanupServiceCerts, serviceName, defaultNamespace)
		DeferCleanup(CleanupAppCerts, appName, defaultNamespace)
		service := setupService(ctx, serviceName, labels)
		DeferCleanup(forceRemove, ctx, &service)
		app := setupApp(ctx, appName, labels, jobsSpec())
		DeferCleanup(forceRemove, ctx, &app)
		verifyProvisioned(ctx, serviceName, appName)
		ep := setupMqttEndpoint(ctx, serviceName, appName)
		DeferCleanup(cleanupMqttEndpoint, ep)

		By("connecting a consumer on 'jobs'")
		var consumerConn, longConn *amqp.Conn
		var receiver *amqp.Receiver
		var longSender *amqp.Sender
		Eventually(func(g Gomega) {
			consumerConn = amqpDialSASLExternal(g, ctx, ep, "fm-f1-consumer")
			session, err := consumerConn.NewSession(ctx, nil)
			g.Expect(err).ShouldNot(HaveOccurred())
			receiver, err = session.NewReceiver(ctx, "jobs", &amqp.ReceiverOptions{Credit: 100})
			g.Expect(err).ShouldNot(HaveOccurred())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		var consumed atomic.Int32
		var released atomic.Bool

		consumerCtx, stopConsumer := context.WithCancel(ctx)
		DeferCleanup(func() {
			stopConsumer()
			_ = consumerConn.Close()
		})
		go func() {
			for {
				msg, err := receiver.Receive(consumerCtx, nil)
				if err != nil {
					return
				}
				_ = receiver.AcceptMessage(consumerCtx, msg)
				consumed.Add(1)
			}
		}()

		type producer struct {
			name                           string
			accepted, acceptedAfterRelease atomic.Int32
			done                           chan error
			err                            error
		}
		producerCtx, stopProducers := context.WithCancel(ctx)
		DeferCleanup(stopProducers)
		run := func(p *producer, send func(ctx context.Context, i int) error) {
			p.done = make(chan error, 1)
			go func() {
				for i := 0; producerCtx.Err() == nil; i++ {
					sendCtx, cancel := context.WithTimeout(producerCtx, 10*time.Second)
					err := send(sendCtx, i)
					cancel()
					if err != nil {
						p.done <- err
						return
					}
					p.accepted.Add(1)
					if released.Load() {
						p.acceptedAfterRelease.Add(1)
					}
					time.Sleep(50 * time.Millisecond)
				}
				p.done <- producerCtx.Err()
			}()
		}

		By("a long-lived producer: one connection and one sender for the whole spec")
		Eventually(func(g Gomega) {
			longConn = amqpDialSASLExternal(g, ctx, ep, "fm-f1-long-producer")
			session, err := longConn.NewSession(ctx, nil)
			g.Expect(err).ShouldNot(HaveOccurred())
			longSender, err = session.NewSender(ctx, "jobs", nil)
			g.Expect(err).ShouldNot(HaveOccurred())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		DeferCleanup(func() { _ = longConn.Close() })
		long := &producer{name: "long-lived"}
		run(long, func(ctx context.Context, i int) error {
			return longSender.Send(ctx, amqp.NewMessage([]byte(fmt.Sprintf("fm-f1-long-%d", i))), nil)
		})

		By("a short-lived producer: a new connection for every message")
		short := &producer{name: "short-lived"}
		run(short, func(ctx context.Context, i int) error {
			conn, err := amqp.Dial(ctx, "amqps://"+clusterIngressHost+":443", &amqp.ConnOptions{
				ContainerID: fmt.Sprintf("fm-f1-short-producer-%d", i),
				SASLType:    amqp.SASLTypeExternal(""),
				TLSConfig:   ep.tlsConfig,
			})
			if err != nil {
				return err
			}
			defer func() { _ = conn.Close() }()
			session, err := conn.NewSession(ctx, nil)
			if err != nil {
				return err
			}
			sender, err := session.NewSender(ctx, "jobs", nil)
			if err != nil {
				return err
			}
			return sender.Send(ctx, amqp.NewMessage([]byte(fmt.Sprintf("fm-f1-short-%d", i))), nil)
		})
		producers := []*producer{long, short}

		By("waiting for steady traffic from both producers with the queue kept near empty")
		Eventually(func(g Gomega) {
			g.Expect(consumed.Load()).Should(BeNumerically(">=", 20))
			for _, p := range producers {
				g.Expect(p.accepted.Load()).Should(BeNumerically(">=", 5), "%s producer", p.name)
			}
		}, 2*time.Minute, time.Second).Should(Succeed())
		for _, p := range producers {
			select {
			case err := <-p.done:
				Fail(fmt.Sprintf("%s producer stopped before the app was deleted: %v", p.name, err))
			default:
			}
		}

		By("deleting the app while both producers are still running")
		Expect(k8sClient.Delete(ctx, &app)).Should(Succeed())
		Eventually(func() bool {
			return errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(&app), &broker.BrokerApp{}))
		}, existingClusterTimeout, 250*time.Millisecond).Should(BeTrue())
		released.Store(true)
		stopConsumer()
		_ = consumerConn.Close()

		By("letting each producer run until the broker refuses it")
		deadline := time.After(3 * time.Minute)
		for _, p := range producers {
			select {
			case p.err = <-p.done:
			case <-deadline:
				stopProducers()
				p.err = <-p.done
			}
		}
		stopProducers()
		for _, p := range producers {
			fmt.Printf("F1 %s: accepted=%d acceptedAfterRelease=%d err=%v\n",
				p.name, p.accepted.Load(), p.acceptedAfterRelease.Load(), p.err)
		}
		fmt.Printf("F1 consumed=%d\n", consumed.Load())

		acceptedAfterRelease := map[string]int32{}
		for _, p := range producers {
			acceptedAfterRelease[p.name] = p.acceptedAfterRelease.Load()
		}
		Expect(acceptedAfterRelease).Should(HaveEach(BeZero()),
			"broker accepted messages after the operator had declared the app empty and deleted it")
	})

	It("unprovisions an app the service's selector stops matching even if its spec turned invalid", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()
		labels := map[string]string{"test": "fm-f2b"}
		tenantName := NextSpecResourceName()
		controlName := NextSpecResourceName()
		DeferCleanup(CleanupServiceCerts, serviceName, defaultNamespace)
		DeferCleanup(CleanupAppCerts, tenantName, defaultNamespace)
		DeferCleanup(CleanupAppCerts, controlName, defaultNamespace)
		service := setupService(ctx, serviceName, labels)
		DeferCleanup(forceRemove, ctx, &service)

		tenant := setupApp(ctx, tenantName, labels, jobsSpec())
		DeferCleanup(forceRemove, ctx, &tenant)
		control := setupApp(ctx, controlName, labels, broker.BrokerAppSpec{
			Addresses: []broker.AddressType{{Address: "control"}},
			Capabilities: []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{{Address: "control"}},
				ConsumerOf: []broker.AddressRef{{Address: "control"}},
			}},
		})
		DeferCleanup(forceRemove, ctx, &control)
		verifyProvisioned(ctx, serviceName, tenantName)
		verifyProvisioned(ctx, serviceName, controlName)

		By("the tenant makes its own spec invalid")
		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&tenant), current)).Should(Succeed())
			current.Spec.ServiceSelector = &metav1.LabelSelector{}
			g.Expect(k8sClient.Update(ctx, current)).Should(Succeed())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&tenant), current)).Should(Succeed())
			g.Expect(meta.IsStatusConditionFalse(current.Status.Conditions, broker.ValidConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("the service owner tightens the selector to exclude both apps")
		setAppSelectorExpression(ctx, &service, `app.metadata.name == "nobody"`)

		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			g.Expect(current.Status.ProvisionedApps).ShouldNot(ContainElement(HavePrefix(AppIdentityPrefix(&control))),
				"precondition: the selector change unprovisions a valid app")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			g.Expect(current.Status.ProvisionedApps).ShouldNot(ContainElement(HavePrefix(AppIdentityPrefix(&tenant))),
				"service keeps provisioning an app its own selector rejects")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	})

	It("keeps a removed subscription pending until its messages are consumed", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()
		labels := map[string]string{"test": "fm-f3b"}
		appName := NextSpecResourceName()
		DeferCleanup(CleanupServiceCerts, serviceName, defaultNamespace)
		DeferCleanup(CleanupAppCerts, appName, defaultNamespace)
		service := setupService(ctx, serviceName, labels)
		DeferCleanup(forceRemove, ctx, &service)

		app := setupApp(ctx, appName, labels, broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{{Address: "orders"}},
				ConsumerOf: []broker.AddressRef{{
					Address:       "orders",
					Subscriptions: []string{"fm-a.orders", "fm-b.orders"},
				}},
			}},
		})
		DeferCleanup(forceRemove, ctx, &app)
		verifyProvisioned(ctx, serviceName, appName)
		ep := setupMqttEndpoint(ctx, serviceName, appName)
		DeferCleanup(cleanupMqttEndpoint, ep)

		By("two durable subscribers each receive 5 messages")
		mqttSubscribe(ctx, ep, "fm-a", "orders")
		mqttSubscribe(ctx, ep, "fm-b", "orders")
		mqttProduce(ctx, ep, "orders", 5)

		By("removing subscription fm-b.orders from the spec")
		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&app), current)).Should(Succeed())
			current.Spec.Capabilities[0].ConsumerOf[0].Subscriptions = []string{"fm-a.orders"}
			g.Expect(k8sClient.Update(ctx, current)).Should(Succeed())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())

			if verbose {
				fmt.Printf("\n Service Status: %v\n", current.Status)
			}
			var pending map[string]int64
			for _, pe := range current.Status.PendingEmpty {
				if pe.App == AppIdentity(&app) {
					pending = pe.Messages
				}
			}
			g.Expect(pending).Should(HaveKeyWithValue("orders::fm-b.orders", int64(5)),
				"5 messages on the removed subscription are not tracked as pending empty, status.pendingEmpty=%+v",
				current.Status.PendingEmpty)
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("the removed subscriber can still drain its messages")
		mqttConsume(ctx, ep, "fm-b", "orders", 5)
	})

	It("retained subscription keeps receiving while a sibling subscription is draining", Label("slow"), func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()
		labels := map[string]string{"test": "fm-f3c"}
		appName := NextSpecResourceName()
		DeferCleanup(CleanupServiceCerts, serviceName, defaultNamespace)
		DeferCleanup(CleanupAppCerts, appName, defaultNamespace)
		service := setupService(ctx, serviceName, labels)
		DeferCleanup(forceRemove, ctx, &service)

		app := setupApp(ctx, appName, labels, broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{{Address: "events"}},
				ConsumerOf: []broker.AddressRef{{
					Address:       "events",
					Subscriptions: []string{"kept.events", "removed.events"},
				}},
			}},
		})
		DeferCleanup(forceRemove, ctx, &app)
		verifyProvisioned(ctx, serviceName, appName)
		ep := setupMqttEndpoint(ctx, serviceName, appName)
		DeferCleanup(cleanupMqttEndpoint, ep)

		By("creating durable subscribers and seeding messages")
		mqttSubscribe(ctx, ep, "kept", "events")
		mqttSubscribe(ctx, ep, "removed", "events")
		mqttProduce(ctx, ep, "events", 3)

		By("starting a background producer that sends 1 message per second")
		producerCtx, stopProducer := context.WithCancel(ctx)
		var produced int32
		go func() {
			defer GinkgoRecover()
			for {
				select {
				case <-producerCtx.Done():
					return
				case <-time.After(1 * time.Second):
					conn, err := autopaho.NewConnection(producerCtx, autopaho.ClientConfig{
						ServerUrls: []*url.URL{ep.serverURL},
						TlsCfg:     ep.tlsConfig,
						KeepAlive:  30,
						ClientConfig: paho.ClientConfig{
							ClientID: "continuous-producer",
						},
					})
					if err != nil {
						continue
					}
					connCtx, connCancel := context.WithTimeout(producerCtx, 10*time.Second)
					if err := conn.AwaitConnection(connCtx); err != nil {
						connCancel()
						continue
					}
					connCancel()
					_, err = conn.Publish(producerCtx, &paho.Publish{
						Topic:   "events",
						QoS:     1,
						Payload: []byte(fmt.Sprintf("continuous-%d", atomic.AddInt32(&produced, 1))),
					})
					_ = conn.Disconnect(producerCtx)
					if err != nil {
						continue
					}
				}
			}
		}()
		DeferCleanup(func() { stopProducer() })

		By("removing subscription removed.events from the spec")
		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&app), current)).Should(Succeed())
			current.Spec.Capabilities[0].ConsumerOf[0].Subscriptions = []string{"kept.events"}
			g.Expect(k8sClient.Update(ctx, current)).Should(Succeed())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("waiting for the removed subscription to appear in pending empty")
		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())

			if verbose {
				fmt.Printf("\n Service Status: %v\n", current.Status)
			}
			var pending map[string]int64
			for _, pe := range current.Status.PendingEmpty {
				if pe.App == AppIdentity(&app) {
					pending = pe.Messages
				}
			}
			g.Expect(pending).Should(HaveKey("events::removed.events"),
				"removed subscription should be tracked as pending empty, status.pendingEmpty=%+v",
				current.Status.PendingEmpty)
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("verifying the retained subscription keeps receiving messages from the continuous producer")
		beforeCount := atomic.LoadInt32(&produced)
		time.Sleep(5 * time.Second)
		afterCount := atomic.LoadInt32(&produced)
		Expect(afterCount).Should(BeNumerically(">", beforeCount),
			"continuous producer should still be sending")

		mqttConsume(ctx, ep, "kept", "events", int(afterCount)+3)

		By("verifying removed.events is not growing — enabled=false stops new message routing")
		Consistently(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			for _, pe := range current.Status.PendingEmpty {
				if pe.App == AppIdentity(&app) {
					count, ok := pe.Messages["events::removed.events"]
					if ok {
						if verbose {
							fmt.Printf("\n BrokerService PendingEmpty: %v\n", current.Status.PendingEmpty)
						}
						g.Expect(count).Should(BeNumerically("<=", int64(15)),
							"removed subscription should not receive continuous messages, got %d", count)
					}
				}
			}
		}, 20*time.Second, 2*time.Second).Should(Succeed())

		stopProducer()

		By("consuming all messages from removed.events to complete the drain")
		mqttConsume(ctx, ep, "removed", "events", 3)

		By("verifying removed.events is cleared from pending empty")
		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			for _, pe := range current.Status.PendingEmpty {
				if pe.App == AppIdentity(&app) {
					g.Expect(pe.Messages).ShouldNot(HaveKey("events::removed.events"),
						"removed subscription should be cleared after drain, status.pendingEmpty=%+v",
						current.Status.PendingEmpty)
				}
			}
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	})

	PIt("F4 keeps the broker and its messages through a foreground cascade delete", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()
		labels := map[string]string{"test": "fm-f4"}
		appName := NextSpecResourceName()
		DeferCleanup(CleanupServiceCerts, serviceName, defaultNamespace)
		DeferCleanup(CleanupAppCerts, appName, defaultNamespace)
		service := setupService(ctx, serviceName, labels)
		DeferCleanup(forceRemove, ctx, &service)
		app := setupApp(ctx, appName, labels, jobsSpec())
		DeferCleanup(forceRemove, ctx, &app)
		verifyProvisioned(ctx, serviceName, appName)
		ep := setupMqttEndpoint(ctx, serviceName, appName)
		DeferCleanup(cleanupMqttEndpoint, ep)

		amqpProduce(ctx, ep, "jobs", 5)

		brokerCR := &broker.Broker{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, brokerCR)).Should(Succeed())
		brokerUID := brokerCR.UID

		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			g.Expect(current.Finalizers).Should(ContainElement(broker.AddressesPendingEmptyFinalizer),
				"precondition: the drain finalizer protects the service")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("deleting the service with foreground propagation, as ArgoCD prunes by default")
		Expect(k8sClient.Delete(ctx, &service, client.PropagationPolicy(metav1.DeletePropagationForeground))).Should(Succeed())

		By("the broker holding the protected messages stays up while the drain is pending")
		Consistently(func(g Gomega) {
			held := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), held)).Should(Succeed(),
				"service deletion completed with 5 messages still pending")
			current := &broker.Broker{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, current)
			g.Expect(err).ShouldNot(HaveOccurred(), "broker gone while the service is still held by %v", held.Finalizers)
			g.Expect(current.UID).Should(Equal(brokerUID), "broker was deleted and recreated while the service is still held by %v", held.Finalizers)
			g.Expect(current.DeletionTimestamp).Should(BeNil(), "broker is being deleted while the service is still held by %v", held.Finalizers)
		}, time.Minute, time.Second).Should(Succeed())

		By("and the 5 messages remain consumable")
		amqpConsume(ctx, ep, "jobs", 5)
	})

	PIt("F5 completes deletion of an app whose service was force-removed", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()
		labels := map[string]string{"test": "fm-f5"}
		appName := NextSpecResourceName()
		DeferCleanup(CleanupServiceCerts, serviceName, defaultNamespace)
		DeferCleanup(CleanupAppCerts, appName, defaultNamespace)
		service := setupService(ctx, serviceName, labels)
		DeferCleanup(forceRemove, ctx, &service)
		app := setupApp(ctx, appName, labels, jobsSpec())
		DeferCleanup(forceRemove, ctx, &app)
		verifyProvisioned(ctx, serviceName, appName)
		ep := setupMqttEndpoint(ctx, serviceName, appName)
		DeferCleanup(cleanupMqttEndpoint, ep)
		amqpProduce(ctx, ep, "jobs", 5)

		By("the service deletion is held by the drain")
		Expect(k8sClient.Delete(ctx, &service)).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			g.Expect(current.DeletionTimestamp).ShouldNot(BeNil())
			g.Expect(current.Finalizers).Should(ContainElement(broker.AddressesPendingEmptyFinalizer))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("an admin accepts the loss and clears the service's finalizer only")
		stripFinalizers(ctx, &service)
		Eventually(func() bool {
			return errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), &broker.BrokerService{}))
		}, existingClusterTimeout, existingClusterInterval).Should(BeTrue())

		By("deleting the app, which is now bound to nothing")
		Expect(k8sClient.Delete(ctx, &app)).Should(Succeed())
		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(&app), current)
			g.Expect(errors.IsNotFound(err)).Should(BeTrue(),
				"app stays Terminating, held by %v: no controller removes it once the service is gone", current.Finalizers)
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	})
})
