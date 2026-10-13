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

		It("does not add finalizer when service has no bound apps", Label(unitLabel), func() {
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
			Expect(updatedSvc.Finalizers).NotTo(ContainElement(v1beta2.AddressesPendingEmptyFinalizer),
				"service should not have drain finalizer without bound apps")
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

			Expect(cl.Delete(context.TODO(), svc)).To(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			Expect(errors.IsNotFound(cl.Get(context.TODO(), req.NamespacedName, &v1beta2.BrokerService{}))).To(BeTrue(),
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

		It("removes finalizer when deleted service apps have completed drain", Label(unitLabel), func() {
			svcName := "draining-done-service"
			appName := "drained-app"

			svc := NewBrokerService(svcName, ns).Build()
			controllerutil.AddFinalizer(svc, v1beta2.AddressesPendingEmptyFinalizer)

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

			Expect(cl.Delete(context.TODO(), svc)).To(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			Expect(errors.IsNotFound(cl.Get(context.TODO(), req.NamespacedName, &v1beta2.BrokerService{}))).To(BeTrue(),
				"service should be deleted when bound apps have no drain finalizer (drain complete)")
		})
	})
})
