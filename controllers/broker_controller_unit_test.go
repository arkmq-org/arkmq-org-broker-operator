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
// +kubebuilder:docs-gen:collapse=Apache License
package controllers

import (
	"context"
	"testing"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia_client"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/event"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
)

func TestErrOnNotFoundSecret(t *testing.T) {

	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "a"},
		Spec:       v1beta2.BrokerSpec{},
	}

	namer := MakeNamersForBroker(cr)

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)

	var times = 0
	interceptorFuncs := interceptor.Funcs{
		Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			times++
			return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
		},
	}

	common.SetOperatorNameSpace("test")
	t.Cleanup(common.UnsetOperatorNameSpace)

	client := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

	error := ri.Process(cr, *namer, client, nil)

	assert.NotNil(t, error)
	assert.ErrorContains(t, error, "not found")
}

func TestValidateRestrictedNeedsSecret(t *testing.T) {

	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "a"},
		Spec:       v1beta2.BrokerSpec{},
	}

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)

	fakeSecrets := map[string]client.Object{}
	interceptorFuncs := interceptor.Funcs{
		Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if o, found := fakeSecrets[key.Name]; found {
				obj.SetName(o.GetName())
				return nil
			}
			return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
		},
	}

	common.SetOperatorNameSpace("test")
	t.Cleanup(common.UnsetOperatorNameSpace)

	client := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

	valid, retry := ri.validate(cr, client)

	assert.False(t, valid)
	assert.True(t, retry)

	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType))

	condition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
	assert.Equal(t, condition.Reason, brokerv1beta1.ValidConditionMissingResourcesReason)
	assert.Contains(t, condition.Message, "failed to get secret")
	assert.Contains(t, condition.Message, common.DefaultOperatorCertSecretName)

	fakeSecrets[common.DefaultOperatorCertSecretName] = &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCertSecretName},
	}

	valid, retry = ri.validate(cr, client)

	assert.False(t, valid)
	assert.True(t, retry)
	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType))
	condition = meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
	assert.Equal(t, condition.Reason, brokerv1beta1.ValidConditionMissingResourcesReason)
	assert.Contains(t, condition.Message, "failed to get secret")
	assert.Contains(t, condition.Message, common.DefaultOperatorCASecretName)

	fakeSecrets[common.DefaultOperatorCASecretName] = &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCASecretName},
	}
	valid, retry = ri.validate(cr, client)

	assert.False(t, valid)
	assert.True(t, retry)
	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType))
	condition = meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
	assert.Equal(t, condition.Reason, brokerv1beta1.ValidConditionMissingResourcesReason)
	assert.Contains(t, condition.Message, "failed to get secret")
	assert.Contains(t, condition.Message, common.DefaultOperandCertSecretName)

	fakeSecrets[common.DefaultOperandCertSecretName] = &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperandCertSecretName},
	}
	valid, retry = ri.validate(cr, client)

	assert.True(t, valid)
	assert.False(t, retry)
	assert.True(t, meta.IsStatusConditionTrue(cr.Status.Conditions, brokerv1beta1.ValidConditionType))
}

func TestReconcileMeAnnotationPredicate(t *testing.T) {
	pred := reconcileMeAnnotationPredicate()

	t.Run("CreateFunc with annotation present", func(t *testing.T) {
		pod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "true"},
		}}
		assert.True(t, pred.Create(event.CreateEvent{Object: pod}))
	})

	t.Run("CreateFunc without annotation", func(t *testing.T) {
		pod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{}}
		assert.False(t, pred.Create(event.CreateEvent{Object: pod}))
	})

	t.Run("UpdateFunc annotation added", func(t *testing.T) {
		oldPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{}}
		newPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "true"},
		}}
		assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod}))
	})

	t.Run("UpdateFunc annotation unchanged", func(t *testing.T) {
		oldPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "true"},
		}}
		newPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "true"},
		}}
		assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod}))
	})

	t.Run("UpdateFunc annotation removed", func(t *testing.T) {
		oldPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "true"},
		}}
		newPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{}}
		assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod}))
	})

	t.Run("DeleteFunc always false", func(t *testing.T) {
		pod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "true"},
		}}
		assert.False(t, pred.Delete(event.DeleteEvent{Object: pod}))
	})
}

func TestMapPodToBrokerCR(t *testing.T) {
	s := scheme.Scheme
	_ = appsv1.AddToScheme(s)
	_ = v1beta2.SchemeBuilder.AddToScheme(s)

	ss := &appsv1.StatefulSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-ss",
			Namespace: "test-ns",
			OwnerReferences: []v1.OwnerReference{
				{Kind: "Broker", Name: "my-broker", APIVersion: "broker.arkmq.org/v1beta2"},
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-ss-0",
			Namespace: "test-ns",
			OwnerReferences: []v1.OwnerReference{
				{Kind: "StatefulSet", Name: "test-ss", APIVersion: "apps/v1"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(ss).Build()
	r := &BrokerReconciler{Client: fakeClient, Scheme: s}

	requests := r.mapPodToBrokerCR(context.TODO(), pod)
	assert.Equal(t, 1, len(requests))
	assert.Equal(t, "my-broker", requests[0].Name)
	assert.Equal(t, "test-ns", requests[0].Namespace)
}

func TestMapPodToBrokerCR_NoBrokerOwner(t *testing.T) {
	s := scheme.Scheme
	_ = appsv1.AddToScheme(s)

	ss := &appsv1.StatefulSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-ss",
			Namespace: "test-ns",
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-ss-0",
			Namespace: "test-ns",
			OwnerReferences: []v1.OwnerReference{
				{Kind: "StatefulSet", Name: "test-ss", APIVersion: "apps/v1"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(ss).Build()
	r := &BrokerReconciler{Client: fakeClient, Scheme: s}

	requests := r.mapPodToBrokerCR(context.TODO(), pod)
	assert.Equal(t, 0, len(requests))
}

func TestCheckProjectionStatus(t *testing.T) {
	checksum := alder32FromData([]byte("globalMaxSize=128m"))

	extractStatus := func(bs *brokerStatus, fileName string) (propertiesStatus, bool) {
		current, present := bs.BrokerConfigStatus.PropertiesStatus[fileName]
		return current, present
	}

	newReconcilerWithStatus := func(props map[string]propertiesStatus) (*BrokerReconcilerImpl, *v1beta2.Broker, client.Client) {
		cr := &v1beta2.Broker{
			ObjectMeta: v1.ObjectMeta{Name: "test", Namespace: "test-ns"},
		}
		cached := brokerStatus{
			BrokerConfigStatus: brokerConfigStatus{
				PropertiesStatus: props,
			},
		}
		ri := &BrokerReconcilerImpl{
			customResource:     cr,
			log:                ctrl.Log,
			cachedBrokerStatus: map[string]any{"0": cached},
			jolokiaEndpoints:   []*jolokia_client.JkInfo{{Ordinal: "0"}},
		}
		fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		return ri, cr, fakeClient
	}

	t.Run("all synced returns nil", func(t *testing.T) {
		proj := &projection{
			Name:            "test-props",
			ResourceVersion: "1",
			Files:           map[string]propertyFile{"broker.properties": {Alder32: checksum}},
		}
		ri, cr, fakeClient := newReconcilerWithStatus(map[string]propertiesStatus{
			"broker.properties": {Alder32: checksum},
		})
		result := ri.checkProjectionStatus(cr, fakeClient, proj, extractStatus)
		assert.Nil(t, result)
	})

	t.Run("checksum mismatch returns OutOfSync", func(t *testing.T) {
		proj := &projection{
			Name:            "test-props",
			ResourceVersion: "1",
			Files:           map[string]propertyFile{"broker.properties": {Alder32: checksum}},
		}
		ri, cr, fakeClient := newReconcilerWithStatus(map[string]propertiesStatus{
			"broker.properties": {Alder32: "99999"},
		})
		result := ri.checkProjectionStatus(cr, fakeClient, proj, extractStatus)
		assert.NotNil(t, result)
		_, ok := result.(statusOutOfSyncError)
		assert.True(t, ok, "expected statusOutOfSyncError, got %T", result)
	})

	t.Run("synced with apply errors returns InSyncApplyError", func(t *testing.T) {
		proj := &projection{
			Name:            "test-props",
			ResourceVersion: "1",
			Files:           map[string]propertyFile{"broker.properties": {Alder32: checksum}},
		}
		ri, cr, fakeClient := newReconcilerWithStatus(map[string]propertiesStatus{
			"broker.properties": {
				Alder32: checksum,
				ApplyErrors: []applyError{
					{PropKeyValue: "addressFullMessagePolicy=INVALID", Reason: "IllegalArgumentException"},
				},
			},
		})
		result := ri.checkProjectionStatus(cr, fakeClient, proj, extractStatus)
		assert.NotNil(t, result)
		_, ok := result.(inSyncApplyError)
		assert.True(t, ok, "expected inSyncApplyError, got %T", result)
		assert.Contains(t, result.Error(), "addressFullMessagePolicy=INVALID")
	})

	t.Run("unchecked prefix files are ignored when missing", func(t *testing.T) {
		proj := &projection{
			Name:            "test-props",
			ResourceVersion: "1",
			Files: map[string]propertyFile{
				"broker.properties":         {Alder32: checksum},
				"_jolokia.config":           {Alder32: "ignored"},
				"_prometheus_exporter.yaml": {Alder32: "ignored"},
			},
		}
		ri, cr, fakeClient := newReconcilerWithStatus(map[string]propertiesStatus{
			"broker.properties": {Alder32: checksum},
		})
		result := ri.checkProjectionStatus(cr, fakeClient, proj, extractStatus)
		assert.Nil(t, result, "underscore-prefixed files should not cause missing key errors")
	})

	t.Run("missing file returns OutOfSyncMissingKey", func(t *testing.T) {
		proj := &projection{
			Name:            "test-props",
			ResourceVersion: "1",
			Files: map[string]propertyFile{
				"broker.properties": {Alder32: checksum},
				"other.properties":  {Alder32: "other"},
			},
		}
		ri, cr, fakeClient := newReconcilerWithStatus(map[string]propertiesStatus{
			"broker.properties": {Alder32: checksum},
		})
		result := ri.checkProjectionStatus(cr, fakeClient, proj, extractStatus)
		assert.NotNil(t, result)
		_, ok := result.(statusOutOfSyncMissingKeyError)
		assert.True(t, ok, "expected statusOutOfSyncMissingKeyError, got %T", result)
	})
}
