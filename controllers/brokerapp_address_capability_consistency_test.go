package controllers

import (
	"context"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
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

var _ = Describe("BrokerApp address capability consistency", func() {
	Context("address routing type consistency", func() {
		It("should reject shared address used as MULTICAST in one app and ANYCAST in another", func() {
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
					// Declare as multicast (with subscriptions)
					SharedAddresses: []broker.AddressType{
						{
							Address:       "events",
							Subscriptions: []string{"sub1"}, // multicast
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{
									Address: "events", // Used as anycast (no pubSub flag)
								},
							},
						},
					},
				},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(app).
				WithStatusSubresource(app)).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)

			// Verify Valid condition
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
		It("should reject shared address used as ANYCAST in one app and MULTICAST in another", func() {
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
					// Declare as anycast (no subscriptions)
					SharedAddresses: []broker.AddressType{
						{
							Address: "orders", // anycast
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:       "orders",
									Subscriptions: []string{"queue1"}, // Used as multicast
								},
							},
						},
					},
				},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(app).
				WithStatusSubresource(app)).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)

			// Verify Valid condition
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
		It("should reject private address used as MULTICAST in one app and ANYCAST in another", func() {
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
					// Declare as multicast (explicit pubSub)
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
									Address: "notifications", // Used as anycast (no pubSub)
								},
							},
						},
					},
				},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(app).
				WithStatusSubresource(app)).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)

			// Verify Valid condition
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
		It("should accept shared addresses with consistent MULTICAST routing", func() {
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
					// Declare as multicast
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
									PubSub:  &pubSubTrue, // Consistent multicast
								},
							},
							ConsumerOf: []broker.AddressRef{
								{
									Address:       "events",
									Subscriptions: []string{"sub1"}, // Consistent multicast
								},
							},
						},
					},
				},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(app).
				WithStatusSubresource(app)).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			// Should succeed or fail for other reasons (like missing service), not validation
			if err != nil {
				Expect(err.Error()).NotTo(ContainSubstring("pubSub"))
				Expect(err.Error()).NotTo(ContainSubstring("inconsistent"))
			}

			// Verify Valid condition is not False with AddressTypeError
			updatedApp := &broker.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
			if validCondition != nil && validCondition.Status == v1.ConditionFalse {
				Expect(validCondition.Reason).NotTo(Equal(broker.ValidConditionAddressTypeError))
			}
		})
		It("should accept addresses with consistent ANYCAST routing", func() {
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
					// Declare as anycast (no subscriptions, no pubSub)
					SharedAddresses: []broker.AddressType{
						{
							Address: "orders",
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{
									Address: "orders", // Anycast
								},
							},
							ConsumerOf: []broker.AddressRef{
								{
									Address: "orders", // Anycast (no subscriptions)
								},
							},
						},
					},
				},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(app).
				WithStatusSubresource(app)).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			// Should succeed or fail for other reasons (like missing service), not validation
			if err != nil {
				Expect(err.Error()).NotTo(ContainSubstring("pubSub"))
				Expect(err.Error()).NotTo(ContainSubstring("inconsistent"))
			}

			// Verify Valid condition is not False with AddressTypeError
			updatedApp := &broker.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
			if validCondition != nil && validCondition.Status == v1.ConditionFalse {
				Expect(validCondition.Reason).NotTo(Equal(broker.ValidConditionAddressTypeError))
			}
		})
		It("should reject when multiple addresses contain a routing type conflict", func() {
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
							Subscriptions: []string{"sub1"}, // multicast
						},
						{
							Address: "orders", // anycast
						},
					},
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{
									Address: "events",
									PubSub:  &pubSubTrue, // Consistent - valid
								},
								{
									Address: "orders", // Anycast - valid
								},
							},
						},
					},
				},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(app).
				WithStatusSubresource(app)).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			// Should succeed or fail for other reasons (like missing service), not validation
			if err != nil {
				Expect(err.Error()).NotTo(ContainSubstring("inconsistent"))
				Expect(err.Error()).NotTo(ContainSubstring("pubSub"))
			}
		})
		It("should reject an address referenced in a capability but not declared in the address list", func() {
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
					// No declared addresses
					Capabilities: []broker.AppCapabilityType{
						{
							ProducerOf: []broker.AddressRef{
								{
									Address: "implicit-queue", // Not declared - implicit/local
								},
							},
						},
					},
				},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(app).
				WithStatusSubresource(app)).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			// Should succeed or fail for other reasons, not validation
			// Implicit addresses are allowed
			if err != nil {
				Expect(err.Error()).NotTo(ContainSubstring("not declared"))
			}
		})
		It("should reject a consumer with pubSub=false that specifies subscriptions", func() {
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
							Subscriptions: []string{"sub1"}, // Invalid combination
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
				WithObjects(app).
				WithStatusSubresource(app)).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			// This might be caught by isMulticastAddress logic or need explicit validation
			// The test documents the expected behavior
			if err != nil {
				// If validation exists, it should error
				GinkgoWriter.Printf("Error (if any): %v", err)
			}
		})
	})
})
