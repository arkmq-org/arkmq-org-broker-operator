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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("BrokerService and BrokerApp Deployed gating", func() {
	Context("BrokerService Deployed condition", func() {
		It("should set Deployed=False when the broker pod is not yet ready", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-broker-service"

			// BrokerService with initial state
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svcName,
					Namespace: ns,
				},
				Status: v1beta2.BrokerServiceStatus{},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc).
				WithStatusSubresource(svc, &v1beta2.BrokerCluster{})).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

			// 1. First reconcile - creates Broker CR but it won't be deployed yet
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// Verify BrokerService.Deployed is False because Broker isn't deployed
			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionNotReadyReason))
		})
		It("should set Deployed=True after port discovery completes", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-broker-service"

			// BrokerService with initial state
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svcName,
					Namespace: ns,
				},
				Status: v1beta2.BrokerServiceStatus{},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc).
				WithStatusSubresource(svc, &v1beta2.BrokerCluster{})).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

			// 1. First reconcile - creates Broker CR
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// 2. Update Broker to Deployed=True
			brokerCR := &v1beta2.BrokerCluster{}
			err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
			Expect(err).NotTo(HaveOccurred())

			meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
				Type:   v1beta2.DeployedConditionType,
				Status: metav1.ConditionTrue,
				Reason: v1beta2.ReadyConditionReason,
			})
			err = cl.Status().Update(context.TODO(), brokerCR)
			Expect(err).NotTo(HaveOccurred())

			// 3. Create StatefulSet to trigger port discovery
			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svcName + "-ss",
					Namespace: ns,
				},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								common.LabelAppKubernetesInstance: svcName,
								common.LabelBrokerService:         svcName,
							},
						},
					},
				},
			}
			err = cl.Create(context.TODO(), ss)
			Expect(err).NotTo(HaveOccurred())

			// 4. Reconcile again - should discover ports and set Deployed=True
			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			// Verify BrokerService.Deployed is True after Broker is deployed
			deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(deployedCond.Reason).To(Equal(v1beta2.ReadyConditionReason))
		})
	})
	Context("BrokerApp gating on BrokerService Deployed status", func() {
		It("should block a BrokerApp from binding to a non-deployed BrokerService", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-broker-service"
			appName := "test-app"

			nsObj := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ns,
				},
			}

			// BrokerService without Deployed=True condition
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
							Status: metav1.ConditionFalse,
							Reason: v1beta2.DeployedConditionCrudKindErrorReason,
						},
					},
				},
			}

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

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc, app, nsObj).
				WithStatusSubresource(app, svc)).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred()) // error in the status

			updatedApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			// Verify app didn't bind to the non-deployed service
			Expect(updatedApp.Status.Service).To(BeNil())

			// Verify Deployed condition reflects the issue
			deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))
		})
		It("should allow a BrokerApp to bind to a deployed BrokerService", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-broker-service"
			appName := "test-app"

			nsObj := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ns,
				},
			}

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

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc, app, nsObj).
				WithStatusSubresource(app, svc)).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			// Verify app successfully bound to the deployed service
			Expect(updatedApp.Status.Service).NotTo(BeNil())
			Expect(updatedApp.Status.Service.Name).To(Equal(svcName))
			Expect(updatedApp.Status.Service.Namespace).To(Equal(ns))

			Expect(updatedApp.Status.Service.AssignedPort).To(Equal(int32(61616)))
		})
	})
})
