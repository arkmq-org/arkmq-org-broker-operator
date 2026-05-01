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
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
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

// TestAppSelectorAllowedNamespace verifies that an app from an allowed namespace can select a service
func TestAppSelectorAllowedNamespace(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	allowedNs := "team-a"
	svcNs := "shared"
	svcName := "shared-broker"
	appName := "myapp"

	allowedNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: allowedNs,
		},
	}
	sharedNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: svcNs,
		},
	}

	// Create BrokerService with CEL expression allowing specific namespace
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: svcNs,
			Labels:    map[string]string{"type": "broker"},
		},
		Spec: v1beta2.BrokerServiceSpec{
			AppSelectorExpression: fmt.Sprintf(`app.metadata.namespace == "%s"`, allowedNs),
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
			Conditions: []v1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: v1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
		},
	}

	// Create BrokerApp from allowed namespace
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: allowedNs,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, app, allowedNsObj, sharedNsObj).
		WithStatusSubresource(app, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile the app
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: allowedNs}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify BrokerApp status
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Should have status binding to the service
	assert.NotNil(t, updatedApp.Status.Service, "App should be bound to service")
	assert.Equal(t, svcName, updatedApp.Status.Service.Name)
	assert.Equal(t, svcNs, updatedApp.Status.Service.Namespace)

	// Check Valid condition - should be True
	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCondition)
	assert.Equal(t, v1.ConditionTrue, validCondition.Status)

	// Check Deployed condition - should be False/ProvisioningPending (waiting for broker to apply)
	deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCondition)
	assert.Equal(t, v1.ConditionFalse, deployedCondition.Status)
	assert.Equal(t, v1beta2.DeployedConditionProvisioningPendingReason, deployedCondition.Reason)
}

// TestAppSelectorDeniedNamespace verifies that an app from a non-allowed namespace is rejected
func TestAppSelectorDeniedNamespace(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	allowedNs := "team-a"
	deniedNs := "team-b"
	svcNs := "shared"
	svcName := "shared-broker"
	appName := "myapp"

	allowedNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: allowedNs,
		},
	}
	sharedNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: svcNs,
		},
	}

	deniedNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: deniedNs,
		},
	}

	// Create BrokerService with CEL expression (only team-a allowed)
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: svcNs,
			Labels:    map[string]string{"type": "broker"},
		},
		Spec: v1beta2.BrokerServiceSpec{
			AppSelectorExpression: fmt.Sprintf(`app.metadata.namespace == "%s"`, allowedNs),
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
			Conditions: []v1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: v1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
		},
	}

	// Create BrokerApp from denied namespace (team-b)
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: deniedNs,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, app, allowedNsObj, deniedNsObj, sharedNsObj).
		WithStatusSubresource(app, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile the app
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: deniedNs}}

	res, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err) // err is reflected in the status
	assert.True(t, res.Requeue)

	// Verify BrokerApp status
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Should NOT have annotation (not bound to service)
	assert.Nil(t, updatedApp.Status.Service, "App should not be bound to service")

	// Check Valid condition - should be True (spec is valid)
	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCondition)
	assert.Equal(t, v1.ConditionTrue, validCondition.Status)

	// Check Deployed condition - should be False with Unauthorized reason
	deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCondition)
	assert.Equal(t, v1.ConditionFalse, deployedCondition.Status)
	assert.Equal(t, v1beta2.DeployedConditionNoMatchingServiceReason, deployedCondition.Reason)
	assert.Contains(t, deployedCondition.Message, "no services")

	// Ready should be False
	readyCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)
	assert.NotNil(t, readyCondition)
	assert.Equal(t, v1.ConditionFalse, readyCondition.Status)
}

// TestAppSelectorEmptyAllowlist verifies that an empty allowlist allows only same namespace (default behavior)
func TestAppSelectorEmptyAllowlist(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	svcNs := "broker-services"
	svcName := "my-broker"
	appName := "myapp"

	svcNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: svcNs,
		},
	}

	// Create BrokerService with empty expression (same namespace only - default)
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: svcNs,
			Labels:    map[string]string{"type": "broker"},
		},
		Spec: v1beta2.BrokerServiceSpec{
			// Empty expression = default: app.metadata.namespace == service.metadata.namespace
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
			Conditions: []v1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: v1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
		},
	}

	// Create BrokerApp from SAME namespace
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: svcNs, // Same namespace as service
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, app, svcNsObj).
		WithStatusSubresource(app, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile the app
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: svcNs}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify BrokerApp status
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Should have annotation binding to the service (allowed because same namespace)
	assert.NotNil(t, updatedApp.Status.Service, "App should be bound to service")
	assert.Equal(t, svcName, updatedApp.Status.Service.Name)
	assert.Equal(t, svcNs, updatedApp.Status.Service.Namespace)

	// Check Valid condition - should be True
	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCondition)
	assert.Equal(t, v1.ConditionTrue, validCondition.Status)
}

// TestAppSelectorEmptyAllowlistDifferentNamespace verifies that empty allowlist denies different namespaces
func TestAppSelectorEmptyAllowlistDifferentNamespace(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	svcNs := "broker-services"
	appNs := "different-namespace"
	svcName := "my-broker"
	appName := "myapp"

	svcNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: svcNs,
		},
	}
	appNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: appNs,
		},
	}

	// Create BrokerService with empty expression (default)
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: svcNs,
			Labels:    map[string]string{"type": "broker"},
		},
		Spec: v1beta2.BrokerServiceSpec{
			// Empty = default: same namespace only
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
			Conditions: []v1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: v1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
		},
	}

	// Create BrokerApp from DIFFERENT namespace
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: appNs, // Different namespace
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, app, svcNsObj, appNsObj).
		WithStatusSubresource(app, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile the app
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: appNs}}
	res, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err) // err is reflected in the status
	assert.True(t, res.Requeue)

	// Verify BrokerApp status
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Should NOT have annotation
	assert.Nil(t, updatedApp.Status.Service, "App should not be bound to service")

	// Check Deployed condition - should be False with Unauthorized reason
	deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCondition)
	assert.Equal(t, v1.ConditionFalse, deployedCondition.Status)
	assert.Equal(t, v1beta2.DeployedConditionNoMatchingServiceReason, deployedCondition.Reason)
}

// TestAppSelectorRevokedAccess verifies that an app loses access when removed from allowlist
func TestAppSelectorRevokedAccess(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	appNs := "team-a"
	svcNs := "shared"
	svcName := "shared-broker"
	appName := "myapp"

	appNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: appNs,
		},
	}
	sharedNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: svcNs,
		},
	}

	// Create BrokerService initially allowing team-a
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: svcNs,
			Labels:    map[string]string{"type": "broker"},
		},
		Spec: v1beta2.BrokerServiceSpec{
			AppSelectorExpression: fmt.Sprintf(`app.metadata.namespace == "%s"`, appNs),
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
			Conditions: []v1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: v1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
		},
	}

	// Create BrokerApp that's already bound to the service
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: appNs,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         svcName,
				Namespace:    svcNs,
				Secret:       "binding-secret",
				AssignedPort: 61616,
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, app, appNsObj, sharedNsObj).
		WithStatusSubresource(app, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// First reconcile - app should be authorized
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: appNs}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Should still be bound
	assert.NotNil(t, updatedApp.Status.Service, "App should remain bound initially")

	// Now update the service to remove team-a from allowed namespaces
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: svcNs}, updatedSvc)
	assert.NoError(t, err)
	updatedSvc.Spec.AppSelectorExpression = `app.metadata.namespace == "team-b"` // Change to only allow team-b
	err = cl.Update(context.TODO(), updatedSvc)
	assert.NoError(t, err)

	// Reconcile again - app should be unbound and unauthorized
	res, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err) // err is reflected in the status
	assert.True(t, res.Requeue)

	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Check Deployed condition - should show Unauthorized
	deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCondition)
	assert.Equal(t, v1.ConditionFalse, deployedCondition.Status)
	assert.Equal(t, v1beta2.DeployedConditionNoMatchingServiceReason, deployedCondition.Reason)
}

// TestAppSelectorMultipleNamespaces verifies that multiple namespaces can be in the allowlist
func TestAppSelectorMultipleNamespaces(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	svcNs := "shared"
	svcName := "shared-broker"

	svcNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: svcNs,
		},
	}

	// Create BrokerService allowing multiple namespaces using CEL 'in' operator
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: svcNs,
			Labels:    map[string]string{"type": "broker"},
		},
		Spec: v1beta2.BrokerServiceSpec{
			AppSelectorExpression: `app.metadata.namespace in ["team-a", "team-b", "team-c"]`,
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
			Conditions: []v1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: v1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
		},
	}

	teamANsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: "team-a",
		},
	}
	// Create apps from different namespaces
	appA := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      "app-a",
			Namespace: "team-a",
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	teamBNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: "team-b",
		},
	}
	// Create apps from different namespaces
	appB := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      "app-b",
			Namespace: "team-b",
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	teamDNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: "team-d",
		},
	}
	// Create apps from different namespaces
	appDenied := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      "app-denied",
			Namespace: "team-d", // Not in allowlist
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, appA, appB, appDenied, svcNsObj, teamANsObj, teamBNsObj, teamDNsObj).
		WithStatusSubresource(appA, appB, appDenied, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile app-a (should succeed)
	reqA := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-a", Namespace: "team-a"}}
	_, err := r.Reconcile(context.TODO(), reqA)
	assert.NoError(t, err)

	updatedAppA := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), reqA.NamespacedName, updatedAppA)
	assert.NoError(t, err)
	hasBinding := updatedAppA.Status.Service != nil
	assert.True(t, hasBinding, "App A should be bound")

	// Reconcile app-b (should succeed)
	reqB := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-b", Namespace: "team-b"}}
	_, err = r.Reconcile(context.TODO(), reqB)
	assert.NoError(t, err)

	updatedAppB := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), reqB.NamespacedName, updatedAppB)
	assert.NoError(t, err)
	hasBinding = updatedAppB.Status.Service != nil
	assert.True(t, hasBinding, "App B should be bound")

	// Reconcile app-denied (should fail)
	reqDenied := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-denied", Namespace: "team-d"}}
	res, err := r.Reconcile(context.TODO(), reqDenied)
	assert.NoError(t, err) // err is reflected in the status
	assert.True(t, res.Requeue)

	updatedAppDenied := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), reqDenied.NamespacedName, updatedAppDenied)
	assert.NoError(t, err)
	hasBinding = updatedAppDenied.Status.Service != nil
	assert.False(t, hasBinding, "Denied app should not be bound")

	deployedCondition := meta.FindStatusCondition(updatedAppDenied.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCondition)
	assert.Equal(t, v1.ConditionFalse, deployedCondition.Status)
	assert.Equal(t, v1beta2.DeployedConditionNoMatchingServiceReason, deployedCondition.Reason)
}

// TestAppSelectorAllowAll verifies that "true" allows all namespaces
func TestAppSelectorAllowAll(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	svcNs := "broker-services"
	appNs := "any-other-namespace"
	svcName := "open-broker"
	appName := "myapp"

	svcNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: svcNs,
		},
	}
	appNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: appNs,
		},
	}

	// Create BrokerService with expression "true" (allow all)
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: svcNs,
			Labels:    map[string]string{"type": "broker"},
		},
		Spec: v1beta2.BrokerServiceSpec{
			AppSelectorExpression: "true", // Allow all namespaces
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
			Conditions: []v1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: v1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
		},
	}

	// Create BrokerApp from any namespace
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: appNs,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, app, svcNsObj, appNsObj).
		WithStatusSubresource(app, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile the app
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: appNs}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify BrokerApp status
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Should have annotation
	assert.NotNil(t, updatedApp.Status.Service, "App should be bound to service")
	assert.Equal(t, svcName, updatedApp.Status.Service.Name)
	assert.Equal(t, svcNs, updatedApp.Status.Service.Namespace)
}

// TestAppSelectorPrefix verifies that startsWith() matches namespaces with prefix
func TestAppSelectorPrefix(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	svcNs := "broker-services"
	svcName := "team-broker"

	svcNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: svcNs,
		},
	}
	// Create BrokerService with prefix expression
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: svcNs,
			Labels:    map[string]string{"type": "broker"},
		},
		Spec: v1beta2.BrokerServiceSpec{
			AppSelectorExpression: `app.metadata.namespace.startsWith("team-")`, // Matches team-a-prod, team-b, etc.
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
			Conditions: []v1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: v1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
		},
	}

	teamAProdNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: "team-a-prod",
		},
	}
	// Create apps with matching and non-matching namespaces
	appMatch := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      "app-match",
			Namespace: "team-a-prod", // Matches team-*
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	appNoMatchNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: "app-nomatch",
		},
	}
	appNoMatch := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      "app-nomatch",
			Namespace: "other-namespace", // Does NOT match team-*
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, appMatch, appNoMatch, svcNsObj, teamAProdNsObj, appNoMatchNsObj).
		WithStatusSubresource(appMatch, appNoMatch, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile matching app - should succeed
	reqMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-match", Namespace: "team-a-prod"}}
	_, err := r.Reconcile(context.TODO(), reqMatch)
	assert.NoError(t, err)

	updatedMatch := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), reqMatch.NamespacedName, updatedMatch)
	assert.NoError(t, err)
	hasBinding := updatedMatch.Status.Service != nil
	assert.True(t, hasBinding, "Matching app should be bound")

	// Reconcile non-matching app - should fail
	reqNoMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-nomatch", Namespace: "other-namespace"}}

	res, err := r.Reconcile(context.TODO(), reqNoMatch)
	assert.NoError(t, err) // err is reflected in the status
	assert.True(t, res.Requeue)

	updatedNoMatch := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), reqNoMatch.NamespacedName, updatedNoMatch)
	assert.NoError(t, err)
	hasBinding = updatedNoMatch.Status.Service != nil
	assert.False(t, hasBinding, "Non-matching app should not be bound")
}

// TestAppSelectorSuffix verifies that endsWith() matches namespaces with suffix
func TestAppSelectorSuffix(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	svcNs := "broker-services"
	svcName := "prod-broker"

	svcNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: svcNs,
		},
	}

	// Create BrokerService with suffix expression
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: svcNs,
			Labels:    map[string]string{"type": "broker"},
		},
		Spec: v1beta2.BrokerServiceSpec{
			AppSelectorExpression: `app.metadata.namespace.endsWith("-prod")`, // Matches team-a-prod, api-prod, etc.
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
			Conditions: []v1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: v1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
		},
	}

	teamAProdNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: "team-a-prod",
		},
	}
	// Create apps
	appMatch := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      "app-match",
			Namespace: "team-a-prod", // Matches *-prod
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	teamADevNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: "team-a-dev",
		},
	}
	appNoMatch := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      "app-nomatch",
			Namespace: "team-a-dev", // Does NOT match *-prod
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, appMatch, appNoMatch, svcNsObj, teamAProdNsObj, teamADevNsObj).
		WithStatusSubresource(appMatch, appNoMatch, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile matching app
	reqMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-match", Namespace: "team-a-prod"}}
	_, err := r.Reconcile(context.TODO(), reqMatch)
	assert.NoError(t, err)

	updatedMatch := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), reqMatch.NamespacedName, updatedMatch)
	assert.NoError(t, err)
	hasBinding := updatedMatch.Status.Service != nil
	assert.True(t, hasBinding)

	// Reconcile non-matching app
	reqNoMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-nomatch", Namespace: "team-a-dev"}}
	res, err := r.Reconcile(context.TODO(), reqNoMatch)
	assert.NoError(t, err)
	assert.True(t, res.Requeue)

	updatedNoMatch := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), reqNoMatch.NamespacedName, updatedNoMatch)
	assert.NoError(t, err)
	hasBinding = updatedNoMatch.Status.Service != nil
	assert.False(t, hasBinding)
}

// TestAppSelectorPrefixAndSuffix verifies that combined startsWith/endsWith works
func TestAppSelectorPrefixAndSuffix(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	svcNs := "broker-services"
	svcName := "pattern-broker"

	svcNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: svcNs,
		},
	}
	// Create BrokerService with prefix and suffix expression
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: svcNs,
			Labels:    map[string]string{"type": "broker"},
		},
		Spec: v1beta2.BrokerServiceSpec{
			AppSelectorExpression: `app.metadata.namespace.startsWith("team-") && app.metadata.namespace.endsWith("-prod")`,
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
			Conditions: []v1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: v1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
		},
	}

	teamAProdNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: "team-a-prod",
		},
	}
	// Create apps
	appMatch1 := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      "app-match1",
			Namespace: "team-a-prod", // Matches team-*-prod
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	teamBackendProdNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: "team-backend-prod",
		},
	}
	appMatch2 := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      "app-match2",
			Namespace: "team-backend-prod", // Matches team-*-prod
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	teamADevNsObj := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: "team-a-dev",
		},
	}
	// Create apps
	appNoMatch := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      "app-nomatch",
			Namespace: "team-a-dev", // Does NOT match team-*-prod
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, appMatch1, appMatch2, appNoMatch, svcNsObj, teamAProdNsObj, teamBackendProdNsObj, teamADevNsObj).
		WithStatusSubresource(appMatch1, appMatch2, appNoMatch, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Test match 1
	req1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-match1", Namespace: "team-a-prod"}}
	_, err := r.Reconcile(context.TODO(), req1)
	assert.NoError(t, err)

	// Test match 2
	req2 := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-match2", Namespace: "team-backend-prod"}}
	_, err = r.Reconcile(context.TODO(), req2)
	assert.NoError(t, err)

	// Test no match
	reqNoMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-nomatch", Namespace: "team-a-dev"}}

	res, err := r.Reconcile(context.TODO(), reqNoMatch)
	assert.NoError(t, err)
	assert.True(t, res.Requeue)

}
