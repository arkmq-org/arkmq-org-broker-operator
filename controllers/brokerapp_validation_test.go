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

var _ = Describe("BrokerApp Validation", func() {
	Context("address reference validation", func() {
		It("should reject a pubSub consumer with an empty subscriptions array", func() {
			scheme := runtime.NewScheme()
			_ = broker.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			appName := "invalid-app"

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
					Capabilities: []broker.AppCapabilityType{
						{
							ConsumerOf: []broker.AddressRef{
								{
									Address:       "events",
									PubSub:        &pubSubTrue, // Explicit pub/sub
									Subscriptions: []string{},  // Invalid: cannot consume with empty subscriptions
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
			// ValidationError results in no error returned (no retry until spec changes)
			Expect(err).NotTo(HaveOccurred())

			// Check the Valid condition reflects the validation error
			updatedApp := &broker.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Message).To(ContainSubstring("pubSub consumers must specify at least one subscription"))
		})
		It("should reject a producer that specifies a non-empty subscriptions array", func() {
			scheme := runtime.NewScheme()
			_ = broker.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			appName := "invalid-producer"

			subs := []string{"queue1"}
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
									Address:       "events",
									Subscriptions: subs, // Invalid: producers cannot specify queue names
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
			// ValidationError results in no error returned
			Expect(err).NotTo(HaveOccurred())

			// Check the Valid condition reflects the validation error
			updatedApp := &broker.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Message).To(ContainSubstring("subscriptions cannot contain queue names"))
		})
		It("should reject a consumer subscription name that uses FQQN format", func() {
			scheme := runtime.NewScheme()
			_ = broker.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			appName := "invalid-queue-name"

			subs := []string{"queue::name"} // Invalid: FQQN format
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
							ConsumerOf: []broker.AddressRef{
								{
									Address:       "events",
									Subscriptions: subs,
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
			// ValidationError results in no error returned
			Expect(err).NotTo(HaveOccurred())

			// Check the Valid condition
			updatedApp := &broker.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Message).To(ContainSubstring("FQQN"))
		})
		It("should reject a consumer subscription with an empty queue name", func() {
			scheme := runtime.NewScheme()
			_ = broker.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			appName := "empty-queue-name"

			subs := []string{""} // Invalid: empty queue name
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
							ConsumerOf: []broker.AddressRef{
								{
									Address:       "events",
									Subscriptions: subs,
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
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &broker.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Message).To(ContainSubstring("queue name cannot be empty"))
		})
		It("should reject a producer address that uses FQQN format", func() {
			scheme := runtime.NewScheme()
			_ = broker.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			appName := "producer-fqqn"

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
									Address: "events::queue", // Invalid: FQQN format
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
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &broker.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Message).To(ContainSubstring("FQQN"))
			Expect(validCond.Message).To(ContainSubstring("ProducerOf"))
		})
	})
})
