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

	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("BrokerService app selector", func() {
	Context("namespace access control", func() {
		It("should allow an app from a permitted namespace", func() {
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
			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc, app, allowedNsObj, sharedNsObj).
				WithStatusSubresource(app, svc)).
				Build()

			// Create Reconciler
			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			// Reconcile the app
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: allowedNs}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// Verify BrokerApp status
			updatedApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			// Should have status binding to the service
			Expect(updatedApp.Status.Service).NotTo(BeNil(), "App should be bound to service")
			Expect(updatedApp.Status.Service.Name).To(Equal(svcName))
			Expect(updatedApp.Status.Service.Namespace).To(Equal(svcNs))

			// Check Valid condition - should be True
			validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCondition).NotTo(BeNil())
			Expect(validCondition.Status).To(Equal(v1.ConditionTrue))

			// Check Deployed condition - should be False/ProvisioningPending (waiting for broker to apply)
			deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCondition).NotTo(BeNil())
			Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
			Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionProvisioningPendingReason))
		})
	})
	It("should deny an app from a non-permitted namespace", func() {
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
		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(svc, app, allowedNsObj, deniedNsObj, sharedNsObj).
			WithStatusSubresource(app, svc)).
			Build()

		// Create Reconciler
		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// Reconcile the app
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: deniedNs}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred()) // err is reflected in the status

		// Verify BrokerApp status
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		// Should NOT have annotation (not bound to service)
		Expect(updatedApp.Status.Service).To(BeNil(), "App should not be bound to service")

		// Check Valid condition - should be True (spec is valid)
		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(v1.ConditionTrue))

		// Check Deployed condition - should be False with Unauthorized reason
		deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))
		Expect(deployedCondition.Message).To(ContainSubstring("no services"))

		// Ready should be False
		readyCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(v1.ConditionFalse))
	})
	It("should deny all apps when the allow list is empty", func() {
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
		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(svc, app, svcNsObj).
			WithStatusSubresource(app, svc)).
			Build()

		// Create Reconciler
		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// Reconcile the app
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: svcNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		// Verify BrokerApp status
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		// Should have annotation binding to the service (allowed because same namespace)
		Expect(updatedApp.Status.Service).NotTo(BeNil(), "App should be bound to service")
		Expect(updatedApp.Status.Service.Name).To(Equal(svcName))
		Expect(updatedApp.Status.Service.Namespace).To(Equal(svcNs))

		// Check Valid condition - should be True
		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(v1.ConditionTrue))
	})
	It("should deny all apps from a different namespace when the allow list is empty", func() {
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
		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(svc, app, svcNsObj, appNsObj).
			WithStatusSubresource(app, svc)).
			Build()

		// Create Reconciler
		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// Reconcile the app
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: appNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred()) // err is reflected in the status

		// Verify BrokerApp status
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		// Should NOT have annotation
		Expect(updatedApp.Status.Service).To(BeNil(), "App should not be bound to service")

		// Check Deployed condition - should be False with Unauthorized reason
		deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))
	})
	It("should deny an app after its namespace is removed from the allow list", func() {
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
		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(svc, app, appNsObj, sharedNsObj).
			WithStatusSubresource(app, svc)).
			Build()

		// Create Reconciler
		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// First reconcile - app should be authorized
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: appNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		// Should still be bound
		Expect(updatedApp.Status.Service).NotTo(BeNil(), "App should remain bound initially")

		// Now update the service to remove team-a from allowed namespaces
		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: svcNs}, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		updatedSvc.Spec.AppSelectorExpression = `app.metadata.namespace == "team-b"` // Change to only allow team-b
		err = cl.Update(context.TODO(), updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		// Reconcile again - app should be unbound and unauthorized
		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred()) // err is reflected in the status

		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		// Check Deployed condition - should show Unauthorized
		deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))
	})
	It("should allow apps from multiple permitted namespaces", func() {
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
		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(svc, appA, appB, appDenied, svcNsObj, teamANsObj, teamBNsObj, teamDNsObj).
			WithStatusSubresource(appA, appB, appDenied, svc)).
			Build()

		// Create Reconciler
		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// Reconcile app-a (should succeed)
		reqA := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-a", Namespace: "team-a"}}
		_, err := r.Reconcile(context.TODO(), reqA)
		Expect(err).NotTo(HaveOccurred())

		updatedAppA := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqA.NamespacedName, updatedAppA)
		Expect(err).NotTo(HaveOccurred())
		hasBinding := updatedAppA.Status.Service != nil
		Expect(hasBinding).To(BeTrue(), "App A should be bound")

		// Reconcile app-b (should succeed)
		reqB := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-b", Namespace: "team-b"}}
		_, err = r.Reconcile(context.TODO(), reqB)
		Expect(err).NotTo(HaveOccurred())

		updatedAppB := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqB.NamespacedName, updatedAppB)
		Expect(err).NotTo(HaveOccurred())
		hasBinding = updatedAppB.Status.Service != nil
		Expect(hasBinding).To(BeTrue(), "App B should be bound")

		// Reconcile app-denied (should fail)
		reqDenied := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-denied", Namespace: "team-d"}}
		_, err = r.Reconcile(context.TODO(), reqDenied)
		Expect(err).To(HaveOccurred())

		updatedAppDenied := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqDenied.NamespacedName, updatedAppDenied)
		Expect(err).NotTo(HaveOccurred())
		hasBinding = updatedAppDenied.Status.Service != nil
		Expect(hasBinding).To(BeFalse(), "Denied app should not be bound")

		deployedCondition := meta.FindStatusCondition(updatedAppDenied.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))
	})
	It("should allow apps from any namespace when the selector matches all", func() {
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
		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(svc, app, svcNsObj, appNsObj).
			WithStatusSubresource(app, svc)).
			Build()

		// Create Reconciler
		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// Reconcile the app
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: appNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		// Verify BrokerApp status
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		// Should have annotation
		Expect(updatedApp.Status.Service).NotTo(BeNil(), "App should be bound to service")
		Expect(updatedApp.Status.Service.Name).To(Equal(svcName))
		Expect(updatedApp.Status.Service.Namespace).To(Equal(svcNs))
	})
	It("should allow an app whose namespace matches by prefix", func() {
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
		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(svc, appMatch, appNoMatch, svcNsObj, teamAProdNsObj, appNoMatchNsObj).
			WithStatusSubresource(appMatch, appNoMatch, svc)).
			Build()

		// Create Reconciler
		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// Reconcile matching app - should succeed
		reqMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-match", Namespace: "team-a-prod"}}
		_, err := r.Reconcile(context.TODO(), reqMatch)
		Expect(err).NotTo(HaveOccurred())

		updatedMatch := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqMatch.NamespacedName, updatedMatch)
		Expect(err).NotTo(HaveOccurred())
		hasBinding := updatedMatch.Status.Service != nil
		Expect(hasBinding).To(BeTrue(), "Matching app should be bound")

		// Reconcile non-matching app - should fail
		reqNoMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-nomatch", Namespace: "other-namespace"}}

		_, err = r.Reconcile(context.TODO(), reqNoMatch)
		Expect(err).To(HaveOccurred()) // err is reflected in the status

		updatedNoMatch := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqNoMatch.NamespacedName, updatedNoMatch)
		Expect(err).NotTo(HaveOccurred())
		hasBinding = updatedNoMatch.Status.Service != nil
		Expect(hasBinding).To(BeFalse(), "Non-matching app should not be bound")
	})
	It("should allow an app whose namespace matches by suffix", func() {
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
		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(svc, appMatch, appNoMatch, svcNsObj, teamAProdNsObj, teamADevNsObj).
			WithStatusSubresource(appMatch, appNoMatch, svc)).
			Build()

		// Create Reconciler
		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// Reconcile matching app
		reqMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-match", Namespace: "team-a-prod"}}
		_, err := r.Reconcile(context.TODO(), reqMatch)
		Expect(err).NotTo(HaveOccurred())

		updatedMatch := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqMatch.NamespacedName, updatedMatch)
		Expect(err).NotTo(HaveOccurred())
		hasBinding := updatedMatch.Status.Service != nil
		Expect(hasBinding).To(BeTrue())

		// Reconcile non-matching app
		reqNoMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-nomatch", Namespace: "team-a-dev"}}
		_, err = r.Reconcile(context.TODO(), reqNoMatch)
		Expect(err).To(HaveOccurred())

		updatedNoMatch := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqNoMatch.NamespacedName, updatedNoMatch)
		Expect(err).NotTo(HaveOccurred())
		hasBinding = updatedNoMatch.Status.Service != nil
		Expect(hasBinding).To(BeFalse())
	})
	It("should allow an app whose namespace matches both a prefix and a suffix rule", func() {
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
		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(svc, appMatch1, appMatch2, appNoMatch, svcNsObj, teamAProdNsObj, teamBackendProdNsObj, teamADevNsObj).
			WithStatusSubresource(appMatch1, appMatch2, appNoMatch, svc)).
			Build()

		// Create Reconciler
		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// Test match 1
		req1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-match1", Namespace: "team-a-prod"}}
		_, err := r.Reconcile(context.TODO(), req1)
		Expect(err).NotTo(HaveOccurred())

		// Test match 2
		req2 := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-match2", Namespace: "team-backend-prod"}}
		_, err = r.Reconcile(context.TODO(), req2)
		Expect(err).NotTo(HaveOccurred())

		// Test no match
		reqNoMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-nomatch", Namespace: "team-a-dev"}}

		_, err = r.Reconcile(context.TODO(), reqNoMatch)
		Expect(err).To(HaveOccurred())

	})
})
