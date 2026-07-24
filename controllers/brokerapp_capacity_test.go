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
	brokerv1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Helper function to create scheme
func createTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = brokerv1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	return scheme
}

var _ = Describe("BrokerApp FindServiceWithCapacity", func() {
	Context("when selecting service for BrokerApp", func() {
		var scheme *runtime.Scheme

		BeforeEach(func() {
			scheme = createTestScheme()
		})

		It("should pick first service when no resource constraints", func() {
			app := &brokerv1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "new-app",
					Namespace: "test",
				},
				Spec: brokerv1beta2.BrokerAppSpec{
					Resources: corev1.ResourceRequirements{}, // No resources specified
				},
			}
			services := []brokerv1beta2.BrokerService{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service1", Namespace: "test"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "service2", Namespace: "test"},
				},
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: app.Namespace},
			}
			objs := []runtime.Object{namespace, app}
			for i := range services {
				services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   brokerv1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: brokerv1beta2.ReadyConditionReason,
					},
				}
				objs = append(objs, &services[i])
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
				instance: app,
			}

			serviceList := &brokerv1beta2.BrokerServiceList{Items: services}
			chosen, assignedPort, err := reconciler.findServiceWithCapacity(serviceList)

			Expect(err).NotTo(HaveOccurred())
			Expect(chosen).NotTo(BeNil())
			Expect(chosen.Name).To(Equal("service1"))
			Expect(assignedPort).NotTo(Equal(UnassignedPort))
		})

		It("should pick service with most available memory", func() {
			app := &brokerv1beta2.BrokerApp{
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
			}
			services := []brokerv1beta2.BrokerService{
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
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: app.Namespace},
			}
			objs := []runtime.Object{namespace, app}
			for i := range services {
				services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   brokerv1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: brokerv1beta2.ReadyConditionReason,
					},
				}
				objs = append(objs, &services[i])
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
				instance: app,
			}

			serviceList := &brokerv1beta2.BrokerServiceList{Items: services}
			chosen, assignedPort, err := reconciler.findServiceWithCapacity(serviceList)

			Expect(err).NotTo(HaveOccurred())
			Expect(chosen).NotTo(BeNil())
			Expect(chosen.Name).To(Equal("service2")) // service2 has more capacity
			Expect(assignedPort).NotTo(Equal(UnassignedPort))
		})

		It("should consider already provisioned apps when calculating capacity", func() {
			app := &brokerv1beta2.BrokerApp{
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
			}
			services := []brokerv1beta2.BrokerService{
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
			}
			existingApps := []brokerv1beta2.BrokerApp{
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
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: app.Namespace},
			}
			objs := []runtime.Object{namespace, app}
			for i := range services {
				services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   brokerv1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: brokerv1beta2.ReadyConditionReason,
					},
				}
				objs = append(objs, &services[i])
			}
			for i := range existingApps {
				objs = append(objs, &existingApps[i])
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
				instance: app,
			}

			serviceList := &brokerv1beta2.BrokerServiceList{Items: services}
			chosen, assignedPort, err := reconciler.findServiceWithCapacity(serviceList)

			Expect(err).NotTo(HaveOccurred())
			Expect(chosen).NotTo(BeNil())
			Expect(chosen.Name).To(Equal("service1")) // service1 has more available capacity now
			Expect(assignedPort).NotTo(Equal(UnassignedPort))
		})

		It("should return error when no service has enough capacity", func() {
			app := &brokerv1beta2.BrokerApp{
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
			}
			services := []brokerv1beta2.BrokerService{
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
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: app.Namespace},
			}
			objs := []runtime.Object{namespace, app}
			for i := range services {
				services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   brokerv1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: brokerv1beta2.ReadyConditionReason,
					},
				}
				objs = append(objs, &services[i])
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
				instance: app,
			}

			serviceList := &brokerv1beta2.BrokerServiceList{Items: services}
			chosen, _, err := reconciler.findServiceWithCapacity(serviceList)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("insufficient memory capacity"))
			Expect(chosen).To(BeNil())
		})

		It("should allow unlimited capacity when service has no limit", func() {
			app := &brokerv1beta2.BrokerApp{
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
			}
			services := []brokerv1beta2.BrokerService{
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
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: app.Namespace},
			}
			objs := []runtime.Object{namespace, app}
			for i := range services {
				services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   brokerv1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: brokerv1beta2.ReadyConditionReason,
					},
				}
				objs = append(objs, &services[i])
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
				instance: app,
			}

			serviceList := &brokerv1beta2.BrokerServiceList{Items: services}
			chosen, assignedPort, err := reconciler.findServiceWithCapacity(serviceList)

			Expect(err).NotTo(HaveOccurred())
			Expect(chosen).NotTo(BeNil())
			Expect(chosen.Name).To(Equal("service2")) // service2 has unlimited capacity
			Expect(assignedPort).NotTo(Equal(UnassignedPort))
		})

		It("should return error when app has missing addressRef dependency", func() {
			app := &brokerv1beta2.BrokerApp{
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
			}
			services := []brokerv1beta2.BrokerService{
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
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: app.Namespace},
			}
			objs := []runtime.Object{namespace, app}
			for i := range services {
				services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   brokerv1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: brokerv1beta2.ReadyConditionReason,
					},
				}
				objs = append(objs, &services[i])
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
				instance: app,
			}

			serviceList := &brokerv1beta2.BrokerServiceList{Items: services}
			chosen, _, err := reconciler.findServiceWithCapacity(serviceList)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("addressRef dependency not satisfied"))
			Expect(chosen).To(BeNil())
		})

		It("should return error when there is an address clash", func() {
			app := &brokerv1beta2.BrokerApp{
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
			}
			services := []brokerv1beta2.BrokerService{
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
			}
			existingApps := []brokerv1beta2.BrokerApp{
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
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: app.Namespace},
			}
			objs := []runtime.Object{namespace, app}
			for i := range services {
				services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   brokerv1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: brokerv1beta2.ReadyConditionReason,
					},
				}
				objs = append(objs, &services[i])
			}
			for i := range existingApps {
				objs = append(objs, &existingApps[i])
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
				instance: app,
			}

			serviceList := &brokerv1beta2.BrokerServiceList{Items: services}
			chosen, _, err := reconciler.findServiceWithCapacity(serviceList)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("address clash"))
			Expect(chosen).To(BeNil())
		})

		It("should allow app with matching addressRef type", func() {
			app := &brokerv1beta2.BrokerApp{
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
									AppName:      "existing-app",
								},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			services := []brokerv1beta2.BrokerService{
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
			}
			existingApps := []brokerv1beta2.BrokerApp{
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
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: app.Namespace},
			}
			objs := []runtime.Object{namespace, app}
			for i := range services {
				services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   brokerv1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: brokerv1beta2.ReadyConditionReason,
					},
				}
				objs = append(objs, &services[i])
			}
			for i := range existingApps {
				objs = append(objs, &existingApps[i])
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
				instance: app,
			}

			serviceList := &brokerv1beta2.BrokerServiceList{Items: services}
			chosen, assignedPort, err := reconciler.findServiceWithCapacity(serviceList)

			Expect(err).NotTo(HaveOccurred())
			Expect(chosen).NotTo(BeNil())
			Expect(chosen.Name).To(Equal("service1"))
			Expect(assignedPort).NotTo(Equal(UnassignedPort))
		})

		It("should return error when addressRef type mismatches", func() {
			app := &brokerv1beta2.BrokerApp{
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
									AppName:      "existing-app",
								},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			services := []brokerv1beta2.BrokerService{
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
			}
			existingApps := []brokerv1beta2.BrokerApp{
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
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: app.Namespace},
			}
			objs := []runtime.Object{namespace, app}
			for i := range services {
				services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   brokerv1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: brokerv1beta2.ReadyConditionReason,
					},
				}
				objs = append(objs, &services[i])
			}
			for i := range existingApps {
				objs = append(objs, &existingApps[i])
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
				instance: app,
			}

			serviceList := &brokerv1beta2.BrokerServiceList{Items: services}
			chosen, _, err := reconciler.findServiceWithCapacity(serviceList)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("addressRef"))
			Expect(chosen).To(BeNil())
		})

		It("should return error when producer ref has semantic mismatch", func() {
			app := &brokerv1beta2.BrokerApp{
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
									PubSub:       &[]bool{true}[0], // pub/sub semantics (multicast)
									AppNamespace: "test",
									AppName:      "existing-app",
								},
							},
						},
					},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			}
			services := []brokerv1beta2.BrokerService{
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
			}
			existingApps := []brokerv1beta2.BrokerApp{
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
			}

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: app.Namespace},
			}
			objs := []runtime.Object{namespace, app}
			for i := range services {
				services[i].Status.Conditions = []metav1.Condition{
					{
						Type:   brokerv1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: brokerv1beta2.ReadyConditionReason,
					},
				}
				objs = append(objs, &services[i])
			}
			for i := range existingApps {
				objs = append(objs, &existingApps[i])
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
				instance: app,
			}

			serviceList := &brokerv1beta2.BrokerServiceList{Items: services}
			chosen, _, err := reconciler.findServiceWithCapacity(serviceList)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("addressRef"))
			Expect(chosen).To(BeNil())
		})
	})
})
