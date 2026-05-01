/*
Copyright 2026.

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

	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// isPortInPool tests - unbounded pool

func TestIsPortInPool_UnboundedPool_PortAtStart(t *testing.T) {
	poolInfo := &v1beta2.PortPoolInfo{
		Source: "Default",
		PortRange: &v1beta2.PortRange{
			Start: 61616,
			End:   62615,
		},
	}

	assert.True(t, isPortInPool(61616, poolInfo))
}

func TestIsPortInPool_UnboundedPool_PortInRange(t *testing.T) {
	poolInfo := &v1beta2.PortPoolInfo{
		Source: "Default",
		PortRange: &v1beta2.PortRange{
			Start: 61616,
			End:   62615,
		},
	}

	assert.True(t, isPortInPool(61650, poolInfo))
}

func TestIsPortInPool_UnboundedPool_PortAtMaxOffset(t *testing.T) {
	poolInfo := &v1beta2.PortPoolInfo{
		Source: "Default",
		PortRange: &v1beta2.PortRange{
			Start: 61616,
			End:   62615,
		},
	}

	assert.True(t, isPortInPool(61715, poolInfo))
}

func TestIsPortInPool_UnboundedPool_PortBeyondMaxOffset(t *testing.T) {
	poolInfo := &v1beta2.PortPoolInfo{
		Source: "Default",
		PortRange: &v1beta2.PortRange{
			Start: 61616,
			End:   62615,
		},
	}

	assert.False(t, isPortInPool(63717, poolInfo))
}

func TestIsPortInPool_UnboundedPool_PortAtExactMaxBoundary(t *testing.T) {
	poolInfo := &v1beta2.PortPoolInfo{
		Source: "Default",
		PortRange: &v1beta2.PortRange{
			Start: 61616,
			End:   62615,
		},
	}

	// Port 62615 = 61616 + 1000 - 1 (max valid port with MaxPortOffset=1000)
	assert.True(t, isPortInPool(62615, poolInfo))
}

func TestIsPortInPool_UnboundedPool_PortJustBeyondMaxBoundary(t *testing.T) {
	poolInfo := &v1beta2.PortPoolInfo{
		Source: "Default",
		PortRange: &v1beta2.PortRange{
			Start: 61616,
			End:   62615,
		},
	}

	// Port 62616 = 61616 + 1000 (first invalid port with MaxPortOffset=1000)
	assert.False(t, isPortInPool(62616, poolInfo))
}

func TestIsPortInPool_UnboundedPool_PortBeforeStart(t *testing.T) {
	poolInfo := &v1beta2.PortPoolInfo{
		Source: "Default",
		PortRange: &v1beta2.PortRange{
			Start: 61616,
			End:   62615,
		},
	}

	assert.False(t, isPortInPool(61615, poolInfo))
}

// isPortInPool tests - bounded pool

func TestIsPortInPool_BoundedPool_FirstPort(t *testing.T) {
	poolInfo := &v1beta2.PortPoolInfo{
		Source: "NetworkPolicy",
		Ports:  []int32{61616, 61617, 61618},
	}

	assert.True(t, isPortInPool(61616, poolInfo))
}

func TestIsPortInPool_BoundedPool_MiddlePort(t *testing.T) {
	poolInfo := &v1beta2.PortPoolInfo{
		Source: "NetworkPolicy",
		Ports:  []int32{61616, 61617, 61618},
	}

	assert.True(t, isPortInPool(61617, poolInfo))
}

func TestIsPortInPool_BoundedPool_LastPort(t *testing.T) {
	poolInfo := &v1beta2.PortPoolInfo{
		Source: "NetworkPolicy",
		Ports:  []int32{61616, 61617, 61618},
	}

	assert.True(t, isPortInPool(61618, poolInfo))
}

func TestIsPortInPool_BoundedPool_PortNotInList(t *testing.T) {
	poolInfo := &v1beta2.PortPoolInfo{
		Source: "NetworkPolicy",
		Ports:  []int32{61616, 61617, 61618},
	}

	assert.False(t, isPortInPool(61619, poolInfo))
}

func TestIsPortInPool_BoundedPool_PortFarOutOfRange(t *testing.T) {
	poolInfo := &v1beta2.PortPoolInfo{
		Source: "NetworkPolicy",
		Ports:  []int32{61616, 61617, 61618},
	}

	assert.False(t, isPortInPool(62000, poolInfo))
}

// isPortInPool tests - nil pool

func TestIsPortInPool_NilPool(t *testing.T) {
	assert.False(t, isPortInPool(61616, nil))
}

// validateAssignedPorts tests

func TestValidateAssignedPorts_AllPortsValid(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	ns := "default"
	svcName := "test-service"

	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "NetworkPolicy",
				Ports:  []int32{61616, 61617, 61618},
			},
		},
	}

	app1 := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app1",
			Namespace: ns,
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         svcName,
				Namespace:    ns,
				AssignedPort: 61616,
			},
		},
	}

	app2 := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app2",
			Namespace: ns,
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         svcName,
				Namespace:    ns,
				AssignedPort: 61617,
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(service, app1, app2).
		WithStatusSubresource(service, app1, app2).
		WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
			app := obj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).
		Build()

	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
	reconciler := &BrokerServiceInstanceReconciler{
		BrokerServiceReconciler: r,
		instance:                service,
		status:                  &service.Status,
	}

	err := reconciler.validateAssignedPorts(service.Status.AvailablePorts)
	assert.NoError(t, err)
	assert.Empty(t, reconciler.status.RejectedApps)
}

func TestValidateAssignedPorts_OrphanedPort(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	ns := "default"
	svcName := "test-service"

	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "NetworkPolicy",
				Ports:  []int32{61616, 61617},
			},
		},
	}

	app1 := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app1",
			Namespace: ns,
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         svcName,
				Namespace:    ns,
				AssignedPort: 61616,
			},
		},
	}

	app2 := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app2",
			Namespace: ns,
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         svcName,
				Namespace:    ns,
				AssignedPort: 61620,
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(service, app1, app2).
		WithStatusSubresource(service, app1, app2).
		WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
			app := obj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).
		Build()

	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
	reconciler := &BrokerServiceInstanceReconciler{
		BrokerServiceReconciler: r,
		instance:                service,
		status:                  &service.Status,
	}

	err := reconciler.validateAssignedPorts(service.Status.AvailablePorts)
	assert.NoError(t, err)
	assert.Len(t, reconciler.status.RejectedApps, 1)
	assert.Equal(t, "app2", reconciler.status.RejectedApps[0].Name)
	assert.Contains(t, reconciler.status.RejectedApps[0].Reason, "61620")
	assert.Contains(t, reconciler.status.RejectedApps[0].Reason, "no longer in allowed pool")
}

func TestValidateAssignedPorts_AppWithNoAssignedPort(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	ns := "default"
	svcName := "test-service"

	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				Ports:  nil,
			},
		},
	}

	app := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app1",
			Namespace: ns,
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         svcName,
				Namespace:    ns,
				AssignedPort: 0,
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(service, app).
		WithStatusSubresource(service, app).
		WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
			app := obj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).
		Build()

	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
	reconciler := &BrokerServiceInstanceReconciler{
		BrokerServiceReconciler: r,
		instance:                service,
		status:                  &service.Status,
	}

	err := reconciler.validateAssignedPorts(service.Status.AvailablePorts)
	assert.NoError(t, err)
	assert.Empty(t, reconciler.status.RejectedApps)
}

// BrokerApp reassignment tests

func TestBrokerAppReassignment_OrphanedPort(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	ns := "default"
	svcName := "test-service"

	nsObj := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: ns,
		},
	}

	service := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
			Labels:    map[string]string{"test": "true"},
		},
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "NetworkPolicy",
				Ports:  []int32{61616, 61617},
			},
			Conditions: []metav1.Condition{
				{
					Type:   v1beta2.DeployedConditionType,
					Status: metav1.ConditionTrue,
					Reason: v1beta2.ReadyConditionReason,
				},
			},
			RejectedApps: []v1beta2.RejectedApp{
				{
					Name:      "app1",
					Namespace: ns,
					Reason:    "assigned port 61620 no longer in allowed pool (source: NetworkPolicy)",
				},
			},
		},
	}

	app := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "app1",
			Namespace: ns,
		},
		Spec: v1beta2.BrokerAppSpec{
			ServiceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"test": "true"},
			},
		},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         svcName,
				Namespace:    ns,
				AssignedPort: 61620,
			},
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(service, app, nsObj).
		WithStatusSubresource(service, app).
		WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
			app := obj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).
		Build()

	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app1", Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	assert.NotNil(t, updatedApp.Status.Service)
	assert.NotEqual(t, int32(61620), updatedApp.Status.Service.AssignedPort)
	// Should be reassigned to one of the valid ports
	assert.True(t, updatedApp.Status.Service.AssignedPort == 61616 || updatedApp.Status.Service.AssignedPort == 61617,
		"Port should be reassigned to 61616 or 61617, got %d", updatedApp.Status.Service.AssignedPort)
}
