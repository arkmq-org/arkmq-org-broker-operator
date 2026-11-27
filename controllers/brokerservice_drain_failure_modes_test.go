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
	"strconv"
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
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/ingresses"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/secrets"
	svc "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/services"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
)

// Each spec asserts a guarantee the drain finalizer is meant to give
var _ = Describe("broker-service drain failure modes", Label("verySlow", "failure-modes", "bsvc"), func() {

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
	setupMqttEndpoint := func(ctx context.Context, serviceName, appName string, appNamespace ...string) mqttTestEndpoint {
		ns := defaultNamespace
		if len(appNamespace) > 0 {
			ns = appNamespace[0]
		}

		createdApp := &broker.BrokerApp{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: appName, Namespace: ns}, createdApp)).Should(Succeed())
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
			types.NamespacedName{Namespace: ns, Name: appCertName},
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
			var lastActivity atomic.Int64
			lastActivity.Store(time.Now().UnixMilli())

			router := paho.NewStandardRouter()
			router.RegisterHandler(topic, func(p *paho.Publish) {
				atomic.AddInt32(&consumed, 1)
				lastActivity.Store(time.Now().UnixMilli())
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

			// wait until no messages arrive for 3 seconds, up to 30 seconds total
			deadline := time.After(30 * time.Second)
			for {
				select {
				case <-deadline:
					goto done
				case <-time.After(1 * time.Second):
					if time.Since(time.UnixMilli(lastActivity.Load())) >= 3*time.Second &&
						atomic.LoadInt32(&consumed) >= int32(expectedMin) {
						goto done
					}
				}
			}
		done:

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
				if verbose {
					fmt.Printf("Consumed: %v\n", msg)
				}
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
		DeferCleanup(GracefulRemove, ctx, &service)
		app := setupApp(ctx, appName, labels, jobsSpec())
		DeferCleanup(GracefulRemove, ctx, &app)
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
		DeferCleanup(GracefulRemove, ctx, &service)

		tenant := setupApp(ctx, tenantName, labels, jobsSpec())
		DeferCleanup(GracefulRemove, ctx, &tenant)
		control := setupApp(ctx, controlName, labels, broker.BrokerAppSpec{
			Addresses: []broker.AddressType{{Address: "control"}},
			Capabilities: []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{{Address: "control"}},
				ConsumerOf: []broker.AddressRef{{Address: "control"}},
			}},
		})
		DeferCleanup(GracefulRemove, ctx, &control)
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
		DeferCleanup(GracefulRemove, ctx, &service)

		app := setupApp(ctx, appName, labels, broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{{Address: "orders", PubSub: ptrTo(true)}},
				ConsumerOf: []broker.AddressRef{{
					Address:       "orders",
					Subscriptions: []string{"fm-a.orders", "fm-b.orders"},
				}},
			}},
		})
		DeferCleanup(GracefulRemove, ctx, &app)
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
			var pending map[string]string
			for _, pe := range current.Status.PendingEmpty {
				if pe.App == AppIdentity(&app) {
					pending = pe.Messages
				}
			}
			g.Expect(pending).Should(HaveKeyWithValue("orders::fm-b.orders", "5"),
				"5 messages on the removed subscription are not tracked as pending empty, status.pendingEmpty=%+v",
				current.Status.PendingEmpty)
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("the removed subscriber can still drain its messages")
		mqttConsume(ctx, ep, "fm-b", "orders", 5)

		By("the remaining subscriber can consume its messages")
		mqttConsume(ctx, ep, "fm-a", "orders", 5)
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
		DeferCleanup(GracefulRemove, ctx, &service)

		app := setupApp(ctx, appName, labels, broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{{Address: "events", PubSub: ptrTo(true)}},
				ConsumerOf: []broker.AddressRef{{
					Address:       "events",
					Subscriptions: []string{"kept.events", "removed.events"},
				}},
			}},
		})
		DeferCleanup(GracefulRemove, ctx, &app)
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
			var pending map[string]string
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

		By("waiting for the drain config to be applied by the broker")
		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			if verbose {
				fmt.Printf("\n Waiting for drain config applied, brokerService Status: %v\n", current.Status)
			}
			g.Expect(meta.IsStatusConditionTrue(current.Status.Conditions, broker.AppsProvisionedConditionType)).Should(BeTrue(),
				"AppsProvisioned should be true once the broker has applied the drain config")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("verifying removed.events is not growing — enabled=false stops new message routing")
		var removedCount int64 = -1
		Consistently(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			if verbose {
				fmt.Printf("\n Checking for stability, brokerService Status: %v\n", current.Status)
			}
			for _, pe := range current.Status.PendingEmpty {
				if pe.App == AppIdentity(&app) {
					countStr, ok := pe.Messages["events::removed.events"]
					if ok && countStr != "unknown" {
						count, err := strconv.ParseInt(countStr, 10, 64)
						g.Expect(err).NotTo(HaveOccurred())
						if removedCount >= 0 {
							g.Expect(count).Should(BeNumerically("<=", removedCount),
								"removed subscription should not receive new messages, was %d now %d", removedCount, count)
						}
						removedCount = count
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

		By("consuming kept events to allow drain")
		mqttConsume(ctx, ep, "kept", "events", 1)
	})

	It("keeps the broker and its messages through a foreground cascade delete", Label("verySlow"), func() {
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
		DeferCleanup(GracefulRemove, ctx, &service)
		app := setupApp(ctx, appName, labels, jobsSpec())
		DeferCleanup(GracefulRemove, ctx, &app)
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
		}, time.Minute, time.Second).Should(Succeed())

		By("and the 5 messages remain consumable")
		amqpConsume(ctx, ep, "jobs", 5)

		By("wait for deletion to ensure finalizer is gone")
		Eventually(func(g Gomega) {
			held := &broker.BrokerService{}
			g.Expect(errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), held))).Should(BeTrue(),
				"service deletion completed with messages consumed")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

	})

	It("abandons deletion of an app whose service was force-removed", func() {
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
		DeferCleanup(ForceRemove, ctx, &service)
		app := setupApp(ctx, appName, labels, jobsSpec())
		DeferCleanup(ForceRemove, ctx, &app)
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
		StripFinalizers(ctx, &service)
		Eventually(func() bool {
			return errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), &broker.BrokerService{}))
		}, existingClusterTimeout, existingClusterInterval).Should(BeTrue())

		By("deleting the app, which is now bound to nothing")
		Expect(k8sClient.Delete(ctx, &app)).Should(Succeed())

		currentApp := &broker.BrokerApp{}
		Consistently(func(g Gomega) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&app), currentApp)).Should(Succeed())
		}, time.Second*10, interval).Should(Succeed())

		By("Seeing orphaned broker")
		currentBroker := &broker.Broker{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), currentBroker)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, currentBroker)).Should(Succeed())

		By("cleaning up app")
		StripFinalizers(ctx, currentApp)

		Eventually(func() bool {
			return errors.IsNotFound(k8sClient.Get(ctx, client.ObjectKeyFromObject(currentApp), &broker.BrokerApp{}))
		}, existingClusterTimeout, existingClusterInterval).Should(BeTrue())

	})

	It("holds a service delete while an owned address read by a sink app has messages", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()
		labels := map[string]string{"test": "fm-f6"}
		ownerName := NextSpecResourceName()
		sinkName := NextSpecResourceName()
		DeferCleanup(CleanupServiceCerts, serviceName, defaultNamespace)
		DeferCleanup(CleanupAppCerts, ownerName, defaultNamespace)
		DeferCleanup(CleanupAppCerts, sinkName, defaultNamespace)

		// the broker carries no owner ref, so a force-removed service leaves it orphaned
		DeferCleanup(func(ctx context.Context) {
			orphan, err := getBrokerForService(ctx, serviceName)
			if err == nil {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, orphan))).Should(Succeed())
			}
		}, ctx)

		service := setupService(ctx, serviceName, labels)
		DeferCleanup(ForceRemove, ctx, &service)

		owner := setupApp(ctx, ownerName, labels, broker.BrokerAppSpec{
			Addresses: []broker.AddressType{{
				Address:               "delivered",
				ConsumerOfAppSelector: fmt.Sprintf("app.metadata.name == '%s'", sinkName),
			}},
			Capabilities: []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{{Address: "delivered"}},
			}},
		})
		DeferCleanup(ForceRemove, ctx, &owner)
		verifyProvisioned(ctx, serviceName, ownerName)

		By("the service and the owner app are protected by the drain finalizer")
		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			g.Expect(current.Finalizers).Should(ContainElement(broker.AddressesPendingEmptyFinalizer))

			currentOwner := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&owner), currentOwner)).Should(Succeed())
			g.Expect(currentOwner.Finalizers).Should(ContainElement(broker.AddressesPendingEmptyFinalizer),
				"precondition: the owner app is protected by the drain finalizer")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		ep := setupMqttEndpoint(ctx, serviceName, ownerName)
		DeferCleanup(cleanupMqttEndpoint, ep)
		amqpProduce(ctx, ep, "delivered", 5)

		// the sink joins once its upstream queue already holds messages
		By("adding a sink app that consumes the owner's address")
		sink := setupApp(ctx, sinkName, labels, broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{{
				ConsumerOf: []broker.AddressRef{{
					Address:      "delivered",
					AppNamespace: defaultNamespace,
					AppName:      ownerName,
				}},
			}},
		})
		DeferCleanup(ForceRemove, ctx, &sink)
		verifyProvisioned(ctx, serviceName, sinkName)

		// the sink consumes an address it does not own; that must not read as a removed
		// address. An empty queue hides it (it reads as drained), so messages are queued first.
		By("nothing is pending empty on a healthy deploy with messages queued")
		Consistently(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			g.Expect(current.Status.PendingEmpty).Should(BeEmpty(), "pendingEmpty with nothing deleted")
		}, 20*time.Second, time.Second*5).Should(Succeed())

		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(current.Status.Conditions, broker.AppsProvisionedConditionType)).Should(BeTrue(),
				"AppsProvisioned: %v", meta.FindStatusCondition(current.Status.Conditions, broker.AppsProvisionedConditionType))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("deleting the service with background propagation, as kubectl delete does")
		Expect(k8sClient.Delete(ctx, &service, client.PropagationPolicy(metav1.DeletePropagationBackground))).Should(Succeed())

		By("the service and the owner app stay held while 5 messages are unconsumed")
		Consistently(func(g Gomega) {
			held := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), held)).Should(Succeed(),
				"service deletion completed with 5 messages unconsumed")
			g.Expect(held.Finalizers).Should(ContainElement(broker.AddressesPendingEmptyFinalizer))

			currentOwner := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&owner), currentOwner)).Should(Succeed())
			g.Expect(currentOwner.Finalizers).Should(ContainElement(broker.AddressesPendingEmptyFinalizer),
				"owner app finalizer cleared with 5 messages unconsumed, conditions: %v", currentOwner.Status.Conditions)

			_, err := getBrokerForService(ctx, serviceName)
			g.Expect(err).ShouldNot(HaveOccurred(), "broker gone while the service is still held")
		}, time.Second*20, time.Second*5).Should(Succeed())
	})

	It("consumer app stays bound when owner removes a shared address", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()
		labels := map[string]string{"test": "fm-f7"}
		ownerName := NextSpecResourceName()
		consumerName := NextSpecResourceName()
		DeferCleanup(CleanupServiceCerts, serviceName, defaultNamespace)
		DeferCleanup(CleanupAppCerts, ownerName, defaultNamespace)
		DeferCleanup(CleanupAppCerts, consumerName, defaultNamespace)

		service := setupService(ctx, serviceName, labels)
		DeferCleanup(GracefulRemove, ctx, &service)

		By("creating owner app with shared address 'orders'")
		owner := setupApp(ctx, ownerName, labels, broker.BrokerAppSpec{
			Addresses: []broker.AddressType{{
				Address:               "orders",
				ConsumerOfAppSelector: fmt.Sprintf("app.metadata.name == '%s'", consumerName),
			}},
			Capabilities: []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{{Address: "orders"}},
			}},
		})
		DeferCleanup(ForceRemove, ctx, &owner)
		verifyProvisioned(ctx, serviceName, ownerName)

		By("creating consumer app that references owner's 'orders' address")
		consumer := setupApp(ctx, consumerName, labels, broker.BrokerAppSpec{
			Capabilities: []broker.AppCapabilityType{{
				ConsumerOf: []broker.AddressRef{{
					Address: "orders",
					AppName: ownerName,
				}},
			}},
		})
		DeferCleanup(ForceRemove, ctx, &consumer)
		verifyProvisioned(ctx, serviceName, consumerName)

		By("owner removes the shared address from its spec")
		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&owner), current)).Should(Succeed())
			current.Spec.Addresses = nil
			current.Spec.Capabilities = nil
			g.Expect(k8sClient.Update(ctx, current)).Should(Succeed())
		}, timeout, interval).Should(Succeed())

		By("verifying the consumer's reaction to the removed shared address")
		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&consumer), current)).Should(Succeed())

			if verbose {
				fmt.Printf("consumer after owner removes shared address STATUS: %v\n", current.Status)
			}

			// The consumer should detect that the address ref is no longer
			// satisfied. Whether it unbinds or goes invalid, it should not
			// remain Ready=True with a stale binding.
			validCond := meta.FindStatusCondition(current.Status.Conditions, broker.ValidConditionType)
			readyCond := meta.FindStatusCondition(current.Status.Conditions, broker.ReadyConditionType)
			g.Expect(validCond).ShouldNot(BeNil())
			g.Expect(readyCond).ShouldNot(BeNil())

			// The consumer must acknowledge the change — either Valid=False
			// or Ready=False (unbinding)
			stale := validCond.Status == metav1.ConditionTrue && readyCond.Status == metav1.ConditionTrue
			g.Expect(stale).To(BeFalse(),
				"consumer should not remain fully ready when owner removes the shared address, valid: %v, ready: %v",
				validCond, readyCond)
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	})

	It("two apps draining different addresses on the same service do not interfere", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()
		labels := map[string]string{"test": "fm-f8"}
		app1Name := NextSpecResourceName()
		app2Name := NextSpecResourceName()
		DeferCleanup(CleanupServiceCerts, serviceName, defaultNamespace)
		DeferCleanup(CleanupAppCerts, app1Name, defaultNamespace)
		DeferCleanup(CleanupAppCerts, app2Name, defaultNamespace)

		service := setupService(ctx, serviceName, labels)
		DeferCleanup(GracefulRemove, ctx, &service)

		By("creating app1 with addresses 'alpha' and 'beta'")
		app1 := setupApp(ctx, app1Name, labels, broker.BrokerAppSpec{
			Addresses: []broker.AddressType{
				{Address: "alpha"},
				{Address: "beta"},
			},
			Capabilities: []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{
					{Address: "alpha"},
					{Address: "beta"},
				},
			}},
		})
		DeferCleanup(ForceRemove, ctx, &app1)

		By("creating app2 with addresses 'gamma' and 'delta'")
		app2 := setupApp(ctx, app2Name, labels, broker.BrokerAppSpec{
			Addresses: []broker.AddressType{
				{Address: "gamma"},
				{Address: "delta"},
			},
			Capabilities: []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{
					{Address: "gamma"},
					{Address: "delta"},
				},
			}},
		})
		DeferCleanup(ForceRemove, ctx, &app2)

		verifyProvisioned(ctx, serviceName, app1Name)
		verifyProvisioned(ctx, serviceName, app2Name)

		By("producing messages so drain will be needed")
		ep1 := setupMqttEndpoint(ctx, serviceName, app1Name)
		DeferCleanup(cleanupMqttEndpoint, ep1)
		amqpProduce(ctx, ep1, "beta", 3)

		ep2 := setupMqttEndpoint(ctx, serviceName, app2Name)
		DeferCleanup(cleanupMqttEndpoint, ep2)
		amqpProduce(ctx, ep2, "delta", 3)

		By("app1 removes 'beta', app2 removes 'delta' — concurrent drains")
		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&app1), current)).Should(Succeed())
			current.Spec.Addresses = []broker.AddressType{{Address: "alpha"}}
			current.Spec.Capabilities = []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{{Address: "alpha"}},
			}}
			g.Expect(k8sClient.Update(ctx, current)).Should(Succeed())
		}, timeout, interval).Should(Succeed())

		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&app2), current)).Should(Succeed())
			current.Spec.Addresses = []broker.AddressType{{Address: "gamma"}}
			current.Spec.Capabilities = []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{{Address: "gamma"}},
			}}
			g.Expect(k8sClient.Update(ctx, current)).Should(Succeed())
		}, timeout, interval).Should(Succeed())

		By("verifying each app drains only its own removed address")
		Eventually(func(g Gomega) {
			svc := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), svc)).Should(Succeed())

			if verbose {
				fmt.Printf("service pending empty: %v\n", svc.Status.PendingEmpty)
			}

			// Both apps should have pending empty entries
			app1Identity := AppIdentity(&app1)
			app2Identity := AppIdentity(&app2)

			var app1Pending, app2Pending *broker.PendingEmptyAddressStatus
			for i := range svc.Status.PendingEmpty {
				switch svc.Status.PendingEmpty[i].App {
				case app1Identity:
					app1Pending = &svc.Status.PendingEmpty[i]
				case app2Identity:
					app2Pending = &svc.Status.PendingEmpty[i]
				}
			}

			g.Expect(app1Pending).ShouldNot(BeNil(), "app1 should have pending empty for 'beta'")
			g.Expect(app1Pending.Messages).Should(HaveKey("beta"),
				"app1 should drain 'beta', not 'alpha'")
			g.Expect(app1Pending.Messages).ShouldNot(HaveKey("alpha"),
				"app1's retained address 'alpha' should not be draining")

			g.Expect(app2Pending).ShouldNot(BeNil(), "app2 should have pending empty for 'delta'")
			g.Expect(app2Pending.Messages).Should(HaveKey("delta"),
				"app2 should drain 'delta', not 'gamma'")
			g.Expect(app2Pending.Messages).ShouldNot(HaveKey("gamma"),
				"app2's retained address 'gamma' should not be draining")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("verifying both apps stay provisioned during drain")
		Eventually(func(g Gomega) {
			svc := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), svc)).Should(Succeed())
			g.Expect(svc.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(app1Name)))
			g.Expect(svc.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(app2Name)))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	})

	It("keeps a renamed cross-namespace subscription pending until its messages are consumed", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}

		ctx := context.Background()
		serviceName := NextSpecResourceName()
		ownerName := NextSpecResourceName()
		consumerName := NextSpecResourceName()

		By("ensuring other namespace exists")
		otherNs := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: otherNamespace}}
		if err := k8sClient.Create(ctx, &otherNs); err != nil && !errors.IsAlreadyExists(err) {
			Fail(fmt.Sprintf("Failed to create other namespace: %v", err))
		}

		By("installing certs")
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
		DeferCleanup(CleanupServiceCerts, serviceName, defaultNamespace)

		InstallCert(common.DefaultPrometheusCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
			candidate.Spec.SecretName = common.DefaultPrometheusCertSecretName
			candidate.Spec.CommonName = "prometheus"
			candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
				Name: caIssuer.Name,
				Kind: "ClusterIssuer",
			}
		})

		By("creating cross-namespace service")
		service := broker.BrokerService{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerService",
				APIVersion: broker.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: defaultNamespace,
				Labels:    map[string]string{"test": "fm-xns-drain"},
			},
			Spec: broker.BrokerServiceSpec{
				AppSelectorExpression: fmt.Sprintf(`app.metadata.namespace in ["%s", "%s"]`, defaultNamespace, otherNamespace),
				Resources: corev1.ResourceRequirements{
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, &service)).Should(Succeed())
		DeferCleanup(GracefulRemove, ctx, &service)

		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(current.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("creating owner app in default namespace that owns the address")
		DeferCleanup(CleanupAppCerts, ownerName, defaultNamespace)
		ownerApp := setupApp(ctx, ownerName, map[string]string{"test": "fm-xns-drain"}, broker.BrokerAppSpec{
			Addresses: []broker.AddressType{{
				Address:               "events",
				PubSub:                ptr.To(true),
				ConsumerOfAppSelector: fmt.Sprintf("app.metadata.namespace == '%s'", otherNamespace),
			}},
			Capabilities: []broker.AppCapabilityType{{
				ProducerOf: []broker.AddressRef{{Address: "events", PubSub: ptr.To(true)}},
				ConsumerOf: []broker.AddressRef{{
					Address:       "events",
					Subscriptions: []string{"owner.events"},
				}},
			}},
		})
		DeferCleanup(GracefulRemove, ctx, &ownerApp)
		verifyProvisioned(ctx, serviceName, ownerName)

		By("creating consumer app in other namespace with a subscription to the owner's address")
		consumerCertName := consumerName + common.AppCertSecretSuffix
		InstallCert(consumerCertName, otherNamespace, func(candidate *cmv1.Certificate) {
			candidate.Spec.SecretName = consumerCertName
			candidate.Spec.CommonName = consumerName
			candidate.Spec.Subject.Organizations = nil
			candidate.Spec.Subject.OrganizationalUnits = []string{otherNamespace}
			candidate.Spec.IssuerRef = cmmetav1.ObjectReference{
				Name: caIssuer.Name,
				Kind: "ClusterIssuer",
			}
		})
		DeferCleanup(UninstallCert, consumerCertName, otherNamespace)

		consumerApp := broker.BrokerApp{
			TypeMeta: metav1.TypeMeta{
				Kind:       "BrokerApp",
				APIVersion: broker.GroupVersion.Identifier(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      consumerName,
				Namespace: otherNamespace,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"test": "fm-xns-drain"},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
				},
				Capabilities: []broker.AppCapabilityType{{
					ConsumerOf: []broker.AddressRef{{
						Address:       "events",
						AppName:       ownerName,
						AppNamespace:  defaultNamespace,
						Subscriptions: []string{"old-sub.events"},
					}},
				}},
			},
		}
		Expect(k8sClient.Create(ctx, &consumerApp)).Should(Succeed())
		DeferCleanup(GracefulRemove, ctx, &consumerApp)

		By("waiting for the cross-namespace consumer to be provisioned")
		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: consumerName, Namespace: otherNamespace}, current)).Should(Succeed())
			g.Expect(meta.IsStatusConditionTrue(current.Status.Conditions, broker.ReadyConditionType)).Should(BeTrue())
			g.Expect(current.Status.Service.AssignedPort).ShouldNot(BeZero())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())
			g.Expect(current.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(ownerName)))
			g.Expect(current.Status.ProvisionedApps).Should(ContainElement(ContainSubstring(consumerName)))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("setting up MQTT endpoint via the owner app's acceptor")
		ep := setupMqttEndpoint(ctx, serviceName, ownerName)
		DeferCleanup(cleanupMqttEndpoint, ep)

		By("establishing durable subscriber via owner")
		mqttSubscribe(ctx, ep, "owner", "events")

		By("setting up MQTT endpoint via the reference app's acceptor")
		ep2 := setupMqttEndpoint(ctx, serviceName, consumerName, otherNamespace)
		DeferCleanup(cleanupMqttEndpoint, ep2)

		By("establishing durable subscriber via ref")
		mqttSubscribe(ctx, ep2, "old-sub", "events")

		By("seeding messages")
		mqttProduce(ctx, ep, "events", 5)

		By("renaming the cross-namespace consumer's subscription from old-sub to new-sub")
		Eventually(func(g Gomega) {
			current := &broker.BrokerApp{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: consumerName, Namespace: otherNamespace}, current)).Should(Succeed())
			current.Spec.Capabilities[0].ConsumerOf[0].Subscriptions = []string{"new-sub.events"}
			g.Expect(k8sClient.Update(ctx, current)).Should(Succeed())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("verifying old-sub.events appears in pendingEmpty for the cross-namespace consumer")
		Eventually(func(g Gomega) {
			current := &broker.BrokerService{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&service), current)).Should(Succeed())

			if verbose {
				fmt.Printf("\n Service Status: %v\n", current.Status)
			}
			var pending map[string]string
			for _, pe := range current.Status.PendingEmpty {
				if pe.App == AppIdentity(&consumerApp) {
					pending = pe.Messages
				}
			}
			g.Expect(pending).Should(HaveKeyWithValue("events::old-sub.events", "5"),
				"5 messages on the renamed subscription are not tracked as pending empty, status.pendingEmpty=%+v",
				current.Status.PendingEmpty)
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("the old subscriber can still drain its messages")
		mqttConsume(ctx, ep2, "old-sub", "events", 5)

		By("the owner subscriber can consume its messages")
		mqttConsume(ctx, ep, "owner", "events", 5)
	})

})

func getBrokerForService(ctx context.Context, serviceName string) (*broker.Broker, error) {
	current := &broker.Broker{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, current)
	return current, err
}
