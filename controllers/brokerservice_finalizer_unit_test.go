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

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func newServiceTestClient(scheme *runtime.Scheme, objects ...client.Object) client.Client {
	objects = WithCerts(objects...)
	var statusObjs []client.Object
	for _, obj := range objects {
		switch obj.(type) {
		case *v1beta2.BrokerApp, *v1beta2.BrokerService:
			statusObjs = append(statusObjs, obj)
		}
	}
	builder := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(statusObjs...)
	builder = SetupBrokerAppIndexer(builder)
	return builder.Build()
}

var _ = Describe("brokerservice finalizer unit", Label(unitLabel), func() {

	var (
		scheme *runtime.Scheme
		ns     string
	)

	BeforeEach(func() {
		scheme = NewServiceUnitTestScheme()
		ns = "default"

		common.SetOperatorNameSpace(ns)
		DeferCleanup(common.UnsetOperatorNameSpace)

		common.SetOperatorCASecretName("op_ca")
		DeferCleanup(common.UnsetOperatorCASecretName)
	})

	Context("service drain finalizer", func() {

		It("adds finalizer when service has bound apps with owned content", Label(unitLabel), func() {
			svcName := "my-service"
			appName := "my-app"

			svc := NewBrokerService(svcName, ns).Build()
			app := NewBrokerApp(appName, ns).
				WithAddresses(NewAddressType("orders").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "binding-secret", 61616).
				Build()

			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}

			cl := newServiceTestClient(scheme, namespace, oc, svc, app)
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed())
			Expect(updatedSvc.Finalizers).To(ContainElement(v1beta2.AddressesPendingEmptyFinalizer),
				"service should have drain finalizer when it has bound apps with owned content")
		})

		It("does add finalizer when service has no bound apps", Label(unitLabel), func() {
			svcName := "empty-service"

			svc := NewBrokerService(svcName, ns).Build()

			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}

			cl := newServiceTestClient(scheme, namespace, oc, svc)
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed())
			Expect(updatedSvc.Finalizers).To(ContainElement(v1beta2.AddressesPendingEmptyFinalizer),
				"service should have drain finalizer without bound apps")
		})

		It("retains service when deleted with bound apps and reports draining status", Label(unitLabel), func() {
			svcName := "deleting-service"
			appName := "bound-app"

			svc := NewBrokerService(svcName, ns).Build()
			controllerutil.AddFinalizer(svc, v1beta2.AddressesPendingEmptyFinalizer)

			app := NewBrokerApp(appName, ns).
				WithAddresses(NewAddressType("orders").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "binding-secret", 61616).
				Build()
			controllerutil.AddFinalizer(app, v1beta2.AddressesPendingEmptyFinalizer)

			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}

			cl := newServiceTestClient(scheme, namespace, oc, svc, app)
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			Expect(cl.Delete(context.TODO(), svc)).To(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed())
			Expect(updatedSvc.DeletionTimestamp).NotTo(BeNil(), "service should be marked for deletion")
			Expect(updatedSvc.Finalizers).To(ContainElement(v1beta2.AddressesPendingEmptyFinalizer),
				"service should retain drain finalizer while apps are still bound")

			readyCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ReadyConditionType)
			Expect(readyCond).NotTo(BeNil(), "Ready condition should be set")
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(v1beta2.ReadyConditionAddressesPendingEmpty))
			Expect(readyCond.Message).To(ContainSubstring("Deletion pending"))
		})

		It("removes finalizer when deleted service has no bound apps", Label(unitLabel), func() {
			svcName := "drained-service"

			svc := NewBrokerService(svcName, ns).Build()
			controllerutil.AddFinalizer(svc, v1beta2.AddressesPendingEmptyFinalizer)

			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}

			cl := newServiceTestClient(scheme, namespace, oc, svc)
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)

			Expect(cl.Delete(context.TODO(), svc)).To(Succeed())

			_, err = r.Reconcile(context.TODO(), req)

			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"service should be fully deleted after finalizer removal")
		})

		It("removes app drain finalizer from live app when drain completes", Label(unitLabel), func() {
			svcName := "active-service"
			appName := "drained-live-app"

			svc := NewBrokerService(svcName, ns).Build()
			controllerutil.AddFinalizer(svc, v1beta2.AddressesPendingEmptyFinalizer)

			app := NewBrokerApp(appName, ns).
				WithServiceBinding(svcName, ns, "binding-secret", 61616).
				Build()
			controllerutil.AddFinalizer(app, v1beta2.AddressesPendingEmptyFinalizer)

			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}

			cl := newServiceTestClient(scheme, namespace, oc, svc, app)
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: appName, Namespace: ns}, updatedApp)).To(Succeed())
			Expect(updatedApp.Finalizers).NotTo(ContainElement(v1beta2.AddressesPendingEmptyFinalizer),
				"live app should have drain finalizer removed when it no longer has owned content")
			Expect(updatedApp.DeletionTimestamp).To(BeNil(),
				"app should not be deleted")
		})

		It("removes app drain finalizer from unbinding app with owned content when drain completes", Label(unitLabel), func() {
			svcName := "active-service-unbind"
			appName := "unbinding-app"

			svc := NewBrokerService(svcName, ns).Build()
			controllerutil.AddFinalizer(svc, v1beta2.AddressesPendingEmptyFinalizer)

			app := NewBrokerApp(appName, ns).
				WithAddresses(NewAddressType("events").Build()).
				WithProducerOf(NewAddressRef("events").Build()).
				WithConsumerOf(NewAddressRef("events").Build()).
				WithServiceBinding(svcName, ns, "binding-secret", 61616).
				Build()
			controllerutil.AddFinalizer(app, v1beta2.AddressesPendingEmptyFinalizer)
			meta.SetStatusCondition(&app.Status.Conditions, metav1.Condition{
				Type:   v1beta2.DeployedConditionType,
				Status: metav1.ConditionFalse,
				Reason: v1beta2.DeployedConditionUnbindingReason,
			})

			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}

			cl := newServiceTestClient(scheme, namespace, oc, svc, app)
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: appName, Namespace: ns}, updatedApp)).To(Succeed())
			Expect(updatedApp.Finalizers).NotTo(ContainElement(v1beta2.AddressesPendingEmptyFinalizer),
				"unbinding app with owned content should have drain finalizer removed after drain completes")
			Expect(updatedApp.DeletionTimestamp).To(BeNil(),
				"app should not be deleted")
		})

		It("removes finalizer when deleted service apps not provisioned but unbinding", Label(unitLabel), func() {
			svcName := "draining-done-service"
			appName := "drained-app"

			svc := NewBrokerService(svcName, ns).Build()

			app := NewBrokerApp(appName, ns).
				WithAddresses(NewAddressType("orders").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "binding-secret", 61616).
				Build()

			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}

			cl := newServiceTestClient(scheme, namespace, oc, svc, app)
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			Expect(cl.Delete(context.TODO(), svc)).To(Succeed())

			currentApp := &v1beta2.BrokerApp{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: appName, Namespace: ns}, currentApp)).To(Succeed())
			currentApp.Status.Conditions = []metav1.Condition{metav1.Condition{Type: v1beta2.DeployedConditionType, Reason: v1beta2.DeployedConditionUnbindingReason}}
			Expect(cl.Status().Update(context.TODO(), currentApp)).To(Succeed())

			_, err = r.Reconcile(context.TODO(), req)

			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"service should be deleted when bound apps have no drain finalizer (drain complete)")
		})

		It("retains drain finalizer when app is marked unbinding during service deletion", Label(unitLabel), func() {
			svcName := "deleting-unbinding-race"
			appName := "unbinding-race-app"

			svc := NewBrokerService(svcName, ns).Build()
			app := NewBrokerApp(appName, ns).
				WithAddresses(NewAddressType("jobs").Build()).
				WithProducerOf(NewAddressRef("jobs").Build()).
				WithConsumerOf(NewAddressRef("jobs").Build()).
				WithServiceBinding(svcName, ns, "binding-secret", 61616).
				Build()

			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}

			objects := WithCerts(namespace, oc, svc, app)
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(svc, app, &v1beta2.Broker{}).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
					a := rawObj.(*v1beta2.BrokerApp)
					if a.Status.Service != nil {
						return []string{a.Status.Service.Key()}
					}
					return nil
				}).Build()
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

			By("initial reconcile establishes finalizers and secret data")
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed())
			Expect(updatedSvc.Finalizers).To(ContainElement(v1beta2.AddressesPendingEmptyFinalizer),
				"precondition: service has drain finalizer")

			currentApp := &v1beta2.BrokerApp{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: appName, Namespace: ns}, currentApp)).To(Succeed())
			Expect(currentApp.Finalizers).To(ContainElement(v1beta2.AddressesPendingEmptyFinalizer),
				"precondition: app has drain finalizer")

			By("simulating the broker applying the app config (reaching provisioned state)")
			secretName := AppPropertiesSecretName(svcName)
			appSecret := &corev1.Secret{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, appSecret)).To(Succeed())

			brokerCR := &v1beta2.Broker{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, brokerCR)).To(Succeed())
			brokerCR.Status.Conditions = []metav1.Condition{
				{Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "Ready"},
			}
			brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
				{Name: secretName, ResourceVersion: appSecret.ResourceVersion},
			}
			Expect(cl.Status().Update(context.TODO(), brokerCR)).To(Succeed())

			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed())
			Expect(updatedSvc.Status.ProvisionedApps).NotTo(BeEmpty(),
				"precondition: app should be provisioned")

			By("simulating what the app controller does when the bound service is being deleted")
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: appName, Namespace: ns}, currentApp)).To(Succeed())
			meta.SetStatusCondition(&currentApp.Status.Conditions, metav1.Condition{
				Type:   v1beta2.DeployedConditionType,
				Status: metav1.ConditionFalse,
				Reason: v1beta2.DeployedConditionUnbindingReason,
			})
			Expect(cl.Status().Update(context.TODO(), currentApp)).To(Succeed())

			By("deleting the service — sets DeletionTimestamp")
			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed())
			Expect(cl.Delete(context.TODO(), updatedSvc)).To(Succeed())

			By("first reconcile after deletion — app has finalizer so service is retained")
			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed(),
				"service should survive first reconcile — app drain finalizer holds it")
			Expect(updatedSvc.Finalizers).To(ContainElement(v1beta2.AddressesPendingEmptyFinalizer),
				"service should retain drain finalizer after first reconcile")

			By("second reconcile — must not lose drain state due to wiped secret data")
			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed(),
				"service should survive second reconcile — messages still exist in the broker")
			Expect(updatedSvc.Finalizers).To(ContainElement(v1beta2.AddressesPendingEmptyFinalizer),
				"service should retain drain finalizer — unbinding app still has pending messages")

			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: appName, Namespace: ns}, currentApp)).To(Succeed())
			Expect(currentApp.Finalizers).To(ContainElement(v1beta2.AddressesPendingEmptyFinalizer),
				"app should retain drain finalizer — messages were never confirmed drained")
		})

		It("deletes dependent resources when finalizer is removed on service deletion", Label(unitLabel), func() {
			svcName := "delete-deps-service"
			appName := "delete-deps-app"

			svc := NewBrokerService(svcName, ns).Build()
			app := NewBrokerApp(appName, ns).
				WithAddresses(NewAddressType("orders").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithConsumerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "binding-secret", 61616).
				Build()

			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("test")},
			}
			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}

			objects := WithCerts(namespace, oc, svc, app)
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				WithStatusSubresource(svc, app, &v1beta2.Broker{}).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
					a := rawObj.(*v1beta2.BrokerApp)
					if a.Status.Service != nil {
						return []string{a.Status.Service.Key()}
					}
					return nil
				}).Build()
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

			By("initial reconcile creates dependent resources")
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			appSecretName := AppPropertiesSecretName(svcName)
			appSecret := &corev1.Secret{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: appSecretName, Namespace: ns}, appSecret)).To(Succeed(),
				"app-properties secret should exist after first reconcile")

			brokerCR := &v1beta2.Broker{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, brokerCR)).To(Succeed(),
				"Broker CR should exist after first reconcile")

			k8sSvc := &corev1.Service{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, k8sSvc)).To(Succeed(),
				"k8s Service should exist after first reconcile")

			By("simulating app controller marking app as unbinding (as it would when bound service is deleted)")
			currentApp := &v1beta2.BrokerApp{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: appName, Namespace: ns}, currentApp)).To(Succeed())
			meta.SetStatusCondition(&currentApp.Status.Conditions, metav1.Condition{
				Type:   v1beta2.DeployedConditionType,
				Status: metav1.ConditionFalse,
				Reason: v1beta2.DeployedConditionUnbindingReason,
			})
			Expect(cl.Status().Update(context.TODO(), currentApp)).To(Succeed())

			By("deleting the service")
			updatedSvc := &v1beta2.BrokerService{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed())
			Expect(cl.Delete(context.TODO(), updatedSvc)).To(Succeed())

			By("reconcile after deletion removes finalizer and deletes all dependents")
			_, err = r.Reconcile(context.TODO(), req)
			Expect(errors.IsNotFound(err)).To(BeTrue(),
				"service should be fully deleted after finalizer removal")

			Expect(errors.IsNotFound(cl.Get(context.TODO(), types.NamespacedName{Name: appSecretName, Namespace: ns}, &corev1.Secret{}))).To(BeTrue(),
				"app-properties secret should be deleted when service is deleted")

			Expect(errors.IsNotFound(cl.Get(context.TODO(), req.NamespacedName, &v1beta2.Broker{}))).To(BeTrue(),
				"Broker CR should be deleted when service is deleted")

			Expect(errors.IsNotFound(cl.Get(context.TODO(), req.NamespacedName, &corev1.Service{}))).To(BeTrue(),
				"k8s Service should be deleted when service is deleted")
		})
	})
})
