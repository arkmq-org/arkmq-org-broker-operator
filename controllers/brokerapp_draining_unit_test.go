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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

var _ = Describe("brokerapp unbinding unit", func() {

	Context("unbinding flow via deployed condition", func() {

		It("sets Deployed=Unbinding when selector no longer matches bound service", Label(unitLabel), func() {
			ns := "default"
			svcName := "my-broker-service"
			appName := "my-app"

			svc := NewBrokerService(svcName, ns).Build()
			app := NewBrokerApp(appName, ns).
				WithAddresses(NewAddressType("orders").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				Build()

			env := NewTestEnvironment(ns, svc, app)
			r := env.Reconciler
			cl := env.Client

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}

			// First reconcile: app gets bound to service
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
			Expect(updatedApp.Status.Service).NotTo(BeNil(), "app should be bound after first reconcile")

			// Simulate drain finalizer (added by service controller) and change selector
			controllerutil.AddFinalizer(updatedApp, v1beta2.AddressesPendingEmptyFinalizer)
			updatedApp.Spec.ServiceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"no-match": "true"},
			}
			Expect(cl.Update(context.TODO(), updatedApp)).To(Succeed())

			// Reconcile with mismatched selector — requeues while unbinding
			result, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero(), "should requeue while unbinding")

			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionUnbindingReason))

			Expect(updatedApp.Status.Service).NotTo(BeNil(), "binding should be preserved while drain finalizer exists")
		})

		It("clears binding after drain finalizer is removed", Label(unitLabel), func() {
			ns := "default"
			svcName := "my-broker-service"
			appName := "my-app"

			svc := NewBrokerService(svcName, ns).Build()
			app := NewBrokerApp(appName, ns).
				WithAddresses(NewAddressType("orders").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				Build()

			env := NewTestEnvironment(ns, svc, app)
			r := env.Reconciler
			cl := env.Client

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}

			// First reconcile: app gets bound
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// Add drain finalizer and change selector
			updatedApp := &v1beta2.BrokerApp{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
			controllerutil.AddFinalizer(updatedApp, v1beta2.AddressesPendingEmptyFinalizer)
			updatedApp.Spec.ServiceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"no-match": "true"},
			}
			Expect(cl.Update(context.TODO(), updatedApp)).To(Succeed())

			// Reconcile: enters Unbinding
			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// Simulate service controller removing the drain finalizer
			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
			controllerutil.RemoveFinalizer(updatedApp, v1beta2.AddressesPendingEmptyFinalizer)
			Expect(cl.Update(context.TODO(), updatedApp)).To(Succeed())

			// Reconcile: no drain finalizer, binding cleared, but no matching service
			// so we get a TransientError. Status still gets updated.
			_, _ = r.Reconcile(context.TODO(), req)

			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
			Expect(updatedApp.Status.Service).To(BeNil(), "binding should be cleared after drain finalizer removed")
		})

		It("clears binding immediately for app without owned content", Label(unitLabel), func() {
			ns := "default"
			svcName := "my-broker-service"
			appName := "no-content-app"

			svc := NewBrokerService(svcName, ns).Build()
			app := NewBrokerApp(appName, ns).Build()

			env := NewTestEnvironment(ns, svc, app)
			r := env.Reconciler
			cl := env.Client

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}

			// First reconcile: app gets bound
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
			Expect(updatedApp.Status.Service).NotTo(BeNil(), "app should be bound after first reconcile")

			// Change selector
			updatedApp.Spec.ServiceSelector = &metav1.LabelSelector{
				MatchLabels: map[string]string{"no-match": "true"},
			}
			Expect(cl.Update(context.TODO(), updatedApp)).To(Succeed())

			// Reconcile: binding cleared immediately, TransientError from no matching service
			_, _ = r.Reconcile(context.TODO(), req)

			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
			Expect(updatedApp.Status.Service).To(BeNil(), "binding should be cleared immediately for app without owned content")
		})
	})

	Context("service deletion triggers unbinding", func() {

		It("enters unbinding when bound service has DeletionTimestamp and drain finalizer present", Label(unitLabel), func() {
			ns := "default"
			svcName := "deleting-service"
			appName := "bound-app"

			svc := NewBrokerService(svcName, ns).Build()
			app := NewBrokerApp(appName, ns).
				WithAddresses(NewAddressType("orders").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				Build()

			env := NewTestEnvironment(ns, svc, app)
			r := env.Reconciler
			cl := env.Client

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}

			// First reconcile: app gets bound to service
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
			Expect(updatedApp.Status.Service).NotTo(BeNil(), "app should be bound after first reconcile")

			// Simulate drain finalizer added by service controller
			controllerutil.AddFinalizer(updatedApp, v1beta2.AddressesPendingEmptyFinalizer)
			Expect(cl.Update(context.TODO(), updatedApp)).To(Succeed())

			updatedSvc := &v1beta2.BrokerService{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, updatedSvc)).To(Succeed())
			updatedSvc.Status.ProvisionedApps = []string{AppIdentityWithGeneration(updatedApp)}
			err = cl.Status().Update(context.TODO(), updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			// Delete the service
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, updatedSvc)).To(Succeed())
			controllerutil.AddFinalizer(updatedSvc, v1beta2.AddressesPendingEmptyFinalizer)
			Expect(cl.Update(context.TODO(), updatedSvc)).To(Succeed())
			Expect(cl.Delete(context.TODO(), updatedSvc)).To(Succeed())

			// Reconcile: should enter unbinding, requeue
			result, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).NotTo(BeZero(), "should requeue while unbinding")

			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
			Expect(updatedApp.Status.Service).NotTo(BeNil(), "binding should be preserved while drain finalizer exists")

			deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionUnbindingReason))

			readyCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(v1beta2.ReadyConditionUnbindingReason))
			Expect(readyCond.Message).To(ContainSubstring("deleted"))
		})

		It("clears binding immediately when bound service is deleting and no drain finalizer", Label(unitLabel), func() {
			ns := "default"
			svcName := "deleting-service"
			appName := "no-drain-app"

			svc := NewBrokerService(svcName, ns).Build()
			app := NewBrokerApp(appName, ns).
				WithAddresses(NewAddressType("orders").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				Build()

			env := NewTestEnvironment(ns, svc, app)
			r := env.Reconciler
			cl := env.Client

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}

			// First reconcile: app gets bound to service
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
			Expect(updatedApp.Status.Service).NotTo(BeNil(), "app should be bound after first reconcile")

			// Delete the service (with a finalizer so it stays around)
			updatedSvc := &v1beta2.BrokerService{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, updatedSvc)).To(Succeed())
			controllerutil.AddFinalizer(updatedSvc, v1beta2.AddressesPendingEmptyFinalizer)
			Expect(cl.Update(context.TODO(), updatedSvc)).To(Succeed())
			Expect(cl.Delete(context.TODO(), updatedSvc)).To(Succeed())

			// Reconcile: no drain finalizer, binding should be cleared
			// Will get TransientError since no matching non-deleting services
			_, _ = r.Reconcile(context.TODO(), req)

			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
			Expect(updatedApp.Status.Service).To(BeNil(), "binding should be cleared when service is deleting and no drain finalizer")
		})
	})

	Context("deletion with drain finalizer reflects service state", func() {

		It("deployed condition shows drain progress during deletion", Label(unitLabel), func() {
			ns := "default"
			svcName := "my-broker-service"
			appName := "draining-app"

			app := NewBrokerApp(appName, ns).
				WithAddresses(NewAddressType("orders").Build()).
				WithProducerOf(NewAddressRef("orders").Build()).
				WithServiceBinding(svcName, ns, "binding-secret", 61616).
				Build()
			controllerutil.AddFinalizer(app, v1beta2.AddressesPendingEmptyFinalizer)

			svc := NewBrokerService(svcName, ns).
				WithPendingEmpty(v1beta2.PendingEmptyAddressStatus{
					App:      AppIdentity(app),
					Messages: map[string]int64{"orders": 10},
				}).
				Build()
			brokerCR := &v1beta2.Broker{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			}

			env := NewTestEnvironment(ns, svc, brokerCR, app)
			r := env.Reconciler
			cl := env.Client

			// Delete the app
			updatedApp := &v1beta2.BrokerApp{}
			Expect(cl.Get(context.TODO(), types.NamespacedName{Name: appName, Namespace: ns}, updatedApp)).To(Succeed())
			Expect(cl.Delete(context.TODO(), updatedApp)).To(Succeed())

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

			readyCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(readyCond.Reason).To(Equal(v1beta2.ReadyConditionUnbindingReason))

			deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionUnbindingReason))
			Expect(deployedCond.Message).To(ContainSubstring("orders"))
		})
	})
})
