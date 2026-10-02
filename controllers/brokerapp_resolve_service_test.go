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

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("brokerapp resolve service", func() {

	type resolveServiceTestCase struct {
		name                   string
		app                    *broker.BrokerApp
		services               []broker.BrokerService
		expectedServiceName    string
		expectedBinding        string
		expectedError          bool
		expectedValidCondition metav1.ConditionStatus
	}

	scheme := runtime.NewScheme()
	_ = broker.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	tests := []resolveServiceTestCase{
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
			expectedValidCondition: metav1.ConditionTrue,
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
						MatchLabels: map[string]string{"env": "prod"},
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
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "service2",
						Namespace: "test",
						Labels:    map[string]string{"env": "prod"},
					},
				},
			},
			expectedServiceName:    "service2",
			expectedBinding:        "test:service2",
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
			services:               []broker.BrokerService{},
			expectedServiceName:    "",
			expectedBinding:        "test:service1",
			expectedError:          false,
			expectedValidCondition: metav1.ConditionTrue,
		},
	}

	for _, tt := range tests {
		tt := tt
		It(tt.name, Label(unitLabel), func() {
			objs := make([]runtime.Object, 0, len(tt.services)+1)
			objs = append(objs, tt.app)
			for i := range tt.services {
				tt.services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   broker.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: broker.ReadyConditionReason,
					},
				}
				objs = append(objs, &tt.services[i])
			}
			objs = append(objs, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.app.Namespace,
				},
			})
			fakeClient := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...)).
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
				instance: tt.app,
				status:   tt.app.Status.DeepCopy(),
			}

			err := reconciler.resolveBrokerService()

			if tt.expectedError {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).NotTo(HaveOccurred())
			}

			if tt.expectedServiceName != "" {
				Expect(reconciler.service).NotTo(BeNil(),
					fmt.Sprintf("expected service to be assigned to %s, got nil", tt.expectedServiceName))
				Expect(reconciler.service.Name).To(Equal(tt.expectedServiceName))
			} else {
				Expect(reconciler.service).To(BeNil())
			}

			if tt.expectedBinding != "" {
				var actualBinding string
				if reconciler.status.Service != nil {
					actualBinding = fmt.Sprintf("%s:%s", reconciler.status.Service.Namespace, reconciler.status.Service.Name)
				}
				Expect(actualBinding).To(Equal(tt.expectedBinding))
			}

			validCond := meta.FindStatusCondition(reconciler.status.Conditions, broker.ValidConditionType)
			if validCond != nil {
				Expect(validCond.Status).To(Equal(tt.expectedValidCondition))
			}
		})
	}
})
