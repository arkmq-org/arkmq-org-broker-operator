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

// Limits of the admission drain gate, on a real broker. Removing a queue from
// the configuration strands its messages: the broker keeps them, nobody may
// reach them. Each limit spec asserts the behaviour that would avoid stranding,
// so a failure demonstrates the limit.

import (
	"context"
	"fmt"
	"os"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia_client"
)

const allowMessageLoss = "broker.arkmq.org/allow-message-loss"

func installGateIssuers() {
	if !CertManagerInstalled() {
		Expect(InstallCertManager()).To(Succeed())
	}
	rootIssuer = InstallClusteredIssuer(rootIssuerName, nil)
	rootCert = InstallCert(rootCertName, rootCertNamespce, func(candidate *cmv1.Certificate) {
		candidate.Spec.IsCA = true
		candidate.Spec.CommonName = "artemis.root.ca"
		candidate.Spec.SecretName = rootCertSecretName
		candidate.Spec.IssuerRef = cmmetav1.ObjectReference{Name: rootIssuer.Name, Kind: "ClusterIssuer"}
	})
	caIssuer = InstallClusteredIssuer(caIssuerName, func(candidate *cmv1.ClusterIssuer) {
		candidate.Spec.SelfSigned = nil
		candidate.Spec.CA = &cmv1.CAIssuer{SecretName: rootCertSecretName}
	})
	InstallCaBundle(common.DefaultOperatorCASecretName, rootCertSecretName, caPemTrustStoreName)
	InstallCert(common.DefaultOperatorCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
		candidate.Spec.SecretName = common.DefaultOperatorCertSecretName
		candidate.Spec.CommonName = common.OperatorName
		candidate.Spec.IssuerRef = cmmetav1.ObjectReference{Name: caIssuer.Name, Kind: "ClusterIssuer"}
	})
}

// deployGateService creates a BrokerService selected by label test=<label>
func deployGateService(ctx context.Context, label string) *broker.BrokerService {
	name := NextSpecResourceName()
	operandCert := name + "-" + common.DefaultOperandCertSecretName
	InstallCert(operandCert, defaultNamespace, func(candidate *cmv1.Certificate) {
		candidate.Spec.SecretName = operandCert
		candidate.Spec.CommonName = name
		candidate.Spec.DNSNames = []string{name, fmt.Sprintf("%s.%s", name, defaultNamespace), fmt.Sprintf("%s.%s.svc.%s", name, defaultNamespace, common.GetClusterDomain()), common.ClusterDNSWildCard(name, defaultNamespace)}
		candidate.Spec.IssuerRef = cmmetav1.ObjectReference{Name: caIssuer.Name, Kind: "ClusterIssuer"}
	})
	InstallCert(common.DefaultPrometheusCertSecretName, defaultNamespace, func(candidate *cmv1.Certificate) {
		candidate.Spec.SecretName = common.DefaultPrometheusCertSecretName
		candidate.Spec.CommonName = "prometheus"
		candidate.Spec.IssuerRef = cmmetav1.ObjectReference{Name: caIssuer.Name, Kind: "ClusterIssuer"}
	})
	svc := &broker.BrokerService{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: defaultNamespace, Labels: map[string]string{"test": label}},
		Spec: broker.BrokerServiceSpec{Resources: corev1.ResourceRequirements{
			Limits: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
		}},
	}
	Expect(k8sClient.Create(ctx, svc)).To(Succeed())
	DeferCleanup(func() {
		forceDelete(context.Background(), &broker.BrokerService{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: defaultNamespace}})
		UninstallCert(common.DefaultPrometheusCertSecretName, defaultNamespace)
		UninstallCert(operandCert, defaultNamespace)
	})
	return svc
}

// deployGateApp creates the app with its client cert, selecting services labelled test=<label>
func deployGateApp(ctx context.Context, label string, app *broker.BrokerApp) (appCertName string) {
	appCertName = app.Name + common.AppCertSecretSuffix
	InstallCert(appCertName, defaultNamespace, func(candidate *cmv1.Certificate) {
		candidate.Spec.SecretName = appCertName
		candidate.Spec.CommonName = app.Name
		candidate.Spec.Subject.Organizations = nil
		candidate.Spec.Subject.OrganizationalUnits = []string{defaultNamespace}
		candidate.Spec.IssuerRef = cmmetav1.ObjectReference{Name: caIssuer.Name, Kind: "ClusterIssuer"}
	})
	app.Namespace = defaultNamespace
	app.Spec.ServiceSelector = &metav1.LabelSelector{MatchLabels: map[string]string{"test": label}}
	Expect(k8sClient.Create(ctx, app)).To(Succeed())
	name := app.Name
	DeferCleanup(func() {
		forceDelete(context.Background(), &broker.BrokerApp{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: defaultNamespace}})
		UninstallCert(appCertName, defaultNamespace)
	})
	return appCertName
}

func forceDelete(ctx context.Context, obj client.Object) {
	if k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), obj) != nil {
		return
	}
	ann := obj.GetAnnotations()
	if ann == nil {
		ann = map[string]string{}
	}
	ann[allowMessageLoss] = "true"
	obj.SetAnnotations(ann)
	_ = k8sClient.Update(ctx, obj)
	_ = k8sClient.Delete(ctx, obj)
}

func updateApp(ctx context.Context, key types.NamespacedName, change func(*broker.BrokerApp), opts ...client.UpdateOption) error {
	latest := &broker.BrokerApp{}
	Expect(k8sClient.Get(ctx, key, latest)).To(Succeed())
	change(latest)
	return k8sClient.Update(ctx, latest, opts...)
}

// waitAppProvisioned waits for the app to be bound and its current generation running on the broker
func waitAppProvisioned(ctx context.Context, key types.NamespacedName) *broker.BrokerApp {
	app := &broker.BrokerApp{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		g.Expect(app.Status.Service).NotTo(BeNil())
		svc := &broker.BrokerService{}
		g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: app.Status.Service.Name, Namespace: app.Status.Service.Namespace}, svc)).To(Succeed())
		g.Expect(svc.Status.ProvisionedApps).To(ContainElement(AppIdentityWithGeneration(app)))
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	return app
}

// brokerQueueCount reads a queue directly on the broker, independent of the app status
func brokerQueueCount(ctx context.Context, serviceName, address, queue, routingType string) (int64, error) {
	brokerCR := &broker.Broker{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, brokerCR); err != nil {
		return 0, err
	}
	agents := jolokia_client.GetMinimalJolokiaAgentsForBroker(brokerCR, k8sClient)
	return agents[0].Artemis.GetQueueMessageCount(address, queue, routingType)
}

func appDrainCounts(app *broker.BrokerApp, address string) map[string]int64 {
	counts := map[string]int64{}
	for _, a := range app.Status.Addresses {
		if a.Address == address {
			for _, q := range a.Queues {
				counts[q.Name] = q.MessageCount
			}
		}
	}
	return counts
}

func brokerTotalMessages(ctx context.Context, serviceName string) (string, error) {
	brokerCR := &broker.Broker{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: defaultNamespace}, brokerCR); err != nil {
		return "", err
	}
	return jolokia_client.GetMinimalJolokiaAgentsForBroker(brokerCR, k8sClient)[0].Artemis.GetTotalMessageCount()
}

func expectStranded(ctx context.Context, serviceName string, messages int) {
	GinkgoHelper()
	Eventually(func(g Gomega) {
		total, err := brokerTotalMessages(ctx, serviceName)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(total).To(Equal(fmt.Sprint(messages)))
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
}

// waitObserved waits for the operator to publish the drain state of the current generation
func waitObserved(ctx context.Context, key types.NamespacedName) *broker.BrokerApp {
	app := &broker.BrokerApp{}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, key, app)).To(Succeed())
		g.Expect(app.Status.Service).NotTo(BeNil())
		g.Expect(app.Status.AddressesObservedGeneration).To(Equal(app.Generation))
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	return app
}

const coreURL = "tcp://${BROKER_SERVICE_HOST}:${BROKER_SERVICE_PORT}" +
	"?sslEnabled=true\\&trustStoreType=PEMCA\\&trustStorePath=/app/tls/ca/ca.pem" +
	"\\&keyStoreType=PEMCFG\\&keyStorePath=/app/tls/pem/tls.pemcfg"

// tryArtemisClientJob runs the artemis CLI once as the app and reports whether it succeeded
func tryArtemisClientJob(ctx context.Context, app *broker.BrokerApp, appCertName, name, args string) bool {
	GinkgoHelper()
	job := artemisClientJob(app, appCertName, name, args)
	job.Spec.BackoffLimit = ptr.To(int32(0))
	job.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyNever
	Expect(k8sClient.Create(ctx, job)).To(Succeed())
	DeferCleanup(func() {
		_ = k8sClient.Delete(context.Background(), job, client.PropagationPolicy(metav1.DeletePropagationBackground))
	})
	succeeded := false
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(job), job)).To(Succeed())
		succeeded = job.Status.Succeeded == 1
		g.Expect(job.Status.Succeeded + job.Status.Failed).To(BeNumerically(">=", 1))
	}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
	return succeeded
}

var _ = Describe("brokerapp drain gate limits", func() {

	BeforeEach(func() {
		if os.Getenv("RUN_GATE_LIMITS") != "true" {
			Skip("documents limits of the drain gate, set RUN_GATE_LIMITS=true to run")
		}
		BeforeEachSpec()
		if os.Getenv("USE_EXISTING_CLUSTER") == "true" {
			installGateIssuers()
		}
	})

	AfterEach(func() {
		if os.Getenv("RUN_GATE_LIMITS") == "true" {
			AfterEachSpec()
		}
	})

	It("premise: removing an address from the configuration strands its messages", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}
		ctx := context.Background()
		label := "gate-premise"
		address := "PREMISE.ORDERS"

		svc := deployGateService(ctx, label)
		app := &broker.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: "premise-app"},
			Spec: broker.BrokerAppSpec{
				Addresses: []broker.AddressType{{Address: address}},
				Capabilities: []broker.AppCapabilityType{{
					ProducerOf: []broker.AddressRef{{Address: address}},
					ConsumerOf: []broker.AddressRef{{Address: address}},
				}},
			},
		}
		certName := deployGateApp(ctx, label, app)
		key := client.ObjectKeyFromObject(app)
		current := waitAppProvisioned(ctx, key)

		runArtemisClientJob(ctx, current, certName, "premise-producer",
			fmt.Sprintf("producer --protocol=AMQP --message-count 3 --destination queue://%s", address))
		Eventually(func(g Gomega) {
			n, err := brokerQueueCount(ctx, svc.Name, address, address, "anycast")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(n).To(Equal(int64(3)))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("removing the address and its capabilities, no gate installed")
		Expect(updateApp(ctx, key, func(a *broker.BrokerApp) {
			a.Spec.Addresses = nil
			a.Spec.Capabilities = nil
		})).To(Succeed())
		waitAppProvisioned(ctx, key)

		By("the broker runs the new configuration: the app's grant to read the queue is gone")
		Eventually(func(g Gomega) {
			_, err := brokerQueueCount(ctx, svc.Name, address, address, "anycast")
			g.Expect(err).To(MatchError(ContainSubstring("does not have permission='VIEW'")))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("broker-wide message count while the address is out of the configuration")
		Consistently(func(g Gomega) {
			total, err := brokerTotalMessages(ctx, svc.Name)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(total).To(Equal("3"))
		}, 60*time.Second, 10*time.Second).Should(Succeed(), "the broker keeps the messages of an address removed from its configuration")

		var err error

		By("re-adding the address with consume only, which restores the right to read its count")
		Expect(updateApp(ctx, key, func(a *broker.BrokerApp) {
			a.Spec.Addresses = []broker.AddressType{{Address: address}}
			a.Spec.Capabilities = []broker.AppCapabilityType{{ConsumerOf: []broker.AddressRef{{Address: address}}}}
		})).To(Succeed())
		waitAppProvisioned(ctx, key)

		var n int64
		Eventually(func(g Gomega) {
			n, err = brokerQueueCount(ctx, svc.Name, address, address, "anycast")
			g.Expect(err).NotTo(HaveOccurred())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		Expect(n).To(Equal(int64(3)), "re-adding the address gives the stranded messages back")
	})

	It("limit 2 (fixed): a service change that would evict an app is refused", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}
		ctx := context.Background()
		installDrainGatePolicies(ctx)
		label, address := "gate-l2", "L2.ORDERS"

		svc := deployGateService(ctx, label)
		app := &broker.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: "l2-app"},
			Spec: broker.BrokerAppSpec{
				Addresses: []broker.AddressType{{Address: address}},
				Capabilities: []broker.AppCapabilityType{{
					ProducerOf: []broker.AddressRef{{Address: address}},
					ConsumerOf: []broker.AddressRef{{Address: address}},
				}},
			},
		}
		cert := deployGateApp(ctx, label, app)
		key := client.ObjectKeyFromObject(app)
		current := waitAppProvisioned(ctx, key)
		runArtemisClientJob(ctx, current, cert, "l2-producer",
			fmt.Sprintf("producer --protocol=AMQP --message-count 3 --destination queue://%s", address))

		By("send revoked, the operator reports 3 messages and the gate refuses removing the address")
		Expect(updateApp(ctx, key, func(a *broker.BrokerApp) { a.Spec.Capabilities[0].ProducerOf = nil })).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(appDrainCounts(waitObserved(ctx, key), address)).To(Equal(map[string]int64{address: 3}))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		expectDrainGateDenial(updateApp(ctx, key, func(a *broker.BrokerApp) { a.Spec.Addresses = nil; a.Spec.Capabilities = nil }), address)

		By("the service stops accepting the app")
		latest := &broker.BrokerService{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(svc), latest)).To(Succeed())
		latest.Spec.AppSelectorExpression = "false"
		evictErr := k8sClient.Update(ctx, latest)

		if evictErr == nil {
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, key, current)).To(Succeed())
				g.Expect(current.Status.Service).To(BeNil())
			}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
			expectStranded(ctx, svc.Name, 3)
			GinkgoWriter.Println("LIMIT 2: app evicted, its 3 messages are still on the broker with no owner")
		}
		Expect(evictErr).To(HaveOccurred(), "evicting an app whose queues hold messages should be refused like removing them")
	})

	It("limit 3 (refuted): an app granted send by another app loses it once the revoked grant is observed", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}
		ctx := context.Background()
		installDrainGatePolicies(ctx)
		label, address := "gate-l3", "L3.ORDERS"

		svc := deployGateService(ctx, label)
		owner := &broker.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: "l3-owner"},
			Spec: broker.BrokerAppSpec{
				Addresses:    []broker.AddressType{{Address: address, ProducerOfAppSelector: "true"}},
				Capabilities: []broker.AppCapabilityType{{ConsumerOf: []broker.AddressRef{{Address: address}}}},
			},
		}
		deployGateApp(ctx, label, owner)
		ownerKey := client.ObjectKeyFromObject(owner)
		peer := &broker.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: "l3-peer"},
			Spec: broker.BrokerAppSpec{
				Capabilities: []broker.AppCapabilityType{{
					ProducerOf: []broker.AddressRef{{Address: address, AppName: owner.Name, AppNamespace: defaultNamespace}},
				}},
			},
		}
		peerCert := deployGateApp(ctx, label, peer)
		waitAppProvisioned(ctx, ownerKey)
		currentPeer := waitAppProvisioned(ctx, client.ObjectKeyFromObject(peer))

		By("the owner revokes the grant, the operator observes that generation with an empty queue")
		Expect(updateApp(ctx, ownerKey, func(a *broker.BrokerApp) { a.Spec.Addresses[0].ProducerOfAppSelector = "" })).To(Succeed())
		Eventually(func(g Gomega) {
			g.Expect(appDrainCounts(waitObserved(ctx, ownerKey), address)).To(Equal(map[string]int64{address: 0}))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("the peer tries to send")
		sent := tryArtemisClientJob(ctx, currentPeer, peerCert, "l3-peer-producer",
			fmt.Sprintf("producer --protocol=AMQP --message-count 1 --destination queue://%s", address))

		if sent {
			n, err := brokerQueueCount(ctx, svc.Name, address, address, "anycast")
			GinkgoWriter.Printf("LIMIT 3: peer sent after the revocation was observed, broker count=%d err=%v, owner status still %v\n",
				n, err, appDrainCounts(waitObserved(ctx, ownerKey), address))
			removeErr := updateApp(ctx, ownerKey, func(a *broker.BrokerApp) { a.Spec.Addresses = nil; a.Spec.Capabilities = nil })
			GinkgoWriter.Printf("LIMIT 3: owner removal on the stale zero: %v\n", removeErr)
			if removeErr == nil {
				waitAppProvisioned(ctx, ownerKey)
				expectStranded(ctx, svc.Name, 1)
			}
		}
		Expect(sent).To(BeFalse(), "once the revoked grant is observed, no app should be able to send to the address")
	})

	It("limit 4 (fixed): a subscription of a topic still receiving cannot be removed", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}
		ctx := context.Background()
		installDrainGatePolicies(ctx)
		label, address := "gate-l4", "L4.PRICES"

		svc := deployGateService(ctx, label)
		app := &broker.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: "l4-app"},
			Spec: broker.BrokerAppSpec{
				Addresses: []broker.AddressType{{Address: address, Subscriptions: []string{"sub-a", "sub-b"}}},
				Capabilities: []broker.AppCapabilityType{{
					ProducerOf: []broker.AddressRef{{Address: address, PubSub: ptr.To(true)}},
				}},
			},
		}
		cert := deployGateApp(ctx, label, app)
		key := client.ObjectKeyFromObject(app)
		var current *broker.BrokerApp
		Eventually(func(g Gomega) {
			current = waitObserved(ctx, key)
			g.Expect(current.Status.Addresses).To(ContainElement(HaveField("SendGranted", true)))
			g.Expect(appDrainCounts(current, address)).To(Equal(map[string]int64{"sub-a": broker.QueueDrainUnknown, "sub-b": broker.QueueDrainUnknown}),
				"a topic that may still receive is not read")
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("the topic delivers")
		runArtemisClientJob(ctx, current, cert, "l4-producer",
			fmt.Sprintf("producer --protocol=AMQP --message-count 5 --destination topic://%s", address))
		n, err := brokerQueueCount(ctx, svc.Name, address, "sub-a", "multicast")
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(int64(5)))
		GinkgoWriter.Printf("LIMIT 4: broker sub-a=%d, status %v\n", n, appDrainCounts(waitObserved(ctx, key), address))

		removeErr := updateApp(ctx, key, func(a *broker.BrokerApp) { a.Spec.Addresses[0].Subscriptions = []string{"sub-b"} })
		if removeErr == nil {
			waitAppProvisioned(ctx, key)
			expectStranded(ctx, svc.Name, 10)
			GinkgoWriter.Println("LIMIT 4: sub-a removed with 5 messages, they are stranded")
		}
		Expect(removeErr).To(HaveOccurred(), "removing a subscription that holds messages should be refused")
	})

	It("limit 5a: a divert fills an address the gate believes empty", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}
		ctx := context.Background()
		installDrainGatePolicies(ctx)
		label, address, source := "gate-l5a", "L5.ORDERS", "L5.SRC"

		svc := deployGateService(ctx, label)
		app := &broker.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: "l5a-app"},
			Spec: broker.BrokerAppSpec{
				Addresses: []broker.AddressType{{Address: address}, {Address: source}},
				Capabilities: []broker.AppCapabilityType{{
					ProducerOf: []broker.AddressRef{{Address: source}},
					ConsumerOf: []broker.AddressRef{{Address: address}, {Address: source}},
				}},
			},
		}
		cert := deployGateApp(ctx, label, app)
		key := client.ObjectKeyFromObject(app)
		current := waitAppProvisioned(ctx, key)

		By("a divert from the source address, added to the control plane override")
		overrideKey := types.NamespacedName{Name: svc.Name + "-control-plane-override", Namespace: defaultNamespace}
		Eventually(func(g Gomega) {
			override := &corev1.Secret{}
			g.Expect(k8sClient.Get(ctx, overrideKey, override)).To(Succeed())
			override.Data["zz_gate_divert.properties"] = []byte(
				"divertConfigurations.gatedivert.address=" + source + "\n" +
					"divertConfigurations.gatedivert.forwardingAddress=" + address + "\n" +
					"divertConfigurations.gatedivert.exclusive=true\n")
			g.Expect(k8sClient.Update(ctx, override)).To(Succeed())
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("waiting for the divert to route")
		Eventually(func(g Gomega) {
			runArtemisClientJob(ctx, current, cert, fmt.Sprintf("l5a-probe-%d", time.Now().UnixNano()%100000),
				fmt.Sprintf("producer --protocol=AMQP --message-count 1 --destination queue://%s", source))
			n, err := brokerQueueCount(ctx, svc.Name, address, address, "anycast")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(n).To(BeNumerically(">", 0))
		}, existingClusterTimeout, 15*time.Second).Should(Succeed())

		By("draining the address until the operator reports zero")
		n, _ := brokerQueueCount(ctx, svc.Name, address, address, "anycast")
		runArtemisClientJob(ctx, current, cert, "l5a-consumer",
			fmt.Sprintf("consumer --protocol=AMQP --message-count %d --destination queue://%s", n, address))
		Eventually(func(g Gomega) {
			g.Expect(appDrainCounts(waitObserved(ctx, key), address)).To(HaveKeyWithValue(address, int64(0)))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())

		By("one more message through the divert, no app granted send on the address")
		runArtemisClientJob(ctx, current, cert, "l5a-producer",
			fmt.Sprintf("producer --protocol=AMQP --message-count 1 --destination queue://%s", source))
		n, err := brokerQueueCount(ctx, svc.Name, address, address, "anycast")
		Expect(err).NotTo(HaveOccurred())
		GinkgoWriter.Printf("LIMIT 5a: broker %s=%d, status %v\n", address, n, appDrainCounts(waitObserved(ctx, key), address))

		removeErr := updateApp(ctx, key, func(a *broker.BrokerApp) {
			a.Spec.Addresses = []broker.AddressType{{Address: source}}
			a.Spec.Capabilities[0].ConsumerOf = []broker.AddressRef{{Address: source}}
		})
		GinkgoWriter.Printf("LIMIT 5a: removal of %s holding %d: %v\n", address, n, removeErr)
		Expect(removeErr).To(HaveOccurred(), "removing an address that holds messages should be refused")
	})

	It("limit 5b (refuted): a scheduled message is counted", func() {
		if os.Getenv("USE_EXISTING_CLUSTER") != "true" {
			return
		}
		ctx := context.Background()
		installDrainGatePolicies(ctx)
		label, address := "gate-l5b", "L5B.ORDERS"

		svc := deployGateService(ctx, label)
		app := &broker.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{Name: "l5b-app"},
			Spec: broker.BrokerAppSpec{
				Addresses: []broker.AddressType{{Address: address}},
				Capabilities: []broker.AppCapabilityType{{
					ProducerOf: []broker.AddressRef{{Address: address}},
					ConsumerOf: []broker.AddressRef{{Address: address}},
				}},
			},
		}
		cert := deployGateApp(ctx, label, app)
		key := client.ObjectKeyFromObject(app)
		current := waitAppProvisioned(ctx, key)

		By("sending one message scheduled an hour ahead, over CORE")
		runArtemisClientJob(ctx, current, cert, "l5b-producer",
			fmt.Sprintf(`producer --protocol=CORE --message-count 1 --destination queue://%s `+
				`--properties '[{"type":"long","key":"_AMQ_SCHED_DELIVERY","value":"'$(( $(date +%%s) * 1000 + 3600000 ))'"}]' --url %s`,
				address, coreURL))

		Expect(updateApp(ctx, key, func(a *broker.BrokerApp) { a.Spec.Capabilities[0].ProducerOf = nil })).To(Succeed())
		var counts map[string]int64
		Eventually(func(g Gomega) {
			counts = appDrainCounts(waitObserved(ctx, key), address)
			g.Expect(counts).To(HaveKey(address))
			g.Expect(counts[address]).NotTo(Equal(broker.QueueDrainUnknown))
		}, existingClusterTimeout, existingClusterInterval).Should(Succeed())
		total, _ := brokerTotalMessages(ctx, svc.Name)
		GinkgoWriter.Printf("LIMIT 5b: status MessageCount=%d, broker TotalMessageCount=%s\n", counts[address], total)
		Expect(counts[address]).To(Equal(int64(1)), "a scheduled message must keep the address from being removed")
	})
})
