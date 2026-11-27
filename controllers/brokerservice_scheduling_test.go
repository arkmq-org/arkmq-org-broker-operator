/*
Copyright 2026.

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
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("brokerservice pod scheduling", func() {

	const ns = "default"

	newReconciler := func(svc *v1beta2.BrokerService) (client.Client, *BrokerServiceReconciler, ctrl.Request) {
		scheme := NewServiceUnitTestScheme()
		nsObj := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, nsObj)...).
			WithStatusSubresource(svc).
			WithIndex(&v1beta2.BrokerApp{}, "status.serviceBinding", func(obj client.Object) []string {
				app := obj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svc.Name, Namespace: ns}}
		return cl, r, req
	}

	getBroker := func(cl client.Client, name string) *v1beta2.Broker {
		b := &v1beta2.Broker{}
		ExpectWithOffset(1, cl.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: ns}, b)).To(Succeed())
		return b
	}

	It("passes nodeSelector through to the owned broker and clears it", Label(unitLabel), func() {
		svcName := "sched-ns"

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Spec: v1beta2.BrokerServiceSpec{
				NodeSelector: map[string]string{"disktype": "ssd", "region": "us-east-1"},
			},
		}

		cl, r, req := newReconciler(svc)
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		broker := getBroker(cl, svcName)
		Expect(broker.Spec.NodeSelector).To(Equal(map[string]string{"disktype": "ssd", "region": "us-east-1"}))

		// Clear and re-reconcile
		updatedSvc := &v1beta2.BrokerService{}
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed())
		updatedSvc.Spec.NodeSelector = nil
		Expect(cl.Update(context.TODO(), updatedSvc)).To(Succeed())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		broker = getBroker(cl, svcName)
		Expect(broker.Spec.NodeSelector).To(BeEmpty())
	})

	It("passes affinity through to the owned broker and clears it", Label(unitLabel), func() {
		svcName := "sched-aff"

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Spec: v1beta2.BrokerServiceSpec{
				Affinity: &corev1.Affinity{
					PodAntiAffinity: &corev1.PodAntiAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchLabels: map[string]string{"app": "broker"},
								},
								TopologyKey: "kubernetes.io/hostname",
							},
						},
					},
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "zone",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"us-east-1a"},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		cl, r, req := newReconciler(svc)
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		broker := getBroker(cl, svcName)
		Expect(broker.Spec.Affinity.PodAntiAffinity).NotTo(BeNil())
		Expect(broker.Spec.Affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].
			LabelSelector.MatchLabels["app"]).To(Equal("broker"))
		Expect(broker.Spec.Affinity.NodeAffinity).NotTo(BeNil())
		Expect(broker.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.
			NodeSelectorTerms[0].MatchExpressions[0].Key).To(Equal("zone"))

		// Clear and re-reconcile
		updatedSvc := &v1beta2.BrokerService{}
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed())
		updatedSvc.Spec.Affinity = nil
		Expect(cl.Update(context.TODO(), updatedSvc)).To(Succeed())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		broker = getBroker(cl, svcName)
		Expect(broker.Spec.Affinity.NodeAffinity).To(BeNil())
		Expect(broker.Spec.Affinity.PodAffinity).To(BeNil())
		Expect(broker.Spec.Affinity.PodAntiAffinity).To(BeNil())
	})

	It("passes tolerations through to the owned broker and clears them", Label(unitLabel), func() {
		svcName := "sched-tol"

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Spec: v1beta2.BrokerServiceSpec{
				Tolerations: []corev1.Toleration{
					{
						Key:      "dedicated",
						Operator: corev1.TolerationOpEqual,
						Value:    "broker",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		}

		cl, r, req := newReconciler(svc)
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		broker := getBroker(cl, svcName)
		Expect(broker.Spec.Tolerations).To(HaveLen(1))
		Expect(broker.Spec.Tolerations[0].Key).To(Equal("dedicated"))
		Expect(broker.Spec.Tolerations[0].Value).To(Equal("broker"))
		Expect(broker.Spec.Tolerations[0].Effect).To(Equal(corev1.TaintEffectNoSchedule))

		// Clear and re-reconcile
		updatedSvc := &v1beta2.BrokerService{}
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed())
		updatedSvc.Spec.Tolerations = nil
		Expect(cl.Update(context.TODO(), updatedSvc)).To(Succeed())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		broker = getBroker(cl, svcName)
		Expect(broker.Spec.Tolerations).To(BeEmpty())
	})

	It("passes topologySpreadConstraints through to the owned broker and clears them", Label(unitLabel), func() {
		svcName := "sched-tsc"

		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
			Spec: v1beta2.BrokerServiceSpec{
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					{
						MaxSkew:           1,
						TopologyKey:       "topology.kubernetes.io/zone",
						WhenUnsatisfiable: corev1.DoNotSchedule,
						LabelSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "broker"},
						},
					},
				},
			},
		}

		cl, r, req := newReconciler(svc)
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		broker := getBroker(cl, svcName)
		Expect(broker.Spec.TopologySpreadConstraints).To(HaveLen(1))
		Expect(broker.Spec.TopologySpreadConstraints[0].MaxSkew).To(Equal(int32(1)))
		Expect(broker.Spec.TopologySpreadConstraints[0].TopologyKey).To(Equal("topology.kubernetes.io/zone"))
		Expect(broker.Spec.TopologySpreadConstraints[0].WhenUnsatisfiable).To(Equal(corev1.DoNotSchedule))

		// Clear and re-reconcile
		updatedSvc := &v1beta2.BrokerService{}
		Expect(cl.Get(context.TODO(), req.NamespacedName, updatedSvc)).To(Succeed())
		updatedSvc.Spec.TopologySpreadConstraints = nil
		Expect(cl.Update(context.TODO(), updatedSvc)).To(Succeed())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		broker = getBroker(cl, svcName)
		Expect(broker.Spec.TopologySpreadConstraints).To(BeEmpty())
	})

})
