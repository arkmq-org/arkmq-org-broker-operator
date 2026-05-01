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
	"fmt"
	"testing"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolveBrokerService(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = broker.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name                   string
		app                    *broker.BrokerApp
		services               []broker.BrokerService
		expectedServiceName    string
		expectedBinding        string
		expectedError          bool
		expectedValidCondition metav1.ConditionStatus
	}{
		{
			name: "initial assignment - finds matching service",
			app: &broker.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-app",
					Namespace: "test",
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
				},
				Status: broker.BrokerAppStatus{
					Service: &broker.BrokerServiceBindingStatus{
						Name:      "service1",
						Namespace: "test",
						Secret:    "binding-secret",
					},
				},
			},
			services: []broker.BrokerService{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service1",
						Namespace: "test",
						Labels:    map[string]string{"env": "dev"},
					},
				},
			},
			expectedServiceName:    "service1",
			expectedBinding:        "test:service1",
			expectedError:          false,
			expectedValidCondition: metav1.ConditionTrue,
		},
		{
			name: "initial assignment - no matching services",
			app: &broker.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-app",
					Namespace: "test",
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "prod"},
					},
				},
			},
			services: []broker.BrokerService{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service1",
						Namespace: "test",
						Labels:    map[string]string{"env": "dev"},
					},
				},
			},
			expectedServiceName:    "",
			expectedBinding:        "",
			expectedError:          true,
			expectedValidCondition: metav1.ConditionTrue, // Selector syntax is valid, runtime issue handled in Deployed
		},
		{
			name: "existing annotation - service still matches",
			app: &broker.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-app",
					Namespace: "test",
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
				},
				Status: broker.BrokerAppStatus{
					Service: &broker.BrokerServiceBindingStatus{
						Name:      "service1",
						Namespace: "test",
						Secret:    "binding-secret",
					},
				},
			},
			services: []broker.BrokerService{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service1",
						Namespace: "test",
						Labels:    map[string]string{"env": "dev"},
					},
				},
			},
			expectedServiceName:    "service1",
			expectedBinding:        "test:service1",
			expectedError:          false,
			expectedValidCondition: metav1.ConditionTrue,
		},
		{
			name: "selector changed - reassign to new service",
			app: &broker.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-app",
					Namespace: "test",
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "prod"}, // Now selecting prod
					},
				},
				Status: broker.BrokerAppStatus{
					Service: &broker.BrokerServiceBindingStatus{
						Name:      "service1",
						Namespace: "test",
						Secret:    "binding-secret",
					},
				},
			},
			services: []broker.BrokerService{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service1",
						Namespace: "test",
						Labels:    map[string]string{"env": "dev"}, // service1 is dev
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service2",
						Namespace: "test",
						Labels:    map[string]string{"env": "prod"}, // service2 is prod
					},
				},
			},
			expectedServiceName:    "service2",
			expectedBinding:        "test:service2", // Should reassign to service2
			expectedError:          false,
			expectedValidCondition: metav1.ConditionTrue,
		},
		{
			name: "service deleted - annotation exists but no matching services",
			app: &broker.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-app",
					Namespace: "test",
				},
				Spec: broker.BrokerAppSpec{
					ServiceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"env": "dev"},
					},
				},
				Status: broker.BrokerAppStatus{
					Service: &broker.BrokerServiceBindingStatus{
						Name:      "service1",
						Namespace: "test",
						Secret:    "binding-secret",
					},
				},
			},
			services:               []broker.BrokerService{}, // Service deleted
			expectedServiceName:    "",
			expectedBinding:        "test:service1", // Annotation preserved
			expectedError:          false,           // No error, just no service available
			expectedValidCondition: metav1.ConditionTrue,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with app and services
			objs := make([]runtime.Object, 0, len(tt.services)+1)
			objs = append(objs, tt.app)
			for i := range tt.services {
				// Add Deployed condition and AvailablePorts to all services
				tt.services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   broker.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: broker.ReadyConditionReason,
					},
				}
				tt.services[i].Status.AvailablePorts = &broker.PortPoolInfo{
					Source: "Default",
					PortRange: &broker.PortRange{
						Start: 61616,
						End:   62615,
					},
				}
				objs = append(objs, &tt.services[i])
			}
			objs = append(objs, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.app.Namespace,
				},
			})
			fakeClient := setupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...)).
				Build()

			// Create reconciler
			reconciler := &BrokerAppInstanceReconciler{
				BrokerAppReconciler: &BrokerAppReconciler{
					ReconcilerLoop: &ReconcilerLoop{
						KubeBits: &KubeBits{
							Client: fakeClient,
							Scheme: scheme,
						},
					},
				},
				instance: tt.app,
				status:   tt.app.Status.DeepCopy(),
			}

			// Call resolveBrokerService
			err := reconciler.resolveBrokerService()

			// Check error expectation
			if (err != nil) != tt.expectedError {
				t.Errorf("resolveBrokerService() error = %v, expectedError %v", err, tt.expectedError)
				return
			}

			// Check service assignment
			if tt.expectedServiceName != "" {
				if reconciler.service == nil {
					t.Errorf("expected service to be assigned to %s, got nil", tt.expectedServiceName)
				} else if reconciler.service.Name != tt.expectedServiceName {
					t.Errorf("expected service name %s, got %s", tt.expectedServiceName, reconciler.service.Name)
				}
			} else {
				if reconciler.service != nil {
					t.Errorf("expected no service assignment, got %s", reconciler.service.Name)
				}
			}

			// Check status binding was updated correctly
			if tt.expectedBinding != "" {
				// Check the reconciler's status (it writes to a copy, not the instance)
				var actualBinding string
				if reconciler.status.Service != nil {
					actualBinding = fmt.Sprintf("%s:%s", reconciler.status.Service.Namespace, reconciler.status.Service.Name)
				}
				if actualBinding != tt.expectedBinding {
					t.Errorf("expected annotation %s, got %s", tt.expectedBinding, actualBinding)
				}
			}

			// Check Valid condition
			validCond := meta.FindStatusCondition(reconciler.status.Conditions, broker.ValidConditionType)
			if validCond != nil && validCond.Status != tt.expectedValidCondition {
				t.Errorf("expected Valid condition status %s, got %s", tt.expectedValidCondition, validCond.Status)
			}
		})
	}
}
