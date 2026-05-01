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
	"strings"
	"testing"

	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestFindServiceWithCapacity(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = brokerv1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name                  string
		app                   *brokerv1beta2.BrokerApp
		services              []brokerv1beta2.BrokerService
		existingApps          []brokerv1beta2.BrokerApp
		expectedServiceName   string
		expectError           bool
		expectedErrorContains string // substring that must appear in error message
	}{
		{
			name: "no resource constraints - picks first service",
			app: &brokerv1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-app",
					Namespace: "test",
				},
				Spec: brokerv1beta2.BrokerAppSpec{
					Resources: corev1.ResourceRequirements{}, // No resources specified
				},
			},
			services: []brokerv1beta2.BrokerService{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service1", Namespace: "test"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service2", Namespace: "test"},
				},
			},
			expectedServiceName: "service1",
			expectError:         false,
		},
		{
			name: "picks service with most available memory",
			app: &brokerv1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-app",
					Namespace: "test",
				},
				Spec: brokerv1beta2.BrokerAppSpec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
			services: []brokerv1beta2.BrokerService{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service1", Namespace: "test"},
					Spec: brokerv1beta2.BrokerServiceSpec{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service2", Namespace: "test"},
					Spec: brokerv1beta2.BrokerServiceSpec{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("4Gi"),
							},
						},
					},
				},
			},
			expectedServiceName: "service2", // service2 has more capacity
			expectError:         false,
		},
		{
			name: "considers already provisioned apps",
			app: &brokerv1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-app",
					Namespace: "test",
				},
				Spec: brokerv1beta2.BrokerAppSpec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
			services: []brokerv1beta2.BrokerService{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service1", Namespace: "test"},
					Spec: brokerv1beta2.BrokerServiceSpec{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service2", Namespace: "test"},
					Spec: brokerv1beta2.BrokerServiceSpec{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("4Gi"),
							},
						},
					},
				},
			},
			existingApps: []brokerv1beta2.BrokerApp{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-app",
						Namespace: "test",
					},
					Spec: brokerv1beta2.BrokerAppSpec{
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("3Gi"), // service2 now has less available
							},
						},
					},
					Status: brokerv1beta2.BrokerAppStatus{
						Service: &brokerv1beta2.BrokerServiceBindingStatus{
							Name:      "service2",
							Namespace: "test",
							Secret:    "binding-secret",
						},
					},
				},
			},
			expectedServiceName: "service1", // service1 has more available capacity now
			expectError:         false,
		},
		{
			name: "no service has enough capacity",
			app: &brokerv1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-app",
					Namespace: "test",
				},
				Spec: brokerv1beta2.BrokerAppSpec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("5Gi"),
						},
					},
				},
			},
			services: []brokerv1beta2.BrokerService{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service1", Namespace: "test"},
					Spec: brokerv1beta2.BrokerServiceSpec{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			expectedServiceName:   "",
			expectError:           true,
			expectedErrorContains: "insufficient memory capacity",
		},
		{
			name: "service with no limit has unlimited capacity",
			app: &brokerv1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-app",
					Namespace: "test",
				},
				Spec: brokerv1beta2.BrokerAppSpec{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("100Gi"),
						},
					},
				},
			},
			services: []brokerv1beta2.BrokerService{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service1", Namespace: "test"},
					Spec: brokerv1beta2.BrokerServiceSpec{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service2", Namespace: "test"},
					Spec: brokerv1beta2.BrokerServiceSpec{
						Resources: corev1.ResourceRequirements{
							// No limits specified = unlimited
						},
					},
				},
			},
			expectedServiceName: "service2", // service2 has unlimited capacity
			expectError:         false,
		},
		{
			name: "app with missing address",
			app: &brokerv1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-app",
					Namespace: "test",
				},
				Spec: brokerv1beta2.BrokerAppSpec{
					Capabilities: []brokerv1beta2.AppCapabilityType{
						{
							ConsumerOf: []brokerv1beta2.AddressRef{
								{
									Address:      "orders",
									AppNamespace: defaultNamespace,
									AppName:      "does-not-exist",
								},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("100Gi"),
						},
					},
				},
			},
			services: []brokerv1beta2.BrokerService{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service1", Namespace: "test"},
					Spec: brokerv1beta2.BrokerServiceSpec{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service2", Namespace: "test"},
					Spec: brokerv1beta2.BrokerServiceSpec{
						Resources: corev1.ResourceRequirements{
							// No limits specified = unlimited
						},
					},
				},
			},
			expectedServiceName:   "",
			expectError:           true,
			expectedErrorContains: "addressRef dependency not satisfied",
		},
		{
			name: "app with address clash",
			app: &brokerv1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-app",
					Namespace: "test",
				},
				Spec: brokerv1beta2.BrokerAppSpec{
					Capabilities: []brokerv1beta2.AppCapabilityType{
						{
							ProducerOf: []brokerv1beta2.AddressRef{
								{Address: "shared-queue"},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
			services: []brokerv1beta2.BrokerService{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service1", Namespace: "test"},
					Spec: brokerv1beta2.BrokerServiceSpec{
						Resources: corev1.ResourceRequirements{
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
					},
				},
			},
			existingApps: []brokerv1beta2.BrokerApp{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "existing-app",
						Namespace: "test",
					},
					Spec: brokerv1beta2.BrokerAppSpec{
						Capabilities: []brokerv1beta2.AppCapabilityType{
							{
								ConsumerOf: []brokerv1beta2.AddressRef{
									{Address: "shared-queue"}, // Same address - clash!
								},
							},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
					},
					Status: brokerv1beta2.BrokerAppStatus{
						Service: &brokerv1beta2.BrokerServiceBindingStatus{
							Name:      "service1",
							Namespace: "test",
							Secret:    "binding-secret",
						},
					},
				},
			},
			expectedServiceName:   "",
			expectError:           true,
			expectedErrorContains: "address clash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with services and existing apps
			objs := make([]runtime.Object, 0, len(tt.services)+len(tt.existingApps)+2)

			// Add namespace object (required for CEL evaluation)
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.app.Namespace,
				},
			}
			objs = append(objs, namespace, tt.app)

			for i := range tt.services {
				// Add Deployed condition and AvailablePorts to all services
				tt.services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   brokerv1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: brokerv1beta2.ReadyConditionReason,
					},
				}
				tt.services[i].Status.AvailablePorts = &brokerv1beta2.PortPoolInfo{
					Source: "Default",
					PortRange: &brokerv1beta2.PortRange{
						Start: 61616,
						End:   62615,
					},
				}
				objs = append(objs, &tt.services[i])
			}
			for i := range tt.existingApps {
				objs = append(objs, &tt.existingApps[i])
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				WithIndex(&brokerv1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
					app := obj.(*brokerv1beta2.BrokerApp)
					if app.Status.Service != nil {
						return []string{app.Status.Service.Key()}
					}
					return nil
				}).
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
			}

			// Create service list
			serviceList := &brokerv1beta2.BrokerServiceList{
				Items: tt.services,
			}

			// Call findServiceWithCapacity
			chosen, assignedPort, err := reconciler.findServiceWithCapacity(serviceList)

			// Check error expectation
			if (err != nil) != tt.expectError {
				t.Errorf("findServiceWithCapacity() error = %v, expectError %v", err, tt.expectError)
				return
			}

			// Check error message content if expected
			if tt.expectError && tt.expectedErrorContains != "" {
				if err == nil {
					t.Errorf("expected error containing '%s', got nil error", tt.expectedErrorContains)
				} else if !strings.Contains(err.Error(), tt.expectedErrorContains) {
					t.Errorf("expected error to contain '%s', got: %v", tt.expectedErrorContains, err.Error())
				}
			}

			// Check chosen service
			if tt.expectedServiceName != "" {
				if chosen == nil {
					t.Errorf("expected service %s, got nil", tt.expectedServiceName)
				} else if chosen.Name != tt.expectedServiceName {
					t.Errorf("expected service %s, got %s", tt.expectedServiceName, chosen.Name)
				}
				// If a service was chosen, port should be assigned
				if assignedPort == UnassignedPort {
					t.Errorf("expected assigned port for service %s, got UnassignedPort", tt.expectedServiceName)
				}
			} else {
				if chosen != nil {
					t.Errorf("expected no service, got %s", chosen.Name)
				}
			}
		})
	}
}
