package controllers

import (
	"context"
	"fmt"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("address references validation", func() {

	const (
		ns      = "default"
		svcName = "test-service"
	)

	svc := func() *v1beta2.BrokerService {
		return NewBrokerService(svcName, ns).
			WithLabels(map[string]string{"type": "broker"}).
			Build()
	}

	Context("per-role grant enforcement", func() {

		It("rejects producer capability when only consumer selector exists", Label(unitLabel), func() {
			ownerApp := NewBrokerApp("owner", ns).
				WithAddresses(NewAddressType("orders").
					WithConsumerOfAppSelector("app.metadata.name == 'requester'").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			requesterApp := NewBrokerApp("requester", ns).
				WithProducerOf(NewAddressRef("orders").WithAppRef(ns, "owner").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), ownerApp, requesterApp)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "requester", Namespace: ns}}
			_, err := env.Reconciler.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(v1.ConditionFalse))
			Expect(deployedCond.Message).To(ContainSubstring("does not share address 'orders' for role 'producer'"))
		})

		It("rejects consumer capability when only producer selector exists", Label(unitLabel), func() {
			ownerApp := NewBrokerApp("owner", ns).
				WithAddresses(NewAddressType("orders").
					WithProducerOfAppSelector("app.metadata.name == 'requester'").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			requesterApp := NewBrokerApp("requester", ns).
				WithConsumerOf(NewAddressRef("orders").WithAppRef(ns, "owner").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), ownerApp, requesterApp)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "requester", Namespace: ns}}
			_, err := env.Reconciler.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(v1.ConditionFalse))
			Expect(deployedCond.Message).To(ContainSubstring("does not share address 'orders' for role 'consumer'"))
		})

		It("accepts consumer capability when consumer selector matches", Label(unitLabel), func() {
			ownerApp := NewBrokerApp("owner", ns).
				WithAddresses(NewAddressType("orders").
					WithConsumerOfAppSelector("app.metadata.name == 'consumer-app'").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			consumerApp := NewBrokerApp("consumer-app", ns).
				WithConsumerOf(NewAddressRef("orders").WithAppRef(ns, "owner").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), ownerApp, consumerApp)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "consumer-app", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionTrue))
		})

		It("accepts producer capability when producer selector matches", Label(unitLabel), func() {
			ownerApp := NewBrokerApp("owner", ns).
				WithAddresses(NewAddressType("orders").
					WithProducerOfAppSelector("app.metadata.name == 'producer-app'").Build()).
				WithConsumerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			producerApp := NewBrokerApp("producer-app", ns).
				WithProducerOf(NewAddressRef("orders").WithAppRef(ns, "owner").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), ownerApp, producerApp)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "producer-app", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionTrue))
		})
	})

	Context("namespace inference", func() {

		It("selector matches by app name regardless of namespace", Label(unitLabel), func() {
			ownerApp := NewBrokerApp("owner", ns).
				WithAddresses(NewAddressType("events").
					WithConsumerOfAppSelector("app.metadata.name == 'consumer-app'").Build()).
				WithProducerOf(NewAddressRef("events").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			consumerApp := NewBrokerApp("consumer-app", ns).
				WithConsumerOf(NewAddressRef("events").WithAppRef(ns, "owner").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), ownerApp, consumerApp)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "consumer-app", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionTrue))
		})

		It("selector matches when app ref omits namespace", Label(unitLabel), func() {
			ownerApp := NewBrokerApp("owner", ns).
				WithAddresses(NewAddressType("events").
					WithConsumerOfAppSelector("app.metadata.name == 'consumer-app'").Build()).
				WithProducerOf(NewAddressRef("events").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			consumerApp := NewBrokerApp("consumer-app", ns).
				WithConsumerOf(NewAddressRef("events").WithAppRef("", "owner").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), ownerApp, consumerApp)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "consumer-app", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionTrue))
		})
	})

	Context("grant matching", func() {

		It("rejects when selector names a different app", Label(unitLabel), func() {
			ownerApp := NewBrokerApp("owner", ns).
				WithAddresses(NewAddressType("orders").
					WithConsumerOfAppSelector("app.metadata.name == 'other-app'").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			wrongApp := NewBrokerApp("wrong-app", ns).
				WithConsumerOf(NewAddressRef("orders").WithAppRef(ns, "owner").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), ownerApp, wrongApp)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "wrong-app", Namespace: ns}}
			_, err := env.Reconciler.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(v1.ConditionFalse))
			Expect(deployedCond.Message).To(ContainSubstring("does not share address 'orders'"))
		})

		It("accepts when selector uses namespace-only (all apps in namespace)", Label(unitLabel), func() {
			ownerApp := NewBrokerApp("owner", ns).
				WithAddresses(NewAddressType("orders").
					WithConsumerOfAppSelector(fmt.Sprintf("app.metadata.namespace == '%s'", ns)).Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			anyApp := NewBrokerApp("any-consumer", ns).
				WithConsumerOf(NewAddressRef("orders").WithAppRef(ns, "owner").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), ownerApp, anyApp)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "any-consumer", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionTrue))
		})

		It("rejects when selector targets different namespace", Label(unitLabel), func() {
			ownerApp := NewBrokerApp("owner", ns).
				WithAddresses(NewAddressType("orders").
					WithConsumerOfAppSelector("app.metadata.namespace == 'other-ns'").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			wrongNsApp := NewBrokerApp("consumer-in-default", ns).
				WithConsumerOf(NewAddressRef("orders").WithAppRef(ns, "owner").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), ownerApp, wrongNsApp)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "consumer-in-default", Namespace: ns}}
			_, err := env.Reconciler.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(v1.ConditionFalse))
			Expect(deployedCond.Message).To(ContainSubstring("does not share address 'orders'"))
		})
	})

	Context("address without sharing selectors", func() {

		It("rejects reference to address with no sharing selector", Label(unitLabel), func() {
			ownerApp := NewBrokerApp("owner", ns).
				WithAddresses(NewAddressType("private-data").Build()).
				WithProducerOf(NewAddressRef("private-data").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			consumerApp := NewBrokerApp("consumer-app", ns).
				WithConsumerOf(NewAddressRef("private-data").WithAppRef(ns, "owner").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), ownerApp, consumerApp)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "consumer-app", Namespace: ns}}
			_, err := env.Reconciler.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(v1.ConditionFalse))
			Expect(deployedCond.Message).To(ContainSubstring("does not share address 'private-data'"))
		})
	})

	Context("empty app reference validation", func() {

		It("rejects appNamespace without appName in producer capability", Label(unitLabel), func() {
			app := NewBrokerApp("bad-ref-app", ns).
				WithProducerOf(v1beta2.AddressRef{
					Address:      "orders",
					AppNamespace: "some-ns",
				}).
				Build()

			env := NewTestEnvironment(ns, svc(), app)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "bad-ref-app", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Reason).To(Equal(v1beta2.ValidConditionAddressTypeError))
			Expect(validCond.Message).To(ContainSubstring("appName is required when appNamespace is set"))
		})

		It("rejects appNamespace without appName in consumer capability", Label(unitLabel), func() {
			app := NewBrokerApp("bad-ref-app", ns).
				WithConsumerOf(v1beta2.AddressRef{
					Address:      "orders",
					AppNamespace: "some-ns",
				}).
				Build()

			env := NewTestEnvironment(ns, svc(), app)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "bad-ref-app", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Reason).To(Equal(v1beta2.ValidConditionAddressTypeError))
			Expect(validCond.Message).To(ContainSubstring("appName is required when appNamespace is set"))
		})
	})

	Context("sharing selector CEL validation", func() {

		It("rejects invalid CEL syntax on producerOfAppSelector", Label(unitLabel), func() {
			app := NewBrokerApp("bad-cel-app", ns).
				WithAddresses(NewAddressType("orders").
					WithProducerOfAppSelector("app.metadata.name ==").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), app)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "bad-cel-app", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Reason).To(Equal(v1beta2.ValidConditionAddressTypeError))
			Expect(validCond.Message).To(ContainSubstring("producerOfAppSelector"))
		})

		It("rejects invalid CEL syntax on consumerOfAppSelector", Label(unitLabel), func() {
			app := NewBrokerApp("bad-cel-app", ns).
				WithAddresses(NewAddressType("orders").
					WithConsumerOfAppSelector("app.metadata.name ==").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), app)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "bad-cel-app", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Reason).To(Equal(v1beta2.ValidConditionAddressTypeError))
			Expect(validCond.Message).To(ContainSubstring("consumerOfAppSelector"))
		})

		It("rejects non-boolean CEL expression on producerOfAppSelector", Label(unitLabel), func() {
			app := NewBrokerApp("non-bool-cel", ns).
				WithAddresses(NewAddressType("orders").
					WithProducerOfAppSelector("app.metadata.name").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), app)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "non-bool-cel", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Reason).To(Equal(v1beta2.ValidConditionAddressTypeError))
			Expect(validCond.Message).To(ContainSubstring("producerOfAppSelector"))
		})

		It("rejects non-boolean CEL expression on consumerOfAppSelector", Label(unitLabel), func() {
			app := NewBrokerApp("non-bool-cel", ns).
				WithAddresses(NewAddressType("orders").
					WithConsumerOfAppSelector("app.metadata.namespace").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), app)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "non-bool-cel", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Reason).To(Equal(v1beta2.ValidConditionAddressTypeError))
			Expect(validCond.Message).To(ContainSubstring("consumerOfAppSelector"))
		})

		It("accepts valid CEL expression on sharing selectors", Label(unitLabel), func() {
			app := NewBrokerApp("valid-cel-app", ns).
				WithAddresses(NewAddressType("orders").
					WithProducerOfAppSelector("app.metadata.name == 'producer'").
					WithConsumerOfAppSelector("app.metadata.namespace == 'team-a'").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			env := NewTestEnvironment(ns, svc(), app)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "valid-cel-app", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionTrue))
		})
	})

	Context("selector matching multiple apps", func() {

		It("accepts when selector matches one of multiple apps", Label(unitLabel), func() {
			ownerApp := NewBrokerApp("owner", ns).
				WithAddresses(NewAddressType("orders").
					WithConsumerOfAppSelector("app.metadata.name == 'app-a' || app.metadata.name == 'app-b'").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			appB := NewBrokerApp("app-b", ns).
				WithConsumerOf(NewAddressRef("orders").WithAppRef(ns, "owner").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), ownerApp, appB)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-b", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionTrue))
		})

		It("accepts both producer and consumer selectors on same address", Label(unitLabel), func() {
			ownerApp := NewBrokerApp("owner", ns).
				WithAddresses(NewAddressType("bidirectional").
					WithProducerOfAppSelector("app.metadata.name == 'peer'").
					WithConsumerOfAppSelector("app.metadata.name == 'peer'").Build()).
				WithProducerOf(NewAddressRef("bidirectional").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			peerApp := NewBrokerApp("peer", ns).
				WithProducerOf(NewAddressRef("bidirectional").WithAppRef(ns, "owner").Build()).
				WithConsumerOf(NewAddressRef("bidirectional").WithAppRef(ns, "owner").Build()).
				Build()

			env := NewTestEnvironment(ns, svc(), ownerApp, peerApp)

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "peer", Namespace: ns}}
			_, _ = env.Reconciler.Reconcile(context.TODO(), req)

			updatedApp := &v1beta2.BrokerApp{}
			Expect(env.Client.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionTrue))
		})
	})
})
