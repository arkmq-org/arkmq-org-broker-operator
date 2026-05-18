package controllers

import (
	"context"
	"testing"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
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
	assert.NoError(t, err)

	// Check the Valid condition reflects the validation error
	updatedApp := &broker.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, v1.ConditionFalse, validCond.Status)
	assert.Contains(t, validCond.Message, "pubSub consumers must specify at least one subscription")
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
	assert.NoError(t, err)

	// Check the Valid condition reflects the validation error
	updatedApp := &broker.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, v1.ConditionFalse, validCond.Status)
	assert.Contains(t, validCond.Message, "subscriptions cannot contain queue names")
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
	assert.NoError(t, err)

	// Check the Valid condition
	updatedApp := &broker.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, v1.ConditionFalse, validCond.Status)
	assert.Contains(t, validCond.Message, "FQQN")
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
	assert.NoError(t, err)

	updatedApp := &broker.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, v1.ConditionFalse, validCond.Status)
	assert.Contains(t, validCond.Message, "queue name cannot be empty")
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

	cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app).
		WithStatusSubresource(app)).
		Build()

	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	updatedApp := &broker.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, v1.ConditionFalse, validCond.Status)
	assert.Contains(t, validCond.Message, "FQQN")
	assert.Contains(t, validCond.Message, "ProducerOf")
}
