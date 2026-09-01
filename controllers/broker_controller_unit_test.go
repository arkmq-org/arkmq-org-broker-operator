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
	"strings"
	"testing"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/environments"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

func TestBrokerCR_ExtraBrokerPropertiesDefaultIsEmpty(t *testing.T) {

	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "broker"},
		Spec:       v1beta2.BrokerSpec{},
	}

	namer := MakeNamersForBroker(cr)
	envVars := MakeEnvVarArrayForCRForBroker(cr, *namer)

	// EXTRA_BROKER_PROPERTIES must be present with an empty default
	extraPathsIdx := -1
	for i, e := range envVars {
		if e.Name == environments.ExtraBrokerPropertiesEnvVar {
			assert.Equal(t, "", e.Value)
			extraPathsIdx = i
			break
		}
	}
	assert.True(t, extraPathsIdx != -1, "EXTRA_BROKER_PROPERTIES must be in env array")

	// brokerPropertiesConfigSystemPropValue must embed the token
	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)
	result := ri.brokerPropertiesConfigSystemPropValue("/config/", "my-resource",
		map[string][]byte{"broker.properties": []byte("")})
	assert.True(t, strings.HasSuffix(result, ",$(EXTRA_BROKER_PROPERTIES)"),
		"brokerPropertiesConfigSystemPropValue must end with ,$(EXTRA_BROKER_PROPERTIES)")
}

func TestBrokerCR_ExtraBrokerPropertiesUserValueOverrides(t *testing.T) {

	const extraPaths = "/my/extra/path/"

	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "broker"},
		Spec: v1beta2.BrokerSpec{
			Env: []corev1.EnvVar{
				{
					Name:  environments.ExtraBrokerPropertiesEnvVar,
					Value: extraPaths,
				},
			},
		},
	}

	namer := MakeNamersForBroker(cr)
	envVars := MakeEnvVarArrayForCRForBroker(cr, *namer)

	// User's value must replace the operator default
	found := false
	for _, e := range envVars {
		if e.Name == environments.ExtraBrokerPropertiesEnvVar {
			assert.Equal(t, extraPaths, e.Value)
			found = true
			break
		}
	}
	assert.True(t, found, "EXTRA_BROKER_PROPERTIES with user value must be in env array")
}

func TestBrokerCR_ExtraBrokerPropertiesValueFromIsRejected(t *testing.T) {

	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "broker"},
		Spec: v1beta2.BrokerSpec{
			Env: []corev1.EnvVar{
				{
					Name: environments.ExtraBrokerPropertiesEnvVar,
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "some-secret"},
							Key:                  "paths",
						},
					},
				},
			},
		},
	}

	condition, _ := validateEnvVarsForBroker(cr)
	assert.NotNil(t, condition)
	assert.Equal(t, brokerv1beta1.ValidConditionInvalidInternalVarUsage, condition.Reason)
	assert.Contains(t, condition.Message, environments.ExtraBrokerPropertiesEnvVar)
}
