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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
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
			Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
		}}
		assert.True(t, pred.Create(event.CreateEvent{Object: pod}))
	})

	t.Run("CreateFunc without annotation", func(t *testing.T) {
		pod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{}}
		assert.False(t, pred.Create(event.CreateEvent{Object: pod}))
	})

	t.Run("UpdateFunc annotation value changed", func(t *testing.T) {
		oldPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
		}}
		newPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300005"},
		}}
		assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod}))
	})

	t.Run("UpdateFunc annotation added", func(t *testing.T) {
		oldPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{}}
		newPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
		}}
		assert.True(t, pred.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod}))
	})

	t.Run("UpdateFunc annotation unchanged", func(t *testing.T) {
		oldPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
		}}
		newPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
		}}
		assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod}))
	})

	t.Run("UpdateFunc annotation removed", func(t *testing.T) {
		oldPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
		}}
		newPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{}}
		assert.False(t, pred.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod}))
	})

	t.Run("DeleteFunc always false", func(t *testing.T) {
		pod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
			Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
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
			Name:      "my-broker-ss",
			Namespace: "test-ns",
			OwnerReferences: []v1.OwnerReference{
				{Kind: "Broker", Name: "my-broker", APIVersion: "broker.arkmq.org/v1beta2"},
			},
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      "my-broker-ss-0",
			Namespace: "test-ns",
			OwnerReferences: []v1.OwnerReference{
				{Kind: "StatefulSet", Name: "my-broker-ss", APIVersion: "apps/v1"},
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
			Name:      "my-broker-ss",
			Namespace: "test-ns",
		},
	}
	pod := &corev1.Pod{
		ObjectMeta: v1.ObjectMeta{
			Name:      "my-broker-ss-0",
			Namespace: "test-ns",
			OwnerReferences: []v1.OwnerReference{
				{Kind: "StatefulSet", Name: "my-broker-ss", APIVersion: "apps/v1"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithObjects(ss).Build()
	r := &BrokerReconciler{Client: fakeClient, Scheme: s}

	requests := r.mapPodToBrokerCR(context.TODO(), pod)
	assert.Equal(t, 0, len(requests))
}

func TestCheckProjectionStatus(t *testing.T) {
	checksum := brokerproperties.Alder32FromData([]byte("globalMaxSize=128m"))

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

func TestWithCompositeLog4j2Config(t *testing.T) {
	const operatorURI = "/amq/extra/configmaps/my-broker-logging-config/logging.properties"
	const extraMountsURI = "/amq/extra/configmaps/custom-logging-config/logging.properties"

	t.Run("empty opts uses classpath base and operator URI", func(t *testing.T) {
		got := withCompositeLog4j2Config("", defaultLog4j2ConfigURI, operatorURI)
		assert.Equal(t, log4j2ConfigurationFileFlag+defaultLog4j2ConfigURI+","+operatorURI, got)
		assert.NotContains(t, got, "-Dlog4j2.level=")
	})

	t.Run("extraMounts path is first and operator last without level overlay", func(t *testing.T) {
		got := withCompositeLog4j2Config("-Djavax.net.debug=ssl", extraMountsURI, operatorURI)
		assert.Contains(t, got, log4j2ConfigurationFileFlag+extraMountsURI+","+operatorURI)
		assert.True(t, strings.HasPrefix(got, "-Djavax.net.debug=ssl "))
		assert.NotContains(t, got, "-Dlog4j2.level=")
		assert.Less(t, strings.Index(got, extraMountsURI), strings.Index(got, operatorURI))
	})

	t.Run("existing configurationFile appends operator without a second -D", func(t *testing.T) {
		existing := log4j2ConfigurationFileFlag + "/user/logging.properties"
		got := withCompositeLog4j2Config(existing, defaultLog4j2ConfigURI, operatorURI)
		assert.Equal(t, log4j2ConfigurationFileFlag+"/user/logging.properties,"+operatorURI, got)
		assert.Equal(t, 1, strings.Count(got, log4j2ConfigurationFileFlag))
	})

	t.Run("second call does not duplicate operator URI", func(t *testing.T) {
		first := withCompositeLog4j2Config("", defaultLog4j2ConfigURI, operatorURI)
		second := withCompositeLog4j2Config(first, defaultLog4j2ConfigURI, operatorURI)
		assert.Equal(t, first, second)
		assert.Equal(t, 1, strings.Count(second, operatorURI))
	})
}

func TestBrokerReloadLogAppenderFragment(t *testing.T) {
	assert.Contains(t, brokerReloadLogAppenderFragment, "RollingFile")
	assert.NotContains(t, brokerReloadLogAppenderFragment, "rootLogger")
	assert.NotContains(t, brokerReloadLogAppenderFragment, "logger.reload.level")
	assert.Contains(t, brokerReloadLogAppenderFragment, "org.apache.activemq.artemis.core.server")
}

func TestApplyReloadLoggingJavaOpts(t *testing.T) {
	const operatorURI = "/amq/extra/configmaps/my-broker-logging-config/logging.properties"
	const extraMountsURI = "/amq/extra/configmaps/custom-logging-config/logging.properties"

	envValue := func(containers []corev1.Container, name string) string {
		for _, env := range containers[0].Env {
			if env.Name == name {
				return env.Value
			}
		}
		return ""
	}

	t.Run("default overlays INFO and composites classpath with operator", func(t *testing.T) {
		containers := []corev1.Container{{}}
		applyReloadLoggingJavaOpts(containers, defaultLog4j2ConfigURI, operatorURI, true)
		got := envValue(containers, jdkJavaOptionsEnvVarName)
		assert.Contains(t, got, "-Dlog4j2.level=INFO")
		assert.Contains(t, got, log4j2ConfigurationFileFlag+defaultLog4j2ConfigURI+","+operatorURI)
	})

	t.Run("extraMounts does not overlay INFO", func(t *testing.T) {
		containers := []corev1.Container{{}}
		applyReloadLoggingJavaOpts(containers, extraMountsURI, operatorURI, false)
		got := envValue(containers, jdkJavaOptionsEnvVarName)
		assert.NotContains(t, got, "-Dlog4j2.level=")
		assert.Contains(t, got, log4j2ConfigurationFileFlag+extraMountsURI+","+operatorURI)
	})

	t.Run("JAVA_ARGS_APPEND configurationFile gets operator URI", func(t *testing.T) {
		containers := []corev1.Container{{
			Env: []corev1.EnvVar{
				{Name: javaArgsAppendEnvVarName, Value: log4j2ConfigurationFileFlag + "/custom/log4j2.properties"},
			},
		}}
		applyReloadLoggingJavaOpts(containers, defaultLog4j2ConfigURI, operatorURI, true)
		got := envValue(containers, javaArgsAppendEnvVarName)
		assert.Equal(t, log4j2ConfigurationFileFlag+"/custom/log4j2.properties,"+operatorURI, got)
		assert.Equal(t, 1, strings.Count(got, log4j2ConfigurationFileFlag))
	})
}

func TestPodTemplateSpecForCR_LoggingCompositeExtraMounts(t *testing.T) {
	certPEM, keyPEM := mustTestKeyPair(t)

	ns := "test"
	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "my-broker", Namespace: ns},
		Spec: v1beta2.BrokerSpec{
			ExtraMounts: v1beta2.ExtraMountsType{
				ConfigMaps: []string{"custom-logging-config"},
			},
		},
	}

	operandSecret := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperandCertSecretName, Namespace: ns},
		Data:       map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
	}
	operatorCert := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCertSecretName, Namespace: ns},
		Data:       map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
	}
	operatorCA := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCASecretName, Namespace: ns},
		Data:       map[string][]byte{"ca.pem": certPEM},
	}

	common.SetOperatorNameSpace(ns)
	t.Cleanup(common.UnsetOperatorNameSpace)

	k8sClient := fake.NewClientBuilder().WithObjects(operandSecret, operatorCert, operatorCA).Build()
	reconciler := NewBrokerReconcilerImpl(cr, NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift))
	namer := MakeNamersForBroker(cr)

	pts, err := reconciler.PodTemplateSpecForCR(cr, *namer, &appsv1.StatefulSet{}, k8sClient)
	assert.NoError(t, err)
	assert.NotNil(t, pts)

	userURI := "/amq/extra/configmaps/custom-logging-config/logging.properties"
	operatorURI := "/amq/extra/configmaps/my-broker-logging-config/logging.properties"

	var jdkOpts string
	for _, env := range pts.Spec.Containers[0].Env {
		if env.Name == jdkJavaOptionsEnvVarName {
			jdkOpts = env.Value
		}
	}
	assert.Contains(t, jdkOpts, userURI)
	assert.Contains(t, jdkOpts, operatorURI)
	assert.Less(t, strings.Index(jdkOpts, userURI), strings.Index(jdkOpts, operatorURI))
	assert.Equal(t, 1, strings.Count(jdkOpts, log4j2ConfigurationFileFlag))
	assert.NotContains(t, jdkOpts, "-Dlog4j2.level=")
}

func TestPodTemplateSpecForCR_SidecarInitContainer(t *testing.T) {
	certPEM, keyPEM := mustTestKeyPair(t)

	ns := "test"
	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "my-broker", Namespace: ns},
	}

	operandSecret := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperandCertSecretName, Namespace: ns},
		Data:       map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
	}
	operatorCert := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCertSecretName, Namespace: ns},
		Data:       map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
	}
	operatorCA := &corev1.Secret{
		ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCASecretName, Namespace: ns},
		Data:       map[string][]byte{"ca.pem": certPEM},
	}

	common.SetOperatorNameSpace(ns)
	t.Cleanup(common.UnsetOperatorNameSpace)

	k8sClient := fake.NewClientBuilder().WithObjects(operandSecret, operatorCert, operatorCA).Build()
	reconciler := NewBrokerReconcilerImpl(cr, NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift))
	namer := MakeNamersForBroker(cr)

	pts, err := reconciler.PodTemplateSpecForCR(cr, *namer, &appsv1.StatefulSet{}, k8sClient)
	assert.NoError(t, err)
	assert.NotNil(t, pts)

	t.Run("sidecar init container exists with restartPolicy Always", func(t *testing.T) {
		assert.Equal(t, 1, len(pts.Spec.InitContainers))
		sidecar := pts.Spec.InitContainers[0]
		assert.Equal(t, sidecarContainerName, sidecar.Name)
		assert.NotEmpty(t, sidecar.Image, "sidecar image should be resolved from init image")
		assert.NotNil(t, sidecar.RestartPolicy)
		assert.Equal(t, corev1.ContainerRestartPolicyAlways, *sidecar.RestartPolicy)
	})

	t.Run("sidecar has required env vars", func(t *testing.T) {
		sidecar := pts.Spec.InitContainers[0]
		envMap := make(map[string]corev1.EnvVar)
		for _, e := range sidecar.Env {
			envMap[e.Name] = e
		}
		assert.Contains(t, envMap, "POD_NAME")
		assert.Contains(t, envMap, "POD_NAMESPACE")
		assert.Contains(t, envMap, "RELOAD_LOG_PATH")
		assert.Equal(t, "/app/log/reload.log", envMap["RELOAD_LOG_PATH"].Value)
	})

	t.Run("sidecar mounts data volume and script configmap", func(t *testing.T) {
		sidecar := pts.Spec.InitContainers[0]
		mountNames := make(map[string]bool)
		for _, vm := range sidecar.VolumeMounts {
			mountNames[vm.Name] = true
		}
		assert.True(t, mountNames[cr.Name], "sidecar should mount the data volume")
		assert.True(t, mountNames["configmap-"+cr.Name+brokerStatusScriptSuffix], "sidecar should mount the script configmap")
	})

	t.Run("broker container command does not contain status script launch", func(t *testing.T) {
		assert.NotEmpty(t, pts.Spec.Containers[0].Command)
		cmd := strings.Join(pts.Spec.Containers[0].Command, " ")
		assert.NotContains(t, cmd, "broker-status.sh")
		assert.NotContains(t, cmd, "status-script")
	})

	t.Run("sidecar has security context", func(t *testing.T) {
		sidecar := pts.Spec.InitContainers[0]
		assert.NotNil(t, sidecar.SecurityContext)
		assert.True(t, *sidecar.SecurityContext.RunAsNonRoot)
		assert.False(t, *sidecar.SecurityContext.AllowPrivilegeEscalation)
	})

	t.Run("sidecar has resource limits", func(t *testing.T) {
		sidecar := pts.Spec.InitContainers[0]
		assert.NotNil(t, sidecar.Resources.Requests)
		assert.NotNil(t, sidecar.Resources.Limits)
	})
}

func mustTestKeyPair(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	assert.NoError(t, err)

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}
