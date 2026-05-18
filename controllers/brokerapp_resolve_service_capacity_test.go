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

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

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

	// Service 1: Not deployed yet
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
					Reason: broker.DeployedConditionNotReadyReason,
				},
			},
		},
	}

	// Service 2: ready with capacity
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
		},
	}

	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
	}

	fakeClient := SetupBrokerAppIndexer(fake.NewClientBuilder().
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
