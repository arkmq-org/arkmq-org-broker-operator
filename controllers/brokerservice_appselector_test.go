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

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("app selector", func() {

	It("allows app from allowed namespace", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		allowedNs := "team-a"
		svcNs := "shared"
		svcName := "shared-broker"
		appName := "myapp"

		allowedNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: allowedNs,
			},
		}
		sharedNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: svcNs,
			},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: fmt.Sprintf(`app.metadata.namespace == "%s"`, allowedNs),
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
				Namespace: allowedNs,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, app, allowedNsObj, sharedNsObj)...).
			WithStatusSubresource(app, svc)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: allowedNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedApp.Status.Service).NotTo(BeNil(), "App should be bound to service")
		Expect(updatedApp.Status.Service.Name).To(Equal(svcName))
		Expect(updatedApp.Status.Service.Namespace).To(Equal(svcNs))

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(v1.ConditionTrue))

		deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionProvisioningPendingReason))
	})

	It("denies app from non-allowed namespace", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		allowedNs := "team-a"
		deniedNs := "team-b"
		svcNs := "shared"
		svcName := "shared-broker"
		appName := "myapp"

		allowedNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: allowedNs,
			},
		}
		sharedNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: svcNs,
			},
		}

		deniedNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: deniedNs,
			},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: fmt.Sprintf(`app.metadata.namespace == "%s"`, allowedNs),
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
				Namespace: deniedNs,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, app, allowedNsObj, deniedNsObj, sharedNsObj)...).
			WithStatusSubresource(app, svc)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: deniedNs}}

		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedApp.Status.Service).To(BeNil(), "App should not be bound to service")

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(v1.ConditionTrue))

		deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))
		Expect(deployedCondition.Message).To(ContainSubstring("no services"))

		readyCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ReadyConditionType)
		Expect(readyCondition).NotTo(BeNil())
		Expect(readyCondition.Status).To(Equal(v1.ConditionFalse))
	})

	It("allows same namespace with empty allowlist", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		svcNs := "broker-services"
		svcName := "my-broker"
		appName := "myapp"

		svcNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: svcNs,
			},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{},
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
				Namespace: svcNs,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, app, svcNsObj)...).
			WithStatusSubresource(app, svc)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: svcNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedApp.Status.Service).NotTo(BeNil(), "App should be bound to service")
		Expect(updatedApp.Status.Service.Name).To(Equal(svcName))
		Expect(updatedApp.Status.Service.Namespace).To(Equal(svcNs))

		validCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.ValidConditionType)
		Expect(validCondition).NotTo(BeNil())
		Expect(validCondition.Status).To(Equal(v1.ConditionTrue))
	})

	It("denies different namespace with empty allowlist", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		svcNs := "broker-services"
		appNs := "different-namespace"
		svcName := "my-broker"
		appName := "myapp"

		svcNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: svcNs,
			},
		}
		appNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: appNs,
			},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{},
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
				Namespace: appNs,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, app, svcNsObj, appNsObj)...).
			WithStatusSubresource(app, svc)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: appNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedApp.Status.Service).To(BeNil(), "App should not be bound to service")

		deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))
	})

	It("revokes access when removed from allowlist", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		appNs := "team-a"
		svcNs := "shared"
		svcName := "shared-broker"
		appName := "myapp"

		appNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: appNs,
			},
		}
		sharedNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: svcNs,
			},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: fmt.Sprintf(`app.metadata.namespace == "%s"`, appNs),
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
				Namespace: appNs,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:         svcName,
					Namespace:    svcNs,
					Secret:       "binding-secret",
					AssignedPort: 61616,
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, app, appNsObj, sharedNsObj)...).
			WithStatusSubresource(app, svc)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: appNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedApp.Status.Service).NotTo(BeNil(), "App should remain bound initially")

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: svcNs}, updatedSvc)
		Expect(err).NotTo(HaveOccurred())
		updatedSvc.Spec.AppSelectorExpression = `app.metadata.namespace == "team-b"`
		err = cl.Update(context.TODO(), updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		_, err = r.Reconcile(context.TODO(), req)
		Expect(err).To(HaveOccurred())

		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		deployedCondition := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))
	})

	It("allows multiple namespaces", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		svcNs := "shared"
		svcName := "shared-broker"

		svcNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: svcNs,
			},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: `app.metadata.namespace in ["team-a", "team-b", "team-c"]`,
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

		teamANsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: "team-a",
			},
		}
		appA := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app-a",
				Namespace: "team-a",
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		teamBNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: "team-b",
			},
		}
		appB := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app-b",
				Namespace: "team-b",
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		teamDNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: "team-d",
			},
		}
		appDenied := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app-denied",
				Namespace: "team-d",
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, appA, appB, appDenied, svcNsObj, teamANsObj, teamBNsObj, teamDNsObj)...).
			WithStatusSubresource(appA, appB, appDenied, svc)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		reqA := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-a", Namespace: "team-a"}}
		_, err := r.Reconcile(context.TODO(), reqA)
		Expect(err).NotTo(HaveOccurred())

		updatedAppA := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqA.NamespacedName, updatedAppA)
		Expect(err).NotTo(HaveOccurred())
		hasBinding := updatedAppA.Status.Service != nil
		Expect(hasBinding).To(BeTrue(), "App A should be bound")

		reqB := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-b", Namespace: "team-b"}}
		_, err = r.Reconcile(context.TODO(), reqB)
		Expect(err).NotTo(HaveOccurred())

		updatedAppB := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqB.NamespacedName, updatedAppB)
		Expect(err).NotTo(HaveOccurred())
		hasBinding = updatedAppB.Status.Service != nil
		Expect(hasBinding).To(BeTrue(), "App B should be bound")

		reqDenied := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-denied", Namespace: "team-d"}}
		_, err = r.Reconcile(context.TODO(), reqDenied)
		Expect(err).To(HaveOccurred())

		updatedAppDenied := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqDenied.NamespacedName, updatedAppDenied)
		Expect(err).NotTo(HaveOccurred())
		hasBinding = updatedAppDenied.Status.Service != nil
		Expect(hasBinding).To(BeFalse(), "Denied app should not be bound")

		deployedCondition := meta.FindStatusCondition(updatedAppDenied.Status.Conditions, v1beta2.DeployedConditionType)
		Expect(deployedCondition).NotTo(BeNil())
		Expect(deployedCondition.Status).To(Equal(v1.ConditionFalse))
		Expect(deployedCondition.Reason).To(Equal(v1beta2.DeployedConditionNoMatchingServiceReason))
	})

	It("allows all namespaces with true expression", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		svcNs := "broker-services"
		appNs := "any-other-namespace"
		svcName := "open-broker"
		appName := "myapp"

		svcNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: svcNs,
			},
		}
		appNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: appNs,
			},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: "true",
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
				Namespace: appNs,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, app, svcNsObj, appNsObj)...).
			WithStatusSubresource(app, svc)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: appNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		updatedApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedApp.Status.Service).NotTo(BeNil(), "App should be bound to service")
		Expect(updatedApp.Status.Service.Name).To(Equal(svcName))
		Expect(updatedApp.Status.Service.Namespace).To(Equal(svcNs))
	})

	It("matches prefix with startsWith", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		svcNs := "broker-services"
		svcName := "team-broker"

		svcNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: svcNs,
			},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: `app.metadata.namespace.startsWith("team-")`,
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

		teamAProdNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: "team-a-prod",
			},
		}
		appMatch := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app-match",
				Namespace: "team-a-prod",
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		appNoMatchNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: "app-nomatch",
			},
		}
		appNoMatch := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app-nomatch",
				Namespace: "other-namespace",
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, appMatch, appNoMatch, svcNsObj, teamAProdNsObj, appNoMatchNsObj)...).
			WithStatusSubresource(appMatch, appNoMatch, svc)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		reqMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-match", Namespace: "team-a-prod"}}
		_, err := r.Reconcile(context.TODO(), reqMatch)
		Expect(err).NotTo(HaveOccurred())

		updatedMatch := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqMatch.NamespacedName, updatedMatch)
		Expect(err).NotTo(HaveOccurred())
		hasBinding := updatedMatch.Status.Service != nil
		Expect(hasBinding).To(BeTrue(), "Matching app should be bound")

		reqNoMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-nomatch", Namespace: "other-namespace"}}

		_, err = r.Reconcile(context.TODO(), reqNoMatch)
		Expect(err).To(HaveOccurred())

		updatedNoMatch := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqNoMatch.NamespacedName, updatedNoMatch)
		Expect(err).NotTo(HaveOccurred())
		hasBinding = updatedNoMatch.Status.Service != nil
		Expect(hasBinding).To(BeFalse(), "Non-matching app should not be bound")
	})

	It("matches suffix with endsWith", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		svcNs := "broker-services"
		svcName := "prod-broker"

		svcNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: svcNs,
			},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: `app.metadata.namespace.endsWith("-prod")`,
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

		teamAProdNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: "team-a-prod",
			},
		}
		appMatch := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app-match",
				Namespace: "team-a-prod",
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		teamADevNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: "team-a-dev",
			},
		}
		appNoMatch := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app-nomatch",
				Namespace: "team-a-dev",
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, appMatch, appNoMatch, svcNsObj, teamAProdNsObj, teamADevNsObj)...).
			WithStatusSubresource(appMatch, appNoMatch, svc)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		reqMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-match", Namespace: "team-a-prod"}}
		_, err := r.Reconcile(context.TODO(), reqMatch)
		Expect(err).NotTo(HaveOccurred())

		updatedMatch := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqMatch.NamespacedName, updatedMatch)
		Expect(err).NotTo(HaveOccurred())
		hasBinding := updatedMatch.Status.Service != nil
		Expect(hasBinding).To(BeTrue())

		reqNoMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-nomatch", Namespace: "team-a-dev"}}
		_, err = r.Reconcile(context.TODO(), reqNoMatch)
		Expect(err).To(HaveOccurred())

		updatedNoMatch := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), reqNoMatch.NamespacedName, updatedNoMatch)
		Expect(err).NotTo(HaveOccurred())
		hasBinding = updatedNoMatch.Status.Service != nil
		Expect(hasBinding).To(BeFalse())
	})

	It("matches combined prefix and suffix", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)

		svcNs := "broker-services"
		svcName := "pattern-broker"

		svcNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: svcNs,
			},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: `app.metadata.namespace.startsWith("team-") && app.metadata.namespace.endsWith("-prod")`,
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

		teamAProdNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: "team-a-prod",
			},
		}
		appMatch1 := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app-match1",
				Namespace: "team-a-prod",
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		teamBackendProdNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: "team-backend-prod",
			},
		}
		appMatch2 := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app-match2",
				Namespace: "team-backend-prod",
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		teamADevNsObj := &corev1.Namespace{
			ObjectMeta: v1.ObjectMeta{
				Name: "team-a-dev",
			},
		}
		appNoMatch := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app-nomatch",
				Namespace: "team-a-dev",
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
		}

		cl := SetupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, appMatch1, appMatch2, appNoMatch, svcNsObj, teamAProdNsObj, teamBackendProdNsObj, teamADevNsObj)...).
			WithStatusSubresource(appMatch1, appMatch2, appNoMatch, svc)).
			Build()

		r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		req1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-match1", Namespace: "team-a-prod"}}
		_, err := r.Reconcile(context.TODO(), req1)
		Expect(err).NotTo(HaveOccurred())

		req2 := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-match2", Namespace: "team-backend-prod"}}
		_, err = r.Reconcile(context.TODO(), req2)
		Expect(err).NotTo(HaveOccurred())

		reqNoMatch := ctrl.Request{NamespacedName: types.NamespacedName{Name: "app-nomatch", Namespace: "team-a-dev"}}

		_, err = r.Reconcile(context.TODO(), reqNoMatch)
		Expect(err).To(HaveOccurred())
	})
})
