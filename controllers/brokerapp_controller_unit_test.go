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
	"time"

	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("BrokerApp Reconciler", func() {
	Context("basic reconcile", func() {
		It("should successfully reconcile and bind to service", func() {
			ns := "default"
			svcName := "my-broker-service"
			appName := "my-app"

			svc := NewBrokerService(svcName, ns).Build()
			app := NewBrokerApp(appName, ns).Build()

			env := NewTestEnvironment(ns, svc, app)
			r := env.Reconciler
			cl := env.Client

			// Reconcile
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// Verify BrokerApp has annotation
			updatedApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			// Verify Service binding
			Expect(updatedApp.Status.Service).NotTo(BeNil())
			Expect(updatedApp.Status.Service.Name).To(Equal(svcName))
			Expect(updatedApp.Status.Service.Namespace).To(Equal(ns))
			Expect(updatedApp.Status.Service.Secret).NotTo(BeEmpty())

			// Verify Status
			Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)).To(BeFalse())
			Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)).To(BeFalse())

			bindingSecret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{Name: updatedApp.Status.Service.Secret, Namespace: ns}, bindingSecret)
			Expect(err).NotTo(HaveOccurred())

			Expect(string(bindingSecret.Data["host"])).To(Equal(fmt.Sprintf("%s.%s.svc.%s", svcName, ns, common.GetClusterDomain())))
			Expect(string(bindingSecret.Data["port"])).To(Equal(fmt.Sprintf("%d", updatedApp.Status.Service.AssignedPort)))
			Expect(string(bindingSecret.Data["uri"])).To(Equal(fmt.Sprintf("amqps://%s.%s.svc.%s:%d", svcName, ns, common.GetClusterDomain(), updatedApp.Status.Service.AssignedPort)))

			// update broker service status to reflect ready with deployed app
			svc.Status.ProvisionedApps = []string{AppIdentity(app)}
			err = cl.Status().Update(context.TODO(), svc)
			Expect(err).NotTo(HaveOccurred())

			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// Verify Status
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())
			Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)).To(BeTrue())
			Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)).To(BeTrue())
		})

		It("should handle no matching service error", func() {
			ns := "default"
			appName := "my-app"

			app := NewBrokerApp(appName, ns).
				WithServiceSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{"type": "non-existent"},
				}).
				Build()

			env := NewTestEnvironment(ns, app)
			r := env.Reconciler
			cl := env.Client

			// Reconcile
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			// TransientError (no matching service) results in requeue
			Expect(err).To(HaveOccurred())

			// Verify BrokerApp status
			updatedApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			// Check Valid condition - should be True (selector syntax is valid)
			validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCondition).NotTo(BeNil())
			Expect(validCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionSuccessReason))

			// Check Deployed condition - should reflect no matching service
			deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCondition).NotTo(BeNil())
			Expect(deployedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))
		})
	})

	Context("Valid condition transitions", func() {
		It("should transition from no matching service to provisioning pending", func() {
			ns := "default"
			svcName := "my-broker-service"
			appName := "my-app"

			svc := NewBrokerService(svcName, ns).Build()
			app := NewBrokerApp(appName, ns).
				WithServiceSelector(&metav1.LabelSelector{
					MatchLabels: map[string]string{"type": "non-existent"},
				}).
				Build()

			env := NewTestEnvironment(ns, svc, app)
			r := env.Reconciler
			cl := env.Client

			// 1. Reconcile with non-matching selector
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred())

			// Verify Valid condition is True (selector syntax is valid)
			updatedApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionTrue))

			// Verify Deployed condition is False (no matching service)
			deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))

			// Wait a bit to ensure time difference
			time.Sleep(1 * time.Second)

			// 2. Update App to match service
			updatedApp.Spec.ServiceSelector.MatchLabels["type"] = "broker"
			err = cl.Update(context.TODO(), updatedApp)
			Expect(err).NotTo(HaveOccurred())

			// Reconcile again
			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// Verify Valid condition is still True
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionTrue))

			// Verify Deployed condition updated (service now available, waiting for provisioning)
			deployedCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionProvisioningPendingReason))
			// Note: LastTransitionTime doesn't change because status is still False (only reason changed)
		})
	})

	It("should return an error when the status update fails", func() {
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
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		}

		// Create BrokerService (with Deployed=True)
		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: ns,
				Labels:    map[string]string{"type": "broker"},
			},
			Status: v1beta2.BrokerServiceStatus{
				Conditions: []metav1.Condition{
					{
						Type:   v1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: v1beta2.ReadyConditionReason,
					},
				},
			},
		}

		// Create BrokerApp matching the service
		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: ns,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{
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

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
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
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated status update error"))
		Expect(result.RequeueAfter).To(Equal(time.Duration(0)))
	})

	It("should set Valid=False when an address type error is detected", func() {
		ns := "default"
		appName := "my-app"

		app := NewBrokerApp(appName, ns).
			WithConsumerOf(NewAddressRef("events::queue").WithSubscriptions("sub1").Build()).
			Build()

		env := NewTestEnvironment(ns, app)
		r := env.Reconciler
		cl := env.Client

		// Reconcile
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		// ValidationError results in no error returned (no retry until spec changes)
		Expect(err).NotTo(HaveOccurred())

		// Verify BrokerApp status
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		// Check Valid condition
		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionAddressTypeError))
		Expect(validCondition.Message).To(ContainSubstring("FQQN"))
	})

	It("should set Valid=False when the resource name is invalid", func() {
		ns := "default"
		invalidName := "invalid/name"

		app := NewBrokerApp(invalidName, ns).Build()

		env := NewTestEnvironment(ns, app)
		r := env.Reconciler
		cl := env.Client

		// Reconcile
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: invalidName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		// ValidationError results in no error returned (no retry until spec changes)
		Expect(err).NotTo(HaveOccurred())

		// Verify BrokerApp status
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		// Check Valid condition
		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionInvalidResourceName))
		Expect(validCondition.Message).NotTo(BeEmpty())
	})
	It("should derive the Deployed condition from the bound BrokerService status", func() {

		ns := "default"
		svcName := "my-broker-service"
		appName := "my-app"

		svc := NewBrokerService(svcName, ns).Build()
		app := NewBrokerApp(appName, ns).Build()

		env := NewTestEnvironment(ns, svc, app)
		r := env.Reconciler
		cl := env.Client

		// 1. Reconcile with BrokerService status not having the app.
		// This first reconcile will annotate the app. The Deployed condition will be False.
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		// Verify Deployed condition is False
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionProvisioningPendingReason))

		// 2. Update BrokerService status to include the app
		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		appIdentity := AppIdentity(app)
		updatedSvc.Status.ProvisionedApps = []string{appIdentity}
		err = cl.Status().Update(context.TODO(), updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		// Reconcile again to pick up the status change
		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		// Verify Deployed condition is True
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		deployedCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionProvisionedReason))
	})

	It("should produce identical status on repeated reconciles", func() {
		// Setup scheme
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		// Data
		ns := "default"
		svcName := "my-broker-service"

		// Create namespace object (required for CEL evaluation)
		namespace := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		}
		appName := "my-app"

		// Create BrokerService (with Deployed=True)
		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: ns,
				Labels:    map[string]string{"type": "broker"},
			},
			Status: v1beta2.BrokerServiceStatus{
				Conditions: []metav1.Condition{
					{
						Type:   v1beta2.DeployedConditionType,
						Status: metav1.ConditionTrue,
						Reason: v1beta2.ReadyConditionReason,
					},
				},
			},
		}

		// Create BrokerApp matching the service
		app := &v1beta2.BrokerApp{
			ObjectMeta: metav1.ObjectMeta{
				Name:      appName,
				Namespace: ns,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		// Setup fake client for first reconcile
		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace, svc, app).WithStatusSubresource(app, svc)).Build()

		// Create Reconciler
		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// 1. First Reconcile to establish a status
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		// Get the updated app from the fake client
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

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

		cl2 := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(namespace, svc, updatedApp).
			WithStatusSubresource(updatedApp, svc).
			WithInterceptorFuncs(interceptorFuncs)).
			Build()
		r2 := NewBrokerAppReconciler(cl2, scheme, nil, logr.New(log.NullLogSink{}))
		_, err = r2.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		// Expect that status update was not called
		Expect(updateCalled).To(BeFalse(), "Status update should not be called on second reconcile if status is unchanged")
	})

	It("should set Valid=False when BrokerService has an invalid resource name", func() {
		ns := "default"
		invalidName := "invalid/name"

		app := NewBrokerApp(invalidName, ns).Build()

		env := NewTestEnvironment(ns, app)
		r := env.Reconciler
		cl := env.Client

		// Reconcile
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: invalidName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		// ValidationError results in no error returned (no retry until spec changes)
		Expect(err).NotTo(HaveOccurred())

		// Verify BrokerApp status
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		// Check Valid condition
		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionInvalidResourceName))
		Expect(validCondition.Message).NotTo(BeEmpty())
	})

	It("should set Valid=False when the serviceSelector contains invalid syntax", func() {
		ns := "default"
		appName := "my-app"

		app := NewBrokerApp(appName, ns).
			WithServiceSelector(&metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "type",
						Operator: "InvalidOperator",
						Values:   []string{"broker"},
					},
				},
			}).
			Build()

		env := NewTestEnvironment(ns, app)
		r := env.Reconciler
		cl := env.Client

		// Reconcile
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		// ValidationError results in no error returned (no retry until spec changes)
		Expect(err).NotTo(HaveOccurred())

		// Verify BrokerApp status
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		// Check Valid condition
		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionSpecSelectorError))
		Expect(validCondition.Message).To(ContainSubstring("Selector"))
	})
	It("should set Valid=False when the matched BrokerService no longer exists", func() {
		ns := "default"
		svcName := "my-broker-service"
		appName := "my-app"

		svc := NewBrokerService(svcName, ns).Build()
		app := NewBrokerApp(appName, ns).
			WithServiceBinding(svcName, ns, "binding-secret", 61616).
			Build()

		env := NewTestEnvironment(ns, svc, app)
		r := env.Reconciler
		cl := env.Client

		// First reconcile - should succeed
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		// Update the app's selector so the service no longer matches
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())
		updatedApp.Spec.ServiceSelector.MatchLabels["type"] = "different-type"
		err = cl.Update(context.TODO(), updatedApp)
		Expect(err).NotTo(HaveOccurred())

		// Reconcile again - service should not be found in new selector results
		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred()) // Should succeed but with condition update

		// Verify status
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		// Valid should still be True (selector syntax is valid)
		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(metav1.ConditionTrue))

		// Deployed should reflect that the matched service was not found
		deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(metav1.ConditionFalse))
		Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionMatchedServiceNotFoundReason))
	})
	It("should handle ConsumerOf references MULTICAST address", func() {
		ns := "default"
		svcName := "my-broker-service"

		svc := NewBrokerService(svcName, ns).
			WithLabels(map[string]string{"type": "messaging"}).
			Build()

		ownerApp := NewBrokerApp("owner-app", ns).
			WithServiceSelector(&metav1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
			WithSharedAddresses(NewAddressType("events").WithPubSub(true).Build()).
			WithConsumerOf(NewAddressRef("events").WithSubscriptions("sub1").Build()).
			WithServiceBinding(svcName, ns, "", 0).
			Build()

		consumerApp := NewBrokerApp("consumer-app", ns).
			WithServiceSelector(&metav1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
			WithConsumerOf(NewAddressRef("events").WithAppRef(ns, "owner-app").Build()).
			Build()

		env := NewTestEnvironment(ns, svc, ownerApp, consumerApp)
		r := env.Reconciler
		cl := env.Client

		// Reconcile consumer app - should requeue (addressRef conflict is transient)
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "consumer-app", Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred())

		// Check that Deployed condition is False (no matching service due to AddressRef conflict)
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(metav1.ConditionFalse))
		// The error message should mention the routing conflict
		Expect(deployedCondition.Message).To(ContainSubstring("events"))
		Expect(deployedCondition.Message).To(ContainSubstring("addressRef"))
		Expect(deployedCondition.Message).To(ContainSubstring("semantic"))
	})

	It("should handle Subscriptions references ANYCAST address", func() {
		ns := "default"
		svcName := "my-broker-service"

		svc := NewBrokerService(svcName, ns).
			WithLabels(map[string]string{"type": "messaging"}).
			Build()

		ownerApp := NewBrokerApp("owner-app-2", ns).
			WithServiceSelector(&metav1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
			WithSharedAddresses(NewAddressType("commands").Build()).
			WithConsumerOf(NewAddressRef("commands").Build()).
			WithServiceBinding(svcName, ns, "", 0).
			Build()

		subscriberApp := NewBrokerApp("subscriber-app", ns).
			WithServiceSelector(&metav1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
			WithConsumerOf(NewAddressRef("commands").
				WithAppRef(ns, "owner-app-2").
				WithSubscriptions("sub1").
				Build()).
			Build()

		env := NewTestEnvironment(ns, svc, ownerApp, subscriberApp)
		r := env.Reconciler
		cl := env.Client

		// Reconcile subscriber app - should requeue (addressRef conflict is transient)
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "subscriber-app", Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred())

		// Check that Deployed condition is False (no matching service due to AddressRef conflict)
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(metav1.ConditionFalse))
		// The error message should mention the routing conflict
		Expect(deployedCondition.Message).To(ContainSubstring("commands"))
		Expect(deployedCondition.Message).To(ContainSubstring("addressRef"))
		Expect(deployedCondition.Message).To(ContainSubstring("semantics"))
	})

	It("should handle Compatible MULTICAST sharing", func() {
		ns := "default"
		svcName := "my-broker-service"

		svc := NewBrokerService(svcName, ns).
			WithLabels(map[string]string{"type": "messaging"}).
			Build()

		ownerApp := NewBrokerApp("owner-app-3", ns).
			WithServiceSelector(&metav1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
			WithSharedAddresses(NewAddressType("topic").Build()).
			WithConsumerOf(NewAddressRef("topic").WithSubscriptions("sub1").Build()).
			WithServiceBinding(svcName, ns, "", 0).
			Build()

		subscriberApp := NewBrokerApp("subscriber-app-2", ns).
			WithServiceSelector(&metav1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
			WithConsumerOf(NewAddressRef("topic").
				WithAppRef(ns, "owner-app-3").
				WithSubscriptions("sub2").
				Build()).
			Build()

		env := NewTestEnvironment(ns, svc, ownerApp, subscriberApp)
		r := env.Reconciler
		cl := env.Client

		// Reconcile subscriber app - expected behavior:
		// ownerApp declares "topic" without pubSub but uses it with subscriptions (inconsistent)
		// subscriberApp references it with subscriptions
		// This is an addressRef semantic conflict detected at runtime
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "subscriber-app-2", Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred())

		// Check that Valid condition is True
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(metav1.ConditionTrue))
	})

	It("should handle Compatible ANYCAST sharing", func() {
		ns := "default"
		svcName := "my-broker-service"

		svc := NewBrokerService(svcName, ns).
			WithLabels(map[string]string{"type": "messaging"}).
			Build()

		ownerApp := NewBrokerApp("owner-app-4", ns).
			WithServiceSelector(&metav1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
			WithSharedAddresses(NewAddressType("queue").Build()).
			WithConsumerOf(NewAddressRef("queue").Build()).
			WithServiceBinding(svcName, ns, "", 0).
			Build()

		consumerApp := NewBrokerApp("consumer-app-2", ns).
			WithServiceSelector(&metav1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
			WithConsumerOf(NewAddressRef("queue").WithAppRef(ns, "owner-app-4").Build()).
			Build()

		env := NewTestEnvironment(ns, svc, ownerApp, consumerApp)
		r := env.Reconciler
		cl := env.Client

		// Reconcile consumer app - should succeed (both ANYCAST)
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "consumer-app-2", Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		// Check that Valid condition is True
		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(metav1.ConditionTrue))
	})
})
