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
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
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

// TestBrokerServiceRejectsManuallyAnnotatedApp verifies that the BrokerService controller
// does NOT provision apps that have manually set the Status.ServiceBinding to bypass
// the appSelectorExpression access control.
func TestBrokerServiceRejectsManuallyAnnotatedApp(t *testing.T) {
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
	assert.NoError(t, err)

	// Verify that the Secret was created (it should exist even with no apps)
	secret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      svc.Name + "-app-bp",
		Namespace: svcNs,
	}, secret)
	assert.NoError(t, err, "App properties secret should be created")

	// Secret should exist but should be EMPTY (no apps provisioned)
	// because the attacker's app doesn't match the selector
	if err == nil {
		// Check that the provisioned apps annotation is empty
		provisionedApps, hasAnnotation := secret.Annotations[common.ProvisionedAppsAnnotation]
		if hasAnnotation {
			assert.Empty(t, provisionedApps, "Should not provision apps that don't match selector")
		}

		// Check that no acceptor config was created for the attacker's app
		acceptorKey := attackerNs + "-" + appName + "-acceptor.properties"
		_, hasAcceptorConfig := secret.Data[acceptorKey]
		assert.False(t, hasAcceptorConfig, "Should not create acceptor config for unauthorized app")
	}

	// Verify the service status does NOT include the attacker's app in provisioned apps
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	// Status should show 0 provisioned apps
	assert.Equal(t, 0, len(updatedSvc.Status.ProvisionedApps),
		"Service should not provision apps that don't match selector")

	// CRITICAL: Verify that the app appears in RejectedApps with the correct reason
	// This proves that:
	// 1. The annotation was found (app was retrieved via the index)
	// 2. The label selector matched (app passed that check)
	// 3. But the appSelectorExpression rejected it (security working as intended)
	assert.Equal(t, 1, len(updatedSvc.Status.RejectedApps),
		"Should track one rejected app")
	if len(updatedSvc.Status.RejectedApps) > 0 {
		rejected := updatedSvc.Status.RejectedApps[0]
		assert.Equal(t, appName, rejected.Name, "Rejected app should be the malicious app")
		assert.Equal(t, attackerNs, rejected.Namespace, "Rejected app should be from attacker namespace")
		assert.Equal(t, "does not match appSelectorExpression", rejected.Reason,
			"Rejection reason should indicate appSelectorExpression mismatch")
	}

	// Verify the attacker app's annotation is still set (proves it was found, not just missed)
	verifyApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      appName,
		Namespace: attackerNs,
	}, verifyApp)
	assert.NoError(t, err)
	assert.NotNil(t, verifyApp.Status.Service)
	assert.Equal(t, svcName, verifyApp.Status.Service.Name)
	assert.Equal(t, svcNs, verifyApp.Status.Service.Namespace,
		"App status binding should still be set, proving it was found but rejected")
}

// TestBrokerServiceAllowsMatchingApp verifies that legitimate apps that match
// the selector ARE provisioned correctly.
func TestBrokerServiceAllowsMatchingApp(t *testing.T) {
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
	t.Cleanup(common.UnsetOperatorCASecretName)
	common.SetOperatorNameSpace(svcNs)
	t.Cleanup(common.UnsetOperatorNameSpace)

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
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
		},
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
	assert.NoError(t, err)

	// Verify that the Secret was created with the legitimate app
	secret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      svc.Name + "-app-bp",
		Namespace: svcNs,
	}, secret)
	assert.NoError(t, err, "App properties secret should be created")

	if err == nil {
		// Check that the provisioned apps annotation includes the legitimate app
		provisionedApps, hasAnnotation := secret.Annotations[common.ProvisionedAppsAnnotation]
		if hasAnnotation {
			assert.Contains(t, provisionedApps, appName, "Should provision apps that match selector")
		}

		// Check that acceptor config was created for the legitimate app
		acceptorKey := allowedNs + "-" + appName + "-acceptor.properties"
		_, hasAcceptorConfig := secret.Data[acceptorKey]
		assert.True(t, hasAcceptorConfig, "Should create acceptor config for authorized app")
	}

	// Verify the service status shows the app as provisioned (not rejected)
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	// Should have 0 rejected apps (legitimate app should not be rejected)
	assert.Equal(t, 0, len(updatedSvc.Status.RejectedApps),
		"Should not reject apps that match selector")
}

// TestBrokerServiceRejectsLabelMismatch verifies that apps with mismatched label selectors
// are NOT provisioned even if they have a manually set annotation.
func TestBrokerServiceRejectsLabelMismatch(t *testing.T) {
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
	t.Cleanup(common.UnsetOperatorCASecretName)
	common.SetOperatorNameSpace(svcNs)
	t.Cleanup(common.UnsetOperatorNameSpace)

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
	assert.NoError(t, err)

	// Verify that the Secret exists but app is NOT provisioned
	secret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      svc.Name + "-app-bp",
		Namespace: svcNs,
	}, secret)
	assert.NoError(t, err, "App properties secret should be created")

	if err == nil {
		// Secret should exist but should be EMPTY
		provisionedApps, hasAnnotation := secret.Annotations[common.ProvisionedAppsAnnotation]
		if hasAnnotation {
			assert.Empty(t, provisionedApps, "Should not provision app with mismatched label selector")
		}

		// Check that no acceptor config was created
		acceptorKey := appNs + "-" + appName + "-acceptor.properties"
		_, hasAcceptorConfig := secret.Data[acceptorKey]
		assert.False(t, hasAcceptorConfig, "Should not create acceptor config for app with mismatched labels")
	}

	// Verify the service status does NOT include the app in provisioned apps
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	// Status should show 0 provisioned apps
	assert.Equal(t, 0, len(updatedSvc.Status.ProvisionedApps),
		"Service should not provision app with mismatched label selector")

	// Verify that the app appears in RejectedApps with the correct reason
	assert.Equal(t, 1, len(updatedSvc.Status.RejectedApps),
		"Should track one rejected app")
	if len(updatedSvc.Status.RejectedApps) > 0 {
		rejected := updatedSvc.Status.RejectedApps[0]
		assert.Equal(t, appName, rejected.Name, "Rejected app should be the attacker app")
		assert.Equal(t, appNs, rejected.Namespace, "Rejected app should be from the app namespace")
		assert.Equal(t, "does not match service labels", rejected.Reason,
			"Rejection reason should indicate label selector mismatch")
	}
}

// TestBrokerServiceMixedApps verifies that when multiple apps have annotations,
// only those matching the selector are provisioned.
func TestBrokerServiceMixedApps(t *testing.T) {
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
	t.Cleanup(common.UnsetOperatorCASecretName)
	common.SetOperatorNameSpace(svcNs)
	t.Cleanup(common.UnsetOperatorNameSpace)

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
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
		},
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
	assert.NoError(t, err)

	// Verify that the Secret only includes matching apps
	secret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      svc.Name + "-app-bp",
		Namespace: svcNs,
	}, secret)
	assert.NoError(t, err, "App properties secret should be created")

	if err == nil {
		// Should have configs for app1 and app2 but NOT attacker-app
		_, hasApp1Config := secret.Data["team-a-app1-acceptor.properties"]
		assert.True(t, hasApp1Config, "Should provision matching app1")

		_, hasApp2Config := secret.Data["team-b-app2-acceptor.properties"]
		assert.True(t, hasApp2Config, "Should provision matching app2")

		_, hasAttackerConfig := secret.Data["other-namespace-attacker-app-acceptor.properties"]
		assert.False(t, hasAttackerConfig, "Should NOT provision non-matching attacker-app")

		// Check provisioned apps annotation
		provisionedApps, _ := secret.Annotations[common.ProvisionedAppsAnnotation]
		assert.Contains(t, provisionedApps, "app1")
		assert.Contains(t, provisionedApps, "app2")
		assert.NotContains(t, provisionedApps, "attacker-app")
	}

	// Verify the service status shows rejected apps
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	// Note: status.ProvisionedApps is only populated when broker deployment reports
	// applied configs via ExternalConfigs, which doesn't happen in unit tests.
	// Provisioned apps are verified above via the secret annotation.

	// Should have 1 rejected app
	assert.Equal(t, 1, len(updatedSvc.Status.RejectedApps),
		"Should track 1 rejected app")
	if len(updatedSvc.Status.RejectedApps) > 0 {
		rejected := updatedSvc.Status.RejectedApps[0]
		assert.Equal(t, "attacker-app", rejected.Name, "Rejected app should be attacker-app")
		assert.Equal(t, "other-namespace", rejected.Namespace, "Rejected app should be from other-namespace")
		assert.Equal(t, "does not match appSelectorExpression", rejected.Reason,
			"Rejection reason should indicate appSelectorExpression mismatch")
	}
}

// TestBrokerServiceRejectsAppsFromPrometheusConfig verifies that rejected apps
// do NOT leak their ConsumerOf addresses into the Prometheus configuration.
// This is a critical security test ensuring that the appSelectorExpression
// boundary is enforced not just for app provisioning but also for metrics config.
func TestBrokerServiceRejectsAppsFromPrometheusConfig(t *testing.T) {
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
	t.Cleanup(common.UnsetOperatorCASecretName)
	common.SetOperatorNameSpace(svcNs)
	t.Cleanup(common.UnsetOperatorNameSpace)

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
		Status: v1beta2.BrokerServiceStatus{
			AvailablePorts: &v1beta2.PortPoolInfo{
				Source: "Default",
				PortRange: &v1beta2.PortRange{
					Start: 61616,
					End:   62615,
				},
			},
		},
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
	assert.NoError(t, err)

	// Verify the control-plane-override secret exists
	overrideSecretName := svcName + "-control-plane-override"
	overrideSecret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{
		Name:      overrideSecretName,
		Namespace: svcNs,
	}, overrideSecret)
	assert.NoError(t, err, "control-plane-override secret should be created")

	// Verify Prometheus config exists
	prometheusYaml, ok := overrideSecret.Data["_prometheus_exporter.yaml"]
	assert.True(t, ok, "should have _prometheus_exporter.yaml key")
	assert.NotEmpty(t, prometheusYaml)

	prometheusConfig := string(prometheusYaml)

	// CRITICAL SECURITY CHECKS:
	// Valid app's queues SHOULD be in the config
	assert.Contains(t, prometheusConfig, "VALID.QUEUE.ONE",
		"Valid app's ConsumerOf addresses should be in Prometheus config")
	assert.Contains(t, prometheusConfig, "VALID.QUEUE.TWO",
		"Valid app's ConsumerOf addresses should be in Prometheus config")

	// Attacker app's queues SHOULD NOT be in the config (security boundary)
	assert.NotContains(t, prometheusConfig, "ATTACKER.SECRET.QUEUE",
		"SECURITY: Rejected app's ConsumerOf addresses should NOT leak into Prometheus config")
	assert.NotContains(t, prometheusConfig, "ATTACKER.RECON.QUEUE",
		"SECURITY: Rejected app's ConsumerOf addresses should NOT leak into Prometheus config")

	// Verify the attacker app was properly rejected in status
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	// Should have 1 rejected app
	assert.Equal(t, 1, len(updatedSvc.Status.RejectedApps),
		"Should track 1 rejected app")
	if len(updatedSvc.Status.RejectedApps) > 0 {
		rejected := updatedSvc.Status.RejectedApps[0]
		assert.Equal(t, "attacker-app", rejected.Name)
		assert.Equal(t, attackerNs, rejected.Namespace)
		assert.Equal(t, "does not match appSelectorExpression", rejected.Reason)
	}
}
