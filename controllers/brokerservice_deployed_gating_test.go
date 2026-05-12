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

// TestBrokerServiceDeployed_WhenBrokerNotReady verifies that BrokerService.Deployed
// remains False when the underlying Broker is not yet deployed.
func TestBrokerServiceDeployed_WhenBrokerNotReady(t *testing.T) {
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

	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc).
		WithStatusSubresource(svc, &v1beta2.Broker{})).
		Build()

	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

	// 1. First reconcile - creates Broker CR but it won't be deployed yet
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// Verify BrokerService.Deployed is False because Broker isn't deployed
	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, metav1.ConditionFalse, deployedCond.Status)
	assert.Equal(t, v1beta2.DeployedConditionNotReadyReason, deployedCond.Reason)

}

// TestBrokerServiceDeployed_AfterPortDiscovery verifies that BrokerService.Deployed
// becomes True only after port discovery completes.
func TestBrokerServiceDeployed_AfterPortDiscovery(t *testing.T) {
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

	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc).
		WithStatusSubresource(svc, &v1beta2.Broker{})).
		Build()

	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}

	// 1. First reconcile - creates Broker CR
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	// 2. Update Broker to Deployed=True
	brokerCR := &v1beta2.Broker{}
	err = cl.Get(context.TODO(), req.NamespacedName, brokerCR)
	assert.NoError(t, err)

	meta.SetStatusCondition(&brokerCR.Status.Conditions, metav1.Condition{
		Type:   v1beta2.DeployedConditionType,
		Status: metav1.ConditionTrue,
		Reason: v1beta2.ReadyConditionReason,
	})
	err = cl.Status().Update(context.TODO(), brokerCR)
	assert.NoError(t, err)

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
	assert.NoError(t, err)

	// 4. Reconcile again - should discover ports and set Deployed=True
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	updatedSvc := &v1beta2.BrokerService{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedSvc)
	assert.NoError(t, err)

	// Verify BrokerService.Deployed is True after Broker is deployed
	deployedCond := meta.FindStatusCondition(updatedSvc.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, metav1.ConditionTrue, deployedCond.Status)
	assert.Equal(t, v1beta2.ReadyConditionReason, deployedCond.Reason)
}

func TestBrokerAppRejectsNonDeployedService(t *testing.T) {
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

	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, app, nsObj).
		WithStatusSubresource(app, svc)).
		Build()

	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	res, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err) // error in the status
	assert.True(t, res.Requeue)

	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Verify app didn't bind to the non-deployed service
	assert.Nil(t, updatedApp.Status.Service)

	// Verify Deployed condition reflects the issue
	deployedCond := meta.FindStatusCondition(updatedApp.Status.Conditions, v1beta2.DeployedConditionType)
	assert.NotNil(t, deployedCond)
	assert.Equal(t, metav1.ConditionFalse, deployedCond.Status)
}

// TestBrokerAppBindsToDeployedService verifies that BrokerApp successfully
// binds to a service that has Deployed=True and completed port discovery.
func TestBrokerAppBindsToDeployedService(t *testing.T) {
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

	cl := setupBrokerAppIndexer(fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(svc, app, nsObj).
		WithStatusSubresource(app, svc)).
		Build()

	r := NewBrokerAppReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: appName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	updatedApp := &v1beta2.BrokerApp{}
	err = cl.Get(context.TODO(), req.NamespacedName, updatedApp)
	assert.NoError(t, err)

	// Verify app successfully bound to the deployed service
	assert.NotNil(t, updatedApp.Status.Service)
	assert.Equal(t, svcName, updatedApp.Status.Service.Name)
	assert.Equal(t, ns, updatedApp.Status.Service.Namespace)

	assert.Equal(t, int32(61616), updatedApp.Status.Service.AssignedPort)
}
