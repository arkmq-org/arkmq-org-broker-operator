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
	"time"

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/brokerproperties"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/resources/environments"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia_client"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
)

func mustTestKeyPairGinkgo() (certPEM, keyPEM []byte) {
	return mustTestKeyPairCNGinkgo("test")
}

func mustTestKeyPairCNGinkgo(commonName string) (certPEM, keyPEM []byte) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certPEM, keyPEM
}

var _ = Describe("broker controller unit", func() {

	It("returns error on not found secret", Label(unitLabel), func() {
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
		DeferCleanup(common.UnsetOperatorNameSpace)

		cl := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

		processErr := ri.Process(cr, *namer, cl, nil)

		Expect(processErr).NotTo(BeNil())
		Expect(processErr.Error()).To(ContainSubstring("not found"))
	})

	Context("repeated reconcile does not update stateful set", func() {
		var deployed, requested *appsv1.StatefulSet
		var reconciler2 *BrokerReconcilerImpl

		BeforeEach(func() {
			certPEM, keyPEM := mustGenerateKeyPairCN("test")

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
			DeferCleanup(common.UnsetOperatorNameSpace)

			localClient := fake.NewClientBuilder().WithObjects(operandSecret, operatorCert, operatorCA).Build()
			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			namer := MakeNamersForBroker(cr)

			reconciler1 := NewBrokerReconcilerImpl(cr, r)
			firstPTS, err := reconciler1.PodTemplateSpecForCR(cr, *namer, &appsv1.StatefulSet{}, localClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(firstPTS.Spec.InitContainers).To(HaveLen(1), "should have one sidecar init container")

			deployed = &appsv1.StatefulSet{
				ObjectMeta: v1.ObjectMeta{Name: namer.SsNameBuilder.Name(), Namespace: ns},
				Spec:       appsv1.StatefulSetSpec{Template: *firstPTS},
			}

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

			currentClone := deployed.DeepCopy()
			reconciler2 = NewBrokerReconcilerImpl(cr, r)
			secondPTS, err := reconciler2.PodTemplateSpecForCR(cr, *namer, currentClone, localClient)
			Expect(err).NotTo(HaveOccurred())

			requested = &appsv1.StatefulSet{
				ObjectMeta: v1.ObjectMeta{Name: namer.SsNameBuilder.Name(), Namespace: ns},
				Spec:       appsv1.StatefulSetSpec{Template: *secondPTS},
			}
		})

		It("main container preserves server-side defaults", Label(unitLabel), func() {
			deployedMain := deployed.Spec.Template.Spec.Containers[0]
			requestedMain := requested.Spec.Template.Spec.Containers[0]
			Expect(requestedMain.TerminationMessagePath).To(Equal(deployedMain.TerminationMessagePath),
				"MakeContainer reuses the deployed container, preserving TerminationMessagePath")
			Expect(requestedMain.ImagePullPolicy).To(Equal(deployedMain.ImagePullPolicy),
				"MakeContainer reuses the deployed container, preserving ImagePullPolicy")
		})

		It("sidecar container preserves server-side defaults from deployed state", Label(unitLabel), func() {
			deployedSidecar := deployed.Spec.Template.Spec.InitContainers[0]
			requestedSidecar := requested.Spec.Template.Spec.InitContainers[0]
			Expect(requestedSidecar.TerminationMessagePath).To(Equal(deployedSidecar.TerminationMessagePath),
				"sidecar should preserve TerminationMessagePath from deployed state")
			Expect(requestedSidecar.TerminationMessagePolicy).To(Equal(deployedSidecar.TerminationMessagePolicy),
				"sidecar should preserve TerminationMessagePolicy from deployed state")
			Expect(requestedSidecar.ImagePullPolicy).To(Equal(deployedSidecar.ImagePullPolicy),
				"sidecar should preserve ImagePullPolicy from deployed state")
		})

		It("CompareMetaAndSpec should detect no change on repeated reconcile", Label(unitLabel), func() {
			isEqual := reconciler2.CompareMetaAndSpec(deployed, requested)
			Expect(isEqual).To(BeTrue(),
				"repeated reconcile should not detect changes in the StatefulSet; "+
					"the sidecar init container is rebuilt from scratch each reconcile, losing "+
					"server-side applied defaults (TerminationMessagePath, TerminationMessagePolicy, "+
					"ImagePullPolicy), which causes a false diff and unnecessary StatefulSet update")
		})
	})

	It("validates restricted needs secret", Label(unitLabel), func() {
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
		DeferCleanup(common.UnsetOperatorNameSpace)

		cl := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

		valid, retry := ri.validate(cr, cl)

		Expect(valid).To(BeFalse())
		Expect(retry).To(BeTrue())

		Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType)).To(BeTrue())

		condition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
		Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionMissingResourcesReason))
		Expect(condition.Message).To(ContainSubstring("failed to get secret"))
		Expect(condition.Message).To(ContainSubstring(common.DefaultOperatorCertSecretName))

		fakeSecrets[common.DefaultOperatorCertSecretName] = &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCertSecretName},
		}

		valid, retry = ri.validate(cr, cl)

		Expect(valid).To(BeFalse())
		Expect(retry).To(BeTrue())
		Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType)).To(BeTrue())
		condition = meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
		Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionMissingResourcesReason))
		Expect(condition.Message).To(ContainSubstring("failed to get secret"))
		Expect(condition.Message).To(ContainSubstring(common.DefaultOperatorCASecretName))

		fakeSecrets[common.DefaultOperatorCASecretName] = &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCASecretName},
		}
		valid, retry = ri.validate(cr, cl)

		Expect(valid).To(BeFalse())
		Expect(retry).To(BeTrue())
		Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType)).To(BeTrue())
		condition = meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
		Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionMissingResourcesReason))
		Expect(condition.Message).To(ContainSubstring("failed to get secret"))
		Expect(condition.Message).To(ContainSubstring(common.DefaultOperandCertSecretName))

		fakeSecrets[common.DefaultOperandCertSecretName] = &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperandCertSecretName},
		}
		valid, retry = ri.validate(cr, cl)

		Expect(valid).To(BeTrue())
		Expect(retry).To(BeFalse())
		Expect(meta.IsStatusConditionTrue(cr.Status.Conditions, brokerv1beta1.ValidConditionType)).To(BeTrue())
	})

	It("makes namers for broker using broker tracking label", Label(unitLabel), func() {
		cr := &v1beta2.Broker{
			ObjectMeta: v1.ObjectMeta{Name: "my-broker"},
		}

		namer := MakeNamersForBroker(cr)
		labels := namer.LabelBuilder.Labels()

		Expect(labels[selectors.LabelBrokerKey]).To(Equal("my-broker"))
		Expect(labels[selectors.LabelAppKey]).To(Equal("my-broker-app"))
		Expect(labels[selectors.LabelPartOfKey]).To(Equal(selectors.LabelPartOfValue))
		_, hasActiveMQArtemis := labels[selectors.LabelActiveMQArtemisKey]
		Expect(hasActiveMQArtemis).To(BeFalse())

		defaultLabels := GetDefaultLabelsForBroker(cr)
		Expect(labels).To(Equal(defaultLabels))
	})

	Context("validate reserved labels for broker", func() {
		It("rejects Broker reserved key in Spec.Labels", Label(unitLabel), func() {
			cr := &v1beta2.Broker{
				Spec: v1beta2.BrokerSpec{
					Labels: map[string]string{selectors.LabelBrokerKey: "x"},
				},
			}
			condition := validateReservedLabelsForBroker(cr)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Reason).To(Equal(v1beta2.ValidConditionFailedReservedLabelReason))
			Expect(condition.Message).To(ContainSubstring("Spec.Labels"))
		})

		It("rejects application reserved key in Spec.Labels", Label(unitLabel), func() {
			cr := &v1beta2.Broker{
				Spec: v1beta2.BrokerSpec{
					Labels: map[string]string{selectors.LabelAppKey: "x"},
				},
			}
			condition := validateReservedLabelsForBroker(cr)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Reason).To(Equal(v1beta2.ValidConditionFailedReservedLabelReason))
		})

		It("rejects part-of reserved key in Spec.Labels", Label(unitLabel), func() {
			cr := &v1beta2.Broker{
				Spec: v1beta2.BrokerSpec{
					Labels: map[string]string{selectors.LabelPartOfKey: "x"},
				},
			}
			condition := validateReservedLabelsForBroker(cr)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Reason).To(Equal(v1beta2.ValidConditionFailedReservedLabelReason))
		})

		It("allows custom labels", Label(unitLabel), func() {
			cr := &v1beta2.Broker{
				Spec: v1beta2.BrokerSpec{
					Labels: map[string]string{"team": "messaging"},
				},
			}
			Expect(validateReservedLabelsForBroker(cr)).To(BeNil())
		})

		It("allows ActiveMQArtemis key on Broker CR", Label(unitLabel), func() {
			cr := &v1beta2.Broker{
				Spec: v1beta2.BrokerSpec{
					Labels: map[string]string{selectors.LabelActiveMQArtemisKey: "x"},
				},
			}
			Expect(validateReservedLabelsForBroker(cr)).To(BeNil())
		})

		It("rejects Broker reserved key in ResourceTemplates", Label(unitLabel), func() {
			cr := &v1beta2.Broker{
				Spec: v1beta2.BrokerSpec{
					ResourceTemplates: []v1beta2.ResourceTemplate{
						{Labels: map[string]string{selectors.LabelBrokerKey: "x"}},
					},
				},
			}
			condition := validateReservedLabelsForBroker(cr)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Reason).To(Equal(v1beta2.ValidConditionFailedReservedLabelReason))
			Expect(condition.Message).To(ContainSubstring("Spec.ResourceTemplates[0].Labels"))
		})
	})

	Context("reconcile me annotation predicate", func() {
		pred := reconcileMeAnnotationPredicate()

		It("CreateFunc with annotation present", Label(unitLabel), func() {
			pod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
			}}
			Expect(pred.Create(event.CreateEvent{Object: pod})).To(BeTrue())
		})

		It("CreateFunc without annotation", Label(unitLabel), func() {
			pod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{}}
			Expect(pred.Create(event.CreateEvent{Object: pod})).To(BeFalse())
		})

		It("UpdateFunc annotation value changed", Label(unitLabel), func() {
			oldPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
			}}
			newPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300005"},
			}}
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod})).To(BeTrue())
		})

		It("UpdateFunc annotation added", Label(unitLabel), func() {
			oldPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{}}
			newPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
			}}
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod})).To(BeTrue())
		})

		It("UpdateFunc annotation unchanged", Label(unitLabel), func() {
			oldPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
			}}
			newPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
			}}
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod})).To(BeFalse())
		})

		It("UpdateFunc annotation removed", Label(unitLabel), func() {
			oldPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
			}}
			newPod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{}}
			Expect(pred.Update(event.UpdateEvent{ObjectOld: oldPod, ObjectNew: newPod})).To(BeFalse())
		})

		It("DeleteFunc always false", Label(unitLabel), func() {
			pod := &corev1.Pod{ObjectMeta: v1.ObjectMeta{
				Annotations: map[string]string{ReconcileMeAnnotationKey: "1719300000"},
			}}
			Expect(pred.Delete(event.DeleteEvent{Object: pod})).To(BeFalse())
		})
	})

	It("maps pod to broker CR", Label(unitLabel), func() {
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
		Expect(requests).To(HaveLen(1))
		Expect(requests[0].Name).To(Equal("my-broker"))
		Expect(requests[0].Namespace).To(Equal("test-ns"))
	})

	It("maps pod to broker CR with no broker owner", Label(unitLabel), func() {
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
		Expect(requests).To(HaveLen(0))
	})

	Context("check projection status", func() {
		var (
			checksum string
		)

		BeforeEach(func() {
			checksum = brokerproperties.Alder32FromData([]byte("globalMaxSize=128m"))
		})

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

		It("all synced returns nil", Label(unitLabel), func() {
			proj := &projection{
				Name:            "test-props",
				ResourceVersion: "1",
				Files:           map[string]propertyFile{"broker.properties": {Alder32: checksum}},
			}
			ri, cr, fakeClient := newReconcilerWithStatus(map[string]propertiesStatus{
				"broker.properties": {Alder32: checksum},
			})
			result := ri.checkProjectionStatus(cr, fakeClient, proj, extractStatus)
			Expect(result).To(BeNil())
		})

		It("checksum mismatch returns OutOfSync", Label(unitLabel), func() {
			proj := &projection{
				Name:            "test-props",
				ResourceVersion: "1",
				Files:           map[string]propertyFile{"broker.properties": {Alder32: checksum}},
			}
			ri, cr, fakeClient := newReconcilerWithStatus(map[string]propertiesStatus{
				"broker.properties": {Alder32: "99999"},
			})
			result := ri.checkProjectionStatus(cr, fakeClient, proj, extractStatus)
			Expect(result).NotTo(BeNil())
			_, ok := result.(statusOutOfSyncError)
			Expect(ok).To(BeTrue(), "expected statusOutOfSyncError, got %T", result)
		})

		It("synced with apply errors returns InSyncApplyError", Label(unitLabel), func() {
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
			Expect(result).NotTo(BeNil())
			_, ok := result.(inSyncApplyError)
			Expect(ok).To(BeTrue(), "expected inSyncApplyError, got %T", result)
			Expect(result.Error()).To(ContainSubstring("addressFullMessagePolicy=INVALID"))
		})

		It("unchecked prefix files are ignored when missing", Label(unitLabel), func() {
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
			Expect(result).To(BeNil(), "underscore-prefixed files should not cause missing key errors")
		})

		It("missing file returns OutOfSyncMissingKey", Label(unitLabel), func() {
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
			Expect(result).NotTo(BeNil())
			_, ok := result.(statusOutOfSyncMissingKeyError)
			Expect(ok).To(BeTrue(), "expected statusOutOfSyncMissingKeyError, got %T", result)
		})
	})

	Context("pod template spec for CR sidecar init container", func() {
		var pts *corev1.PodTemplateSpec

		BeforeEach(func() {
			certPEM, keyPEM := mustTestKeyPairGinkgo()

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
			DeferCleanup(common.UnsetOperatorNameSpace)

			localClient := fake.NewClientBuilder().WithObjects(operandSecret, operatorCert, operatorCA).Build()
			reconciler := NewBrokerReconcilerImpl(cr, NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift))
			namer := MakeNamersForBroker(cr)

			var err error
			pts, err = reconciler.PodTemplateSpecForCR(cr, *namer, &appsv1.StatefulSet{}, localClient)
			Expect(err).NotTo(HaveOccurred())
			Expect(pts).NotTo(BeNil())
		})

		It("sidecar init container exists with restartPolicy Always", Label(unitLabel), func() {
			Expect(pts.Spec.InitContainers).To(HaveLen(1))
			sidecar := pts.Spec.InitContainers[0]
			Expect(sidecar.Name).To(Equal(sidecarContainerName))
			Expect(sidecar.Image).NotTo(BeEmpty(), "sidecar image should be resolved from init image")
			Expect(sidecar.RestartPolicy).NotTo(BeNil())
			Expect(*sidecar.RestartPolicy).To(Equal(corev1.ContainerRestartPolicyAlways))
		})

		It("sidecar has required env vars", Label(unitLabel), func() {
			sidecar := pts.Spec.InitContainers[0]
			envMap := make(map[string]corev1.EnvVar)
			for _, e := range sidecar.Env {
				envMap[e.Name] = e
			}
			Expect(envMap).To(HaveKey("POD_NAME"))
			Expect(envMap).To(HaveKey("POD_NAMESPACE"))
			Expect(envMap).To(HaveKey("RELOAD_LOG_PATH"))
			Expect(envMap["RELOAD_LOG_PATH"].Value).To(Equal("/app/log/event_stream.log"))
		})

		It("broker container mounts sidecar secret", Label(unitLabel), func() {
			broker := pts.Spec.Containers[0]
			mountNames := make(map[string]bool)
			for _, vm := range broker.VolumeMounts {
				mountNames[vm.Name] = true
			}
			Expect(mountNames["secret-my-broker"+sidecarSecretSuffix]).To(BeTrue(), "broker should mount the sidecar secret")
		})

		It("broker container command does not contain status script launch", Label(unitLabel), func() {
			Expect(pts.Spec.Containers[0].Command).NotTo(BeEmpty())
			cmd := strings.Join(pts.Spec.Containers[0].Command, " ")
			Expect(cmd).NotTo(ContainSubstring("broker-status.sh"))
			Expect(cmd).NotTo(ContainSubstring("status-script"))
		})

		It("sidecar has security context", Label(unitLabel), func() {
			sidecar := pts.Spec.InitContainers[0]
			Expect(sidecar.SecurityContext).NotTo(BeNil())
			Expect(*sidecar.SecurityContext.RunAsNonRoot).To(BeTrue())
			Expect(*sidecar.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
		})

		It("sidecar has resource limits", Label(unitLabel), func() {
			sidecar := pts.Spec.InitContainers[0]
			Expect(sidecar.Resources.Limits).NotTo(BeNil())
		})

		It("broker container has log4j2 configurationFile with default and operator URIs", Label(unitLabel), func() {
			var jdkOpts string
			for _, env := range pts.Spec.Containers[0].Env {
				if env.Name == jdkJavaOptionsEnvVarName {
					jdkOpts = env.Value
					break
				}
			}
			sidecarSecretPath := path.Join(common.SecretPathBase, "my-broker"+sidecarSecretSuffix)
			operatorURI := path.Join(sidecarSecretPath, LoggingConfigKey)
			Expect(jdkOpts).To(ContainSubstring(log4j2ConfigurationFileFlag + operatorURI))
		})
	})

	It("empty env produces no env vars", Label(unitLabel), func() {
		cr := &v1beta2.Broker{
			ObjectMeta: v1.ObjectMeta{Name: "broker"},
			Spec:       v1beta2.BrokerSpec{},
		}

		namer := MakeNamersForBroker(cr)
		envVars := MakeEnvVarArrayForCRForBroker(cr, *namer)

		Expect(envVars).To(BeEmpty(), "Broker CR with empty Spec.Env must produce no env vars")
	})

	It("spec env passed through", Label(unitLabel), func() {
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

		Expect(envVars).To(HaveLen(3))
		Expect(envVars[0].Name).To(Equal("MY_VAR"))
		Expect(envVars[0].Value).To(Equal("my-value"))
		Expect(envVars[1].Name).To(Equal("AMQ_NAME"))
		Expect(envVars[1].Value).To(Equal("my-custom-broker"))
		Expect(envVars[2].Name).To(Equal("ANOTHER_VAR"))
		Expect(envVars[2].Value).To(Equal("another-value"))
	})

	It("extra broker properties absent when not set", Label(unitLabel), func() {
		cr := &v1beta2.Broker{
			ObjectMeta: v1.ObjectMeta{Name: "broker"},
			Spec:       v1beta2.BrokerSpec{},
		}

		namer := MakeNamersForBroker(cr)
		envVars := MakeEnvVarArrayForCRForBroker(cr, *namer)

		for _, e := range envVars {
			Expect(e.Name).NotTo(Equal(environments.ExtraBrokerPropertiesEnvVar),
				"EXTRA_BROKER_PROPERTIES must not be injected when user has not set it")
		}

		r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
		ri := NewBrokerReconcilerImpl(cr, r)
		result := ri.brokerPropertiesConfigSystemPropValue("/config/", "my-resource",
			map[string][]byte{"broker.properties": []byte("")})
		Expect(strings.Contains(result, "$(EXTRA_BROKER_PROPERTIES)")).To(BeFalse(),
			"brokerPropertiesConfigSystemPropValue must not contain the token when EXTRA_BROKER_PROPERTIES is unset")
	})

	It("extra broker properties user value appears with token", Label(unitLabel), func() {
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
				Expect(e.Value).To(Equal(extraPaths))
				found = true
				break
			}
		}
		Expect(found).To(BeTrue(), "EXTRA_BROKER_PROPERTIES with user value must be in env array")

		r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
		ri := NewBrokerReconcilerImpl(cr, r)
		result := ri.brokerPropertiesConfigSystemPropValue("/config/", "my-resource",
			map[string][]byte{"broker.properties": []byte("")})
		Expect(strings.HasSuffix(result, ",$(EXTRA_BROKER_PROPERTIES)")).To(BeTrue(),
			"brokerPropertiesConfigSystemPropValue must end with ,$(EXTRA_BROKER_PROPERTIES) when user has set it")
	})

	Context("extra broker properties valueFrom passed through", func() {
		type verifyFunc func(e corev1.EnvVar)

		testCases := []struct {
			name      string
			envSource *corev1.EnvVarSource
			verify    verifyFunc
		}{
			{
				name: "secretKeyRef",
				envSource: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: "my-secret"},
						Key:                  "props-path",
					},
				},
				verify: func(e corev1.EnvVar) {
					Expect(e.ValueFrom).NotTo(BeNil())
					Expect(e.ValueFrom.SecretKeyRef).NotTo(BeNil())
					Expect(e.ValueFrom.SecretKeyRef.Name).To(Equal("my-secret"))
					Expect(e.ValueFrom.SecretKeyRef.Key).To(Equal("props-path"))
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
				verify: func(e corev1.EnvVar) {
					Expect(e.ValueFrom).NotTo(BeNil())
					Expect(e.ValueFrom.ConfigMapKeyRef).NotTo(BeNil())
					Expect(e.ValueFrom.ConfigMapKeyRef.Name).To(Equal("my-cm"))
					Expect(e.ValueFrom.ConfigMapKeyRef.Key).To(Equal("props-path"))
				},
			},
		}

		for _, tc := range testCases {
			tc := tc
			It(tc.name, Label(unitLabel), func() {
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

				found := false
				for _, e := range envVars {
					if e.Name == environments.ExtraBrokerPropertiesEnvVar {
						tc.verify(e)
						found = true
						break
					}
				}
				Expect(found).To(BeTrue(), "EXTRA_BROKER_PROPERTIES with valueFrom must be in env array")

				r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
				ri := NewBrokerReconcilerImpl(cr, r)
				result := ri.brokerPropertiesConfigSystemPropValue("/config/", "my-resource",
					map[string][]byte{"broker.properties": []byte("")})
				Expect(strings.HasSuffix(result, ",$(EXTRA_BROKER_PROPERTIES)")).To(BeTrue(),
					"brokerPropertiesConfigSystemPropValue must end with ,$(EXTRA_BROKER_PROPERTIES)")
			})
		}
	})

	It("Reconcile requeues when ProcessBrokerStatus returns retry", Label(unitLabel), func() {
		ns := "test-ns"

		s := runtime.NewScheme()
		_ = v1beta2.SchemeBuilder.AddToScheme(s)
		_ = corev1.AddToScheme(s)
		_ = appsv1.AddToScheme(s)

		cr := &v1beta2.Broker{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-broker",
				Namespace: ns,
				Annotations: map[string]string{
					common.BlockReconcileAnnotation: "true",
				},
			},
		}
		meta.SetStatusCondition(&cr.Status.Conditions, v1.Condition{
			Type:   v1beta2.DeployedConditionType,
			Status: v1.ConditionTrue,
			Reason: v1beta2.DeployedConditionReadyReason,
		})

		certPEM, keyPEM := mustTestKeyPairGinkgo()

		operatorCert := &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCertSecretName, Namespace: ns},
			Data:       map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
		}
		operatorCA := &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCASecretName, Namespace: ns},
			Data:       map[string][]byte{"ca.pem": certPEM},
		}
		operandCert := &corev1.Secret{
			ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperandCertSecretName, Namespace: ns},
			Data:       map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM},
		}

		common.SetOperatorNameSpace(ns)
		DeferCleanup(common.UnsetOperatorNameSpace)

		cl := fake.NewClientBuilder().WithScheme(s).
			WithObjects(cr, operatorCert, operatorCA, operandCert).
			WithStatusSubresource(cr).Build()

		r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
		r.Client = cl
		r.Scheme = s

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      "test-broker",
				Namespace: ns,
			},
		}

		res, err := r.Reconcile(context.TODO(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(common.GetReconcileResyncPeriod()),
			"Reconcile must requeue when ProcessBrokerStatus returns true "+
				"(jolokia unreachable); the if block at broker_controller.go:148 "+
				"captures this retry signal into requeueRequest")
	})
})
