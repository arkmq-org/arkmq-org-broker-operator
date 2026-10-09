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
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func ptrTo[T any](v T) *T {
	return &v
}

func intstrFromInt(i int) intstr.IntOrString {
	return intstr.FromInt(i)
}

var _ = Describe("brokerservice pod labels", func() {

	It("applies standard kubernetes labels", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-broker"

		nsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		}

		service := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: ns,
			},
			Spec: v1beta2.BrokerServiceSpec{},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(service, nsObj)...).
			WithStatusSubresource(service).
			WithIndex(&v1beta2.BrokerApp{}, "status.serviceBinding", func(obj client.Object) []string {
				app := obj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}), nil)

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		broker := &v1beta2.Broker{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, broker)
		Expect(err).NotTo(HaveOccurred())

		labels := broker.Spec.Labels
		Expect(labels).NotTo(HaveKey(selectors.LabelAppKubernetesInstance))
		Expect(labels).NotTo(HaveKey(selectors.LabelAppKubernetesName))
		Expect(labels[selectors.LabelAppKubernetesComponent]).To(Equal("broker-service"))
		Expect(labels[selectors.LabelAppKubernetesManagedBy]).To(Equal(common.OperatorName))
		Expect(labels[selectors.LabelBrokerService]).To(Equal(svcName))
		Expect(labels[selectors.LabelBrokerPeerIndex]).To(Equal("0"))
	})

	It("network policy matches broker service label", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-broker"

		nsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		}

		netpol := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "broker-netpol",
				Namespace: ns,
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						selectors.LabelBrokerService: svcName,
					},
				},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{
						Ports: []networkingv1.NetworkPolicyPort{
							{
								Protocol: ptrTo(corev1.ProtocolTCP),
								Port:     ptrTo(intstrFromInt(61616)),
							},
							{
								Protocol: ptrTo(corev1.ProtocolTCP),
								Port:     ptrTo(intstrFromInt(61617)),
							},
							{
								Protocol: ptrTo(corev1.ProtocolTCP),
								Port:     ptrTo(intstrFromInt(61618)),
							},
						},
					},
				},
			},
		}

		service := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: ns,
			},
			Spec: v1beta2.BrokerServiceSpec{},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(service, netpol, nsObj)...).
			WithStatusSubresource(service, &v1beta2.Broker{}).
			WithIndex(&v1beta2.BrokerApp{}, "status.serviceBinding", func(obj client.Object) []string {
				app := obj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}), nil)

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedService := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedService)
		Expect(err).NotTo(HaveOccurred())

		Expect(meta.IsStatusConditionTrue(updatedService.Status.Conditions, v1beta2.ValidConditionType)).To(BeTrue())

		ss := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName + "-ss",
				Namespace: ns,
			},
			Spec: appsv1.StatefulSetSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							selectors.LabelAppKubernetesInstance: svcName,
							selectors.LabelBrokerService:         svcName,
						},
					},
				},
			},
		}
		Expect(cl.Create(context.TODO(), ss)).NotTo(HaveOccurred())

		broker := &v1beta2.Broker{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, broker)
		Expect(err).NotTo(HaveOccurred())
		meta.SetStatusCondition(&broker.Status.Conditions, metav1.Condition{
			Type:   v1beta2.DeployedConditionType,
			Status: metav1.ConditionTrue,
		})
		err = cl.Status().Update(context.TODO(), broker)
		Expect(err).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedService)
		Expect(err).NotTo(HaveOccurred())
	})

	It("network policy matches component label", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)
		_ = appsv1.AddToScheme(scheme)

		ns := "default"
		svcName := "my-broker"

		nsObj := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: ns,
			},
		}

		netpol := &networkingv1.NetworkPolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "all-brokers-policy",
				Namespace: ns,
			},
			Spec: networkingv1.NetworkPolicySpec{
				PodSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						selectors.LabelAppKubernetesComponent: "broker-service",
					},
				},
				Ingress: []networkingv1.NetworkPolicyIngressRule{
					{
						Ports: []networkingv1.NetworkPolicyPort{
							{
								Protocol: ptrTo(corev1.ProtocolTCP),
								Port:     ptrTo(intstrFromInt(61616)),
								EndPort:  ptrTo(int32(61620)),
							},
						},
					},
				},
			},
		}

		service := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: ns,
			},
			Spec: v1beta2.BrokerServiceSpec{},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(service, netpol, nsObj)...).
			WithStatusSubresource(service, &v1beta2.Broker{}).
			WithIndex(&v1beta2.BrokerApp{}, "status.serviceBinding", func(obj client.Object) []string {
				app := obj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}), nil)

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedService := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedService)
		Expect(err).NotTo(HaveOccurred())

		Expect(meta.IsStatusConditionTrue(updatedService.Status.Conditions, v1beta2.ValidConditionType)).To(BeTrue())

		ss := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName + "-ss",
				Namespace: ns,
			},
			Spec: appsv1.StatefulSetSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							selectors.LabelAppKubernetesInstance:  svcName,
							selectors.LabelAppKubernetesComponent: "broker-service",
							selectors.LabelBrokerService:          svcName,
						},
					},
				},
			},
		}
		Expect(cl.Create(context.TODO(), ss)).NotTo(HaveOccurred())

		broker := &v1beta2.Broker{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, broker)
		Expect(err).NotTo(HaveOccurred())
		meta.SetStatusCondition(&broker.Status.Conditions, metav1.Condition{
			Type:   v1beta2.DeployedConditionType,
			Status: metav1.ConditionTrue,
		})
		err = cl.Status().Update(context.TODO(), broker)
		Expect(err).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedService)
		Expect(err).NotTo(HaveOccurred())
	})
})
