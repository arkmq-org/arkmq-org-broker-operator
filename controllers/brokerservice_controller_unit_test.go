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
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("BrokerService Controller Unit Tests", func() {

	Context("when app moves between services", func() {
		It("should update secrets for both source and destination service", func() {
			// Setup scheme
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			// Data
			ns := "default"
			s1Name := "service1"
			s2Name := "service2"
			appName := "my-app"

			common.SetOperatorCASecretName("op_ca")
			DeferCleanup(common.UnsetOperatorCASecretName)

			common.SetOperatorNameSpace(ns)
			DeferCleanup(common.UnsetOperatorNameSpace)

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: ns,
				},
			}

			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "op_ca",
					Namespace: ns,
				},
				Data: map[string][]byte{"ca.pem": []byte("bla")},
			}
			s1 := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      s1Name,
					Namespace: ns,
					UID:       types.UID("uid-s1"),
				},
				Status: v1beta2.BrokerServiceStatus{},
			}
			s2 := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      s2Name,
					Namespace: ns,
					UID:       types.UID("uid-s2"),
				},
				Status: v1beta2.BrokerServiceStatus{},
			}
			app := &v1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: ns,
					UID:       types.UID("uid-app"),
				},
				Spec: v1beta2.BrokerAppSpec{},
				Status: v1beta2.BrokerAppStatus{
					Service: &v1beta2.BrokerServiceBindingStatus{
						Name:         s1Name,
						Namespace:    ns,
						Secret:       "binding-secret",
						AssignedPort: 61616,
					},
				},
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace, oc, s1, s2, app).WithStatusSubresource(s1, s2, app)
			builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
				a := rawObj.(*v1beta2.BrokerApp)
				if a.Status.Service != nil {
					return []string{a.Status.Service.Key()}
				}
				return nil
			})

			cl := builder.Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			reqS1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s1Name, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), reqS1)
			Expect(err).NotTo(HaveOccurred())

			secretS1 := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{Name: AppPropertiesSecretName(s1Name), Namespace: ns}, secretS1)
			Expect(err).NotTo(HaveOccurred())
			Expect(hasKeyContaining(secretS1.Data, appName)).To(BeTrue(), "S1 secret should contain app config")

			err = cl.Get(context.TODO(), types.NamespacedName{Name: appName, Namespace: ns}, app)
			Expect(err).NotTo(HaveOccurred())
			app.Status.Service = &v1beta2.BrokerServiceBindingStatus{
				Name:         s2Name,
				Namespace:    ns,
				Secret:       "app-binding-secret",
				AssignedPort: 61617,
			}
			Expect(cl.Status().Update(context.TODO(), app)).To(Succeed())

			_, err = r.Reconcile(context.TODO(), reqS1)
			Expect(err).NotTo(HaveOccurred())

			err = cl.Get(context.TODO(), types.NamespacedName{Name: AppPropertiesSecretName(s1Name), Namespace: ns}, secretS1)
			Expect(err).NotTo(HaveOccurred())
			Expect(hasKeyContaining(secretS1.Data, appName)).To(BeFalse(), "S1 secret should NOT contain app config after move")

			reqS2 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s2Name, Namespace: ns}}
			_, err = r.Reconcile(context.TODO(), reqS2)
			Expect(err).NotTo(HaveOccurred())

			secretS2 := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{Name: AppPropertiesSecretName(s2Name), Namespace: ns}, secretS2)
			Expect(err).NotTo(HaveOccurred())
			Expect(hasKeyContaining(secretS2.Data, appName)).To(BeTrue(), "S2 secret should contain app config after move")
		})
	})

	Context("when a List error occurs during reconcile", func() {
		It("should propagate the error and set appropriate conditions", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			s1Name := "service1"
			ns := "default"
			s1 := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      s1Name,
					Namespace: ns,
					UID:       types.UID("uid-s1"),
				},
			}

			interceptorFuncs := interceptor.Funcs{
				List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*corev1.SecretList); ok {
						return fmt.Errorf("simulated list error")
					}
					return client.List(ctx, list, opts...)
				},
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(s1).WithStatusSubresource(s1).WithInterceptorFuncs(interceptorFuncs)
			builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
				a := rawObj.(*v1beta2.BrokerApp)
				if a.Status.Service != nil {
					return []string{a.Status.Service.Key()}
				}
				return nil
			})

			cl := builder.Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			reqS1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s1Name, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), reqS1)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("simulated list error"))

			err = cl.Get(context.TODO(), reqS1.NamespacedName, s1)
			Expect(err).To(BeNil())

			Expect(meta.IsStatusConditionPresentAndEqual(s1.Status.Conditions, v1beta2.DeployedConditionType, metav1.ConditionUnknown)).To(BeTrue())
			Expect(meta.IsStatusConditionFalse(s1.Status.Conditions, v1beta2.ReadyConditionType)).To(BeTrue())

			validCond := meta.FindStatusCondition(s1.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(validCond.Reason).To(Equal(v1beta2.ValidConditionSuccessReason))
		})
	})

	Context("when status update fails", func() {
		It("should return the status update error", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			s1Name := "service1"
			ns := "default"
			s1 := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      s1Name,
					Namespace: ns,
					UID:       types.UID("uid-s1"),
				},
			}

			interceptorFuncs := interceptor.Funcs{
				SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					return fmt.Errorf("simulated status update error")
				},
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(s1).WithStatusSubresource(s1).WithInterceptorFuncs(interceptorFuncs)
			builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
				return nil
			})

			cl := builder.Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			reqS1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s1Name, Namespace: ns}}
			result, err := r.Reconcile(context.TODO(), reqS1)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("simulated status update error"))
			Expect(result.RequeueAfter).To(Equal(time.Duration(0)))
		})
	})

	Context("when field index is missing", func() {
		It("should return an error mentioning index", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			s1Name := "service1"
			appName := "my-app"

			s1 := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      s1Name,
					Namespace: ns,
					UID:       types.UID("uid-s1"),
				},
			}
			app := &v1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{
					Name:      appName,
					Namespace: ns,
					UID:       types.UID("uid-app"),
				},
			}

			cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(s1, app).WithStatusSubresource(s1, app).Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			reqS1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s1Name, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), reqS1)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("index"))
		})
	})

	Context("Deployed condition transitions", func() {
		It("should transition from False to True when broker becomes ready", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-broker-service"

			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      svcName,
					Namespace: ns,
				},
				Status: v1beta2.BrokerServiceStatus{},
			}

			builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(svc).WithStatusSubresource(svc, &v1beta2.Broker{})
			builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
				a := rawObj.(*v1beta2.BrokerApp)
				if a.Status.Service != nil {
					return []string{a.Status.Service.Key()}
				}
				return nil
			})
			cl := builder.Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionNotReadyReason))
			creationTime := deployedCond.LastTransitionTime

			time.Sleep(1 * time.Second)

			brokerCR := &v1beta2.Broker{}
			err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
			Expect(err).NotTo(HaveOccurred())

			meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
				Type:   v1beta2.ReadyConditionType,
				Status: metav1.ConditionTrue,
			})
			meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
				Type:   v1beta2.DeployedConditionType,
				Status: metav1.ConditionTrue,
			})
			err = cl.Status().Update(context.TODO(), brokerCR)
			Expect(err).NotTo(HaveOccurred())

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

			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			deployedCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(deployedCond.Reason).To(Equal(v1beta2.ReadyConditionReason))
			Expect(deployedCond.LastTransitionTime.After(creationTime.Time)).To(BeTrue())
		})
	})

	Context("ProvisionedApps status", func() {
		It("should populate ProvisionedApps after broker picks up config", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-service"
			appName := "my-app"

			common.SetOperatorCASecretName("op_ca")
			DeferCleanup(common.UnsetOperatorCASecretName)

			common.SetOperatorNameSpace(ns)
			DeferCleanup(common.UnsetOperatorNameSpace)

			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("bla")},
			}
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
			}
			app := &v1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns},
				Spec:       v1beta2.BrokerAppSpec{},
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
				WithObjects(namespace, oc, svc, app).
				WithStatusSubresource(svc, &v1beta2.Broker{}).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
					a := rawObj.(*v1beta2.BrokerApp)
					if a.Status.Service != nil {
						return []string{a.Status.Service.Key()}
					}
					return nil
				}).Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedSvc.Status.ProvisionedApps).To(BeEmpty())

			secretName := AppPropertiesSecretName(svcName)
			secret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.ResourceVersion).NotTo(BeEmpty())
			Expect(secret.Annotations[common.ProvisionedAppsAnnotation]).To(Equal(fmt.Sprintf("%s-%s", ns, appName)))

			brokerCR := &v1beta2.Broker{}
			err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
			Expect(err).NotTo(HaveOccurred())

			brokerCR.Status.Conditions = []metav1.Condition{
				{Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "Ready"},
			}
			brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
				{Name: secretName, ResourceVersion: secret.ResourceVersion},
			}
			err = cl.Status().Update(context.TODO(), brokerCR)
			Expect(err).NotTo(HaveOccurred())

			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedSvc.Status.ProvisionedApps).To(Equal([]string{fmt.Sprintf("%s-%s", ns, appName)}))
		})

		It("should incrementally update ProvisionedApps as apps are added", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-service"
			app1Name := "my-app-1"
			app2Name := "my-app-2"

			common.SetOperatorCASecretName("op_ca")
			DeferCleanup(common.UnsetOperatorCASecretName)

			common.SetOperatorNameSpace(ns)
			DeferCleanup(common.UnsetOperatorNameSpace)

			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("bla")},
			}
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
			}
			app1 := &v1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{Name: app1Name, Namespace: ns},
				Spec:       v1beta2.BrokerAppSpec{},
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
				WithObjects(namespace, oc, svc, app1).
				WithStatusSubresource(svc, &v1beta2.Broker{}).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
					a := rawObj.(*v1beta2.BrokerApp)
					if a.Status.Service != nil {
						return []string{a.Status.Service.Key()}
					}
					return nil
				}).Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			secretName := AppPropertiesSecretName(svcName)
			secret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
			Expect(err).NotTo(HaveOccurred())
			secretV1 := secret.ResourceVersion

			brokerCR := &v1beta2.Broker{}
			err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
			Expect(err).NotTo(HaveOccurred())
			brokerCR.Status.Conditions = []metav1.Condition{
				{Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "Ready"},
			}
			brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
				{Name: secretName, ResourceVersion: secretV1},
			}
			err = cl.Status().Update(context.TODO(), brokerCR)
			Expect(err).NotTo(HaveOccurred())

			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedSvc.Status.ProvisionedApps).To(Equal([]string{fmt.Sprintf("%s-%s", ns, app1Name)}))

			app2 := &v1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{Name: app2Name, Namespace: ns},
				Spec:       v1beta2.BrokerAppSpec{},
				Status: v1beta2.BrokerAppStatus{
					Service: &v1beta2.BrokerServiceBindingStatus{
						Name:         svcName,
						Namespace:    ns,
						Secret:       "binding-secret",
						AssignedPort: 61616,
					},
				},
			}
			err = cl.Create(context.TODO(), app2)
			Expect(err).NotTo(HaveOccurred())

			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.ResourceVersion).NotTo(Equal(secretV1))
			secretV2 := secret.ResourceVersion

			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedSvc.Status.ProvisionedApps).To(Equal([]string{fmt.Sprintf("%s-%s", ns, app1Name)}))

			err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
			Expect(err).NotTo(HaveOccurred())
			brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
				{Name: secretName, ResourceVersion: secretV2},
			}
			err = cl.Status().Update(context.TODO(), brokerCR)
			Expect(err).NotTo(HaveOccurred())

			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())
			expectedApps := []string{fmt.Sprintf("%s-%s", ns, app1Name), fmt.Sprintf("%s-%s", ns, app2Name)}
			sort.Strings(expectedApps)
			sort.Strings(updatedSvc.Status.ProvisionedApps)
			Expect(updatedSvc.Status.ProvisionedApps).To(Equal(expectedApps))
		})
	})

	Context("AppsProvisioned condition", func() {
		It("should transition from WaitingForBroker to Synced after broker picks up config", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-service"

			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc).
				WithStatusSubresource(svc, &v1beta2.Broker{}).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
					a := rawObj.(*v1beta2.BrokerApp)
					if a.Status.Service != nil {
						return []string{a.Status.Service.Key()}
					}
					return nil
				}).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			cond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.AppsProvisionedConditionType)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Reason).To(Equal(v1beta2.AppsProvisionedConditionWaitingReason))

			secretName := AppPropertiesSecretName(svcName)
			secret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.ResourceVersion).NotTo(BeEmpty())

			brokerCR := &v1beta2.Broker{}
			err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
			Expect(err).NotTo(HaveOccurred())

			brokerCR.Status.Conditions = []metav1.Condition{
				{Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue, Reason: "Ready"},
			}
			brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
				{Name: secretName, ResourceVersion: secret.ResourceVersion},
			}
			err = cl.Status().Update(context.TODO(), brokerCR)
			Expect(err).NotTo(HaveOccurred())

			currentSecretResourceVersion := secret.ResourceVersion

			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
			Expect(err).NotTo(HaveOccurred())
			Expect(secret.ResourceVersion).To(Equal(currentSecretResourceVersion))

			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			cond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.AppsProvisionedConditionType)
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionTrue))
			Expect(cond.Reason).To(Equal(v1beta2.AppsProvisionedConditionSyncedReason))
		})
	})

	Context("Prometheus override secret", func() {
		It("should create override secret with queue metrics when apps have ConsumerOf", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-service"
			appName := "metrics-app"

			common.SetOperatorCASecretName("op_ca")
			DeferCleanup(common.UnsetOperatorCASecretName)

			common.SetOperatorNameSpace(ns)
			DeferCleanup(common.UnsetOperatorNameSpace)

			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("bla")},
			}
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
			}
			app := &v1beta2.BrokerApp{
				ObjectMeta: metav1.ObjectMeta{Name: appName, Namespace: ns},
				Spec: v1beta2.BrokerAppSpec{
					Capabilities: []v1beta2.AppCapabilityType{
						{
							ConsumerOf: []v1beta2.AddressRef{
								{Address: "TEST.QUEUE.ONE"},
								{Address: "TEST.QUEUE.TWO"},
							},
							ProducerOf: []v1beta2.AddressRef{
								{Address: "TEST.QUEUE.ONE"},
							},
						},
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
				WithObjects(namespace, oc, svc, app).
				WithStatusSubresource(svc, &v1beta2.Broker{}).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
					a := rawObj.(*v1beta2.BrokerApp)
					if a.Status.Service != nil {
						return []string{a.Status.Service.Key()}
					}
					return nil
				}).Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			overrideSecretName := svcName + "-control-plane-override"
			overrideSecret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{Name: overrideSecretName, Namespace: ns}, overrideSecret)
			Expect(err).NotTo(HaveOccurred(), "control-plane-override secret should exist")

			prometheusYaml, ok := overrideSecret.Data["_prometheus_exporter.yaml"]
			Expect(ok).To(BeTrue(), "should have _prometheus_exporter.yaml key")
			Expect(prometheusYaml).NotTo(BeEmpty())

			prometheusConfig := string(prometheusYaml)
			Expect(prometheusConfig).To(ContainSubstring("org.apache.activemq.artemis:broker=*,component=addresses,address=*,subcomponent=queues,routing-type=*,queue=*"))
			Expect(prometheusConfig).To(ContainSubstring("TEST.QUEUE.ONE"))
			Expect(prometheusConfig).To(ContainSubstring("TEST.QUEUE.TWO"))
			Expect(prometheusConfig).To(ContainSubstring("MessageCount"))
			Expect(prometheusConfig).To(ContainSubstring("ConsumerCount"))
			Expect(prometheusConfig).To(ContainSubstring("DeliveringCount"))
		})

		It("should create override secret with queue metrics even without apps", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-service"

			common.SetOperatorCASecretName("op_ca")
			DeferCleanup(common.UnsetOperatorCASecretName)

			common.SetOperatorNameSpace(ns)
			DeferCleanup(common.UnsetOperatorNameSpace)

			namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}
			oc := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "op_ca", Namespace: ns},
				Data:       map[string][]byte{"ca.pem": []byte("bla")},
			}
			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Spec:       v1beta2.BrokerServiceSpec{Image: StringToPtr("placeholder")},
			}

			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(namespace, oc, svc).
				WithStatusSubresource(svc, &v1beta2.Broker{}).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
					a := rawObj.(*v1beta2.BrokerApp)
					if a.Status.Service != nil {
						return []string{a.Status.Service.Key()}
					}
					return nil
				}).Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			overrideSecretName := svcName + "-control-plane-override"
			overrideSecret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{Name: overrideSecretName, Namespace: ns}, overrideSecret)
			Expect(err).NotTo(HaveOccurred(), "control-plane-override secret should exist even without apps")

			prometheusYaml, ok := overrideSecret.Data["_prometheus_exporter.yaml"]
			Expect(ok).To(BeTrue(), "should have _prometheus_exporter.yaml key")
			Expect(prometheusYaml).NotTo(BeEmpty())

			prometheusConfig := string(prometheusYaml)
			Expect(prometheusConfig).To(ContainSubstring("component=addresses,address=*,subcomponent=queues"))
		})
	})

	Context("Valid condition", func() {
		It("should set Valid=True for valid spec", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-broker-service"

			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Status:     v1beta2.BrokerServiceStatus{},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc).
				WithStatusSubresource(svc)).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			validCondition := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCondition).NotTo(BeNil())
			Expect(validCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionSuccessReason))
		})

		It("should set Valid=False for invalid resource name", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			invalidName := "broker/service"

			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: invalidName, Namespace: ns},
				Status:     v1beta2.BrokerServiceStatus{},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc).
				WithStatusSubresource(svc)).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: invalidName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			validCondition := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCondition).NotTo(BeNil(), "Valid condition should be set for validation errors")
			Expect(validCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(validCondition.Reason).To(Equal(v1beta2.ValidConditionInvalidResourceName))
			Expect(validCondition.Message).NotTo(BeEmpty())

			deployedCondition := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCondition).NotTo(BeNil())
			Expect(deployedCondition.Status).To(Equal(metav1.ConditionFalse))
			Expect(deployedCondition.Reason).To(Equal(v1beta2.ValidConditionInvalidResourceName))
			Expect(deployedCondition.Message).NotTo(BeEmpty())
		})

		It("should persist Valid condition across reconciles without changing LastTransitionTime", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "my-broker-service-persist"

			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Status:     v1beta2.BrokerServiceStatus{},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc).
				WithStatusSubresource(svc)).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())
			validCondition1 := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCondition1).NotTo(BeNil())
			Expect(validCondition1.Status).To(Equal(metav1.ConditionTrue))

			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())
			validCondition2 := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCondition2).NotTo(BeNil())
			Expect(validCondition2.Status).To(Equal(metav1.ConditionTrue))
			Expect(validCondition2.LastTransitionTime).To(Equal(validCondition1.LastTransitionTime))
		})
	})

	Context("Idempotent status", func() {
		It("should produce identical status on repeated reconciles", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "test-service"

			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Status:     v1beta2.BrokerServiceStatus{},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc).
				WithStatusSubresource(svc)).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())
			firstStatus := updatedSvc.Status.DeepCopy()

			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			Expect(updatedSvc.Status.Conditions).To(Equal(firstStatus.Conditions))

			Expect(meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)).NotTo(BeNil())
			Expect(meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)).NotTo(BeNil())
			Expect(meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.AppsProvisionedConditionType)).NotTo(BeNil())
			Expect(meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ReadyConditionType)).NotTo(BeNil())
		})
	})

	Context("Condition independence", func() {
		It("should allow Valid=True while Deployed, AppsProvisioned, and Ready are False", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "test-service"

			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Status:     v1beta2.BrokerServiceStatus{},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc).
				WithStatusSubresource(svc)).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionTrue))

			deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))

			appsCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.AppsProvisionedConditionType)
			Expect(appsCond).NotTo(BeNil())
			Expect(appsCond.Status).To(Equal(metav1.ConditionFalse))

			readyCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ReadyConditionType)
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionFalse))
		})
	})

	Context("Valid condition with runtime errors", func() {
		It("should preserve Valid=True even when List errors cause Deployed=Unknown", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			ns := "default"
			svcName := "test-service"

			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Status:     v1beta2.BrokerServiceStatus{},
			}

			interceptorFuncs := interceptor.Funcs{
				List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
					if _, ok := list.(*corev1.SecretList); ok {
						return fmt.Errorf("simulated API list error")
					}
					return client.List(ctx, list, opts...)
				},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc).
				WithStatusSubresource(svc).
				WithInterceptorFuncs(interceptorFuncs)).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(validCond.Reason).To(Equal(v1beta2.ValidConditionSuccessReason))

			deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionUnknown))
			Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionCrudKindErrorReason))
		})
	})

	Context("Condition transitions on recovery", func() {
		It("should transition Deployed from False to True when broker becomes ready after initial failure", func() {
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = appsv1.AddToScheme(scheme)

			ns := "default"
			svcName := "test-service"

			svc := &v1beta2.BrokerService{
				ObjectMeta: metav1.ObjectMeta{Name: svcName, Namespace: ns},
				Status:     v1beta2.BrokerServiceStatus{},
			}

			cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc).
				WithStatusSubresource(svc, &v1beta2.Broker{})).
				Build()

			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionTrue))

			deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(deployedCond.Reason).To(Equal(v1beta2.DeployedConditionNotReadyReason))

			brokerCR := &v1beta2.Broker{}
			err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
			Expect(err).NotTo(HaveOccurred())

			brokerCR.Status.Conditions = []metav1.Condition{
				{Type: v1beta2.ReadyConditionType, Status: metav1.ConditionTrue},
				{Type: v1beta2.DeployedConditionType, Status: metav1.ConditionTrue},
			}
			err = cl.Status().Update(context.TODO(), brokerCR)
			Expect(err).NotTo(HaveOccurred())

			ss := &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: svcName + "-ss", Namespace: ns},
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

			_, err = r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			validCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
			Expect(validCond).NotTo(BeNil())
			Expect(validCond.Status).To(Equal(metav1.ConditionTrue))

			deployedCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
			Expect(deployedCond).NotTo(BeNil())
			Expect(deployedCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(deployedCond.Reason).To(Equal(v1beta2.ReadyConditionReason))
		})
	})
})

func hasKeyContaining(data map[string][]byte, substring string) bool {
	for k := range data {
		if strings.Contains(k, substring) {
			return true
		}
	}
	return false
}
