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
	"testing"
	"time"

	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
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

// setupBrokerAppIndexer adds the status.service field indexer to avoid duplication in tests
func setupBrokerAppIndexer(builder *fake.ClientBuilder) *fake.ClientBuilder {
	return builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(obj client.Object) []string {
		app := obj.(*v1beta2.BrokerApp)
		if app.Status.Service != nil {
			return []string{app.Status.Service.Key()}
		}
		return nil
	})
}

func TestBrokerServiceReconcileWithAppMove(t *testing.T) {
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
	t.Cleanup(common.UnsetOperatorCASecretName)

	common.SetOperatorNameSpace(ns)
	t.Cleanup(common.UnsetOperatorNameSpace)

	// Create namespace object (required for CEL evaluation)
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
	s2 := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s2Name,
			Namespace: ns,
			UID:       types.UID("uid-s2"),
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

	// Setup fake client with indexer
	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(namespace, oc, s1, s2, app).WithStatusSubresource(s1, s2, app)
	builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
		app := rawObj.(*v1beta2.BrokerApp)
		if app.Status.Service != nil {
			return []string{app.Status.Service.Key()}
		}
		return nil
	})

	cl := builder.Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile S1
	reqS1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s1Name, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), reqS1)
	assert.NoError(t, err)

	// Check S1 secret has app config
	secretS1 := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: AppPropertiesSecretName(s1Name), Namespace: ns}, secretS1)
	assert.NoError(t, err)
	// Check for some key related to the app
	assert.True(t, hasKeyContaining(secretS1.Data, appName), "S1 secret should contain app config")

	// Move App to S2
	err = cl.Get(context.TODO(), types.NamespacedName{Name: appName, Namespace: ns}, app)
	assert.NoError(t, err)
	app.Status.Service = &v1beta2.BrokerServiceBindingStatus{
		Name:         s2Name,
		Namespace:    ns,
		Secret:       "app-binding-secret",
		AssignedPort: 61617,
	}
	assert.NoError(t, cl.Status().Update(context.TODO(), app))

	// Reconcile S1 (should remove app)
	_, err = r.Reconcile(context.TODO(), reqS1)
	assert.NoError(t, err)

	err = cl.Get(context.TODO(), types.NamespacedName{Name: AppPropertiesSecretName(s1Name), Namespace: ns}, secretS1)
	assert.NoError(t, err)
	assert.False(t, hasKeyContaining(secretS1.Data, appName), "S1 secret should NOT contain app config after move")

	// Reconcile S2 (should add app)
	reqS2 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s2Name, Namespace: ns}}
	_, err = r.Reconcile(context.TODO(), reqS2)
	assert.NoError(t, err)

	secretS2 := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: AppPropertiesSecretName(s2Name), Namespace: ns}, secretS2)
	assert.NoError(t, err)
	assert.True(t, hasKeyContaining(secretS2.Data, appName), "S2 secret should contain app config after move")
}

func hasKeyContaining(data map[string][]byte, substring string) bool {
	for k := range data {
		if strings.Contains(k, substring) {
			return true
		}
	}
	return false
}

func TestBrokerServiceReconcileErrorPropagation(t *testing.T) {
	// Setup scheme
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

	// Setup fake client with interceptor to fail List
	interceptorFuncs := interceptor.Funcs{
		List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*corev1.SecretList); ok {
				return fmt.Errorf("simulated list error")
			}
			return client.List(ctx, list, opts...)
		},
	}

	// Setup fake client with indexer
	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(s1).WithStatusSubresource(s1).WithInterceptorFuncs(interceptorFuncs)
	builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
		app := rawObj.(*v1beta2.BrokerApp)
		if app.Status.Service != nil {
			return []string{app.Status.Service.Key()}
		}
		return nil
	})

	cl := builder.Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile S1
	reqS1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s1Name, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), reqS1)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "simulated list error")

	err = cl.Get(context.TODO(), reqS1.NamespacedName, s1)
	assert.Nil(t, err)

	// Verify condition states
	assert.True(t, meta.IsStatusConditionPresentAndEqual(s1.Status.Conditions, v1beta2.DeployedConditionType, metav1.ConditionUnknown))
	assert.True(t, meta.IsStatusConditionFalse(s1.Status.Conditions, v1beta2.ReadyConditionType))

	// Valid should be True even when runtime errors occur (spec is valid)
	validCond := meta.FindStatusCondition(s1.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, metav1.ConditionTrue, validCond.Status)
	assert.Equal(t, v1beta2.ValidConditionSuccessReason, validCond.Reason)
}

func TestBrokerServiceReconcileStatusUpdateFailure(t *testing.T) {
	// Setup scheme
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

	// Setup fake client with interceptor to fail Status Update
	interceptorFuncs := interceptor.Funcs{
		SubResourceUpdate: func(ctx context.Context, client client.Client, subResourceName string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
			return fmt.Errorf("simulated status update error")
		},
	}

	// Setup fake client with indexer
	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(s1).WithStatusSubresource(s1).WithInterceptorFuncs(interceptorFuncs)
	builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
		return nil
	})

	cl := builder.Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile S1
	reqS1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s1Name, Namespace: ns}}
	result, err := r.Reconcile(context.TODO(), reqS1)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "simulated status update error")
	assert.Equal(t, time.Duration(0), result.RequeueAfter)
}

func TestBrokerServiceReconcileRequiresIndex(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
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

	// Setup fake client WITHOUT indexer
	// This simulates what happens if SetupWithManager doesn't register the indexer
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(s1, app).WithStatusSubresource(s1, app).Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile S1
	reqS1 := ctrl.Request{NamespacedName: types.NamespacedName{Name: s1Name, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), reqS1)

	// Expect error because List with MatchingFields requires an index in the fake client (and real client)
	assert.Error(t, err)
	// The error message from controller-runtime fake client when index is missing
	assert.Contains(t, err.Error(), "index")
}

func TestReconcileDeployedConditionTransition(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	// Data
	ns := "default"
	svcName := "my-broker-service"

	// Create BrokerService
	svc := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
		},
		Status: v1beta2.BrokerServiceStatus{},
	}

	// Setup fake client with indexer required by controller
	builder := fake.NewClientBuilder().WithScheme(scheme).WithObjects(svc).WithStatusSubresource(svc, &v1beta2.Broker{})
	builder.WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
		app := rawObj.(*v1beta2.BrokerApp)
		if app.Status.Service != nil {
			return []string{app.Status.Service.Key()}
		}
		return nil
	})
	cl := builder.Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// 1. Reconcile - should create broker but it won't be ready
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify Deployed condition is False (NotReady)
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, metav1.ConditionFalse, deployedCond.Status)
	assert.Equal(t, v1beta2.DeployedConditionNotReadyReason, deployedCond.Reason)
	creationTime := deployedCond.LastTransitionTime

	// Wait a bit to ensure time difference
	time.Sleep(1 * time.Second)

	// 2. Update underlying Broker to be Ready
	brokerCR := &v1beta2.Broker{}
	err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
	assert.NoError(t, err)

	// Update status of brokerCR
	meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
		Type:   v1beta2.ReadyConditionType,
		Status: metav1.ConditionTrue,
	})
	meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
		Type:   v1beta2.DeployedConditionType,
		Status: metav1.ConditionTrue,
	})
	err = cl.Status().Update(context.TODO(), brokerCR)
	assert.NoError(t, err)

	// Create StatefulSet (simulating Broker controller)
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
	assert.NoError(t, err)

	// Reconcile again
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify Deployed condition is True and time updated
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	deployedCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, metav1.ConditionTrue, deployedCond.Status)
	assert.Equal(t, v1beta2.ReadyConditionReason, deployedCond.Reason)
	assert.True(t, deployedCond.LastTransitionTime.After(creationTime.Time))
}

func TestBrokerServiceReconcileStatusAppliedApps(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	svcName := "my-service"
	appName := "my-app"

	common.SetOperatorCASecretName("op_ca")
	t.Cleanup(common.UnsetOperatorCASecretName)

	common.SetOperatorNameSpace(ns)
	t.Cleanup(common.UnsetOperatorNameSpace)

	// Create namespace object (required for CEL evaluation)
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

	// BrokerService
	svc := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerServiceSpec{
			Image: StringToPtr("placeholder"),
		},
	}

	// BrokerApp
	app := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerAppSpec{},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         svcName,
				Namespace:    ns,
				Secret:       "binding-secret",
				AssignedPort: 61616,
			},
		},
	}

	// Setup fake client
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, oc, svc, app).
		WithStatusSubresource(svc, &v1beta2.Broker{}).
		WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
			app := rawObj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

	// 1. First Reconcile: Creates resources
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify BrokerService status is not yet populated with AppliedApps
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)
	assert.Empty(t, updatedSvc.Status.ProvisionedApps)

	// 2. Get generated Secret and its ResourceVersion
	secretName := AppPropertiesSecretName(svcName)
	secret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
	assert.NoError(t, err)
	assert.NotEmpty(t, secret.ResourceVersion)
	// Verify annotation is present on the secret
	assert.Equal(t, fmt.Sprintf("%s-%s", ns, appName), secret.Annotations[common.ProvisionedAppsAnnotation])

	// 3. Update Broker status to simulate broker picking up the config
	brokerCR := &v1beta2.Broker{}
	err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
	assert.NoError(t, err)

	brokerCR.Status.Conditions = []metav1.Condition{
		{
			Type:   v1beta2.ReadyConditionType,
			Status: metav1.ConditionTrue,
			Reason: "Ready",
		},
	}
	brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
		{
			Name:            secretName,
			ResourceVersion: secret.ResourceVersion,
		},
	}
	err = cl.Status().Update(context.TODO(), brokerCR)
	assert.NoError(t, err)

	// 4. Second Reconcile: Should update AppliedApps
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify BrokerService status
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)
	assert.Equal(t, []string{fmt.Sprintf("%s-%s", ns, appName)}, updatedSvc.Status.ProvisionedApps)
}

func TestBrokerServiceReconcileStatusAppliedAppsIncremental(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	svcName := "my-service"
	app1Name := "my-app-1"
	app2Name := "my-app-2"

	common.SetOperatorCASecretName("op_ca")
	t.Cleanup(common.UnsetOperatorCASecretName)

	common.SetOperatorNameSpace(ns)
	t.Cleanup(common.UnsetOperatorNameSpace)

	// Create namespace object (required for CEL evaluation)
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

	// BrokerService
	svc := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerServiceSpec{
			Image: StringToPtr("placeholder"),
		},
	}

	// BrokerApp 1
	app1 := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app1Name,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerAppSpec{},
		Status: v1beta2.BrokerAppStatus{
			Service: &v1beta2.BrokerServiceBindingStatus{
				Name:         svcName,
				Namespace:    ns,
				Secret:       "binding-secret",
				AssignedPort: 61616,
			},
		},
	}

	// Setup fake client
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, oc, svc, app1).
		WithStatusSubresource(svc, &v1beta2.Broker{}).
		WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
			app := rawObj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

	// 1. Reconcile with App1
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Get generated Secret (v1)
	secretName := AppPropertiesSecretName(svcName)
	secret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
	assert.NoError(t, err)
	secretV1 := secret.ResourceVersion

	// Update Broker Status to point to Secret v1
	brokerCR := &v1beta2.Broker{}
	err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
	assert.NoError(t, err)
	brokerCR.Status.Conditions = []metav1.Condition{
		{
			Type:   v1beta2.ReadyConditionType,
			Status: metav1.ConditionTrue,
			Reason: "Ready",
		},
	}
	brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
		{
			Name:            secretName,
			ResourceVersion: secretV1,
		},
	}
	err = cl.Status().Update(context.TODO(), brokerCR)
	assert.NoError(t, err)

	// Reconcile again to update AppliedApps
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify AppliedApps has App1
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)
	assert.Equal(t, []string{fmt.Sprintf("%s-%s", ns, app1Name)}, updatedSvc.Status.ProvisionedApps)

	// 2. Add App2
	app2 := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app2Name,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerAppSpec{},
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
	assert.NoError(t, err)

	// Reconcile to pick up App2. This updates the Secret to v2.
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify Secret is updated
	err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
	assert.NoError(t, err)
	assert.NotEqual(t, secretV1, secret.ResourceVersion)
	secretV2 := secret.ResourceVersion

	// Verify AppliedApps STILL has App1 (and not App2 yet, because Broker Status still points to v1)
	// IMPORTANT: It should NOT be empty.
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)
	assert.Equal(t, []string{fmt.Sprintf("%s-%s", ns, app1Name)}, updatedSvc.Status.ProvisionedApps)

	// 3. Update Broker Status to point to Secret v2
	err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
	assert.NoError(t, err)
	brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
		{
			Name:            secretName,
			ResourceVersion: secretV2,
		},
	}
	err = cl.Status().Update(context.TODO(), brokerCR)
	assert.NoError(t, err)

	// Reconcile again
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify AppliedApps has App1 and App2
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)
	expectedApps := []string{fmt.Sprintf("%s-%s", ns, app1Name), fmt.Sprintf("%s-%s", ns, app2Name)}
	sort.Strings(expectedApps)
	sort.Strings(updatedSvc.Status.ProvisionedApps)
	assert.Equal(t, expectedApps, updatedSvc.Status.ProvisionedApps)
}

func TestBrokerServiceReconcileAppsProvisionedCondition(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	svcName := "my-service"

	// BrokerService
	svc := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerServiceSpec{
			Image: StringToPtr("placeholder"),
		},
	}

	// Setup fake client
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc).
		WithStatusSubresource(svc, &v1beta2.Broker{}).
		WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
			app := rawObj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).
		Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

	// 1. First Reconcile: Creates resources
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify AppsProvisioned condition is False (WaitingForBroker)
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	cond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.AppsProvisionedConditionType)
	assert.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionFalse, cond.Status)
	assert.Equal(t, v1beta2.AppsProvisionedConditionWaitingReason, cond.Reason)

	// 2. Get generated Secret and its ResourceVersion
	secretName := AppPropertiesSecretName(svcName)
	secret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
	assert.NoError(t, err)
	assert.NotEmpty(t, secret.ResourceVersion)

	// 3. Update Broker status to simulate broker picking up the config
	brokerCR := &v1beta2.Broker{}
	err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
	assert.NoError(t, err)

	brokerCR.Status.Conditions = []metav1.Condition{
		{
			Type:   v1beta2.ReadyConditionType,
			Status: metav1.ConditionTrue,
			Reason: "Ready",
		},
	}
	brokerCR.Status.ExternalConfigs = []v1beta2.ExternalConfigStatus{
		{
			Name:            secretName,
			ResourceVersion: secret.ResourceVersion,
		},
	}
	err = cl.Status().Update(context.TODO(), brokerCR)
	assert.NoError(t, err)

	currentSecretResourceVersion := secret.ResourceVersion

	// 4. Second Reconcile: Should update AppsProvisioned
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// verify no resource version change, still no apps
	err = cl.Get(context.TODO(), types.NamespacedName{Name: secretName, Namespace: ns}, secret)
	assert.NoError(t, err)
	assert.Equal(t, currentSecretResourceVersion, secret.ResourceVersion)

	// Verify AppsProvisioned condition is True
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	cond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.AppsProvisionedConditionType)
	assert.NotNil(t, cond)
	assert.Equal(t, metav1.ConditionTrue, cond.Status)
	assert.Equal(t, v1beta2.AppsProvisionedConditionSyncedReason, cond.Reason)
}

func TestBrokerServiceReconcilePrometheusOverrideSecret(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	svcName := "my-service"
	appName := "metrics-app"

	common.SetOperatorCASecretName("op_ca")
	t.Cleanup(common.UnsetOperatorCASecretName)

	common.SetOperatorNameSpace(ns)
	t.Cleanup(common.UnsetOperatorNameSpace)

	// Create namespace object (required for CEL evaluation)
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

	// BrokerService
	svc := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerServiceSpec{
			Image: StringToPtr("placeholder"),
		},
	}

	// BrokerApp with ConsumerOf queues
	app := &v1beta2.BrokerApp{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appName,
			Namespace: ns,
		},
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

	// Setup fake client
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, oc, svc, app).
		WithStatusSubresource(svc, &v1beta2.Broker{}).
		WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
			app := rawObj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

	// Reconcile
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify control-plane-override secret exists
	overrideSecretName := svcName + "-control-plane-override"
	overrideSecret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: overrideSecretName, Namespace: ns}, overrideSecret)
	assert.NoError(t, err, "control-plane-override secret should exist")

	// Verify prometheus config exists in the secret
	prometheusYaml, ok := overrideSecret.Data["_prometheus_exporter.yaml"]
	assert.True(t, ok, "should have _prometheus_exporter.yaml key")
	assert.NotEmpty(t, prometheusYaml)

	prometheusConfig := string(prometheusYaml)

	// Verify it includes queue-level object names
	assert.Contains(t, prometheusConfig, "org.apache.activemq.artemis:broker=*,component=addresses,address=*,subcomponent=queues,routing-type=*,queue=*")

	// Verify it includes specific queues from ConsumerOf
	assert.Contains(t, prometheusConfig, "TEST.QUEUE.ONE")
	assert.Contains(t, prometheusConfig, "TEST.QUEUE.TWO")

	// Verify it includes queue-level attributes
	assert.Contains(t, prometheusConfig, "MessageCount")
	assert.Contains(t, prometheusConfig, "ConsumerCount")
	assert.Contains(t, prometheusConfig, "DeliveringCount")
}

func TestBrokerServiceReconcilePrometheusOverrideNoApps(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	// Data
	ns := "default"
	svcName := "my-service"

	common.SetOperatorCASecretName("op_ca")
	t.Cleanup(common.UnsetOperatorCASecretName)

	common.SetOperatorNameSpace(ns)
	t.Cleanup(common.UnsetOperatorNameSpace)

	// Create namespace object (required for CEL evaluation)
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

	// BrokerService (no apps)
	svc := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
		},
		Spec: v1beta2.BrokerServiceSpec{
			Image: StringToPtr("placeholder"),
		},
	}

	// Setup fake client
	cl := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, oc, svc).
		WithStatusSubresource(svc, &v1beta2.Broker{}).
		WithIndex(&v1beta2.BrokerApp{}, common.AppServiceBindingField, func(rawObj client.Object) []string {
			app := rawObj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

	// Reconcile
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify control-plane-override secret exists
	overrideSecretName := svcName + "-control-plane-override"
	overrideSecret := &corev1.Secret{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: overrideSecretName, Namespace: ns}, overrideSecret)
	assert.NoError(t, err, "control-plane-override secret should exist even without apps")

	// Verify prometheus config exists with queue-level metrics
	prometheusYaml, ok := overrideSecret.Data["_prometheus_exporter.yaml"]
	assert.True(t, ok, "should have _prometheus_exporter.yaml key")
	assert.NotEmpty(t, prometheusYaml)

	prometheusConfig := string(prometheusYaml)

	// Should have queue-level object names even without apps
	assert.Contains(t, prometheusConfig, "component=addresses,address=*,subcomponent=queues")
}

func TestBrokerServiceValidCondition(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	ns := "default"
	svcName := "my-broker-service"

	t.Run("sets Valid=True for valid spec", func(t *testing.T) {
		// Create BrokerService with valid spec
		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: ns,
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

		// Setup fake client with field indexer using helper
		cl := setupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(svc).
			WithStatusSubresource(svc)).
			Build()

		// Create Reconciler
		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// Reconcile
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		assert.NoError(t, err)

		// Verify Status
		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		assert.NoError(t, err)

		// Check Valid condition
		validCondition := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
		assert.NotNil(t, validCondition)
		assert.Equal(t, metav1.ConditionTrue, validCondition.Status)
		assert.Equal(t, v1beta2.ValidConditionSuccessReason, validCondition.Reason)
	})

	t.Run("sets Valid=False for invalid resource name", func(t *testing.T) {
		// Create BrokerService with invalid name (contains invalid characters)
		invalidName := "broker/service"
		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      invalidName,
				Namespace: ns,
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

		// Setup fake client with field indexer using helper
		cl := setupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(svc).
			WithStatusSubresource(svc)).
			Build()

		// Create Reconciler
		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// Reconcile
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: invalidName, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		assert.Error(t, err)

		// Verify Status
		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		assert.NoError(t, err)

		// Check Valid condition
		validCondition := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
		assert.NotNil(t, validCondition)
		assert.Equal(t, metav1.ConditionFalse, validCondition.Status)
		assert.Equal(t, v1beta2.ValidConditionInvalidResourceName, validCondition.Reason)
		assert.NotEmpty(t, validCondition.Message)
	})

	t.Run("Valid condition persists across reconciles", func(t *testing.T) {
		// Create BrokerService
		svc := &v1beta2.BrokerService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName + "-persist",
				Namespace: ns,
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

		// Setup fake client with field indexer using helper
		cl := setupBrokerAppIndexer(fake.NewClientBuilder().
			WithScheme(scheme).
			WithObjects(svc).
			WithStatusSubresource(svc)).
			Build()

		// Create Reconciler
		r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

		// First reconcile
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svc.Name, Namespace: ns}}
		_, err := r.Reconcile(context.TODO(), req)
		assert.NoError(t, err)

		// Get first Valid condition
		updatedSvc := &v1beta2.BrokerService{}
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		assert.NoError(t, err)
		validCondition1 := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
		assert.NotNil(t, validCondition1)
		assert.Equal(t, metav1.ConditionTrue, validCondition1.Status)

		// Second reconcile
		_, err = r.Reconcile(context.TODO(), req)
		assert.NoError(t, err)

		// Get second Valid condition
		err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
		assert.NoError(t, err)
		validCondition2 := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
		assert.NotNil(t, validCondition2)
		assert.Equal(t, metav1.ConditionTrue, validCondition2.Status)

		// LastTransitionTime should not change when status stays the same
		assert.Equal(t, validCondition1.LastTransitionTime, validCondition2.LastTransitionTime)
	})
}

func TestBrokerServiceIdempotentStatus(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	ns := "default"
	svcName := "test-service"

	// Create BrokerService
	svc := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
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

	// Setup fake client with indexer
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc).
		WithStatusSubresource(svc)).
		Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// First reconcile
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Get first status
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)
	firstStatus := updatedSvc.Status.DeepCopy()

	// Second reconcile with no changes
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Get second status
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)
	secondStatus := updatedSvc.Status

	// Status should be identical (idempotent)
	assert.Equal(t, firstStatus.Conditions, secondStatus.Conditions)

	// Verify all expected conditions are present
	assert.NotNil(t, meta.FindStatusCondition(secondStatus.Conditions, v1beta2.ValidConditionType))
	assert.NotNil(t, meta.FindStatusCondition(secondStatus.Conditions, v1beta2.DeployedConditionType))
	assert.NotNil(t, meta.FindStatusCondition(secondStatus.Conditions, v1beta2.AppsProvisionedConditionType))
	assert.NotNil(t, meta.FindStatusCondition(secondStatus.Conditions, v1beta2.ReadyConditionType))
}

func TestBrokerServiceConditionIndependence(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	ns := "default"
	svcName := "test-service"

	// Create BrokerService
	svc := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
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

	// Setup fake client with indexer
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc).
		WithStatusSubresource(svc)).
		Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Get status
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	// Verify Valid is independent of other conditions
	// Valid=True (spec is correct)
	validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, metav1.ConditionTrue, validCond.Status)

	// Deployed=False (no broker pod yet)
	deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, metav1.ConditionFalse, deployedCond.Status)

	// AppsProvisioned=False (waiting for broker)
	appsCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.AppsProvisionedConditionType)
	assert.NotNil(t, appsCond)
	assert.Equal(t, metav1.ConditionFalse, appsCond.Status)

	// Ready=False (not all conditions met)
	readyCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ReadyConditionType)
	assert.NotNil(t, readyCond)
	assert.Equal(t, metav1.ConditionFalse, readyCond.Status)

	// This demonstrates that Valid condition is independent:
	// Valid can be True while Deployed, AppsProvisioned, and Ready are False
}

func TestBrokerServiceValidPersistsThroughRuntimeErrors(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	ns := "default"
	svcName := "test-service"

	// Create BrokerService with valid spec
	svc := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
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

	// Setup fake client with interceptor to fail List operations
	interceptorFuncs := interceptor.Funcs{
		List: func(ctx context.Context, client client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*corev1.SecretList); ok {
				return fmt.Errorf("simulated API list error")
			}
			return client.List(ctx, list, opts...)
		},
	}

	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc).
		WithStatusSubresource(svc).
		WithInterceptorFuncs(interceptorFuncs)).
		Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	// Reconcile - will fail due to API error
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.Error(t, err)

	// Verify Valid=True persists even with runtime errors
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	// Valid should be True (spec is valid)
	validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, metav1.ConditionTrue, validCond.Status)
	assert.Equal(t, v1beta2.ValidConditionSuccessReason, validCond.Reason)

	// Deployed should be Unknown (runtime error)
	deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, metav1.ConditionUnknown, deployedCond.Status)
	assert.Equal(t, v1beta2.DeployedConditionCrudKindErrorReason, deployedCond.Reason)
}

func TestBrokerServiceConditionTransitionsOnRecovery(t *testing.T) {
	// Setup scheme
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	ns := "default"
	svcName := "test-service"

	// Create BrokerService
	svc := &v1beta2.BrokerService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      svcName,
			Namespace: ns,
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

	// Setup fake client
	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc).
		WithStatusSubresource(svc, &v1beta2.Broker{})).
		Build()

	// Create Reconciler
	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

	// 1. First reconcile - broker not ready
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	// Verify initial state
	validCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, metav1.ConditionTrue, validCond.Status)

	deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, metav1.ConditionFalse, deployedCond.Status)
	assert.Equal(t, v1beta2.DeployedConditionNotReadyReason, deployedCond.Reason)

	// 2. Update Broker to be ready and create StatefulSet
	brokerCR := &v1beta2.Broker{}
	err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
	assert.NoError(t, err)

	brokerCR.Status.Conditions = []metav1.Condition{
		{
			Type:   v1beta2.ReadyConditionType,
			Status: metav1.ConditionTrue,
		},
		{
			Type:   v1beta2.DeployedConditionType,
			Status: metav1.ConditionTrue,
		},
	}
	err = cl.Status().Update(context.TODO(), brokerCR)
	assert.NoError(t, err)

	// Create StatefulSet (simulating Broker controller)
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
	assert.NoError(t, err)

	// 3. Reconcile again
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	// Verify Valid stayed True
	validCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.ValidConditionType)
	assert.NotNil(t, validCond)
	assert.Equal(t, metav1.ConditionTrue, validCond.Status)

	// Verify Deployed transitioned to True
	deployedCond = meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, metav1.ConditionTrue, deployedCond.Status)
	assert.Equal(t, v1beta2.ReadyConditionReason, deployedCond.Reason)
}
