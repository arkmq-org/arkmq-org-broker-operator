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

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta1"
	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/selectors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestValidate(t *testing.T) {

	cr := &v1beta2.Broker{
		Spec: v1beta2.BrokerSpec{
			ResourceTemplates: []v1beta2.ResourceTemplate{
				{
					// reserved key
					Labels: map[string]string{selectors.LabelAppKey: "myAppKey"},
				},
			},
		},
	}

	namer := MakeNamers(cr)

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)

	valid, retry := ri.validate(cr, k8sClient, *namer)

	assert.False(t, valid)
	assert.False(t, retry)

	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, v1beta2.ValidConditionType))

	condition := meta.FindStatusCondition(cr.Status.Conditions, v1beta2.ValidConditionType)
	assert.Equal(t, condition.Reason, v1beta2.ValidConditionFailedReservedLabelReason)
	assert.True(t, strings.Contains(condition.Message, "Templates[0]"))
}

func TestValidateBrokerPropsDuplicate(t *testing.T) {

	cr := &v1beta2.Broker{
		Spec: v1beta2.BrokerSpec{
			BrokerProperties: []string{
				"min=X",
				"min=y",
			},
		},
	}

	namer := MakeNamers(cr)

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)

	valid, retry := ri.validate(cr, k8sClient, *namer)

	assert.False(t, valid)
	assert.False(t, retry)

	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, v1beta2.ValidConditionType))

	condition := meta.FindStatusCondition(cr.Status.Conditions, v1beta2.ValidConditionType)
	assert.Equal(t, condition.Reason, v1beta2.ValidConditionFailedDuplicateBrokerPropertiesKey)
	assert.True(t, strings.Contains(condition.Message, "min"))
}

func TestValidateBrokerPropsDuplicateOnFirstEquals(t *testing.T) {

	cr := &v1beta2.Broker{
		Spec: v1beta2.BrokerSpec{
			BrokerProperties: []string{
				"nameWith\\=equals_not_matched=X",
				"nameWith\\=equals_not_matched=Y",
			},
		},
	}

	namer := MakeNamers(cr)

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)

	valid, retry := ri.validate(cr, k8sClient, *namer)

	assert.False(t, valid)
	assert.False(t, retry)

	assert.True(t, meta.IsStatusConditionFalse(cr.Status.Conditions, v1beta2.ValidConditionType))

	condition := meta.FindStatusCondition(cr.Status.Conditions, v1beta2.ValidConditionType)
	assert.Equal(t, condition.Reason, v1beta2.ValidConditionFailedDuplicateBrokerPropertiesKey)
	assert.True(t, strings.Contains(condition.Message, "nameWith"))
}

func TestValidateBrokerPropsDuplicateOnFirstEqualsCorrect(t *testing.T) {

	cr := &v1beta2.Broker{
		Spec: v1beta2.BrokerSpec{
			BrokerProperties: []string{
				"nameWith\\=equals_A_not_matched=X",
				"nameWith\\=equals_B_not_matched=Y",
			},
		},
	}

	namer := MakeNamers(cr)

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)

	valid, retry := ri.validate(cr, k8sClient, *namer)

	assert.True(t, valid)
	assert.False(t, retry)

	assert.True(t, meta.IsStatusConditionTrue(cr.Status.Conditions, brokerv1beta1.ValidConditionType))
}

func TestCheckStatusNoBrokerInstances(t *testing.T) {

	replicas := int32(1)
	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{
			Name:      "broker",
			Namespace: "some-ns",
		},
		Spec: v1beta2.BrokerSpec{
			DeploymentPlan: v1beta2.DeploymentPlanType{
				Size: &replicas,
			},
		},
		Status: v1beta2.BrokerStatus{
			DeploymentPlanSize: replicas,
		},
	}

	s := runtime.NewScheme()
	corev1.AddToScheme(s)
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(
		&corev1.Pod{
			ObjectMeta: v1.ObjectMeta{
				Name:      "broker-ss-0",
				Namespace: "some-ns",
				Labels:    map[string]string{selectors.LabelResourceKey: "broker"},
			},
		},
	).Build()

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)

	checkOk := func(ordinal string, instance *BrokerInstanceStatus) ArtemisError {
		return nil
	}

	err := ri.CheckBrokerInstanceStatuses(cr, cl, checkOk)
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "Waiting for")
}

func TestCheckBrokerInstanceStatuses(t *testing.T) {

	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "a", Namespace: "test"},
		Spec:       v1beta2.BrokerSpec{},
	}

	s := runtime.NewScheme()
	corev1.AddToScheme(s)
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(
		&corev1.Pod{
			ObjectMeta: v1.ObjectMeta{
				Name:      "a-ss-0",
				Namespace: "test",
				Labels:    map[string]string{selectors.LabelResourceKey: "a"},
				Annotations: map[string]string{
					BrokerStatusAnnotationKey: `{"configuration":{"properties":{}},"server":{"state":"STARTED","version":"2.33.0","nodeId":"test-node","uptime":"1 min"}}`,
				},
			},
		},
	).Build()

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)

	var callCount int
	checkFn := func(ordinal string, instance *BrokerInstanceStatus) ArtemisError {
		callCount++
		assert.Equal(t, "0", ordinal)
		assert.Equal(t, "a-ss-0", instance.PodName)
		assert.Equal(t, "STARTED", instance.ServerState)
		assert.Equal(t, "2.33.0", instance.ServerVersion)
		return nil
	}

	err := ri.CheckBrokerInstanceStatuses(cr, cl, checkFn)
	assert.Nil(t, err)
	assert.Equal(t, 1, callCount)
}

func TestMakeExtraVolumeMounts_NoExtraVolumes(t *testing.T) {
	cr := &v1beta2.Broker{
		Spec: v1beta2.BrokerSpec{},
	}

	volumeMounts := MakeExtraVolumeMounts(cr)
	assert.Empty(t, volumeMounts)
}

func TestMakeExtraVolumeMounts_WithExtraVolumes(t *testing.T) {
	cr := &v1beta2.Broker{
		Spec: v1beta2.BrokerSpec{
			DeploymentPlan: v1beta2.DeploymentPlanType{
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
	cr := &v1beta2.Broker{
		Spec: v1beta2.BrokerSpec{
			DeploymentPlan: v1beta2.DeploymentPlanType{
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
	cr := &v1beta2.Broker{
		Spec: v1beta2.BrokerSpec{
			DeploymentPlan: v1beta2.DeploymentPlanType{
				ExtraVolumeClaimTemplates: []v1beta2.VolumeClaimTemplate{
					{
						ObjectMeta: v1beta2.ObjectMeta{
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
	cr := &v1beta2.Broker{
		Spec: v1beta2.BrokerSpec{
			DeploymentPlan: v1beta2.DeploymentPlanType{
				ExtraVolumes: []corev1.Volume{
					{
						Name: "my-volume",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
				ExtraVolumeClaimTemplates: []v1beta2.VolumeClaimTemplate{
					{
						ObjectMeta: v1beta2.ObjectMeta{
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

func TestReconcileRequeuesOnNotReady(t *testing.T) {
	s := runtime.NewScheme()
	_ = brokerv1beta1.AddToScheme(s)
	_ = corev1.AddToScheme(s)
	_ = appsv1.AddToScheme(s)
	_ = rbacv1.AddToScheme(s)

	crd := &brokerv1beta1.ActiveMQArtemis{
		ObjectMeta: v1.ObjectMeta{
			Name:      "test-broker",
			Namespace: "default",
		},
		Spec: brokerv1beta1.ActiveMQArtemisSpec{},
	}

	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(crd).WithStatusSubresource(crd).Build()

	r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, false)
	r.Client = cl
	r.Scheme = s

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-broker",
			Namespace: "default",
		},
	}

	res, err := r.Reconcile(context.TODO(), req)
	assert.NoError(t, err)
	assert.Equal(t, common.GetReconcileResyncPeriod(), res.RequeueAfter)

	// refresh the crd to see the status update
	assert.NoError(t, cl.Get(context.TODO(), req.NamespacedName, crd))
	assert.True(t, meta.IsStatusConditionFalse(crd.Status.Conditions, brokerv1beta1.DeployedConditionType))
}

func TestAggregateSyncStatusNoStatusData(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "broker", Namespace: "test"},
	}
	s := runtime.NewScheme()
	corev1.AddToScheme(s)
	cl := fake.NewClientBuilder().WithScheme(s).Build()

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)

	condition := ri.aggregateSyncStatus(cr, cl)
	assert.Equal(t, v1beta2.ConfigAppliedConditionType, condition.Type)
	assert.Equal(t, v1.ConditionUnknown, condition.Status)
	assert.Equal(t, v1beta2.ConfigAppliedConditionOutOfSyncReason, condition.Reason)
	assert.Contains(t, condition.Message, "status script")
}

func TestAggregateSyncStatusWithPropertiesData(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "broker", Namespace: "test"},
		Spec: v1beta2.BrokerSpec{
			DeploymentPlan: v1beta2.DeploymentPlanType{
				ExtraMounts: v1beta2.ExtraMountsType{
					Secrets: []string{"my-secret-dp"},
				},
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      "broker-ss-0",
			Namespace: "test",
			Labels:    map[string]string{selectors.LabelResourceKey: "broker"},
			Annotations: map[string]string{
				BrokerStatusAnnotationKey: `{"configuration":{"properties":{"broker.properties":{"alder32":"123","fileAlder32":"456"}}},"server":{"state":"STARTED","version":"2.55.0","nodeId":"abc","uptime":"1 min"}}`,
			},
		},
	}
	s := runtime.NewScheme()
	corev1.AddToScheme(s)
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(pod).Build()

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)

	condition := ri.aggregateSyncStatus(cr, cl)
	assert.Equal(t, v1beta2.ConfigAppliedConditionType, condition.Type)
	assert.Equal(t, v1.ConditionTrue, condition.Status)
	assert.Equal(t, v1beta2.ConfigAppliedConditionSynchedReason, condition.Reason)
}

func TestAggregateSyncStatusReloadInProgress(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "broker", Namespace: "test"},
	}
	// Pod has the annotation key present but empty (cleared by status script)
	pod := &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      "broker-ss-0",
			Namespace: "test",
			Labels:    map[string]string{selectors.LabelResourceKey: "broker"},
			Annotations: map[string]string{
				BrokerStatusAnnotationKey: "",
			},
		},
	}
	s := runtime.NewScheme()
	corev1.AddToScheme(s)
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(pod).Build()

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)

	condition := ri.aggregateSyncStatus(cr, cl)
	assert.Equal(t, v1beta2.ConfigAppliedConditionType, condition.Type)
	assert.Equal(t, v1.ConditionUnknown, condition.Status)
	assert.Contains(t, condition.Message, "Reload in progress")
}

func TestAggregateSyncStatusReloadInProgressMixedPods(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "broker", Namespace: "test"},
	}
	// Pod 0 has completed status, pod 1 is reloading
	pod0 := &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      "broker-ss-0",
			Namespace: "test",
			Labels:    map[string]string{selectors.LabelResourceKey: "broker"},
			Annotations: map[string]string{
				BrokerStatusAnnotationKey: `{"configuration":{"properties":{"broker.properties":{"alder32":"123","fileAlder32":"123"}}},"server":{"state":"STARTED","version":"2.55.0","nodeId":"abc","uptime":"1 min"}}`,
			},
		},
	}
	pod1 := &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      "broker-ss-1",
			Namespace: "test",
			Labels:    map[string]string{selectors.LabelResourceKey: "broker"},
			Annotations: map[string]string{
				BrokerStatusAnnotationKey: "",
			},
		},
	}
	s := runtime.NewScheme()
	corev1.AddToScheme(s)
	cl := fake.NewClientBuilder().WithScheme(s).WithObjects(pod0, pod1).Build()

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)

	// Even though pod0 is synced, pod1 is reloading so overall status is Unknown
	condition := ri.aggregateSyncStatus(cr, cl)
	assert.Equal(t, v1beta2.ConfigAppliedConditionType, condition.Type)
	assert.Equal(t, v1.ConditionUnknown, condition.Status)
	assert.Contains(t, condition.Message, "Reload in progress")
}

func TestDefaultLoggingConfigWithReloadAppender(t *testing.T) {
	config := defaultLoggingConfigWithReloadAppender()
	assert.Contains(t, config, "reload_trigger")
	assert.Contains(t, config, "AMQ221087")
	assert.Contains(t, config, "appender.reload_trigger.type = File")
	assert.Contains(t, config, "${sys:artemis.instance}/log/reload.log")
	assert.Contains(t, config, "STDOUT")
}

func TestMergeReloadAppender(t *testing.T) {
	userConfig := "appender.stdout.name = STDOUT\nappender.stdout.type = Console\nrootLogger = info, STDOUT\n"

	merged := mergeReloadAppender(userConfig)
	assert.Contains(t, merged, "reload_trigger")
	assert.Contains(t, merged, "AMQ221087")
	assert.Contains(t, merged, userConfig)
}

func TestMergeReloadAppenderAlreadyPresent(t *testing.T) {
	configWithReload := "appender.reload_trigger.name = reload_trigger\nappender.reload_trigger.type = File\n"
	merged := mergeReloadAppender(configWithReload)
	assert.Equal(t, configWithReload, merged)
}
