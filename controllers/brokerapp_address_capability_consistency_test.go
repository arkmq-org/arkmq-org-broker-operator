package controllers

import (
	"context"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("brokerapp address capability consistency", func() {

	It("rejects shared multicast address used as anycast", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = broker.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		appName := "inconsistent-app"

		app := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      appName,
				Namespace: ns,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
				SharedAddresses: []broker.AddressType{
					{
						Address:       "events",
						Subscriptions: []string{"sub1"},
					},
				},
				Capabilities: []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{
							{
								Address: "events",
							},
						},
					},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(app)...).
			WithStatusSubresource(app)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)

		updatedApp := &broker.BrokerApp{}
		getErr := cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(getErr).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
		Expect(validCondition).NotTo(BeNil(), "Valid condition should be set")
		Expect(validCondition.Status).To(Equal(v1.ConditionFalse), "Valid condition should be False")
		Expect(validCondition.Reason).To(Equal(broker.ValidConditionAddressTypeError), "Reason should be ValidConditionAddressTypeError")

		if err != nil {
			Expect(err.Error()).To(ContainSubstring("events"))
			Expect(err.Error()).To(ContainSubstring("pubSub"))
		}
	})

	It("rejects shared anycast address used as multicast", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = broker.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		appName := "mismatch-app"

		app := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      appName,
				Namespace: ns,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
				SharedAddresses: []broker.AddressType{
					{
						Address: "orders",
					},
				},
				Capabilities: []broker.AppCapabilityType{
					{
						ConsumerOf: []broker.AddressRef{
							{
								Address:       "orders",
								Subscriptions: []string{"queue1"},
							},
						},
					},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(app)...).
			WithStatusSubresource(app)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)

		updatedApp := &broker.BrokerApp{}
		getErr := cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(getErr).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
		Expect(validCondition).NotTo(BeNil(), "Valid condition should be set")
		Expect(validCondition.Status).To(Equal(v1.ConditionFalse), "Valid condition should be False")
		Expect(validCondition.Reason).To(Equal(broker.ValidConditionAddressTypeError), "Reason should be ValidConditionAddressTypeError")

		if err != nil {
			Expect(err.Error()).To(ContainSubstring("orders"))
		}
	})

	It("rejects private multicast address used as anycast", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = broker.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		appName := "private-mismatch"

		pubSubTrue := true
		app := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      appName,
				Namespace: ns,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
				Addresses: []broker.AddressType{
					{
						Address: "notifications",
						PubSub:  &pubSubTrue,
					},
				},
				Capabilities: []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{
							{
								Address: "notifications",
							},
						},
					},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(app)...).
			WithStatusSubresource(app)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)

		updatedApp := &broker.BrokerApp{}
		getErr := cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(getErr).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
		Expect(validCondition).NotTo(BeNil(), "Valid condition should be set")
		Expect(validCondition.Status).To(Equal(v1.ConditionFalse), "Valid condition should be False")
		Expect(validCondition.Reason).To(Equal(broker.ValidConditionAddressTypeError), "Reason should be ValidConditionAddressTypeError")

		if err != nil {
			Expect(err.Error()).To(ContainSubstring("notifications"))
		}
	})

	It("accepts consistent multicast usage", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = broker.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		appName := "valid-multicast"

		pubSubTrue := true
		app := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      appName,
				Namespace: ns,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
				SharedAddresses: []broker.AddressType{
					{
						Address:       "events",
						Subscriptions: []string{"sub1"},
					},
				},
				Capabilities: []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{
							{
								Address: "events",
								PubSub:  &pubSubTrue,
							},
						},
						ConsumerOf: []broker.AddressRef{
							{
								Address:       "events",
								Subscriptions: []string{"sub1"},
							},
						},
					},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(app)...).
			WithStatusSubresource(app)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		if err != nil {
			Expect(err.Error()).NotTo(ContainSubstring("pubSub"))
			Expect(err.Error()).NotTo(ContainSubstring("multicast"))
			Expect(err.Error()).NotTo(ContainSubstring("anycast"))
		}

		updatedApp := &broker.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
		if validCondition != nil && validCondition.Status == v1.ConditionFalse {
			Expect(validCondition.Reason).NotTo(Equal(broker.ValidConditionAddressTypeError))
		}
	})

	It("accepts consistent anycast usage", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = broker.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		appName := "valid-anycast"

		app := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      appName,
				Namespace: ns,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
				SharedAddresses: []broker.AddressType{
					{
						Address: "orders",
					},
				},
				Capabilities: []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{
							{
								Address: "orders",
							},
						},
						ConsumerOf: []broker.AddressRef{
							{
								Address: "orders",
							},
						},
					},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(app)...).
			WithStatusSubresource(app)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		if err != nil {
			Expect(err.Error()).NotTo(ContainSubstring("pubSub"))
			Expect(err.Error()).NotTo(ContainSubstring("multicast"))
			Expect(err.Error()).NotTo(ContainSubstring("anycast"))
		}

		updatedApp := &broker.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
		if validCondition != nil && validCondition.Status == v1.ConditionFalse {
			Expect(validCondition.Reason).NotTo(Equal(broker.ValidConditionAddressTypeError))
		}
	})

	It("accepts multiple addresses with mixed consistent types", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = broker.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		appName := "mixed-addresses"

		pubSubTrue := true
		app := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      appName,
				Namespace: ns,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
				SharedAddresses: []broker.AddressType{
					{
						Address:       "events",
						Subscriptions: []string{"sub1"},
					},
					{
						Address: "orders",
					},
				},
				Capabilities: []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{
							{
								Address: "events",
								PubSub:  &pubSubTrue,
							},
							{
								Address: "orders",
							},
						},
					},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(app)...).
			WithStatusSubresource(app)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		if err != nil {
			Expect(err.Error()).NotTo(ContainSubstring("pubSub"))
			Expect(err.Error()).NotTo(ContainSubstring("inconsistent"))
		}
	})

	It("allows addresses only referenced in capabilities", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = broker.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		appName := "implicit-address"

		app := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      appName,
				Namespace: ns,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
				Capabilities: []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{
							{
								Address: "implicit-queue",
							},
						},
					},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(app)...).
			WithStatusSubresource(app)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		if err != nil {
			Expect(err.Error()).NotTo(ContainSubstring("not declared"))
		}
	})

	It("handles pubSub false with subscriptions in declaration", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = broker.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		appName := "invalid-declaration"

		pubSubFalse := false
		app := &broker.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      appName,
				Namespace: ns,
			},
			Spec: broker.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
				SharedAddresses: []broker.AddressType{
					{
						Address:       "events",
						PubSub:        &pubSubFalse,
						Subscriptions: []string{"sub1"},
					},
				},
				Capabilities: []broker.AppCapabilityType{
					{
						ProducerOf: []broker.AddressRef{
							{
								Address: "events",
							},
						},
					},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(app)...).
			WithStatusSubresource(app)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, _ = r.Reconcile(context.TODO(), req)
	})
})
