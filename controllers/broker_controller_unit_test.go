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
	"path"
	"strings"
	"testing"
	"time"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/environments"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia_client"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestMakeNamersForBrokerUsesBrokerTrackingLabel(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "my-broker"},
	}

	namer := MakeNamersForBroker(cr)
	labels := namer.LabelBuilder.Labels()

	assert.Equal(t, "my-broker", labels[selectors.LabelBrokerKey])
	assert.Equal(t, "my-broker-app", labels[selectors.LabelAppKey])
	assert.Equal(t, selectors.LabelPartOfValue, labels[selectors.LabelPartOfKey])
	_, hasActiveMQArtemis := labels[selectors.LabelActiveMQArtemisKey]
	assert.False(t, hasActiveMQArtemis)

	defaultLabels := GetDefaultLabelsForBroker(cr)
	assert.Equal(t, labels, defaultLabels)
}

func TestValidateReservedLabelsForBroker(t *testing.T) {
	t.Run("rejects Broker reserved key in Spec.Labels", func(t *testing.T) {
		cr := &v1beta2.Broker{
			Spec: v1beta2.BrokerSpec{
				Labels: map[string]string{selectors.LabelBrokerKey: "x"},
			},
		}
		condition := validateReservedLabelsForBroker(cr)
		assert.NotNil(t, condition)
		assert.Equal(t, v1beta2.ValidConditionFailedReservedLabelReason, condition.Reason)
		assert.Contains(t, condition.Message, "Spec.Labels")
	})

	t.Run("rejects application reserved key in Spec.Labels", func(t *testing.T) {
		cr := &v1beta2.Broker{
			Spec: v1beta2.BrokerSpec{
				Labels: map[string]string{selectors.LabelAppKey: "x"},
			},
		}
		condition := validateReservedLabelsForBroker(cr)
		assert.NotNil(t, condition)
		assert.Equal(t, v1beta2.ValidConditionFailedReservedLabelReason, condition.Reason)
	})

	t.Run("rejects part-of reserved key in Spec.Labels", func(t *testing.T) {
		cr := &v1beta2.Broker{
			Spec: v1beta2.BrokerSpec{
				Labels: map[string]string{selectors.LabelPartOfKey: "x"},
			},
		}
		condition := validateReservedLabelsForBroker(cr)
		assert.NotNil(t, condition)
		assert.Equal(t, v1beta2.ValidConditionFailedReservedLabelReason, condition.Reason)
	})

	t.Run("allows custom labels", func(t *testing.T) {
		cr := &v1beta2.Broker{
			Spec: v1beta2.BrokerSpec{
				Labels: map[string]string{"team": "messaging"},
			},
		}
		assert.Nil(t, validateReservedLabelsForBroker(cr))
	})

	t.Run("allows ActiveMQArtemis key on Broker CR", func(t *testing.T) {
		cr := &v1beta2.Broker{
			Spec: v1beta2.BrokerSpec{
				Labels: map[string]string{selectors.LabelActiveMQArtemisKey: "x"},
			},
		}
		assert.Nil(t, validateReservedLabelsForBroker(cr))
	})

	t.Run("rejects Broker reserved key in ResourceTemplates", func(t *testing.T) {
		cr := &v1beta2.Broker{
			Spec: v1beta2.BrokerSpec{
				ResourceTemplates: []v1beta2.ResourceTemplate{
					{Labels: map[string]string{selectors.LabelBrokerKey: "x"}},
				},
			},
		}
		condition := validateReservedLabelsForBroker(cr)
		assert.NotNil(t, condition)
		assert.Equal(t, v1beta2.ValidConditionFailedReservedLabelReason, condition.Reason)
		assert.Contains(t, condition.Message, "Spec.ResourceTemplates[0].Labels")
	})
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
		assert.Equal(t, "/app/log/event_stream.log", envMap["RELOAD_LOG_PATH"].Value)
	})

	t.Run("broker container mounts sidecar secret", func(t *testing.T) {
		broker := pts.Spec.Containers[0]
		mountNames := make(map[string]bool)
		for _, vm := range broker.VolumeMounts {
			mountNames[vm.Name] = true
		}
		assert.True(t, mountNames["secret-"+cr.Name+sidecarSecretSuffix], "broker should mount the sidecar secret")
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
		assert.NotNil(t, sidecar.Resources.Limits)
	})

	t.Run("broker container has log4j2 configurationFile with default and operator URIs", func(t *testing.T) {
		var jdkOpts string
		for _, env := range pts.Spec.Containers[0].Env {
			if env.Name == jdkJavaOptionsEnvVarName {
				jdkOpts = env.Value
				break
			}
		}
		sidecarSecretPath := path.Join(common.SecretPathBase, cr.Name+sidecarSecretSuffix)
		operatorURI := path.Join(sidecarSecretPath, LoggingConfigKey)
		assert.Contains(t, jdkOpts, log4j2ConfigurationFileFlag+operatorURI)
	})
}

func mustTestKeyPair(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	return mustTestKeyPairCN(t, "test")
}

func mustTestKeyPairCN(t *testing.T, commonName string) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
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

func TestBrokerCR_ExtraBrokerPropertiesAbsentWhenNotSet(t *testing.T) {

	// When the user does not define EXTRA_BROKER_PROPERTIES, the operator must not
	// inject it and must not append the token to the JVM property value.

	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "broker"},
		Spec:       v1beta2.BrokerSpec{},
	}

	namer := MakeNamersForBroker(cr)
	envVars := MakeEnvVarArrayForCRForBroker(cr, *namer)

	for _, e := range envVars {
		assert.NotEqual(t, environments.ExtraBrokerPropertiesEnvVar, e.Name,
			"EXTRA_BROKER_PROPERTIES must not be injected when user has not set it")
	}

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)
	result := ri.brokerPropertiesConfigSystemPropValue("/config/", "my-resource",
		map[string][]byte{"broker.properties": []byte("")})
	assert.False(t, strings.Contains(result, "$(EXTRA_BROKER_PROPERTIES)"),
		"brokerPropertiesConfigSystemPropValue must not contain the token when EXTRA_BROKER_PROPERTIES is unset")
}

func TestBrokerCR_ExtraBrokerPropertiesUserValueAppearsWithToken(t *testing.T) {

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

	found := false
	for _, e := range envVars {
		if e.Name == environments.ExtraBrokerPropertiesEnvVar {
			assert.Equal(t, extraPaths, e.Value)
			found = true
			break
		}
	}
	assert.True(t, found, "EXTRA_BROKER_PROPERTIES with user value must be in env array")

	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	ri := NewBrokerReconcilerImpl(cr, r)
	result := ri.brokerPropertiesConfigSystemPropValue("/config/", "my-resource",
		map[string][]byte{"broker.properties": []byte("")})
	assert.True(t, strings.HasSuffix(result, ",$(EXTRA_BROKER_PROPERTIES)"),
		"brokerPropertiesConfigSystemPropValue must end with ,$(EXTRA_BROKER_PROPERTIES) when user has set it")
}

func TestRepeatedReconcileDoesNotUpdateStatefulSet(t *testing.T) {
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
	r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
	namer := MakeNamersForBroker(cr)

	// First reconcile: generate desired PodTemplateSpec from scratch (no deployed state)
	reconciler1 := NewBrokerReconcilerImpl(cr, r)
	firstPTS, err := reconciler1.PodTemplateSpecForCR(cr, *namer, &appsv1.StatefulSet{}, k8sClient)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(firstPTS.Spec.InitContainers), "should have one sidecar init container")

	// Build the "deployed" StatefulSet as Kubernetes would store it
	deployed := &appsv1.StatefulSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      namer.SsNameBuilder.Name(),
			Namespace: ns,
		},
		Spec: appsv1.StatefulSetSpec{
			Template: *firstPTS,
		},
	}

	// Simulate Kubernetes API server applying defaults to all containers
	applyServerSideDefaults := func(containers []corev1.Container) {
		for i := range containers {
			c := &containers[i]
			if c.TerminationMessagePath == "" {
				c.TerminationMessagePath = "/dev/termination-log"
			}
			if c.TerminationMessagePolicy == "" {
				c.TerminationMessagePolicy = corev1.TerminationMessageReadFile
			}
			if c.ImagePullPolicy == "" {
				c.ImagePullPolicy = corev1.PullIfNotPresent
			}
		}
	}
	applyServerSideDefaults(deployed.Spec.Template.Spec.InitContainers)
	applyServerSideDefaults(deployed.Spec.Template.Spec.Containers)

	// Second reconcile: generate desired using a clone of the deployed state as current
	currentClone := deployed.DeepCopy()
	reconciler2 := NewBrokerReconcilerImpl(cr, r)
	secondPTS, err := reconciler2.PodTemplateSpecForCR(cr, *namer, currentClone, k8sClient)
	assert.NoError(t, err)

	requested := &appsv1.StatefulSet{
		ObjectMeta: v1.ObjectMeta{
			Name:      namer.SsNameBuilder.Name(),
			Namespace: ns,
		},
		Spec: appsv1.StatefulSetSpec{
			Template: *secondPTS,
		},
	}

	t.Run("main container preserves server-side defaults", func(t *testing.T) {
		deployedMain := deployed.Spec.Template.Spec.Containers[0]
		requestedMain := requested.Spec.Template.Spec.Containers[0]
		assert.Equal(t, deployedMain.TerminationMessagePath, requestedMain.TerminationMessagePath,
			"MakeContainer reuses the deployed container, preserving TerminationMessagePath")
		assert.Equal(t, deployedMain.ImagePullPolicy, requestedMain.ImagePullPolicy,
			"MakeContainer reuses the deployed container, preserving ImagePullPolicy")
	})

	t.Run("sidecar container preserves server-side defaults from deployed state", func(t *testing.T) {
		deployedSidecar := deployed.Spec.Template.Spec.InitContainers[0]
		requestedSidecar := requested.Spec.Template.Spec.InitContainers[0]

		assert.Equal(t, deployedSidecar.TerminationMessagePath, requestedSidecar.TerminationMessagePath,
			"sidecar should preserve TerminationMessagePath from deployed state")
		assert.Equal(t, deployedSidecar.TerminationMessagePolicy, requestedSidecar.TerminationMessagePolicy,
			"sidecar should preserve TerminationMessagePolicy from deployed state")
		assert.Equal(t, deployedSidecar.ImagePullPolicy, requestedSidecar.ImagePullPolicy,
			"sidecar should preserve ImagePullPolicy from deployed state")
	})

	t.Run("CompareMetaAndSpec should detect no change on repeated reconcile", func(t *testing.T) {
		isEqual := reconciler2.CompareMetaAndSpec(deployed, requested)
		assert.True(t, isEqual,
			"repeated reconcile should not detect changes in the StatefulSet; "+
				"the sidecar init container is rebuilt from scratch each reconcile, losing "+
				"server-side applied defaults (TerminationMessagePath, TerminationMessagePolicy, "+
				"ImagePullPolicy), which causes a false diff and unnecessary StatefulSet update")
	})
}

func TestBrokerCR_ExtraBrokerPropertiesValueFromPassedThrough(t *testing.T) {

	// When the user sets EXTRA_BROKER_PROPERTIES via valueFrom (e.g. secretKeyRef or configMapKeyRef),
	// the operator must leave it intact: the kubelet resolves the secret or configmap value first
	// and then expands $(EXTRA_BROKER_PROPERTIES) inside the JVM args correctly.

	for _, tc := range []struct {
		name       string
		envSource  *corev1.EnvVarSource
		verifyFunc func(t *testing.T, e corev1.EnvVar)
	}{
		{
			name: "secretKeyRef",
			envSource: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
					Key:                  "props-path",
				},
			},
			verifyFunc: func(t *testing.T, e corev1.EnvVar) {
				assert.NotNil(t, e.ValueFrom)
				assert.NotNil(t, e.ValueFrom.SecretKeyRef)
				assert.Equal(t, "my-secret", e.ValueFrom.SecretKeyRef.Name)
				assert.Equal(t, "props-path", e.ValueFrom.SecretKeyRef.Key)
			},
		},
		{
			name: "configMapKeyRef",
			envSource: &corev1.EnvVarSource{
				ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "my-cm"},
					Key:                  "props-path",
				},
			},
			verifyFunc: func(t *testing.T, e corev1.EnvVar) {
				assert.NotNil(t, e.ValueFrom)
				assert.NotNil(t, e.ValueFrom.ConfigMapKeyRef)
				assert.Equal(t, "my-cm", e.ValueFrom.ConfigMapKeyRef.Name)
				assert.Equal(t, "props-path", e.ValueFrom.ConfigMapKeyRef.Key)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cr := &v1beta2.Broker{
				ObjectMeta: v1.ObjectMeta{Name: "broker"},
				Spec: v1beta2.BrokerSpec{
					Env: []corev1.EnvVar{
						{
							Name:      environments.ExtraBrokerPropertiesEnvVar,
							ValueFrom: tc.envSource,
						},
					},
				},
			}

			namer := MakeNamersForBroker(cr)
			envVars := MakeEnvVarArrayForCRForBroker(cr, *namer)

			// valueFrom must be preserved unmodified by the operator
			found := false
			for _, e := range envVars {
				if e.Name == environments.ExtraBrokerPropertiesEnvVar {
					tc.verifyFunc(t, e)
					found = true
					break
				}
			}
			assert.True(t, found, "EXTRA_BROKER_PROPERTIES with valueFrom must be in env array")

			// The token must still be present in the JVM property so the kubelet can expand it
			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)
			result := ri.brokerPropertiesConfigSystemPropValue("/config/", "my-resource",
				map[string][]byte{"broker.properties": []byte("")})
			assert.True(t, strings.HasSuffix(result, ",$(EXTRA_BROKER_PROPERTIES)"),
				"brokerPropertiesConfigSystemPropValue must end with ,$(EXTRA_BROKER_PROPERTIES)")
		})
	}
}

func TestMakeEnvVarArrayForCRForBroker_EmptyEnv(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "broker"},
		Spec:       v1beta2.BrokerSpec{},
	}

	namer := MakeNamersForBroker(cr)
	envVars := MakeEnvVarArrayForCRForBroker(cr, *namer)

	assert.Empty(t, envVars, "Broker CR with empty Spec.Env must produce no env vars")
}

func TestMakeEnvVarArrayForCRForBroker_SpecEnvPassedThrough(t *testing.T) {
	cr := &v1beta2.Broker{
		ObjectMeta: v1.ObjectMeta{Name: "broker"},
		Spec: v1beta2.BrokerSpec{
			Env: []corev1.EnvVar{
				{Name: "MY_VAR", Value: "my-value"},
				{Name: "AMQ_NAME", Value: "my-custom-broker"},
				{Name: "ANOTHER_VAR", Value: "another-value"},
			},
		},
	}

	namer := MakeNamersForBroker(cr)
	envVars := MakeEnvVarArrayForCRForBroker(cr, *namer)

	require.Len(t, envVars, 3)
	assert.Equal(t, "MY_VAR", envVars[0].Name)
	assert.Equal(t, "my-value", envVars[0].Value)
	assert.Equal(t, "AMQ_NAME", envVars[1].Name)
	assert.Equal(t, "my-custom-broker", envVars[1].Value)
	assert.Equal(t, "ANOTHER_VAR", envVars[2].Name)
	assert.Equal(t, "another-value", envVars[2].Value)
}
