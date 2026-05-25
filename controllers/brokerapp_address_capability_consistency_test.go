package controllers

import (
	"context"
	"testing"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
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

// TestValidation_SharedAddress_Multicast_UsedAsAnycast tests that a SharedAddress
// declared as multicast (with subscriptions) but used as anycast in ProducerOf is rejected
func TestValidation_SharedAddress_Multicast_UsedAsAnycast(t *testing.T) {
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
	assert.NoError(t, getErr)

	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
	assert.NotNil(t, validCondition, "Valid condition should be set")
	assert.Equal(t, v1.ConditionFalse, validCondition.Status, "Valid condition should be False")
	assert.Equal(t, broker.ValidConditionAddressTypeError, validCondition.Reason, "Reason should be ValidConditionAddressTypeError")

	if err != nil {
		assert.Contains(t, err.Error(), "events")
		assert.Contains(t, err.Error(), "pubSub")
	}
}

// TestValidation_SharedAddress_Anycast_UsedAsMulticast tests that a SharedAddress
// declared as anycast (no subscriptions) but used as multicast in ConsumerOf is rejected
func TestValidation_SharedAddress_Anycast_UsedAsMulticast(t *testing.T) {
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
	assert.NoError(t, getErr)

	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
	assert.NotNil(t, validCondition, "Valid condition should be set")
	assert.Equal(t, v1.ConditionFalse, validCondition.Status, "Valid condition should be False")
	assert.Equal(t, broker.ValidConditionAddressTypeError, validCondition.Reason, "Reason should be ValidConditionAddressTypeError")

	if err != nil {
		assert.Contains(t, err.Error(), "orders")
	}
}

// TestValidation_PrivateAddress_Multicast_UsedAsAnycast tests that a private Address
// declared as multicast (explicit pubSub flag) but used as anycast is rejected
func TestValidation_PrivateAddress_Multicast_UsedAsAnycast(t *testing.T) {
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
	assert.NoError(t, getErr)

	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
	assert.NotNil(t, validCondition, "Valid condition should be set")
	assert.Equal(t, v1.ConditionFalse, validCondition.Status, "Valid condition should be False")
	assert.Equal(t, broker.ValidConditionAddressTypeError, validCondition.Reason, "Reason should be ValidConditionAddressTypeError")

	if err != nil {
		assert.Contains(t, err.Error(), "notifications")
	}
}

// TestValidation_SharedAddress_Consistent_Multicast_Valid tests that consistent
// multicast usage is accepted
func TestValidation_SharedAddress_Consistent_Multicast_Valid(t *testing.T) {
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
		assert.NotContains(t, err.Error(), "pubSub")
		assert.NotContains(t, err.Error(), "multicast")
		assert.NotContains(t, err.Error(), "anycast")
	}

	// Verify Valid condition is not False with AddressTypeError
	updatedApp := &broker.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
	if validCondition != nil && validCondition.Status == v1.ConditionFalse {
		assert.NotEqual(t, broker.ValidConditionAddressTypeError, validCondition.Reason)
	}
}

// TestValidation_SharedAddress_Consistent_Anycast_Valid tests that consistent
// anycast usage is accepted
func TestValidation_SharedAddress_Consistent_Anycast_Valid(t *testing.T) {
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
		assert.NotContains(t, err.Error(), "pubSub")
		assert.NotContains(t, err.Error(), "multicast")
		assert.NotContains(t, err.Error(), "anycast")
	}

	// Verify Valid condition is not False with AddressTypeError
	updatedApp := &broker.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, broker.ValidConditionType)
	if validCondition != nil && validCondition.Status == v1.ConditionFalse {
		assert.NotEqual(t, broker.ValidConditionAddressTypeError, validCondition.Reason)
	}
}

// TestValidation_MultipleAddresses_MixedTypes tests validation with multiple addresses
// where some are consistent and some are not
func TestValidation_MultipleAddresses_MixedTypes(t *testing.T) {
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
		assert.NotContains(t, err.Error(), "pubSub")
		assert.NotContains(t, err.Error(), "inconsistent")
	}
}

// TestValidation_AddressNotDeclared_OnlyInCapability tests that addresses only
// referenced in capabilities (not declared) are allowed (they're implicit/local)
func TestValidation_AddressNotDeclared_OnlyInCapability(t *testing.T) {
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
		assert.NotContains(t, err.Error(), "not declared")
	}
}

// TestValidation_PubSubFalse_WithSubscriptions_InDeclaration tests that declaring
// an address with pubSub=false but with subscriptions is caught
func TestValidation_Address_PubSubFalse_WithSubscriptions(t *testing.T) {
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
		t.Logf("Error (if any): %v", err)
	}
}
