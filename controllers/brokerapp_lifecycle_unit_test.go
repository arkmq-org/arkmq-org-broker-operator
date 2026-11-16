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

var _ = Describe("brokerapp lifecycle", func() {

	It("walks through the full condition lifecycle", Label(unitLabel), func() {
		ns := "default"
		svcName := "lifecycle-svc"
		appName := "lifecycle-app"

		svc := NewBrokerService(svcName, ns).Build()
		app := NewBrokerApp(appName, ns).
			WithAddresses(NewAddressType("orders").Build()).
			WithProducerOf(NewAddressRef("orders").Build()).
			WithServiceSelector(&metav1.LabelSelector{
				MatchLabels: map[string]string{"type": "non-existent"},
			}).
			Build()

		env := NewTestEnvironment(ns, svc, app)
		r := env.Reconciler
		cl := env.Client
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}

		// ── State 1: No matching service ─────────────────────────────
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred(), "TransientError for no matching service")

		updatedApp := &v1beta2.BrokerApp{}
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

		validCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond.Status).To(Equal(metav1.ConditionTrue))

		deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))

		Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)).To(BeFalse())

		// ── State 2: Service appears — binding ───────────────────────
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
		updatedApp.Spec.ServiceSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"type": "broker"},
		}
		Expect(cl.Update(context.TODO(), updatedApp)).To(Succeed())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
		Expect(updatedApp.Status.Service).NotTo(BeNil())

		deployedCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionBindingReason))

		Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)).To(BeFalse())

		// ── State 3: Broker applies config — deployed ────────────────
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
		svc.Status.ProvisionedApps = []string{AppIdentityWithGeneration(updatedApp)}
		Expect(cl.Status().Update(context.TODO(), svc)).To(Succeed())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

		deployedCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionBoundReason))

		Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)).To(BeTrue())
		Expect(updatedApp.Status.BoundSpec).NotTo(BeNil())

		deployedGen := deployedCond.ObservedGeneration

		// ── State 4: Bad spec — validation failure ───────────────────
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
		updatedApp.Spec.ServiceSelector = &metav1.LabelSelector{}
		updatedApp.Generation = deployedGen + 1
		Expect(cl.Update(context.TODO(), updatedApp)).To(Succeed())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred(), "ValidationError is absorbed")

		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

		validCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(validCond.ObservedGeneration).To(Equal(deployedGen + 1))
		Expect(validCond.Message).To(ContainSubstring("reconcile blocked"))

		deployedCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue), "Deployed preserved")
		Expect(deployedCond.ObservedGeneration).To(Equal(deployedGen), "Deployed stays at old generation")

		Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)).To(BeTrue(),
			"Ready=True: app is still running (Kubernetes convention)")

		Expect(updatedApp.Status.BoundSpec).NotTo(BeNil())
		Expect(updatedApp.Status.BoundSpec.Generation).To(Equal(deployedGen), "BoundSpec preserved at old generation")

		// ── State 5: Spec fixed — recovery ───────────────────────────
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
		updatedApp.Spec.ServiceSelector = &metav1.LabelSelector{
			MatchLabels: map[string]string{"type": "broker"},
		}
		updatedApp.Generation = deployedGen + 2
		Expect(cl.Update(context.TODO(), updatedApp)).To(Succeed())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())

		validCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCond.Status).To(Equal(metav1.ConditionTrue))
		Expect(validCond.ObservedGeneration).To(Equal(deployedGen + 2))

		deployedCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue), "Deployed still True from old generation")

		Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)).To(BeTrue())

		Expect(updatedApp.Status.BoundSpec).NotTo(BeNil())
		Expect(updatedApp.Status.BoundSpec.Generation).To(Equal(deployedGen+2), "BoundSpec updated to fixed generation")

		// Simulate broker catching up with the fixed generation
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
		svc.Status.ProvisionedApps = []string{AppIdentityWithGeneration(updatedApp)}
		Expect(cl.Status().Update(context.TODO(), svc)).To(Succeed())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
		deployedCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond.ObservedGeneration).To(Equal(deployedGen+2), "Deployed advances to fixed generation")

		// ── State 6: Service deleted — unbinding with drain ──────────
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
		controllerutil.AddFinalizer(updatedApp, v1beta2.AddressesPendingEmptyFinalizer)
		Expect(cl.Update(context.TODO(), updatedApp)).To(Succeed())

		updatedSvc := &v1beta2.BrokerService{}
		Expect(cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, updatedSvc)).To(Succeed())
		controllerutil.AddFinalizer(updatedSvc, v1beta2.AddressesPendingEmptyFinalizer)
		Expect(cl.Update(context.TODO(), updatedSvc)).To(Succeed())
		Expect(cl.Delete(context.TODO(), updatedSvc)).To(Succeed())

		result, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.RequeueAfter).NotTo(BeZero(), "requeues while drain finalizer holds binding")

		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
		Expect(updatedApp.Status.Service).NotTo(BeNil(), "binding held by finalizer")

		deployedCond = meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionUnbindingReason))

		readyCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)
		Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
		Expect(readyCond.Reason).To(Equal(v1beta2.ReadyConditionUnbindingReason))
		Expect(readyCond.Message).To(ContainSubstring("deleted"))

		// ── State 7: Drain complete — binding cleared ────────────────
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
		controllerutil.RemoveFinalizer(updatedApp, v1beta2.AddressesPendingEmptyFinalizer)
		Expect(cl.Update(context.TODO(), updatedApp)).To(Succeed())

		_, _ = r.Reconcile(context.TODO(), req)

		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedApp)).To(Succeed())
		Expect(updatedApp.Status.Service).To(BeNil(), "binding cleared after drain")
		Expect(updatedApp.Status.BoundSpec).To(BeNil(), "boundSpec cleared after drain")

		Expect(meta.IsStatusConditionTrue(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)).To(BeFalse())
	})
})
