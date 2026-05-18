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
	"fmt"
	"strings"
	"testing"

	brokerv1beta1 "github.com/arkmq-org/activemq-artemis-operator/v2/api/v1beta1"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	artemis_client "github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/artemis"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/jolokia"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/jolokia_client"
	"github.com/arkmq-org/activemq-artemis-operator/v2/pkg/utils/selectors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestValidate(t *testing.T) {

	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			ResourceTemplates: []brokerv1beta1.ResourceTemplate{
				{
					// reserved key
					Labels: map[string]string{selectors.LabelAppKey: "myAppKey"},
				},
			},
		},
	}

	namer := MakeNamers(cr)

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewActiveMQArtemisReconcilerImpl(cr, r)

	valid, retry := ri.validate(cr, k8sClient, *namer)

	assert.False(t, valid)
	assert.False(t, retry)

	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType))

	condition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
	assert.Equal(t, condition.Reason, brokerv1beta1.ValidConditionFailedReservedLabelReason)
	assert.True(t, strings.Contains(condition.Message, "Templates[0]"))
}

func TestValidateBrokerPropsDuplicate(t *testing.T) {

	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			BrokerProperties: []string{
				"min=X",
				"min=y",
			},
		},
	}

	namer := MakeNamers(cr)

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewActiveMQArtemisReconcilerImpl(cr, r)

	valid, retry := ri.validate(cr, k8sClient, *namer)

	assert.False(t, valid)
	assert.False(t, retry)

	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType))

	condition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
	assert.Equal(t, condition.Reason, brokerv1beta1.ValidConditionFailedDuplicateBrokerPropertiesKey)
	assert.True(t, strings.Contains(condition.Message, "min"))
}

func TestValidateBrokerPropsDuplicateOnFirstEquals(t *testing.T) {

	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			BrokerProperties: []string{
				"nameWith\\=equals_not_matched=X",
				"nameWith\\=equals_not_matched=Y",
			},
		},
	}

	namer := MakeNamers(cr)

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewActiveMQArtemisReconcilerImpl(cr, r)

	valid, retry := ri.validate(cr, k8sClient, *namer)

	assert.False(t, valid)
	assert.False(t, retry)

	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType))

	condition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
	assert.Equal(t, condition.Reason, brokerv1beta1.ValidConditionFailedDuplicateBrokerPropertiesKey)
	assert.True(t, strings.Contains(condition.Message, "nameWith"))
}

func TestValidateBrokerPropsDuplicateOnFirstEqualsIncorrectButUnrealisticForOurBrokerConfigUsecase(t *testing.T) {

	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			BrokerProperties: []string{
				"nameWith\\=equals_A_not_matched=X",
				"nameWith\\=equals_B_not_matched=Y",
			},
		},
	}

	namer := MakeNamers(cr)

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewActiveMQArtemisReconcilerImpl(cr, r)

	valid, retry := ri.validate(cr, k8sClient, *namer)

	assert.False(t, valid)
	assert.False(t, retry)

	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType))

	condition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
	assert.Equal(t, condition.Reason, brokerv1beta1.ValidConditionFailedDuplicateBrokerPropertiesKey)
	assert.True(t, strings.Contains(condition.Message, "nameWith"))
}

func TestStatusPodsCheckCached(t *testing.T) {

	replicas := int32(1)
	cr := &brokerv1beta1.ActiveMQArtemis{
		ObjectMeta: v1.ObjectMeta{
			Name:      "broker",
			Namespace: "some-ns",
		},
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			DeploymentPlan: brokerv1beta1.DeploymentPlanType{
				Size: &replicas,
			},
		},
		Status: brokerv1beta1.ActiveMQArtemisStatus{
			DeploymentPlanSize: replicas,
		},
	}

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewActiveMQArtemisReconcilerImpl(cr, r)

	checkOk := func(brokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError {
		return nil
	}

	var times = 0
	interceptorFuncs := interceptor.Funcs{
		Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			times++
			return apierrors.NewNotFound(schema.GroupResource{}, "")
		},
	}

	client := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

	valid := ri.CheckStatus(cr, client, checkOk)
	assert.NotNil(t, valid)
	assert.Contains(t, valid.Error(), "Waiting for")
	assert.Equal(t, times, 1)

	// repeat to verify fake client not called again
	valid = ri.CheckStatus(cr, client, checkOk)
	assert.NotNil(t, valid)
	assert.Contains(t, valid.Error(), "Waiting for")

	assert.Equal(t, times, 1)
}

func TestJolokiaStatusCached(t *testing.T) {

	cr := &brokerv1beta1.ActiveMQArtemis{
		ObjectMeta: v1.ObjectMeta{Name: "a"},
		Spec:       brokerv1beta1.ActiveMQArtemisSpec{},
	}

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewActiveMQArtemisReconcilerImpl(cr, r)

	checkOk := func(brokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError {
		return nil
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	j := jolokia.NewMockIJolokia(ctrl)
	a := artemis_client.GetArtemisWithJolokia(j, "a")

	j.EXPECT().
		Read(gomock.Eq("org.apache.activemq.artemis:broker=\"a\"/Status")).
		DoAndReturn(func(_ string) (*jolokia.ResponseData, error) {
			return &jolokia.ResponseData{
				Status:    404,
				Value:     "",
				ErrorType: "javax.management.AttributeNotFoundException",
				Error:     "javax.management.AttributeNotFoundException : No such attribute: Status",
			}, fmt.Errorf("javax.management.AttributeNotFoundException")
		}).Times(1)

	valid := ri.CheckStatusFromJolokia(&jolokia_client.JkInfo{Artemis: a, IP: "IP", Ordinal: "0"}, checkOk)
	assert.NotNil(t, valid)
	assert.True(t, strings.Contains(valid.Error(), "AttributeNotFoundException"))

	// verify status call is cached for second call
	valid = ri.CheckStatusFromJolokia(&jolokia_client.JkInfo{Artemis: a, IP: "IP", Ordinal: "0"}, checkOk)
	assert.NotNil(t, valid)
	assert.True(t, strings.Contains(valid.Error(), "AttributeNotFoundException"))

}

func TestMakeExtraVolumeMounts_NoExtraVolumes(t *testing.T) {
	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{},
	}

	volumeMounts := MakeExtraVolumeMounts(cr)
	assert.Empty(t, volumeMounts)
}

func TestMakeExtraVolumeMounts_WithExtraVolumes(t *testing.T) {
	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			DeploymentPlan: brokerv1beta1.DeploymentPlanType{
				ExtraVolumes: []corev1.Volume{
					{
						Name: "my-volume",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
			},
		},
	}

	volumeMounts := MakeExtraVolumeMounts(cr)
	assert.Len(t, volumeMounts, 1)
	assert.Equal(t, "my-volume", volumeMounts[0].Name)
	assert.Equal(t, "/amq/extra/volumes/my-volume", volumeMounts[0].MountPath)
}

func TestMakeExtraVolumeMounts_WithExtraVolumesAndMountOverride(t *testing.T) {
	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			DeploymentPlan: brokerv1beta1.DeploymentPlanType{
				ExtraVolumes: []corev1.Volume{
					{
						Name: "my-volume",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
				ExtraVolumeMounts: []corev1.VolumeMount{
					{
						Name:      "my-volume",
						MountPath: "/custom/path",
					},
				},
			},
		},
	}

	volumeMounts := MakeExtraVolumeMounts(cr)
	assert.Len(t, volumeMounts, 1)
	assert.Equal(t, "my-volume", volumeMounts[0].Name)
	assert.Equal(t, "/custom/path", volumeMounts[0].MountPath)
}

func TestMakeExtraVolumeMounts_WithExtraVolumeClaimTemplates(t *testing.T) {
	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			DeploymentPlan: brokerv1beta1.DeploymentPlanType{
				ExtraVolumeClaimTemplates: []brokerv1beta1.VolumeClaimTemplate{
					{
						ObjectMeta: brokerv1beta1.ObjectMeta{
							Name: "my-pvc",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							AccessModes: []corev1.PersistentVolumeAccessMode{
								corev1.ReadWriteOnce,
							},
						},
					},
				},
			},
		},
	}

	volumeMounts := MakeExtraVolumeMounts(cr)
	assert.Len(t, volumeMounts, 1)
	assert.Equal(t, "my-pvc", volumeMounts[0].Name)
	assert.Equal(t, "/opt/my-pvc/data", volumeMounts[0].MountPath)
}

func TestMakeExtraVolumeMounts_WithBothExtraVolumesAndClaims(t *testing.T) {
	cr := &brokerv1beta1.ActiveMQArtemis{
		Spec: brokerv1beta1.ActiveMQArtemisSpec{
			DeploymentPlan: brokerv1beta1.DeploymentPlanType{
				ExtraVolumes: []corev1.Volume{
					{
						Name: "my-volume",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
				ExtraVolumeClaimTemplates: []brokerv1beta1.VolumeClaimTemplate{
					{
						ObjectMeta: brokerv1beta1.ObjectMeta{
							Name: "my-pvc",
						},
						Spec: corev1.PersistentVolumeClaimSpec{
							AccessModes: []corev1.PersistentVolumeAccessMode{
								corev1.ReadWriteOnce,
							},
						},
					},
				},
			},
		},
	}

	volumeMounts := MakeExtraVolumeMounts(cr)
	assert.Len(t, volumeMounts, 2)
	assert.Equal(t, "my-volume", volumeMounts[0].Name)
	assert.Equal(t, "my-pvc", volumeMounts[1].Name)
}
