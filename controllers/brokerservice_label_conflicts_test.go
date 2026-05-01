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
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func TestLabelConflicts_NoReservedKeys(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	ns := "default"
	svcName := "test-broker"

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
		WithObjects(service, nsObj).
		WithStatusSubresource(service).
		WithIndex(&v1beta2.BrokerApp{}, "status.serviceBinding", func(obj client.Object) []string {
			app := obj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).
		Build()

	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	broker := &v1beta2.Broker{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, broker)
	assert.NoError(t, err)

	labels := broker.Spec.DeploymentPlan.Labels

	// Verify we don't use the reserved keys
	_, hasBroker := labels["Broker"]
	_, hasApplication := labels["application"]

	assert.False(t, hasBroker, "Must not use reserved label key 'Broker'")
	assert.False(t, hasApplication, "Must not use reserved label key 'application'")

	// Verify we're using standard Kubernetes labels with proper prefixes
	assert.Contains(t, labels, common.LabelAppKubernetesInstance)
	assert.Contains(t, labels, common.LabelAppKubernetesComponent)
	assert.Contains(t, labels, common.LabelAppKubernetesManagedBy)
	assert.Contains(t, labels, common.LabelBrokerService)
	assert.Contains(t, labels, common.LabelBrokerPeerIndex)
}

func TestLabelConflicts_ProperDomainPrefixes(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = v1beta2.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = networkingv1.AddToScheme(scheme)

	ns := "default"
	svcName := "test-broker"

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
		WithObjects(service, nsObj).
		WithStatusSubresource(service).
		WithIndex(&v1beta2.BrokerApp{}, "status.serviceBinding", func(obj client.Object) []string {
			app := obj.(*v1beta2.BrokerApp)
			if app.Status.Service != nil {
				return []string{app.Status.Service.Key()}
			}
			return nil
		}).
		Build()

	r := NewBrokerServiceReconciler(cl, scheme, nil, logr.New(log.NullLogSink{}))

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: svcName, Namespace: ns}}
	_, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)

	broker := &v1beta2.Broker{}
	err = cl.Get(context.TODO(), types.NamespacedName{Name: svcName, Namespace: ns}, broker)
	assert.NoError(t, err)

	labels := broker.Spec.DeploymentPlan.Labels

	validPrefixes := []string{
		"app.kubernetes.io/",
		"broker.arkmq.org/",
	}

	for key := range labels {
		hasValidPrefix := false
		for _, prefix := range validPrefixes {
			if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
				hasValidPrefix = true
				break
			}
		}
		assert.True(t, hasValidPrefix,
			"Label key '%s' must use a domain prefix (app.kubernetes.io/ or broker.arkmq.org/)", key)
	}
}
