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
	"testing"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestResolveBrokerService_DeployedButNoPortPool tests the edge case where a service
// has Deployed==True but AvailablePorts==nil (shouldn't happen but must handle gracefully).
func TestResolveBrokerService_DeployedButNoPortPool(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = broker.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			ServiceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "dev"},
			},
		},
	}

	// Service with inconsistent state: Deployed==True but AvailablePorts==nil
	// This should not happen in practice but we must handle it gracefully
	service1 := &broker.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service1",
			Namespace: "test",
			Labels:    map[string]string{"env": "dev"},
		},
		Status: broker.BrokerServiceStatus{
			Conditions: []metav1.Condition{
				{
					Type:   broker.DeployedConditionType,
					Status: metav1.ConditionTrue,
					Reason: broker.ReadyConditionReason,
				},
			},
			// Inconsistent: Deployed but no AvailablePorts
			AvailablePorts: nil,
		},
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	fakeClient := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(app, service1, ns)).
		Build()

	reconciler := &BrokerAppInstanceReconciler{
		BrokerAppReconciler: &BrokerAppReconciler{
			ReconcilerLoop: &ReconcilerLoop{
				KubeBits: &KubeBits{
					Client: fakeClient,
					Scheme: scheme,
				},
			},
		},
		instance: app,
		status:   app.Status.DeepCopy(),
	}

	// Call resolveBrokerService
	err := reconciler.resolveBrokerService()

	// Should error with capacity issue, not panic
	assert.Error(t, err, "should error when service has no port pool configured")

	// Should be a ConditionError with NoServiceCapacity reason
	condErr, ok := AsConditionError(err)
	assert.True(t, ok, "error should be a ConditionError")
	if ok {
		assert.Equal(t, broker.DeployedConditionNoServiceCapacityReason, condErr.Reason,
			"should use NoServiceCapacity reason when service has no port pool")
	}

	// Should not have selected a service (no panic)
	assert.Nil(t, reconciler.service, "should not have selected a service")
}

// TestResolveBrokerService_MultipleServices_OneNotDeployed tests that when an app
// matches multiple services and one is not deployed (port discovery pending),
// we should skip it and consider the other deployed service.
func TestResolveBrokerService_MultipleServices_OneNotDeployed(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = broker.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			ServiceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "dev"},
			},
		},
	}

	// Service 1: Not deployed yet (port discovery pending)
	service1 := &broker.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service1",
			Namespace: "test",
			Labels:    map[string]string{"env": "dev"},
		},
		Status: broker.BrokerServiceStatus{
			Conditions: []metav1.Condition{
				{
					Type:   broker.DeployedConditionType,
					Status: metav1.ConditionFalse,
					Reason: broker.DeployedConditionPortDiscoveryPendingReason,
				},
			},
			// AvailablePorts is nil when not deployed
			AvailablePorts: nil,
		},
	}

	// Service 2: Has AvailablePorts configured with capacity
	service2 := &broker.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service2",
			Namespace: "test",
			Labels:    map[string]string{"env": "dev"},
		},
		Status: broker.BrokerServiceStatus{
			Conditions: []metav1.Condition{
				{
					Type:   broker.DeployedConditionType,
					Status: metav1.ConditionTrue,
					Reason: broker.ReadyConditionReason,
				},
			},
			AvailablePorts: &broker.PortPoolInfo{
				Source: "Default",
				PortRange: &broker.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
		},
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	fakeClient := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(app, service1, service2, ns)).
		Build()

	reconciler := &BrokerAppInstanceReconciler{
		BrokerAppReconciler: &BrokerAppReconciler{
			ReconcilerLoop: &ReconcilerLoop{
				KubeBits: &KubeBits{
					Client: fakeClient,
					Scheme: scheme,
				},
			},
		},
		instance: app,
		status:   app.Status.DeepCopy(),
	}

	// Call resolveBrokerService
	err := reconciler.resolveBrokerService()

	// Should NOT error - should skip not-deployed service1 and select deployed service2
	assert.NoError(t, err, "should skip not-deployed service and select deployed one")

	// Should have selected service2 (the deployed one)
	assert.NotNil(t, reconciler.service, "should have selected a service")
	if reconciler.service != nil {
		assert.Equal(t, "service2", reconciler.service.Name, "should select deployed service")
	}
}

// TestResolveBrokerService_MultipleServices_OneWithPortsExhausted tests that when an app
// matches multiple services and one has all ports exhausted, we should select the other service.
func TestResolveBrokerService_MultipleServices_OneWithPortsExhausted(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = broker.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	app := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-app",
			Namespace: "test",
		},
		Spec: broker.BrokerAppSpec{
			ServiceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"env": "dev"},
			},
		},
	}

	// Service 1: Has small port pool (3 ports)
	service1 := &broker.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service1",
			Namespace: "test",
			Labels:    map[string]string{"env": "dev"},
		},
		Status: broker.BrokerServiceStatus{
			Conditions: []metav1.Condition{
				{
					Type:   broker.DeployedConditionType,
					Status: metav1.ConditionTrue,
					Reason: broker.ReadyConditionReason,
				},
			},
			AvailablePorts: &broker.PortPoolInfo{
				Source: "NetworkPolicy",
				Ports:  []int32{61616, 61617, 61618}, // 3 ports
			},
		},
	}

	// Service 2: Has larger port pool
	service2 := &broker.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service2",
			Namespace: "test",
			Labels:    map[string]string{"env": "dev"},
		},
		Status: broker.BrokerServiceStatus{
			Conditions: []metav1.Condition{
				{
					Type:   broker.DeployedConditionType,
					Status: metav1.ConditionTrue,
					Reason: broker.ReadyConditionReason,
				},
			},
			AvailablePorts: &broker.PortPoolInfo{
				Source: "Default",
				PortRange: &broker.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
		},
	}

	// Create 3 existing apps that have exhausted service1's port pool
	app1 := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-app-1",
			Namespace: "test",
		},
		Status: broker.BrokerAppStatus{
			Service: &broker.BrokerServiceBindingStatus{
				Name:         "service1",
				Namespace:    "test",
				AssignedPort: 61616,
			},
		},
	}
	app2 := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-app-2",
			Namespace: "test",
		},
		Status: broker.BrokerAppStatus{
			Service: &broker.BrokerServiceBindingStatus{
				Name:         "service1",
				Namespace:    "test",
				AssignedPort: 61617,
			},
		},
	}
	app3 := &broker.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-app-3",
			Namespace: "test",
		},
		Status: broker.BrokerAppStatus{
			Service: &broker.BrokerServiceBindingStatus{
				Name:         "service1",
				Namespace:    "test",
				AssignedPort: 61618,
			},
		},
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	fakeClient := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithRuntimeObjects(app, service1, service2, app1, app2, app3, ns).
		WithStatusSubresource(app, app1, app2, app3, service1, service2)).
		Build()

	reconciler := &BrokerAppInstanceReconciler{
		BrokerAppReconciler: &BrokerAppReconciler{
			ReconcilerLoop: &ReconcilerLoop{
				KubeBits: &KubeBits{
					Client: fakeClient,
					Scheme: scheme,
				},
			},
		},
		instance: app,
		status:   app.Status.DeepCopy(),
	}

	// Call resolveBrokerService
	err := reconciler.resolveBrokerService()

	// Should NOT error - should skip exhausted service1 and select service2 with capacity
	assert.NoError(t, err, "should skip service with exhausted ports and select one with capacity")

	// Should have selected service2 (the one with available ports)
	assert.NotNil(t, reconciler.service, "should have selected a service")
	if reconciler.service != nil {
		assert.Equal(t, "service2", reconciler.service.Name, "should select service with available port capacity")
	}

	// Should have assigned a port from service2
	if reconciler.status.Service != nil {
		assert.Equal(t, "service2", reconciler.status.Service.Name)
		assert.NotZero(t, reconciler.status.Service.AssignedPort, "should have assigned a port")
	}
}
