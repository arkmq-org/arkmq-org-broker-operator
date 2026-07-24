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

var _ = Describe("BrokerService app security", func() {
	Context("app label matching enforcement", func() {
		It("should reject manually annotated app", func() {
			// Setup scheme
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)

			// Data
			svcNs := "broker-services"
			svcName := "premium-broker"
			attackerNs := "untrusted-team"
			appName := "malicious-app"

			// Create BrokerService that only allows "trusted-team" namespace
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

			// Create BrokerApp from UNTRUSTED namespace with MANUALLY SET annotation
			// (simulating an attacker trying to bypass access control)
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

			// Setup fake client with indexer
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc, attackerApp).
				WithStatusSubresource(svc, attackerApp).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
					app := obj.(*v1beta2.BrokerApp)
					if app.Status.Service != nil {
						return []string{app.Status.Service.Key()}
					}
					return nil
				}).
				Build()

			// Create BrokerService Reconciler
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			// Reconcile the service
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// Verify that the Secret was created (it should exist even with no apps)
			secret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{
				Name:      svc.Name + "-app-bp",
				Namespace: svcNs,
			}, secret)
			Expect(err).NotTo(HaveOccurred())

			// Secret should exist but should be EMPTY (no apps provisioned)
			// because the attacker's app doesn't match the selector
			if err == nil {
				// Check that the provisioned apps annotation is empty
				provisionedApps, hasAnnotation := secret.Annotations[common.ProvisionedAppsAnnotation]
				if hasAnnotation {
					Expect(provisionedApps).To(BeEmpty())
				}

				// Check that no acceptor config was created for the attacker's app
				acceptorKey := attackerNs + "-" + appName + "-acceptor.properties"
				_, hasAcceptorConfig := secret.Data[acceptorKey]
				Expect(hasAcceptorConfig).To(BeFalse())
			}

			// Verify the service status does NOT include the attacker's app in provisioned apps
			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			// Status should show 0 provisioned apps
			Expect(len(updatedSvc.Status.ProvisionedApps)).To(Equal(0),
				"Service should not provision apps that don't match selector")

			// CRITICAL: Verify that the app appears in RejectedApps with the correct reason
			// This proves that:
			// 1. The annotation was found (app was retrieved via the index)
			// 2. The label selector matched (app passed that check)
			// 3. But the appSelectorExpression rejected it (security working as intended)
			Expect(updatedSvc.Status.RejectedApps).To(HaveLen(1))
			if len(updatedSvc.Status.RejectedApps) > 0 {
				rejected := updatedSvc.Status.RejectedApps[0]
				Expect(rejected.Name).To(Equal(appName))
				Expect(rejected.Namespace).To(Equal(attackerNs))
				Expect(rejected.Reason).To(Equal("does not match appSelectorExpression"))
			}

			// Verify the attacker app's annotation is still set (proves it was found, not just missed)
			verifyApp := &v1beta2.BrokerApp{}
			err = cl.Get(context.TODO(), types.NamespacedName{
				Name:      appName,
				Namespace: attackerNs,
			}, verifyApp)
			Expect(err).NotTo(HaveOccurred())
			Expect(verifyApp.Status.Service).NotTo(BeNil())
			Expect(verifyApp.Status.Service.Name).To(Equal(svcName))
			Expect(verifyApp.Status.Service.Namespace).To(Equal(svcNs))
		})
		It("should allow matching app", func() {
			// Setup scheme
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)

			// Data
			svcNs := "broker-services"
			svcName := "premium-broker"
			allowedNs := "trusted-team"
			appName := "legitimate-app"

			// Setup operator environment
			common.SetOperatorCASecretName("op-ca")
			DeferCleanup(common.UnsetOperatorCASecretName)
			common.SetOperatorNameSpace(svcNs)
			DeferCleanup(common.UnsetOperatorNameSpace)

			// Create operator CA secret
			opCASecret := &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{
					Name:      "op-ca",
					Namespace: svcNs,
				},
				Data: map[string][]byte{"ca.pem": []byte("test-ca")},
			}

			// Create BrokerService
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

			// Create legitimate BrokerApp from ALLOWED namespace with status binding set by controller
			legitimateApp := &v1beta2.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      appName,
					Namespace: allowedNs, // Matches the selector
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

			// Setup fake client
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc, legitimateApp, opCASecret).
				WithStatusSubresource(svc, legitimateApp).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
					app := obj.(*v1beta2.BrokerApp)
					if app.Status.Service != nil {
						return []string{app.Status.Service.Key()}
					}
					return nil
				}).
				Build()

			// Create BrokerService Reconciler
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			// Reconcile the service
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// Verify that the Secret was created with the legitimate app
			secret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{
				Name:      svc.Name + "-app-bp",
				Namespace: svcNs,
			}, secret)
			Expect(err).NotTo(HaveOccurred())

			if err == nil {
				// Check that the provisioned apps annotation includes the legitimate app
				provisionedApps, hasAnnotation := secret.Annotations[common.ProvisionedAppsAnnotation]
				if hasAnnotation {
					Expect(provisionedApps).To(ContainSubstring(appName))
				}

				// Check that acceptor config was created for the legitimate app
				acceptorKey := allowedNs + "-" + appName + "-acceptor.properties"
				_, hasAcceptorConfig := secret.Data[acceptorKey]
				Expect(hasAcceptorConfig).To(BeTrue())
			}

			// Verify the service status shows the app as provisioned (not rejected)
			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			// Should have 0 rejected apps (legitimate app should not be rejected)
			Expect(updatedSvc.Status.RejectedApps).To(HaveLen(0))
		})
		It("should reject label mismatch", func() {
			// Setup scheme
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)

			// Data
			svcNs := "broker-services"
			svcName := "premium-broker"
			appNs := "default"
			appName := "attacker-app"

			// Setup operator environment
			common.SetOperatorCASecretName("op-ca")
			DeferCleanup(common.UnsetOperatorCASecretName)
			common.SetOperatorNameSpace(svcNs)
			DeferCleanup(common.UnsetOperatorNameSpace)

			// Create operator CA secret
			opCASecret := &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{
					Name:      "op-ca",
					Namespace: svcNs,
				},
				Data: map[string][]byte{"ca.pem": []byte("test-ca")},
			}

			// Create BrokerService with tier=premium label
			svc := &v1beta2.BrokerService{
				ObjectMeta: v1.ObjectMeta{
					Name:      svcName,
					Namespace: svcNs,
					Labels: map[string]string{
						"tier": "premium", // Service has "premium" tier
					},
				},
				Spec: v1beta2.BrokerServiceSpec{
					AppSelectorExpression: "true", // CEL allows all (so only label check matters)
				},
			}

			// Create BrokerApp that selects tier=basic (NOT premium)
			// But has manually set annotation pointing to premium-broker
			app := &v1beta2.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      appName,
					Namespace: appNs,
				},
				Spec: v1beta2.BrokerAppSpec{
					ServiceSelector: &v1.LabelSelector{
						MatchLabels: map[string]string{
							"tier": "basic", // App wants BASIC, not premium!
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

			// Setup fake client
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc, app, opCASecret).
				WithStatusSubresource(svc, app).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
					app := obj.(*v1beta2.BrokerApp)
					if app.Status.Service != nil {
						return []string{app.Status.Service.Key()}
					}
					return nil
				}).
				Build()

			// Create BrokerService Reconciler
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			// Reconcile the service
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// Verify that the Secret exists but app is NOT provisioned
			secret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{
				Name:      svc.Name + "-app-bp",
				Namespace: svcNs,
			}, secret)
			Expect(err).NotTo(HaveOccurred())

			if err == nil {
				// Secret should exist but should be EMPTY
				provisionedApps, hasAnnotation := secret.Annotations[common.ProvisionedAppsAnnotation]
				if hasAnnotation {
					Expect(provisionedApps).To(BeEmpty())
				}

				// Check that no acceptor config was created
				acceptorKey := appNs + "-" + appName + "-acceptor.properties"
				_, hasAcceptorConfig := secret.Data[acceptorKey]
				Expect(hasAcceptorConfig).To(BeFalse())
			}

			// Verify the service status does NOT include the app in provisioned apps
			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			// Status should show 0 provisioned apps
			Expect(len(updatedSvc.Status.ProvisionedApps)).To(Equal(0))

			// Verify that the app appears in RejectedApps with the correct reason
			Expect(updatedSvc.Status.RejectedApps).To(HaveLen(1))
			if len(updatedSvc.Status.RejectedApps) > 0 {
				rejected := updatedSvc.Status.RejectedApps[0]
				Expect(rejected.Name).To(Equal(appName))
				Expect(rejected.Namespace).To(Equal(appNs))
				Expect(rejected.Reason).To(Equal("does not match service labels"))
			}
		})
		It("should allow mixed apps", func() {
			// Setup scheme
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)

			// Data
			svcNs := "broker-services"
			svcName := "premium-broker"

			// Setup operator environment
			common.SetOperatorCASecretName("op-ca")
			DeferCleanup(common.UnsetOperatorCASecretName)
			common.SetOperatorNameSpace(svcNs)
			DeferCleanup(common.UnsetOperatorNameSpace)

			// Create operator CA secret
			opCASecret := &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{
					Name:      "op-ca",
					Namespace: svcNs,
				},
				Data: map[string][]byte{"ca.pem": []byte("test-ca")},
			}

			// Create BrokerService
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

			// Create multiple apps - some matching, some not
			matchingApp1 := &v1beta2.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      "app1",
					Namespace: "team-a", // Matches
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
					Namespace: "team-b", // Matches
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
					Namespace: "other-namespace", // Does NOT match
				},
				Status: v1beta2.BrokerAppStatus{
					Service: &v1beta2.BrokerServiceBindingStatus{ // Manually set!
						Name:      svcName,
						Namespace: svcNs,
						Secret:    "binding-secret",
					},
				},
				Spec: v1beta2.BrokerAppSpec{},
			}

			// Setup fake client
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc, matchingApp1, matchingApp2, attackerApp, opCASecret).
				WithStatusSubresource(svc, matchingApp1, matchingApp2, attackerApp).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
					app := obj.(*v1beta2.BrokerApp)
					if app.Status.Service != nil {
						return []string{app.Status.Service.Key()}
					}
					return nil
				}).
				Build()

			// Create BrokerService Reconciler
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			// Reconcile the service
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// Verify that the Secret only includes matching apps
			secret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{
				Name:      svc.Name + "-app-bp",
				Namespace: svcNs,
			}, secret)
			Expect(err).NotTo(HaveOccurred())

			if err == nil {
				// Should have configs for app1 and app2 but NOT attacker-app
				_, hasApp1Config := secret.Data["team-a-app1-acceptor.properties"]
				Expect(hasApp1Config).To(BeTrue())

				_, hasApp2Config := secret.Data["team-b-app2-acceptor.properties"]
				Expect(hasApp2Config).To(BeTrue())

				_, hasAttackerConfig := secret.Data["other-namespace-attacker-app-acceptor.properties"]
				Expect(hasAttackerConfig).To(BeFalse())

				// Check provisioned apps annotation
				provisionedApps, _ := secret.Annotations[common.ProvisionedAppsAnnotation]
				Expect(provisionedApps).To(ContainSubstring("app1"))
				Expect(provisionedApps).To(ContainSubstring("app2"))
				Expect(provisionedApps).NotTo(ContainSubstring("attacker-app"))
			}

			// Verify the service status shows rejected apps
			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			// Note: status.ProvisionedApps is only populated when broker deployment reports
			// applied configs via ExternalConfigs, which doesn't happen in unit tests.
			// Provisioned apps are verified above via the secret annotation.

			// Should have 1 rejected app
			Expect(updatedSvc.Status.RejectedApps).To(HaveLen(1))
			if len(updatedSvc.Status.RejectedApps) > 0 {
				rejected := updatedSvc.Status.RejectedApps[0]
				Expect(rejected.Name).To(Equal("attacker-app"))
				Expect(rejected.Namespace).To(Equal("other-namespace"))
				Expect(rejected.Reason).To(Equal("does not match appSelectorExpression"))
			}
		})
		It("should reject apps from prometheus config", func() {
			// Setup scheme
			scheme := runtime.NewScheme()
			_ = v1beta2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			_ = networkingv1.AddToScheme(scheme)

			// Data
			svcNs := "broker-services"
			svcName := "metrics-broker"
			allowedNs := "trusted-team"
			attackerNs := "untrusted-team"

			// Setup operator environment
			common.SetOperatorCASecretName("op-ca")
			DeferCleanup(common.UnsetOperatorCASecretName)
			common.SetOperatorNameSpace(svcNs)
			DeferCleanup(common.UnsetOperatorNameSpace)

			// Create operator CA secret
			opCASecret := &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{
					Name:      "op-ca",
					Namespace: svcNs,
				},
				Data: map[string][]byte{"ca.pem": []byte("test-ca")},
			}

			// Create BrokerService that only allows "trusted-team" namespace
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

			// Create VALID app from allowed namespace with ConsumerOf queues
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

			// Create ATTACKER app from untrusted namespace with manually set status binding
			// This app should be REJECTED and should NOT leak its ConsumerOf addresses
			attackerApp := &v1beta2.BrokerApp{
				ObjectMeta: v1.ObjectMeta{
					Name:      "attacker-app",
					Namespace: attackerNs,
				},
				Status: v1beta2.BrokerAppStatus{
					Service: &v1beta2.BrokerServiceBindingStatus{ // SECURITY: Manually set to bypass selector
						Name:      svcName,
						Namespace: svcNs,
						Secret:    "binding-secret",
					},
				},
				Spec: v1beta2.BrokerAppSpec{
					Capabilities: []v1beta2.AppCapabilityType{
						{
							ConsumerOf: []v1beta2.AddressRef{
								// SENSITIVE: These should NOT appear in Prometheus config
								{Address: "ATTACKER.SECRET.QUEUE"},
								{Address: "ATTACKER.RECON.QUEUE"},
							},
						},
					},
				},
			}

			// Setup fake client
			cl := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(svc, validApp, attackerApp, opCASecret).
				WithStatusSubresource(svc, validApp, attackerApp).
				WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
					app := obj.(*v1beta2.BrokerApp)
					if app.Status.Service != nil {
						return []string{app.Status.Service.Key()}
					}
					return nil
				}).
				Build()

			// Create BrokerService Reconciler
			r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

			// Reconcile the service
			req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: svcNs}}
			_, err := r.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())

			// Verify the control-plane-override secret exists
			overrideSecretName := svcName + "-control-plane-override"
			overrideSecret := &corev1.Secret{}
			err = cl.Get(context.TODO(), types.NamespacedName{
				Name:      overrideSecretName,
				Namespace: svcNs,
			}, overrideSecret)
			Expect(err).NotTo(HaveOccurred())

			// Verify Prometheus config exists
			prometheusYaml, ok := overrideSecret.Data["_prometheus_exporter.yaml"]
			Expect(ok).To(BeTrue())
			Expect(prometheusYaml).NotTo(BeEmpty())

			prometheusConfig := string(prometheusYaml)

			// CRITICAL SECURITY CHECKS:
			// Valid app's queues SHOULD be in the config
			Expect(prometheusConfig).To(ContainSubstring("VALID.QUEUE.ONE"))
			Expect(prometheusConfig).To(ContainSubstring("VALID.QUEUE.TWO"))

			// Attacker app's queues SHOULD NOT be in the config (security boundary)
			Expect(prometheusConfig).NotTo(ContainSubstring("ATTACKER.SECRET.QUEUE"))
			Expect(prometheusConfig).NotTo(ContainSubstring("ATTACKER.RECON.QUEUE"))

			// Verify the attacker app was properly rejected in status
			updatedSvc := &v1beta2.BrokerService{}
			err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
			Expect(err).NotTo(HaveOccurred())

			// Should have 1 rejected app
			Expect(updatedSvc.Status.RejectedApps).To(HaveLen(1))
			if len(updatedSvc.Status.RejectedApps) > 0 {
				rejected := updatedSvc.Status.RejectedApps[0]
				Expect(rejected.Name).To(Equal("attacker-app"))
				Expect(rejected.Namespace).To(Equal(attackerNs))
				Expect(rejected.Reason).To(Equal("does not match appSelectorExpression"))
			}
		})
	})
})
