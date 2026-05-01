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
// +kubebuilder:docs-gen:collapse=Apache License
package controllers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func TestSimpleReconcile(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	svcName := "my-broker-service"
	appName := "my-app"

	// Create namespace object (required for CEL evaluation)
	namespace := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: ns,
		},
	}

	// Create BrokerService (with Deployed=True since ports are discovered)
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
			Labels:    map[string]string{"type": "broker"},
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

	// Create BrokerApp matching the service
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
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
		WithObjects(namespace, svc, app).
		WithStatusSubresource(app, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify BrokerApp has annotation
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Verify Service binding
	assert.NotNil(t, updatedApp.Status.Service)
	assert.Equal(t, svcName, updatedApp.Status.Service.Name)
	assert.Equal(t, ns, updatedApp.Status.Service.Namespace)
	assert.NotEmpty(t, updatedApp.Status.Service.Secret)

	// Verify Status
	assert.False(t, meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.DeployedConditionType))
	assert.False(t, meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.ReadyConditionType))

	bindingSecret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: updatedApp.Status.Service.Secret, Namespace: ns}, bindingSecret)
	assert.NoError(t, err)

	assert.Equal(t, fmt.Sprintf("%s.%s.svc.%s", svcName, ns, common.GetClusterDomain()), string(bindingSecret.Data["host"]))
	assert.Equal(t, fmt.Sprintf("%d", updatedApp.Status.Service.AssignedPort), string(bindingSecret.Data["port"]))
	assert.Equal(t, fmt.Sprintf("amqps://%s.%s.svc.%s:%d", svcName, ns, common.GetClusterDomain(), updatedApp.Status.Service.AssignedPort), string(bindingSecret.Data["uri"]))

	// update broker service status to reflect ready with deployed app
	svc.Status.ProvisionedApps = []string{AppIdentity(app)}
	err = cl.Status().Update(context.TODO(), svc)
	assert.NoError(t, err)

	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify Status
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)
	assert.True(t, meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.DeployedConditionType))
	assert.True(t, meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.ReadyConditionType))
	assert.NotNil(t, updatedApp.Status.Service)

}

func TestReconcileNoMatchingService(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	appName := "my-app"

	// Create BrokerApp with a selector that won't match anything
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "non-existent"},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app).
		WithStatusSubresource(app)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	res, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err) // err is reflected in the status
	assert.True(t, res.Requeue)

	// Verify BrokerApp status
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Check Valid condition - should be True (selector syntax is valid)
	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCondition)
	assert.Equal(t, v1.ConditionTrue, validCondition.Status)
	assert.Equal(t, v1beta2.ValidConditionSuccessReason, validCondition.Reason)

	// Check Deployed condition - should reflect no matching service
	deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCondition)
	assert.Equal(t, v1.ConditionFalse, deployedCondition.Status)
	assert.Equal(t, v1beta2.DeployedConditionNoMatchingServiceReason, deployedCondition.Reason)
}

func TestReconcileValidConditionTransition(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	svcName := "my-broker-service"

	// Create namespace object (required for CEL evaluation)
	namespace := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: ns,
		},
	}
	appName := "my-app"

	// Create BrokerService (with Deployed=True since ports are discovered)
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
			Labels:    map[string]string{"type": "broker"},
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

	// Create BrokerApp with non-matching selector
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "non-existent"},
			},
		},
	}

	// Setup fake client
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, svc, app).
		WithStatusSubresource(app).
		WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
			app := obj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// 1. Reconcile with non-matching selector
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	res, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err) // err is reflected in the status
	assert.True(t, res.Requeue)

	// Verify Valid condition is True (selector syntax is valid)
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, v1.ConditionTrue, validCond.Status)

	// Verify Deployed condition is False (no matching service)
	deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, v1.ConditionFalse, deployedCond.Status)
	assert.Equal(t, v1beta2.DeployedConditionNoMatchingServiceReason, deployedCond.Reason)

	// Wait a bit to ensure time difference
	time.Sleep(1 * time.Second)

	// 2. Update App to match service
	updatedApp.Spec.ServiceSelector.MatchLabels["type"] = "broker"
	err = cl.Update(context.TODO(), updatedApp)
	assert.NoError(t, err)

	// Reconcile again
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify Valid condition is still True
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	validCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, v1.ConditionTrue, validCond.Status)

	// Verify Deployed condition updated (service now available, waiting for provisioning)
	deployedCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, v1.ConditionFalse, deployedCond.Status)
	assert.Equal(t, v1beta2.DeployedConditionProvisioningPendingReason, deployedCond.Reason)
	// Note: LastTransitionTime doesn't change because status is still False (only reason changed)
}

func TestReconcileStatusUpdateFailure(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	appName := "my-app"
	svcName := "my-broker-service"

	// Create namespace object (required for CEL evaluation)
	namespace := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: ns,
		},
	}

	// Create BrokerService (with Deployed=True since ports are discovered)
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
			Labels:    map[string]string{"type": "broker"},
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

	// Create BrokerApp matching the service
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	// Setup fake client with interceptor to fail Status Update
	interceptorFuncs := interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			return fmt.Errorf("simulated status update error")
		},
	}

	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, svc, app).
		WithStatusSubresource(app).
		WithInterceptorFuncs(interceptorFuncs)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	result, err := r.Reconcile(context.TODO(), req)

	// Verify error is returned
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "simulated status update error")
	assert.Equal(t, time.Duration(0), result.RequeueAfter)
}

func TestReconcileAddressTypeError(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	appName := "my-app"

	// Create BrokerApp with invalid subscriber address (simple address instead of FQQN)
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
			Capabilities: []v1beta2.AppCapabilityType{
				{
					SubscriberOf: []v1beta2.AddressRef{
						{Address: "simple-address"}, // Missing "::"
					},
				},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(app).
		WithStatusSubresource(app)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.Error(t, err)

	// Verify BrokerApp status
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Check Valid condition
	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCondition)
	assert.Equal(t, v1.ConditionFalse, validCondition.Status)
	assert.Equal(t, v1beta2.ValidConditionAddressTypeError, validCondition.Reason)
	assert.Contains(t, validCondition.Message, "FQQN")
}

func TestReconcileDeployedConditionFromBrokerServiceStatus(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	svcName := "my-broker-service"

	// Create namespace object (required for CEL evaluation)
	namespace := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: ns,
		},
	}
	appName := "my-app"

	// Create BrokerService (with Deployed=True since ports are discovered)
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
			Labels:    map[string]string{"type": "broker"},
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

	// Create BrokerApp matching the service
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
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
		WithObjects(namespace, svc, app).
		WithStatusSubresource(app, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// 1. Reconcile with BrokerService status not having the app.
	// This first reconcile will annotate the app. The Deployed condition will be False.
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify Deployed condition is False
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, v1.ConditionFalse, deployedCond.Status)
	assert.Equal(t, v1beta2.DeployedConditionProvisioningPendingReason, deployedCond.Reason)

	// 2. Update BrokerService status to include the app
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, updatedSvc)
	assert.NoError(t, err)

	appIdentity := AppIdentity(app)
	updatedSvc.Status.ProvisionedApps = []string{appIdentity}
	err = cl.Status().Update(context.TODO(), updatedSvc)
	assert.NoError(t, err)

	// Reconcile again to pick up the status change
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify Deployed condition is True
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	deployedCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, v1.ConditionTrue, deployedCond.Status)
	assert.Equal(t, v1beta2.DeployedConditionProvisionedReason, deployedCond.Reason)
}

func TestReconcileIdempotentStatus(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	svcName := "my-broker-service"

	// Create namespace object (required for CEL evaluation)
	namespace := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: ns,
		},
	}
	appName := "my-app"

	// Create BrokerService (with Deployed=True since ports are discovered)
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
			Labels:    map[string]string{"type": "broker"},
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

	// Create BrokerApp matching the service
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	// Setup fake client for first reconcile
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace, svc, app).WithStatusSubresource(app, svc)).Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// 1. First Reconcile to establish a status
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Get the updated app from the fake client
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// 2. Second Reconcile with the already updated app
	// We need a new client with the updated app and an interceptor to track status updates.
	updateCalled := false
	interceptorFuncs := interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			if _, ok := obj.(*v1beta2.BrokerApp); ok {
				updateCalled = true
			}
			return client.SubResource(subResourceName).Update(ctx, obj, opts...)
		},
	}

	cl2 := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, svc, updatedApp).
		WithStatusSubresource(updatedApp, svc).
		WithInterceptorFuncs(interceptorFuncs)).
		Build()
	r2 := NewBrokerAppReconciler(cl2, scheme, nil, logr.New(log.NullLogSink{}))
	_, err = r2.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Assert that status update was not called
	assert.False(t, updateCalled, "Status update should not be called on second reconcile if status is unchanged")
}

func TestReconcileInvalidResourceName(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	invalidName := "invalid/name"

	// Create BrokerApp with invalid name
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      invalidName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app)).Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: invalidName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.Error(t, err)

	// Verify BrokerApp status
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Check Valid condition
	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCondition)
	assert.Equal(t, v1.ConditionFalse, validCondition.Status)
	assert.Equal(t, v1beta2.ValidConditionInvalidResourceName, validCondition.Reason)
	assert.NotEmpty(t, validCondition.Message)
}

func TestReconcileInvalidSelectorSyntax(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	appName := "my-app"

	// Create BrokerApp with invalid selector syntax
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchExpressions: []v1.LabelSelectorRequirement{
					{
						Key:      "type",
						Operator: "InvalidOperator", // Invalid operator
						Values:   []string{"broker"},
					},
				},
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().WithScheme(scheme).WithObjects(app).WithStatusSubresource(app)).Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.Error(t, err)

	// Verify BrokerApp status
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Check Valid condition
	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCondition)
	assert.Equal(t, v1.ConditionFalse, validCondition.Status)
	assert.Equal(t, v1beta2.ValidConditionSpecSelectorError, validCondition.Reason)
	assert.Contains(t, validCondition.Message, "Selector")
}

func TestReconcileMatchedServiceNotFound(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	svcName := "my-broker-service"

	// Create namespace object (required for CEL evaluation)
	namespace := &corev1.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: ns,
		},
	}
	appName := "my-app"

	// Create BrokerService (with Deployed=True since ports are discovered)
	svc := &v1beta2.BrokerService{
		ObjectMeta: v1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
			Labels:    map[string]string{"type": "broker"},
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

	// Create BrokerApp with status binding pointing to the service
	app := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &v1.LabelSelector{
				MatchLabels: map[string]string{"type": "broker"},
			},
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         svcName,
				Namespace:    ns,
				Secret:       "binding-secret",
				AssignedPort: 61616,
			},
		},
	}

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, svc, app).
		WithStatusSubresource(app, svc)).
		Build()

	// Create Reconciler
	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// First reconcile - should succeed
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Update the app's selector so the service no longer matches
	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)
	updatedApp.Spec.ServiceSelector.MatchLabels["type"] = "different-type"
	err = cl.Update(context.TODO(), updatedApp)
	assert.NoError(t, err)

	// Reconcile again - service should not be found in new selector results
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err) // Should succeed but with condition update

	// Verify status
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Valid should still be True (selector syntax is valid)
	validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCondition)
	assert.Equal(t, v1.ConditionTrue, validCondition.Status)

	// Deployed should reflect that the matched service was not found
	deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCondition)
	assert.Equal(t, v1.ConditionFalse, deployedCondition.Status)
	assert.Equal(t, v1beta2.DeployedConditionMatchedServiceNotFoundReason, deployedCondition.Reason)
}
