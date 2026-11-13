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
	"fmt"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type fakeBroker struct {
	counts map[string]int64
	reads  []string
	err    error
}

func (f *fakeBroker) install() {
	previous := newQueueCounter
	newQueueCounter = func(_ client.Client, _ *v1beta2.BrokerService) (queueCounter, error) {
		if f.err != nil {
			return nil, f.err
		}
		return func(address, queue, routingType string) (int64, error) {
			key := address + "::" + queue
			f.reads = append(f.reads, key)
			if n, ok := f.counts[key]; ok {
				return n, nil
			}
			return 0, fmt.Errorf("queue %s not found", key)
		}, nil
	}
	DeferCleanup(func() { newQueueCounter = previous })
}

func provisionedService(name, ns string, apps ...*v1beta2.BrokerApp) *v1beta2.BrokerService {
	b := NewBrokerService(name, ns)
	for _, app := range apps {
		b.WithProvisionedApp(AppIdentityWithGeneration(app))
	}
	svc := b.Build()
	svc.Status.Conditions = append(svc.Status.Conditions, metav1.Condition{
		Type:   v1beta2.AppsProvisionedConditionType,
		Status: metav1.ConditionTrue,
		Reason: v1beta2.AppsProvisionedConditionSyncedReason,
	})
	return svc
}

func drainReconciler(app *v1beta2.BrokerApp, svc *v1beta2.BrokerService, others ...client.Object) *BrokerAppInstanceReconciler {
	env := NewTestEnvironment(app.Namespace, append([]client.Object{app, svc}, others...)...)
	r := newAppReconcilerWithClient(env.Client, app.Name, app.Namespace)
	r.instance = app
	r.status = app.Status.DeepCopy()
	r.service = svc
	return &r
}

func drainEntry(status *v1beta2.BrokerAppStatus, address string) *v1beta2.AddressDrainStatus {
	for i := range status.Addresses {
		if status.Addresses[i].Address == address {
			return &status.Addresses[i]
		}
	}
	return nil
}

var _ = Describe("brokerapp drain state", Label(unitLabel), func() {

	ns := "default"
	svcName := "svc"

	boundApp := func(name string) *BrokerAppBuilder {
		return NewBrokerApp(name, ns).WithServiceBinding(svcName, ns, BindingsSecretName(name), 61616)
	}

	It("reports owned anycast queue counts once the broker runs the current generation", func() {
		app := boundApp("orders-app").
			WithAddresses(NewAddressType("orders").Build()).
			WithConsumerOf(NewAddressRef("orders").Build()).
			Build()
		app.Generation = 3

		fb := &fakeBroker{counts: map[string]int64{"orders::orders": 7}}
		fb.install()

		r := drainReconciler(app, provisionedService(svcName, ns, app))
		Expect(r.processDrainState()).To(BeTrue(), "a queue nobody may send to that holds messages is draining and is read again")

		Expect(r.status.AddressesObservedGeneration).To(Equal(int64(3)))
		e := drainEntry(r.status, "orders")
		Expect(e).NotTo(BeNil())
		Expect(e.Queues).To(Equal([]v1beta2.QueueDrainStatus{{Name: "orders", MessageCount: 7}}))
	})

	It("does not read a queue the app may still send to", func() {
		app := boundApp("orders-app").
			WithAddresses(NewAddressType("orders").Build()).
			WithProducerOf(NewAddressRef("orders").Build()).
			Build()
		app.Generation = 1

		fb := &fakeBroker{counts: map[string]int64{"orders::orders": 7}}
		fb.install()

		r := drainReconciler(app, provisionedService(svcName, ns, app))
		Expect(r.processDrainState()).To(BeFalse())

		Expect(fb.reads).To(BeEmpty())
		e := drainEntry(r.status, "orders")
		Expect(e.Queues[0].MessageCount).To(Equal(v1beta2.QueueDrainUnknown))
	})

	It("keeps the previous observation while the current generation is not provisioned", func() {
		app := boundApp("orders-app").WithAddresses(NewAddressType("orders").Build()).Build()
		app.Generation = 2
		app.Status.AddressesObservedGeneration = 1
		app.Status.Addresses = []v1beta2.AddressDrainStatus{{Address: "old"}}

		fb := &fakeBroker{}
		fb.install()

		older := app.DeepCopy()
		older.Generation = 1
		r := drainReconciler(app, provisionedService(svcName, ns, older))
		Expect(r.processDrainState()).To(BeFalse())

		Expect(r.status.AddressesObservedGeneration).To(Equal(int64(1)))
		Expect(r.status.Addresses).To(Equal(app.Status.Addresses))
		Expect(fb.reads).To(BeEmpty())
	})

	It("does not read a queue other apps may be granted send to", func() {
		app := boundApp("owner").
			WithAddresses(NewAddressType("orders").WithProducerOfAppSelector("true").Build()).
			WithConsumerOf(NewAddressRef("orders").Build()).
			Build()
		app.Generation = 1

		fb := &fakeBroker{counts: map[string]int64{"orders::orders": 0}}
		fb.install()

		r := drainReconciler(app, provisionedService(svcName, ns, app))
		Expect(r.processDrainState()).To(BeFalse())

		Expect(fb.reads).To(BeEmpty())
		Expect(r.status.AddressesObservedGeneration).To(Equal(int64(1)))
		Expect(drainEntry(r.status, "orders").SendGranted).To(BeTrue())
		Expect(drainEntry(r.status, "orders").Queues[0].MessageCount).To(Equal(v1beta2.QueueDrainUnknown))
	})

	It("reads send permission of a consumed topic from the app that owns it", func() {
		publisher := boundApp("publisher").
			WithAddresses(NewAddressType("news").WithPubSub(true).WithConsumerOfAppSelector("true").Build()).
			WithProducerOf(NewAddressRef("news").Build()).
			Build()
		reader := boundApp("reader").
			WithConsumerOf(NewAddressRef("news").WithAppRef(ns, "publisher").WithSubscriptions("mine").Build()).
			Build()
		reader.Generation = 1

		fb := &fakeBroker{counts: map[string]int64{"news::mine": 0}}
		fb.install()

		By("the owner still produces: the subscription is not read")
		r := drainReconciler(reader, provisionedService(svcName, ns, reader), publisher)
		Expect(r.processDrainState()).To(BeFalse())
		Expect(fb.reads).To(BeEmpty())
		news := drainEntry(r.status, "news")
		Expect(news.SendGranted).To(BeTrue())
		Expect(news.Queues).To(Equal([]v1beta2.QueueDrainStatus{{Name: "mine", MessageCount: v1beta2.QueueDrainUnknown}}))

		By("the owner revoked send: the subscription is read")
		publisher.Spec.Capabilities = nil
		r = drainReconciler(reader, provisionedService(svcName, ns, reader), publisher)
		r.processDrainState()
		news = drainEntry(r.status, "news")
		Expect(news.SendGranted).To(BeFalse())
		Expect(news.Queues).To(Equal([]v1beta2.QueueDrainStatus{{Name: "mine", MessageCount: 0}}))

		By("the owner cannot be read: send is assumed")
		r = drainReconciler(reader, provisionedService(svcName, ns, reader))
		r.processDrainState()
		Expect(drainEntry(r.status, "news").SendGranted).To(BeTrue())
	})

	It("lists subscription queues of owned topics and of consumed topics", func() {
		app := boundApp("subs-app").
			WithAddresses(NewAddressType("prices").WithSubscriptions("a", "b").Build()).
			WithConsumerOf(NewAddressRef("news").WithAppRef(ns, "publisher").WithSubscriptions("mine").Build()).
			Build()
		app.Generation = 1

		publisher := boundApp("publisher").
			WithAddresses(NewAddressType("news").WithPubSub(true).WithConsumerOfAppSelector("true").Build()).
			Build()

		fb := &fakeBroker{counts: map[string]int64{"prices::a": 0, "prices::b": 2, "news::mine": 0}}
		fb.install()

		r := drainReconciler(app, provisionedService(svcName, ns, app), publisher)
		r.processDrainState()

		prices := drainEntry(r.status, "prices")
		Expect(prices.Queues).To(Equal([]v1beta2.QueueDrainStatus{{Name: "a", MessageCount: 0}, {Name: "b", MessageCount: 2}}))

		news := drainEntry(r.status, "news")
		Expect(news.Queues).To(Equal([]v1beta2.QueueDrainStatus{{Name: "mine", MessageCount: 0}}))
	})

	It("does not poll a topic that keeps receiving, its counts cannot settle", func() {
		app := boundApp("busy-topic").
			WithAddresses(NewAddressType("prices").WithSubscriptions("a", "b", "c").Build()).
			WithProducerOf(NewAddressRef("prices").Build()).
			Build()
		app.Generation = 1

		fb := &fakeBroker{counts: map[string]int64{"prices::a": 40, "prices::b": 12, "prices::c": 7}}
		fb.install()

		r := drainReconciler(app, provisionedService(svcName, ns, app))
		refresh := r.processDrainState()
		Expect(refresh).To(BeFalse())
		Expect(fb.reads).To(BeEmpty())
		Expect(drainEntry(r.status, "prices").SendGranted).To(BeTrue())
	})

	It("reports unknown, never zero, when the broker cannot be read", func() {
		app := boundApp("orders-app").
			WithAddresses(NewAddressType("orders").Build(), NewAddressType("missing").Build()).
			Build()
		app.Generation = 1

		By("unreachable broker")
		fb := &fakeBroker{err: fmt.Errorf("connection refused")}
		fb.install()
		r := drainReconciler(app, provisionedService(svcName, ns, app))
		Expect(r.processDrainState()).To(BeTrue())
		Expect(drainEntry(r.status, "orders").Queues[0].MessageCount).To(Equal(v1beta2.QueueDrainUnknown))

		By("queue absent from the broker")
		fb = &fakeBroker{counts: map[string]int64{"orders::orders": 0}}
		fb.install()
		r = drainReconciler(app, provisionedService(svcName, ns, app))
		Expect(r.processDrainState()).To(BeTrue())
		Expect(drainEntry(r.status, "orders").Queues[0].MessageCount).To(BeZero())
		Expect(drainEntry(r.status, "missing").Queues[0].MessageCount).To(Equal(v1beta2.QueueDrainUnknown))
	})

	It("clears the drain state of an unbound app", func() {
		app := NewBrokerApp("lonely", ns).WithAddresses(NewAddressType("orders").Build()).Build()
		app.Generation = 5
		app.Status.Addresses = []v1beta2.AddressDrainStatus{{Address: "orders"}}

		r := drainReconciler(app, provisionedService(svcName, ns))
		r.service = nil
		Expect(r.processDrainState()).To(BeFalse())
		Expect(r.status.Addresses).To(BeNil())
		Expect(r.status.AddressesObservedGeneration).To(Equal(int64(5)))
	})

	It("publishes the drain state through reconcile", func() {
		app := NewBrokerApp("orders-app", ns).
			WithAddresses(NewAddressType("orders").Build()).
			WithConsumerOf(NewAddressRef("orders").Build()).
			Build()
		app.Generation = 1

		fb := &fakeBroker{counts: map[string]int64{"orders::orders": 0}}
		fb.install()

		env := NewTestEnvironment(ns, provisionedService(svcName, ns, app), app)
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: app.Name, Namespace: ns}}

		_, err := env.Reconciler.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		current := &v1beta2.BrokerApp{}
		Expect(env.Client.Get(context.TODO(), req.NamespacedName, current)).To(Succeed())
		Expect(current.Status.Service).NotTo(BeNil())
		Expect(current.Status.AddressesObservedGeneration).To(Equal(int64(1)))
		Expect(drainEntry(&current.Status, "orders").Queues).To(Equal([]v1beta2.QueueDrainStatus{{Name: "orders", MessageCount: 0}}))
	})
})
