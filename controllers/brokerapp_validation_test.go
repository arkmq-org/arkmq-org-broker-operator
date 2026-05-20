package controllers

import (
	"context"
	"testing"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// TestValidation_ConsumerOf_EmptySubscriptionsArray tests that empty subscriptions array is rejected
func TestValidation_ConsumerOf_EmptySubscriptionsArray(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = broker.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	ns := "default"
	appName := "invalid-app"

	emptyArray := []string{}
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
							Subscriptions: &emptyArray, // Invalid: cannot consume with empty array
						},
					},
				},
			},
		},
	}

	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app).
		WithStatusSubresource(app)).
		Build()

	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty subscriptions array not allowed")
}

// TestValidation_ProducerOf_NonEmptySubscriptionsArray tests that non-empty subscriptions array is rejected
func TestValidation_ProducerOf_NonEmptySubscriptionsArray(t *testing.T) {
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
							Subscriptions: &subs, // Invalid: producers cannot specify queue names
						},
					},
				},
			},
		},
	}

	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app).
		WithStatusSubresource(app)).
		Build()

	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "subscriptions array cannot contain queue names")
}

// TestValidation_QueueName_FQQN tests that FQQN format is rejected in queue names
func TestValidation_QueueName_FQQN(t *testing.T) {
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
							Subscriptions: &subs,
						},
					},
				},
			},
		},
	}

	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app).
		WithStatusSubresource(app)).
		Build()

	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "FQQN")
}

// TestValidation_QueueName_Empty tests that empty queue names are rejected
func TestValidation_QueueName_Empty(t *testing.T) {
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
							Subscriptions: &subs,
						},
					},
				},
			},
		},
	}

	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app).
		WithStatusSubresource(app)).
		Build()

	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "queue name cannot be empty")
}

// TestValidation_ProducerOf_FQQN tests that FQQN format is rejected in ProducerOf address
func TestValidation_ProducerOf_FQQN(t *testing.T) {
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

	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app).
		WithStatusSubresource(app)).
		Build()

	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "FQQN")
	assert.Contains(t, err.Error(), "ProducerOf")
}
