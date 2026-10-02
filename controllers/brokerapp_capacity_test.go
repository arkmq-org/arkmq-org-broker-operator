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

	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("brokerapp capacity", func() {

	Context("findServiceWithCapacity", func() {

		type capacityTestCase struct {
			name                  string
			app                   *brokerv1beta2.BrokerApp
			services              []brokerv1beta2.BrokerService
			existingApps          []brokerv1beta2.BrokerApp
			expectedServiceName   string
			expectError           bool
			expectedErrorContains string
		}

		tests := []capacityTestCase{
			{
				name: "no resource constraints - picks first service",
				app: &brokerv1beta2.BrokerApp{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "new-app",
						Namespace: "test",
					},
					Spec: brokerv1beta2.BrokerAppSpec{
						Resources: corev1.ResourceRequirements{},
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
				expectedServiceName: "service2",
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
									corev1.ResourceMemory: resource.MustParse("3Gi"),
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
				expectedServiceName: "service1",
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
							Resources: corev1.ResourceRequirements{},
						},
					},
				},
				expectedServiceName: "service2",
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
							Resources: corev1.ResourceRequirements{},
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
			{
				name: "app with address ref type match",
				app: &brokerv1beta2.BrokerApp{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "new-app",
						Namespace: "test",
					},
					Spec: brokerv1beta2.BrokerAppSpec{
						Capabilities: []brokerv1beta2.AppCapabilityType{
							{
								ProducerOf: []brokerv1beta2.AddressRef{
									{
										Address:      "shared-queue",
										AppNamespace: "test",
										AppName:      "existing-app"},
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
							SharedAddresses: []brokerv1beta2.AddressType{NewAddressType("shared-queue").Build()},
							Capabilities: []brokerv1beta2.AppCapabilityType{
								{
									ConsumerOf: []brokerv1beta2.AddressRef{
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
						Status: brokerv1beta2.BrokerAppStatus{
							Service: &brokerv1beta2.BrokerServiceBindingStatus{
								Name:      "service1",
								Namespace: "test",
								Secret:    "binding-secret",
							},
						},
					},
				},
				expectedServiceName:   "service1",
				expectError:           false,
				expectedErrorContains: "",
			},
			{
				name: "app with ref type mis match",
				app: &brokerv1beta2.BrokerApp{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "new-app",
						Namespace: "test",
					},
					Spec: brokerv1beta2.BrokerAppSpec{
						Capabilities: []brokerv1beta2.AppCapabilityType{
							{
								ProducerOf: []brokerv1beta2.AddressRef{
									{
										Address:      "shared",
										AppNamespace: "test",
										AppName:      "existing-app"},
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
							SharedAddresses: []brokerv1beta2.AddressType{NewAddressType("shared").WithPubSub(true).Build()},
							Capabilities: []brokerv1beta2.AppCapabilityType{
								{
									ConsumerOf: []brokerv1beta2.AddressRef{
										{Address: "shared", Subscriptions: []string{"sub1"}},
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
				expectedErrorContains: "addressRef",
			},
			{
				name: "app producer to shared with ref semantic mis match",
				app: &brokerv1beta2.BrokerApp{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "new-app",
						Namespace: "test",
					},
					Spec: brokerv1beta2.BrokerAppSpec{
						Capabilities: []brokerv1beta2.AppCapabilityType{
							{
								ProducerOf: []brokerv1beta2.AddressRef{
									{
										Address:      "shared",
										PubSub:       &[]bool{true}[0],
										AppNamespace: "test",
										AppName:      "existing-app"},
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
							SharedAddresses: []brokerv1beta2.AddressType{NewAddressType("shared").Build()},
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
				expectedErrorContains: "addressRef",
			},
		}

		scheme := runtime.NewScheme()
		_ = brokerv1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		for _, tt := range tests {
			tt := tt
			It(tt.name, Label(unitLabel), func() {
				objs := make([]runtime.Object, 0, len(tt.services)+len(tt.existingApps)+2)

				namespace := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: tt.app.Namespace,
					},
				}
				objs = append(objs, namespace, tt.app)

				for i := range tt.services {
					tt.services[i].Status.Conditions = []metav1.Condition{
						{
							Type:   brokerv1beta2.DeployedConditionType,
							Status: metav1.ConditionTrue,
							Reason: brokerv1beta2.ReadyConditionReason,
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

				serviceList := &brokerv1beta2.BrokerServiceList{
					Items: tt.services,
				}

				chosen, assignedPort, err := reconciler.findServiceWithCapacity(serviceList)

				if tt.expectError {
					Expect(err).To(HaveOccurred())
					if tt.expectedErrorContains != "" {
						Expect(err.Error()).To(ContainSubstring(tt.expectedErrorContains),
							fmt.Sprintf("expected error containing '%s', got: %v", tt.expectedErrorContains, err))
					}
				} else {
					Expect(err).NotTo(HaveOccurred())
				}

				if tt.expectedServiceName != "" {
					Expect(chosen).NotTo(BeNil(), fmt.Sprintf("expected service %s, got nil", tt.expectedServiceName))
					Expect(chosen.Name).To(Equal(tt.expectedServiceName))
					Expect(assignedPort).NotTo(Equal(UnassignedPort),
						fmt.Sprintf("expected assigned port for service %s", tt.expectedServiceName))
				} else {
					Expect(chosen).To(BeNil())
				}
			})
		}
	})
})
