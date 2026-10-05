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

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func newAppReconcilerWithClient(cl client.Client, name, namespace string) BrokerAppInstanceReconciler {
	instance := &v1beta2.BrokerApp{
		ObjectMeta: v1.ObjectMeta{Name: name, Namespace: namespace},
	}
	return BrokerAppInstanceReconciler{
		BrokerAppReconciler: &BrokerAppReconciler{
			ReconcilerLoop: &ReconcilerLoop{
				KubeBits: &KubeBits{Client: cl, log: logr.Discard()},
			},
		},
		instance: instance,
		status:   instance.Status.DeepCopy(),
	}
}

var _ = Describe("brokerapp controller unit", func() {

	It("simple reconcile", Label(unitLabel), func() {
		ns := "default"
		svcName := "my-broker-service"
		appName := "my-app"

		svc := NewBrokerService(svcName, ns).Build()
		app := NewBrokerApp(appName, ns).Build()

		env := NewTestEnvironment(ns, svc, app)
		r := env.Reconciler
		cl := env.Client

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedApp.Status.Service).NotTo(BeNil())
		Expect(updatedApp.Status.Service.Name).To(Equal(svcName))
		Expect(updatedApp.Status.Service.Namespace).To(Equal(ns))
		Expect(updatedApp.Status.Service.Secret).NotTo(BeEmpty())

		Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)).To(BeFalse())
		Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)).To(BeFalse())

		bindingSecret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: updatedApp.Status.Service.Secret, Namespace: ns}, bindingSecret)
		Expect(err).NotTo(HaveOccurred())

		Expect(string(bindingSecret.Data["host"])).To(Equal(fmt.Sprintf("%s.%s.svc.%s", svcName, ns, common.GetClusterDomain())))
		Expect(string(bindingSecret.Data["port"])).To(Equal(fmt.Sprintf("%d", updatedApp.Status.Service.AssignedPort)))
		Expect(string(bindingSecret.Data["uri"])).To(Equal(fmt.Sprintf("amqps://%s.%s.svc.%s:%d", svcName, ns, common.GetClusterDomain(), updatedApp.Status.Service.AssignedPort)))

		svc.Status.ProvisionedApps = []string{AppIdentityWithGeneration(app)}
		err = cl.Status().Update(context.TODO(), svc)
		Expect(err).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())
		Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)).To(BeTrue())
		Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)).To(BeTrue())
		Expect(updatedApp.Status.Service).NotTo(BeNil())
	})

	It("no matching service", Label(unitLabel), func() {
		ns := "default"
		appName := "my-app"

		app := NewBrokerApp(appName, ns).
			WithServiceSelector(&v1.LabelSelector{
				MatchLabels: map[string]string{"type": "non-existent"},
			}).
			Build()

		env := NewTestEnvironment(ns, app)
		r := env.Reconciler
		cl := env.Client

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(v1.ConditionTrue))
		Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionSuccessReason))

		deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))
	})

	It("valid condition transition", Label(unitLabel), func() {
		ns := "default"
		svcName := "my-broker-service"
		appName := "my-app"

		svc := NewBrokerService(svcName, ns).Build()
		app := NewBrokerApp(appName, ns).
			WithServiceSelector(&v1.LabelSelector{
				MatchLabels: map[string]string{"type": "non-existent"},
			}).
			Build()

		env := NewTestEnvironment(ns, svc, app)
		r := env.Reconciler
		cl := env.Client

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond).NotTo(BeNil())
		Expect(validCond.Status).To(Equal(v1.ConditionTrue))

		deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))

		time.Sleep(1 * time.Second)

		updatedApp.Spec.ServiceSelector.MatchLabels["type"] = "broker"
		err = cl.Update(context.TODO(), updatedApp)
		Expect(err).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond).NotTo(BeNil())
		Expect(validCond.Status).To(Equal(v1.ConditionTrue))

		deployedCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionProvisioningPendingReason))
	})

	It("status update failure", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		appName := "my-app"
		svcName := "my-broker-service"

		namespace := &corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: ns}}
		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{Name: svcName, Namespace: ns, Labels: map[string]string{"type": "broker"}},
			Status: v1beta2.BrokerServiceStatus{
				Conditions: []v1.Condition{
					{Type: v1beta2.DeployedConditionType, Status: v1.ConditionTrue, Reason: v1beta2.ReadyConditionReason},
				},
			},
		}
		app := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{Name: appName, Namespace: ns},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{MatchLabels: map[string]string{"type": "broker"}},
			},
		}

		interceptorFuncs := interceptor.Funcs{
			SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				return fmt.Errorf("simulated status update error")
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(namespace, svc, app)...).
			WithStatusSubresource(app).
			WithInterceptorFuncs(interceptorFuncs)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		result, err := r.Reconcile(context.TODO(), req)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("simulated status update error"))
		Expect(result.RequeueAfter).To(Equal(time.Duration(0)))
	})

	It("address type error", Label(unitLabel), func() {
		ns := "default"
		appName := "my-app"

		app := NewBrokerApp(appName, ns).
			WithConsumerOf(NewAddressRef("events::queue").WithSubscriptions("sub1").Build()).
			Build()

		env := NewTestEnvironment(ns, app)
		r := env.Reconciler
		cl := env.Client

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionAddressTypeError))
		Expect(validCondition.Message).To(ContainSubstring("FQQN"))
	})

	It("deployed condition from broker service status", Label(unitLabel), func() {
		ns := "default"
		svcName := "my-broker-service"
		appName := "my-app"

		svc := NewBrokerService(svcName, ns).Build()
		app := NewBrokerApp(appName, ns).Build()

		env := NewTestEnvironment(ns, svc, app)
		r := env.Reconciler
		cl := env.Client

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionProvisioningPendingReason))

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		appIdentity := AppIdentityWithGeneration(app)
		updatedSvc.Status.ProvisionedApps = []string{appIdentity}
		err = cl.Status().Update(context.TODO(), updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		deployedCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond).NotTo(BeNil())
		Expect(deployedCond.Status).To(Equal(v1.ConditionTrue))
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionProvisionedReason))
	})

	It("idempotent status", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-broker-service"
		appName := "my-app"

		namespace := &corev1.Namespace{ObjectMeta: v1.ObjectMeta{Name: ns}}
		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{Name: svcName, Namespace: ns, Labels: map[string]string{"type": "broker"}},
			Status: v1beta2.BrokerServiceStatus{
				Conditions: []v1.Condition{
					{Type: v1beta2.DeployedConditionType, Status: v1.ConditionTrue, Reason: v1beta2.ReadyConditionReason},
				},
			},
		}
		app := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{Name: appName, Namespace: ns},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{MatchLabels: map[string]string{"type": "broker"}},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().WithScheme(scheme).WithObjects(WithCerts(namespace, svc, app)...).WithStatusSubresource(app, svc)).Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

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
			WithObjects(WithCerts(namespace, svc, updatedApp)...).
			WithStatusSubresource(updatedApp, svc).
			WithInterceptorFuncs(interceptorFuncs)).
			Build()
		r2 := NewBrokerAppReconciler(cl2, scheme, nil, logr.New(log.NullLogSink{}))
		_, err = r2.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		Expect(updateCalled).To(BeFalse(), "Status update should not be called on second reconcile if status is unchanged")
	})

	It("invalid resource name", Label(unitLabel), func() {
		ns := "default"
		invalidName := "invalid/name"

		app := NewBrokerApp(invalidName, ns).Build()

		env := NewTestEnvironment(ns, app)
		r := env.Reconciler
		cl := env.Client

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: invalidName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionInvalidResourceName))
		Expect(validCondition.Message).NotTo(BeEmpty())
	})

	It("invalid selector syntax", Label(unitLabel), func() {
		ns := "default"
		appName := "my-app"

		app := NewBrokerApp(appName, ns).
			WithServiceSelector(&v1.LabelSelector{
				MatchExpressions: []v1.LabelSelectorRequirement{
					{Key: "type", Operator: "InvalidOperator", Values: []string{"broker"}},
				},
			}).
			Build()

		env := NewTestEnvironment(ns, app)
		r := env.Reconciler
		cl := env.Client

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionSpecSelectorError))
		Expect(validCondition.Message).To(ContainSubstring("Selector"))
	})

	It("invalid service selector", Label(unitLabel), func() {
		ns := "default"
		appName := "my-app"

		app := NewBrokerApp(appName, ns).
			WithServiceSelector(&v1.LabelSelector{
				MatchExpressions: []v1.LabelSelectorRequirement{},
			}).
			Build()

		env := NewTestEnvironment(ns, app)
		r := env.Reconciler
		cl := env.Client

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionServiceSelectorError))
		Expect(validCondition.Message).To(ContainSubstring("Selector"))
	})

	It("invalid service selector empty match label", Label(unitLabel), func() {
		ns := "default"
		appName := "my-app"

		app := NewBrokerApp(appName, ns).
			WithServiceSelector(&v1.LabelSelector{
				MatchLabels: map[string]string{},
			}).
			Build()

		env := NewTestEnvironment(ns, app)
		r := env.Reconciler
		cl := env.Client

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionServiceSelectorError))
		Expect(validCondition.Message).To(ContainSubstring("Selector"))
	})

	It("matched service not found", Label(unitLabel), func() {
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

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())
		updatedApp.Spec.ServiceSelector.MatchLabels["type"] = "different-type"
		err = cl.Update(context.TODO(), updatedApp)
		Expect(err).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(v1.ConditionTrue))

		deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionMatchedServiceNotFoundReason))
	})

	Context("routing type conflict validation", func() {
		It("ConsumerOf references MULTICAST address", Label(unitLabel), func() {
			ns := "default"
			svcName := "my-broker-service"

			svc := NewBrokerService(svcName, ns).
				WithLabels(map[string]string{"type": "messaging"}).
				Build()

			ownerApp := NewBrokerApp("owner-app", ns).
				WithServiceSelector(&v1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
				WithSharedAddresses(NewAddressType("events").WithPubSub(true).Build()).
				WithConsumerOf(NewAddressRef("events").WithSubscriptions("sub1").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			consumerApp := NewBrokerApp("consumer-app", ns).
				WithServiceSelector(&v1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
				WithConsumerOf(NewAddressRef("events").WithAppRef(ns, "owner-app").Build()).
				Build()

			env := NewTestEnvironment(ns, svc, ownerApp, consumerApp)
			r := env.Reconciler
			cl := env.Client

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "consumer-app", Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCondition).NotTo(BeNil())
			Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
			Expect(deployedCondition.Message).To(ContainSubstring("events"))
			Expect(deployedCondition.Message).To(ContainSubstring("addressRef"))
			Expect(deployedCondition.Message).To(ContainSubstring("semantic"))
		})

		It("Subscriptions references ANYCAST address", Label(unitLabel), func() {
			ns := "default"
			svcName := "my-broker-service"

			svc := NewBrokerService(svcName, ns).
				WithLabels(map[string]string{"type": "messaging"}).
				Build()

			ownerApp := NewBrokerApp("owner-app-2", ns).
				WithServiceSelector(&v1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
				WithSharedAddresses(NewAddressType("commands").Build()).
				WithConsumerOf(NewAddressRef("commands").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			subscriberApp := NewBrokerApp("subscriber-app", ns).
				WithServiceSelector(&v1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
				WithConsumerOf(NewAddressRef("commands").
					WithAppRef(ns, "owner-app-2").
					WithSubscriptions("sub1").
					Build()).
				Build()

			env := NewTestEnvironment(ns, svc, ownerApp, subscriberApp)
			r := env.Reconciler
			cl := env.Client

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "subscriber-app", Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCondition).NotTo(BeNil())
			Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
			Expect(deployedCondition.Message).To(ContainSubstring("commands"))
			Expect(deployedCondition.Message).To(ContainSubstring("addressRef"))
			Expect(deployedCondition.Message).To(ContainSubstring("semantics"))
		})

		It("Compatible MULTICAST sharing", Label(unitLabel), func() {
			ns := "default"
			svcName := "my-broker-service"

			svc := NewBrokerService(svcName, ns).
				WithLabels(map[string]string{"type": "messaging"}).
				Build()

			ownerApp := NewBrokerApp("owner-app-3", ns).
				WithServiceSelector(&v1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
				WithSharedAddresses(NewAddressType("topic").Build()).
				WithConsumerOf(NewAddressRef("topic").WithSubscriptions("sub1").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			subscriberApp := NewBrokerApp("subscriber-app-2", ns).
				WithServiceSelector(&v1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
				WithConsumerOf(NewAddressRef("topic").
					WithAppRef(ns, "owner-app-3").
					WithSubscriptions("sub2").
					Build()).
				Build()

			env := NewTestEnvironment(ns, svc, ownerApp, subscriberApp)
			r := env.Reconciler
			cl := env.Client

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "subscriber-app-2", Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCondition).NotTo(BeNil())
			Expect(validCondition.Status).To(Equal(v1.ConditionTrue))
		})

		It("Compatible ANYCAST sharing", Label(unitLabel), func() {
			ns := "default"
			svcName := "my-broker-service"

			svc := NewBrokerService(svcName, ns).
				WithLabels(map[string]string{"type": "messaging"}).
				Build()

			ownerApp := NewBrokerApp("owner-app-4", ns).
				WithServiceSelector(&v1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
				WithSharedAddresses(NewAddressType("queue").Build()).
				WithConsumerOf(NewAddressRef("queue").Build()).
				WithServiceBinding(svcName, ns, "", 0).
				Build()

			consumerApp := NewBrokerApp("consumer-app-2", ns).
				WithServiceSelector(&v1.LabelSelector{MatchLabels: map[string]string{"type": "messaging"}}).
				WithConsumerOf(NewAddressRef("queue").WithAppRef(ns, "owner-app-4").Build()).
				Build()

			env := NewTestEnvironment(ns, svc, ownerApp, consumerApp)
			r := env.Reconciler
			cl := env.Client

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "consumer-app-2", Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCondition).NotTo(BeNil())
			Expect(validCondition.Status).To(Equal(v1.ConditionTrue))
		})
	})

	It("verify app cert reports missing cert on the app", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		reconciler := newAppReconcilerWithClient(
			fake.NewClientBuilder().WithScheme(scheme).Build(), "app-alpha", "ns1")

		err := reconciler.verifyAppCert()

		transErr, ok := err.(*TransientError)
		Expect(ok).To(BeTrue(), "must be transient so the app recovers when the cert appears")
		Expect(transErr.ConditionReason()).To(Equal(v1beta2.DeployedConditionMissingAppCertReason))
		Expect(transErr.Error()).To(ContainSubstring("app-alpha" + common.AppCertSecretSuffix))
	})

	It("verify app cert rejects unreadable cert", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app-alpha" + common.AppCertSecretSuffix,
				Namespace: "ns1",
			},
			Data: map[string][]byte{"tls.crt": []byte("not a certificate")},
		}).Build()

		err := newAppReconcilerWithClient(cl, "app-alpha", "ns1").verifyAppCert()

		transErr, ok := err.(*TransientError)
		Expect(ok).To(BeTrue())
		Expect(transErr.ConditionReason()).To(Equal(v1beta2.DeployedConditionMissingAppCertReason))
	})

	It("verify app cert accepts a valid cert", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		certPEM, keyPEM := mustGenerateKeyPairCN("app-alpha")
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(WithCerts(&corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app-alpha" + common.AppCertSecretSuffix,
				Namespace: "ns1",
			},
			Data: map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
		})...).Build()

		Expect(newAppReconcilerWithClient(cl, "app-alpha", "ns1").verifyAppCert()).NotTo(HaveOccurred())
	})
})
