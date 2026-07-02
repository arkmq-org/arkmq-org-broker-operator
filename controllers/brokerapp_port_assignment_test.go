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

	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("BrokerApp port assignment", func() {
	Context("when assigning ports during reconcile", func() {
		It("should assign a port from the default pool on first reconcile", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-broker-service"
			appName := "test-app"

			nsObj := &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name: ns,
				},
			}

			// BrokerService with default port range (unbounded starting at 61616)
			svc := &v1beta2.BrokerService{
				ObjectMeta: v1.ObjectMeta{
					Name:      svcName,
					Namespace: ns,
					Labels:    map[string]string{"type": "broker"},
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

			app := &v1beta2.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      appName,
					Namespace: ns,
				},
				Spec: v1beta2.BrokerAppSpec{
					ServiceSelector: &v1.LabelSelector{
						MatchLabels: map[string]string{"type": "broker"},
					},
				},
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc, app, nsObj).
				WithStatusSubresource(app, svc).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
					app := obj.(*v1beta2.BrokerApp)
					if app.Status.Service != nil {
						return []string{app.Status.Service.Key()}
					}
					return nil
				}).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			Expect(updatedApp.Status.Service).NotTo(BeNil())
			Expect(updatedApp.Status.Service.AssignedPort).To(Equal(int32(61616)))
			Expect(updatedApp.Status.Service.Name).To(Equal(svcName))
		})
		It("should preserve an already-assigned port on re-reconcile", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-broker-service"
			appName := "test-app"

			nsObj := &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name: ns,
				},
			}

			svc := &v1beta2.BrokerService{
				ObjectMeta: v1.ObjectMeta{
					Name:      svcName,
					Namespace: ns,
					Labels:    map[string]string{"type": "broker"},
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

			app := &v1beta2.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      appName,
					Namespace: ns,
				},
				Spec: v1beta2.BrokerAppSpec{
					ServiceSelector: &v1.LabelSelector{
						MatchLabels: map[string]string{"type": "broker"},
					},
				},
				Status: v1beta2.BrokerAppStatus{
					Service: &v1beta2.BrokerServiceBindingStatus{
						Name:         svcName,
						Namespace:    ns,
						Secret:       "binding-secret",
						AssignedPort: 61616,
					},
				},
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc, app, nsObj).
				WithStatusSubresource(app, svc).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
					app := obj.(*v1beta2.BrokerApp)
					if app.Status.Service != nil {
						return []string{app.Status.Service.Key()}
					}
					return nil
				}).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
			Expect(err).NotTo(HaveOccurred())

			Expect(updatedApp.Status.Service).NotTo(BeNil())
			Expect(updatedApp.Status.Service.AssignedPort).To(Equal(int32(61616)))
		})
		It("should assign distinct ports to multiple apps", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-broker-service"

			nsObj := &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{
					Name: ns,
				},
			}

			svc := &v1beta2.BrokerService{
				ObjectMeta: v1.ObjectMeta{
					Name:      svcName,
					Namespace: ns,
					Labels:    map[string]string{"type": "broker"},
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

			app1 := &v1beta2.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      "app1",
					Namespace: ns,
				},
				Spec: v1beta2.BrokerAppSpec{
					ServiceSelector: &v1.LabelSelector{
						MatchLabels: map[string]string{"type": "broker"},
					},
				},
			}

			app2 := &v1beta2.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      "app2",
					Namespace: ns,
				},
				Spec: v1beta2.BrokerAppSpec{
					ServiceSelector: &v1.LabelSelector{
						MatchLabels: map[string]string{"type": "broker"},
					},
				},
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc, app1, app2, nsObj).
				WithStatusSubresource(app1, app2, svc).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
					app := obj.(*v1beta2.BrokerApp)
					if app.Status.Service != nil {
						return []string{app.Status.Service.Key()}
					}
					return nil
				}).
				Build()

			r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app1", Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req1)
			Expect(err).NotTo(HaveOccurred())

			updatedApp1 := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req1.NamespacedName, updatedApp1)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedApp1.Status.Service.AssignedPort).To(Equal(int32(61616)))

			req2 := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app2", Namespace: ns}}
			_, err = r.Reconcile(context.TODO(), req2)
			Expect(err).NotTo(HaveOccurred())

			updatedApp2 := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), req2.NamespacedName, updatedApp2)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedApp2.Status.Service.AssignedPort).To(Equal(int32(61617)))

			Expect(updatedApp2.Status.Service.AssignedPort).NotTo(Equal(updatedApp1.Status.Service.AssignedPort))
		})
	})
})
