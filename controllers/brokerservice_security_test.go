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
	networkingv1 "k8s.io/api/networking/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var _ = Describe("brokerservice security", func() {

	It("rejects manually annotated app", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)

		svcNs := "broker-services"
		svcName := "premium-broker"
		attackerNs := "untrusted-team"
		appName := "malicious-app"

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: `app.metadata.namespace == "trusted-team"`,
			},
		}

		attackerApp := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      appName,
				Namespace: attackerNs,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{"type": "broker"},
				},
			},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:      svcName,
					Namespace: svcNs,
					Secret:    "binding-secret",
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, attackerApp)...).
			WithStatusSubresource(svc, attackerApp).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
				app := obj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}), nil)

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		secret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{
			Name:      svc.Name + "-app-bp",
			Namespace: svcNs,
		}, secret)
		Expect(err).NotTo(HaveOccurred(), "App properties secret should be created")

		if err == nil {
			provisionedApps, hasAnnotation := secret.Annotations[common.ProvisionedAppsAnnotation]
			if hasAnnotation {
				Expect(provisionedApps).To(BeEmpty(), "Should not provision apps that don't match selector")
			}

			acceptorKey := attackerNs + "-" + appName + "-acceptor.json"
			_, hasAcceptorConfig := secret.Data[acceptorKey]
			Expect(hasAcceptorConfig).To(BeFalse(), "Should not create acceptor config for unauthorized app")
		}

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedSvc.Status.ProvisionedApps).To(HaveLen(0),
			"Service should not provision apps that don't match selector")

		Expect(updatedSvc.Status.RejectedApps).To(HaveLen(1),
			"Should track one rejected app")
		if len(updatedSvc.Status.RejectedApps) > 0 {
			rejected := updatedSvc.Status.RejectedApps[0]
			Expect(rejected.Name).To(Equal(appName), "Rejected app should be the malicious app")
			Expect(rejected.Namespace).To(Equal(attackerNs), "Rejected app should be from attacker namespace")
			Expect(rejected.Reason).To(Equal("does not match appSelectorExpression"),
				"Rejection reason should indicate appSelectorExpression mismatch")
		}

		verifyApp := &v1beta2.BrokerApp{}
		err = cl.Get(context.TODO(), types.NamespacedName{
			Name:      appName,
			Namespace: attackerNs,
		}, verifyApp)
		Expect(err).NotTo(HaveOccurred())
		Expect(verifyApp.Status.Service).NotTo(BeNil())
		Expect(verifyApp.Status.Service.Name).To(Equal(svcName))
		Expect(verifyApp.Status.Service.Namespace).To(Equal(svcNs),
			"App status binding should still be set, proving it was found but rejected")
	})

	It("allows matching app", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)

		svcNs := "broker-services"
		svcName := "premium-broker"
		allowedNs := "trusted-team"
		appName := "legitimate-app"

		common.SetOperatorCASecretName("op-ca")
		defer common.UnsetOperatorCASecretName()
		common.SetOperatorNameSpace(svcNs)
		defer common.UnsetOperatorNameSpace()

		opCASecret := &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      "op-ca",
				Namespace: svcNs,
			},
			Data: map[string][]byte{"ca.pem": []byte("test-ca")},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels:    map[string]string{"type": "broker"},
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: `app.metadata.namespace == "trusted-team"`,
			},
			Status: v1beta2.BrokerServiceStatus{},
		}

		legitimateApp := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      appName,
				Namespace: allowedNs,
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

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, legitimateApp, opCASecret)...).
			WithStatusSubresource(svc, legitimateApp).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
				app := obj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}), nil)

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		secret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{
			Name:      svc.Name + "-app-bp",
			Namespace: svcNs,
		}, secret)
		Expect(err).NotTo(HaveOccurred(), "App properties secret should be created")

		if err == nil {
			provisionedApps, hasAnnotation := secret.Annotations[common.ProvisionedAppsAnnotation]
			if hasAnnotation {
				Expect(provisionedApps).To(ContainSubstring(appName), "Should provision apps that match selector")
			}

			acceptorKey := allowedNs + "-" + appName + "-acceptor.json"
			_, hasAcceptorConfig := secret.Data[acceptorKey]
			Expect(hasAcceptorConfig).To(BeTrue(), "Should create acceptor config for authorized app")
		}

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedSvc.Status.RejectedApps).To(HaveLen(0),
			"Should not reject apps that match selector")
	})

	It("rejects label mismatch", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)

		svcNs := "broker-services"
		svcName := "premium-broker"
		appNs := "default"
		appName := "attacker-app"

		common.SetOperatorCASecretName("op-ca")
		defer common.UnsetOperatorCASecretName()
		common.SetOperatorNameSpace(svcNs)
		defer common.UnsetOperatorNameSpace()

		opCASecret := &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      "op-ca",
				Namespace: svcNs,
			},
			Data: map[string][]byte{"ca.pem": []byte("test-ca")},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
				Labels: map[string]string{
					"tier": "premium",
				},
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: "true",
			},
		}

		app := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      appName,
				Namespace: appNs,
			},
			Spec: v1beta2.BrokerAppSpec{
				ServiceSelector: &v1.LabelSelector{
					MatchLabels: map[string]string{
						"tier": "basic",
					},
				},
			},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:      svcName,
					Namespace: svcNs,
					Secret:    "binding-secret",
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, app, opCASecret)...).
			WithStatusSubresource(svc, app).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
				app := obj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}), nil)

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		secret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{
			Name:      svc.Name + "-app-bp",
			Namespace: svcNs,
		}, secret)
		Expect(err).NotTo(HaveOccurred(), "App properties secret should be created")

		if err == nil {
			provisionedApps, hasAnnotation := secret.Annotations[common.ProvisionedAppsAnnotation]
			if hasAnnotation {
				Expect(provisionedApps).To(BeEmpty(), "Should not provision app with mismatched label selector")
			}

			acceptorKey := appNs + "-" + appName + "-acceptor.json"
			_, hasAcceptorConfig := secret.Data[acceptorKey]
			Expect(hasAcceptorConfig).To(BeFalse(), "Should not create acceptor config for app with mismatched labels")
		}

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedSvc.Status.ProvisionedApps).To(HaveLen(0),
			"Service should not provision app with mismatched label selector")

		Expect(updatedSvc.Status.RejectedApps).To(HaveLen(1),
			"Should track one rejected app")
		if len(updatedSvc.Status.RejectedApps) > 0 {
			rejected := updatedSvc.Status.RejectedApps[0]
			Expect(rejected.Name).To(Equal(appName), "Rejected app should be the attacker app")
			Expect(rejected.Namespace).To(Equal(appNs), "Rejected app should be from the app namespace")
			Expect(rejected.Reason).To(Equal("does not match service labels"),
				"Rejection reason should indicate label selector mismatch")
		}
	})

	It("provisions matching apps and rejects non-matching in mixed scenario", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)

		svcNs := "broker-services"
		svcName := "premium-broker"

		common.SetOperatorCASecretName("op-ca")
		defer common.UnsetOperatorCASecretName()
		common.SetOperatorNameSpace(svcNs)
		defer common.UnsetOperatorNameSpace()

		opCASecret := &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      "op-ca",
				Namespace: svcNs,
			},
			Data: map[string][]byte{"ca.pem": []byte("test-ca")},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: `app.metadata.namespace.startsWith("team-")`,
			},
			Status: v1beta2.BrokerServiceStatus{},
		}

		matchingApp1 := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app1",
				Namespace: "team-a",
			},
			Spec: v1beta2.BrokerAppSpec{},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:         svcName,
					Namespace:    svcNs,
					Secret:       "binding-secret",
					AssignedPort: 61616,
				},
			},
		}

		matchingApp2 := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "app2",
				Namespace: "team-b",
			},
			Spec: v1beta2.BrokerAppSpec{},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:         svcName,
					Namespace:    svcNs,
					Secret:       "binding-secret",
					AssignedPort: 61617,
				},
			},
		}

		attackerApp := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "attacker-app",
				Namespace: "other-namespace",
			},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:      svcName,
					Namespace: svcNs,
					Secret:    "binding-secret",
				},
			},
			Spec: v1beta2.BrokerAppSpec{},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, matchingApp1, matchingApp2, attackerApp, opCASecret)...).
			WithStatusSubresource(svc, matchingApp1, matchingApp2, attackerApp).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
				app := obj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}), nil)

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		secret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{
			Name:      svc.Name + "-app-bp",
			Namespace: svcNs,
		}, secret)
		Expect(err).NotTo(HaveOccurred(), "App properties secret should be created")

		if err == nil {
			_, hasApp1Config := secret.Data["team-a-app1-acceptor.json"]
			Expect(hasApp1Config).To(BeTrue(), "Should provision matching app1")

			_, hasApp2Config := secret.Data["team-b-app2-acceptor.json"]
			Expect(hasApp2Config).To(BeTrue(), "Should provision matching app2")

			_, hasAttackerConfig := secret.Data["other-namespace-attacker-app-acceptor.json"]
			Expect(hasAttackerConfig).To(BeFalse(), "Should NOT provision non-matching attacker-app")

			provisionedApps := secret.Annotations[common.ProvisionedAppsAnnotation]
			Expect(provisionedApps).To(ContainSubstring("app1"))
			Expect(provisionedApps).To(ContainSubstring("app2"))
			Expect(provisionedApps).NotTo(ContainSubstring("attacker-app"))
		}

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedSvc.Status.RejectedApps).To(HaveLen(1),
			"Should track 1 rejected app")
		if len(updatedSvc.Status.RejectedApps) > 0 {
			rejected := updatedSvc.Status.RejectedApps[0]
			Expect(rejected.Name).To(Equal("attacker-app"), "Rejected app should be attacker-app")
			Expect(rejected.Namespace).To(Equal("other-namespace"), "Rejected app should be from other-namespace")
			Expect(rejected.Reason).To(Equal("does not match appSelectorExpression"),
				"Rejection reason should indicate appSelectorExpression mismatch")
		}
	})

	It("rejects apps from prometheus config", Label(unitLabel), func() {
		scheme := runtime.NewScheme()
		_ = v1beta2.AddToScheme(scheme)
		_ = corev1.AddToScheme(scheme)
		_ = networkingv1.AddToScheme(scheme)

		svcNs := "broker-services"
		svcName := "metrics-broker"
		allowedNs := "trusted-team"
		attackerNs := "untrusted-team"

		common.SetOperatorCASecretName("op-ca")
		defer common.UnsetOperatorCASecretName()
		common.SetOperatorNameSpace(svcNs)
		defer common.UnsetOperatorNameSpace()

		opCASecret := &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{
				Name:      "op-ca",
				Namespace: svcNs,
			},
			Data: map[string][]byte{"ca.pem": []byte("test-ca")},
		}

		svc := &v1beta2.BrokerService{
			ObjectMeta: v1.ObjectMeta{
				Name:      svcName,
				Namespace: svcNs,
			},
			Spec: v1beta2.BrokerServiceSpec{
				AppSelectorExpression: `app.metadata.namespace == "trusted-team"`,
			},
			Status: v1beta2.BrokerServiceStatus{},
		}

		validApp := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "valid-app",
				Namespace: allowedNs,
			},
			Spec: v1beta2.BrokerAppSpec{
				Capabilities: []v1beta2.AppCapabilityType{
					{
						ConsumerOf: []v1beta2.AddressRef{
							{Address: "VALID.QUEUE.ONE"},
							{Address: "VALID.QUEUE.TWO"},
						},
					},
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

		attackerApp := &v1beta2.BrokerApp{
			ObjectMeta: v1.ObjectMeta{
				Name:      "attacker-app",
				Namespace: attackerNs,
			},
			Status: v1beta2.BrokerAppStatus{
				Service: &v1beta2.BrokerServiceBindingStatus{
					Name:      svcName,
					Namespace: svcNs,
					Secret:    "binding-secret",
				},
			},
			Spec: v1beta2.BrokerAppSpec{
				Capabilities: []v1beta2.AppCapabilityType{
					{
						ConsumerOf: []v1beta2.AddressRef{
							{Address: "ATTACKER.SECRET.QUEUE"},
							{Address: "ATTACKER.RECON.QUEUE"},
						},
					},
				},
			},
		}

		cl := fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(WithCerts(svc, validApp, attackerApp, opCASecret)...).
			WithStatusSubresource(svc, validApp, attackerApp).
			WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
				app := obj.(*v1beta2.BrokerApp)
				if app.Status.Service != nil {
					return []string{app.Status.Service.Key()}
				}
				return nil
			}).
			Build()

		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}), nil)

		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
		_, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())

		overrideSecretName := svcName + "-control-plane-override"
		overrideSecret := &corev1.Secret{}
		err = cl.Get(context.TODO(), types.NamespacedName{
			Name:      overrideSecretName,
			Namespace: svcNs,
		}, overrideSecret)
		Expect(err).NotTo(HaveOccurred(), "control-plane-override secret should be created")

		prometheusYaml, ok := overrideSecret.Data[PrometheusConfigFileName]
		Expect(ok).To(BeTrue(), "should have prometheus key")
		Expect(prometheusYaml).NotTo(BeEmpty())

		prometheusConfig := string(prometheusYaml)

		Expect(prometheusConfig).To(ContainSubstring("VALID.QUEUE.ONE"),
			"Valid app's ConsumerOf addresses should be in Prometheus config")
		Expect(prometheusConfig).To(ContainSubstring("VALID.QUEUE.TWO"),
			"Valid app's ConsumerOf addresses should be in Prometheus config")

		Expect(prometheusConfig).NotTo(ContainSubstring("ATTACKER.SECRET.QUEUE"),
			"SECURITY: Rejected app's ConsumerOf addresses should NOT leak into Prometheus config")
		Expect(prometheusConfig).NotTo(ContainSubstring("ATTACKER.RECON.QUEUE"),
			"SECURITY: Rejected app's ConsumerOf addresses should NOT leak into Prometheus config")

		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		Expect(err).NotTo(HaveOccurred())

		Expect(updatedSvc.Status.RejectedApps).To(HaveLen(1),
			"Should track 1 rejected app")
		if len(updatedSvc.Status.RejectedApps) > 0 {
			rejected := updatedSvc.Status.RejectedApps[0]
			Expect(rejected.Name).To(Equal("attacker-app"))
			Expect(rejected.Namespace).To(Equal(attackerNs))
			Expect(rejected.Reason).To(Equal("does not match appSelectorExpression"))
		}
	})
})
