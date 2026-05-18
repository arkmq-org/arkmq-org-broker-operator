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
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TestDeployedCondition_ValidationError_WithPreviousDeployment tests that when
// validation fails but the app was previously deployed, the Deployed condition
// stays True (deployment is still active with old config)
func TestDeployedCondition_ValidationError_WithPreviousDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Create a service
	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test",
			Labels:    map[string]string{"app": "broker"},
		},
		Status: v1beta2.BrokerServiceStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: metav1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
			ProvisionedApps: []string{"test/test-app"},
		},
	}

	// Create an app that was previously successfully deployed
	// Generation=2 simulates that the spec was changed (from gen 1 to gen 2)
	app := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-app",
			Namespace:  "test",
			Generation: 2, // Current generation (spec was updated to invalid)
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "broker"},
			},
			// Invalid spec: address appears in both Addresses and SharedAddresses
			Addresses: []v1beta2.AddressType{
				{Address: "conflicting-address"},
			},
			SharedAddresses: []v1beta2.AddressType{
				{Address: "conflicting-address"}, // CONFLICT!
			},
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         "test-service",
				Namespace:    "test",
				Secret:       "test-app-binding-secret",
				AssignedPort: 61616,
			},
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: metav1.ConditionTrue, // Was previously deployed
					Reason: v1beta2.DeployedConditionProvisionedReason,
				},
				{
					Type:   v1beta2.ValidConditionType,
					Status: metav1.ConditionTrue, // Was previously valid
					Reason: v1beta2.ValidConditionSuccessReason,
				},
			},
		},
	}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(namespace, service, app).
		WithStatusSubresource(app).
		Build()

	reconciler := NewBrokerAppReconciler(fakeClient, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-app",
			Namespace: "test",
		},
	}
	result, err := reconciler.Reconcile(context.TODO(), req)

	// ValidationError results in no error returned (no retry until spec changes)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	// Fetch updated app
	updatedApp := &v1beta2.BrokerApp{}
	err = fakeClient.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Check conditions
	validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, metav1.ConditionFalse, validCond.Status, "Valid should be False due to address conflict")
	assert.Equal(t, v1beta2.ValidConditionAddressTypeError, validCond.Reason)
	assert.Contains(t, validCond.Message, "cannot be both private and public")
	// Valid condition should have current generation
	assert.Equal(t, app.Generation, validCond.ObservedGeneration)

	deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	// KEY ASSERTION: Deployed condition is NOT updated when validation fails
	// It retains the old observedGeneration, showing old config is still active
	assert.Equal(t, metav1.ConditionTrue, deployedCond.Status,
		"Deployed should remain True - validation failed but broker wasn't updated, old config still active")
	assert.Equal(t, v1beta2.DeployedConditionProvisionedReason, deployedCond.Reason)
	// ObservedGeneration should NOT be updated (stays at old generation or 0 if not set)
	assert.True(t, deployedCond.ObservedGeneration < app.Generation,
		"Deployed observedGeneration should be less than current generation - it reflects old spec (may be 0 if condition predates observedGeneration)")

	// Service binding should still be present (not cleared by validation failure)
	assert.NotNil(t, updatedApp.Status.Service)
	assert.Equal(t, "test-service", updatedApp.Status.Service.Name)
}

// TestDeployedCondition_ValidationError_WithoutPreviousDeployment tests that when
// validation fails for a new app that was never deployed, the Deployed condition
// should reflect that uncertain state
func TestDeployedCondition_ValidationError_WithoutPreviousDeployment(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Create a service
	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test",
			Labels:    map[string]string{"app": "broker"},
		},
		Status: v1beta2.BrokerServiceStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: metav1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
		},
	}

	// Create a NEW app with invalid spec - never deployed before
	app := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-app",
			Namespace: "test",
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "broker"},
			},
			// Invalid spec: address appears in both Addresses and SharedAddresses
			Addresses: []v1beta2.AddressType{
				{Address: "conflicting-address"},
			},
			SharedAddresses: []v1beta2.AddressType{
				{Address: "conflicting-address"}, // CONFLICT!
			},
		},
		// No status - brand new app
	}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(namespace, service, app).
		WithStatusSubresource(app).
		Build()

	reconciler := NewBrokerAppReconciler(fakeClient, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "new-app",
			Namespace: "test",
		},
	}
	result, err := reconciler.Reconcile(context.TODO(), req)

	// ValidationError results in no error returned (no retry until spec changes)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	// Fetch updated app
	updatedApp := &v1beta2.BrokerApp{}
	err = fakeClient.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Check conditions
	validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, metav1.ConditionFalse, validCond.Status, "Valid should be False due to address conflict")

	deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)

	// KEY ASSERTION: Deployed should be False because:
	// 1. Never deployed before (no previous Deployed=True status)
	// 2. Validation failed before we could attempt deployment
	// 3. We know we didn't deploy anything (early return)
	// Therefore: definitely not deployed = False (not Unknown)
	//
	// WITHOUT the fix (before checking previous status), this would be True
	assert.Equal(t, metav1.ConditionFalse, deployedCond.Status,
		"Deployed should be False - never deployed and validation failed")

	t.Logf("SUCCESS: Deployed condition is correctly set to False for new app with validation error")
	t.Logf("  Status=%s, Reason=%s, Message=%s",
		deployedCond.Status, deployedCond.Reason, deployedCond.Message)
}

// TestDeployedCondition_ValidationError_WithPreviousDeployedFalse tests that when
// validation fails but the app was previously in Deployed=False state, we should
// reflect that uncertain state
func TestDeployedCondition_ValidationError_WithPreviousDeployedFalse(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Create a service
	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test",
			Labels:    map[string]string{"app": "broker"},
		},
		Status: v1beta2.BrokerServiceStatus{
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: metav1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
		},
	}

	// Create an app that has a binding but Deployed=False (e.g., pending provisioning)
	app := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "test",
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "broker"},
			},
			// Invalid spec
			Addresses: []v1beta2.AddressType{
				{Address: "conflicting-address"},
			},
			SharedAddresses: []v1beta2.AddressType{
				{Address: "conflicting-address"},
			},
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         "test-service",
				Namespace:    "test",
				Secret:       "test-app-binding-secret",
				AssignedPort: 61616,
			},
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: metav1.ConditionFalse, // Was NOT deployed (e.g., pending)
					Reason: v1beta2.DeployedConditionProvisioningPendingReason,
				},
				{
					Type:   v1beta2.ValidConditionType,
					Status: metav1.ConditionTrue,
					Reason: v1beta2.ValidConditionSuccessReason,
				},
			},
		},
	}

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "test"},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(namespace, service, app).
		WithStatusSubresource(app).
		Build()

	reconciler := NewBrokerAppReconciler(fakeClient, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile
	req := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-app",
			Namespace: "test",
		},
	}
	result, err := reconciler.Reconcile(context.TODO(), req)

	// ValidationError results in no error returned
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	// Fetch updated app
	updatedApp := &v1beta2.BrokerApp{}
	err = fakeClient.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Check conditions
	validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, metav1.ConditionFalse, validCond.Status)

	deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)

	// KEY ASSERTION: Deployed should be False because:
	// 1. Previous state was Deployed=False (not deployed)
	// 2. Validation failed before we could attempt deployment
	// 3. We know we didn't deploy anything (early return)
	// Therefore: still not deployed = False
	// WITHOUT the fix, this would incorrectly be True
	assert.Equal(t, metav1.ConditionFalse, deployedCond.Status,
		"Deployed should be False - previous state was Deployed=False and validation failed")

	t.Logf("SUCCESS: Deployed condition is correctly set to False for app with previous Deployed=False")
	t.Logf("  Status=%s, Reason=%s, Message=%s",
		deployedCond.Status, deployedCond.Reason, deployedCond.Message)
}

// TestDeployedCondition_DemonstrateOldBug shows what the old "likely validation failed"
// logic would have done - always set Deployed=True when service lookup failed with an error,
// regardless of previous deployment state
func TestDeployedCondition_DemonstrateOldBug(t *testing.T) {
	t.Skip("This test demonstrates the OLD buggy behavior - skipped because we fixed it")

	// The OLD code at line 1318-1324 was:
	//   } else if reconcilerError != nil {
	//       // We didn't look up the service (likely validation failed)
	//       deployedCondition.Status = metav1.ConditionTrue
	//       deployedCondition.Reason = broker.DeployedConditionProvisionedReason
	//
	// This would ALWAYS set Deployed=True when:
	// - We have a status.Service binding
	// - reconciler.service is nil
	// - There's an error
	//
	// WITHOUT checking if the app was actually previously deployed!
	//
	// So for a brand new app that fails validation (never deployed):
	//   OLD BUG:  Deployed=True (assumes active deployment that doesn't exist)
	//   FIXED:    Deployed=False (we know we didn't deploy anything)
	//
	// For an app with Deployed=False that fails validation:
	//   OLD BUG:  Deployed=True (contradicts previous state)
	//   FIXED:    Deployed=False (still not deployed, we didn't deploy it)
	//
	// The fix checks prevDeployed status before deciding:
	//   if prevDeployed.Status == True:
	//       Deployed=True (deployment still active with old config)
	//   else:
	//       Deployed=False (not deployed - we didn't deploy anything)
}
