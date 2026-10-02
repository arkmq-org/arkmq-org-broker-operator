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

var _ = Describe("brokerapp validation", func() {

	Context("ConsumerOf validation", func() {
		It("rejects empty subscriptions array", Label(unitLabel), func() {
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
									PubSub:        &pubSubTrue,
									Subscriptions: []string{},
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
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &broker.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Message).To(ContainSubstring("pubSub consumers must specify at least one subscription"))
		})
	})

	Context("ProducerOf validation", func() {
		It("rejects non-empty subscriptions array", Label(unitLabel), func() {
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
									Subscriptions: subs,
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
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &broker.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(v1.ConditionFalse))
			Expect(validCond.Message).To(ContainSubstring("subscriptions cannot contain queue names"))
		})

		It("rejects FQQN format in address", Label(unitLabel), func() {
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
									Address: "events::queue",
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

	Context("queue name validation", func() {
		It("rejects FQQN format in queue names", Label(unitLabel), func() {
			scheme := runtime.NewScheme()
			_ = broker.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			appName := "invalid-queue-name"

			subs := []string{"queue::name"}
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
				WithObjects(WithCerts(app)...).
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
		})

		It("rejects empty queue names", Label(unitLabel), func() {
			scheme := runtime.NewScheme()
			_ = broker.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			appName := "empty-queue-name"

			subs := []string{""}
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
				WithObjects(WithCerts(app)...).
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
	})
})
