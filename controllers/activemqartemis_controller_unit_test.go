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

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta1"
	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	artemis_client "github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/artemis"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/jolokia"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/jolokia_client"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/selectors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("ActiveMQArtemis Controller Unit Tests", func() {

	Context("validate", func() {
		It("should fail validation when reserved label key is used in resource template", func() {
			cr := &v1beta2.Broker{
				Spec: v1beta2.BrokerSpec{
					ResourceTemplates: []v1beta2.ResourceTemplate{
						{
							Labels: map[string]string{selectors.LabelAppKey: "myAppKey"},
						},
					},
				},
			}

			namer := MakeNamers(cr)
			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			valid, retry := ri.validate(cr, k8sClient, *namer)

			Expect(valid).To(BeFalse())
			Expect(retry).To(BeFalse())
			Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, v1beta2.ValidConditionType)).To(BeTrue())

			condition := meta.FindStatusCondition(cr.Status.Conditions, v1beta2.ValidConditionType)
			Expect(condition.Reason).To(Equal(v1beta2.ValidConditionFailedReservedLabelReason))
			Expect(condition.Message).To(ContainSubstring("Templates[0]"))
		})

		It("should fail validation when broker properties have duplicate key", func() {
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

			Expect(valid).To(BeFalse())
			Expect(retry).To(BeFalse())
			Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, v1beta2.ValidConditionType)).To(BeTrue())

			condition := meta.FindStatusCondition(cr.Status.Conditions, v1beta2.ValidConditionType)
			Expect(condition.Reason).To(Equal(v1beta2.ValidConditionFailedDuplicateBrokerPropertiesKey))
			Expect(condition.Message).To(ContainSubstring("min"))
		})

		It("should fail validation when broker properties have duplicate key split on first equals", func() {
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

			Expect(valid).To(BeFalse())
			Expect(retry).To(BeFalse())
			Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, v1beta2.ValidConditionType)).To(BeTrue())

			condition := meta.FindStatusCondition(cr.Status.Conditions, v1beta2.ValidConditionType)
			Expect(condition.Reason).To(Equal(v1beta2.ValidConditionFailedDuplicateBrokerPropertiesKey))
			Expect(condition.Message).To(ContainSubstring("nameWith"))
		})

		It("should pass validation when broker properties keys differ after escaped equals", func() {
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

			Expect(valid).To(BeTrue())
			Expect(retry).To(BeFalse())
			Expect(meta.IsStatusConditionTrue(cr.Status.Conditions, brokerv1beta1.ValidConditionType)).To(BeTrue())
		})
	})

	Context("CheckStatus", func() {
		It("should cache pod status check and not call fake client twice", func() {
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

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			checkOk := func(brokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError {
				return nil
			}

			times := 0
			interceptorFuncs := interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					times++
					return apierrors.NewNotFound(schema.GroupResource{}, "")
				},
			}

			fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

			valid := ri.CheckStatus(cr, fakeClient, checkOk)
			Expect(valid).NotTo(BeNil())
			Expect(valid.Error()).To(ContainSubstring("Waiting for"))
			Expect(times).To(Equal(1))

			// repeat to verify fake client not called again
			valid = ri.CheckStatus(cr, fakeClient, checkOk)
			Expect(valid).NotTo(BeNil())
			Expect(valid.Error()).To(ContainSubstring("Waiting for"))
			Expect(times).To(Equal(1))
		})

		It("should cache jolokia status and not call jolokia twice", func() {
			cr := &v1beta2.Broker{
				ObjectMeta: v1.ObjectMeta{Name: "a"},
				Spec:       v1beta2.BrokerSpec{},
			}

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			checkOk := func(brokerStatus *brokerStatus, jk *jolokia_client.JkInfo) ArtemisError {
				return nil
			}

			mockCtrl := gomock.NewController(GinkgoT())
			defer mockCtrl.Finish()

			j := jolokia.NewMockIJolokia(mockCtrl)
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
			Expect(valid).NotTo(BeNil())
			Expect(strings.Contains(valid.Error(), "AttributeNotFoundException")).To(BeTrue())

			// verify status call is cached for second call
			valid = ri.CheckStatusFromJolokia(&jolokia_client.JkInfo{Artemis: a, IP: "IP", Ordinal: "0"}, checkOk)
			Expect(valid).NotTo(BeNil())
			Expect(strings.Contains(valid.Error(), "AttributeNotFoundException")).To(BeTrue())
		})
	})

	Context("Process with restricted mode", func() {
		It("should return error when secret not found", func() {
			boolTrue = true
			cr := &v1beta2.Broker{
				ObjectMeta: v1.ObjectMeta{Name: "a"},
				Spec: v1beta2.BrokerSpec{
					Restricted: &boolTrue,
				},
			}

			namer := MakeNamers(cr)
			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			times := 0
			interceptorFuncs := interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					times++
					return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
				},
			}

			common.SetOperatorNameSpace("test")
			DeferCleanup(common.UnsetOperatorNameSpace)

			fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

			err := ri.Process(cr, *namer, fakeClient, nil)

			Expect(err).NotTo(BeNil())
			Expect(err).To(MatchError(ContainSubstring("not found")))
		})
	})

	Context("MakeExtraVolumeMounts", func() {
		It("should return empty when no extra volumes", func() {
			cr := &v1beta2.Broker{
				Spec: v1beta2.BrokerSpec{},
			}

			volumeMounts := MakeExtraVolumeMounts(cr)
			Expect(volumeMounts).To(BeEmpty())
		})

		It("should return mount with default path for extra volumes", func() {
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
			Expect(volumeMounts).To(HaveLen(1))
			Expect(volumeMounts[0].Name).To(Equal("my-volume"))
			Expect(volumeMounts[0].MountPath).To(Equal("/amq/extra/volumes/my-volume"))
		})

		It("should use custom mount path when ExtraVolumeMounts override is provided", func() {
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
			Expect(volumeMounts).To(HaveLen(1))
			Expect(volumeMounts[0].Name).To(Equal("my-volume"))
			Expect(volumeMounts[0].MountPath).To(Equal("/custom/path"))
		})

		It("should return mount for extra volume claim templates", func() {
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
			Expect(volumeMounts).To(HaveLen(1))
			Expect(volumeMounts[0].Name).To(Equal("my-pvc"))
			Expect(volumeMounts[0].MountPath).To(Equal("/opt/my-pvc/data"))
		})

		It("should return mounts for both extra volumes and claim templates", func() {
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
			Expect(volumeMounts).To(HaveLen(2))
			Expect(volumeMounts[0].Name).To(Equal("my-volume"))
			Expect(volumeMounts[1].Name).To(Equal("my-pvc"))
		})
	})

	Context("validate with restricted mode and secret requirements", func() {
		It("should fail validation step by step until all secrets are present", func() {
			boolTrue = true
			cr := &v1beta2.Broker{
				ObjectMeta: v1.ObjectMeta{Name: "a"},
				Spec: v1beta2.BrokerSpec{
					Restricted: &boolTrue,
				},
			}

			namer := MakeNamers(cr)
			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			fakeSecrets := map[string]client.Object{}
			interceptorFuncs := interceptor.Funcs{
				Get: func(ctx context.Context, fakeClient client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if o, found := fakeSecrets[key.Name]; found {
						obj.SetName(o.GetName())
						return nil
					}
					return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
				},
			}

			common.SetOperatorNameSpace("test")
			DeferCleanup(common.UnsetOperatorNameSpace)

			fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

			By("validating with no secrets present")
			valid, retry := ri.validate(cr, fakeClient, *namer)

			Expect(valid).To(BeFalse())
			Expect(retry).To(BeTrue())
			Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType)).To(BeTrue())

			condition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
			Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionMissingResourcesReason))
			Expect(condition.Message).To(ContainSubstring("failed to get secret"))
			Expect(condition.Message).To(ContainSubstring(common.DefaultOperatorCertSecretName))

			By("adding operator cert secret")
			fakeSecrets[common.DefaultOperatorCertSecretName] = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCertSecretName},
			}

			valid, retry = ri.validate(cr, fakeClient, *namer)

			Expect(valid).To(BeFalse())
			Expect(retry).To(BeTrue())
			Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType)).To(BeTrue())
			condition = meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
			Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionMissingResourcesReason))
			Expect(condition.Message).To(ContainSubstring("failed to get secret"))
			Expect(condition.Message).To(ContainSubstring(common.DefaultOperatorCASecretName))

			By("adding operator CA secret")
			fakeSecrets[common.DefaultOperatorCASecretName] = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCASecretName},
			}

			valid, retry = ri.validate(cr, fakeClient, *namer)

			Expect(valid).To(BeFalse())
			Expect(retry).To(BeTrue())
			Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType)).To(BeTrue())
			condition = meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
			Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionMissingResourcesReason))
			Expect(condition.Message).To(ContainSubstring("failed to get secret"))
			Expect(condition.Message).To(ContainSubstring(common.DefaultOperandCertSecretName))

			By("adding operand cert secret")
			fakeSecrets[common.DefaultOperandCertSecretName] = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperandCertSecretName},
			}

			valid, retry = ri.validate(cr, fakeClient, *namer)

			Expect(valid).To(BeTrue())
			Expect(retry).To(BeFalse())
			Expect(meta.IsStatusConditionTrue(cr.Status.Conditions, brokerv1beta1.ValidConditionType)).To(BeTrue())
		})
	})

	Context("Reconcile", func() {
		It("should requeue with resync period when broker is not ready", func() {
			s := runtime.NewScheme()
			_ = brokerv1beta1.AddToScheme(s)
			_ = corev1.AddToScheme(s)
			_ = appsv1.AddToScheme(s)

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
			Expect(err).NotTo(HaveOccurred())
			Expect(res.RequeueAfter).To(Equal(common.GetReconcileResyncPeriod()))

			// refresh the crd to see the status update
			Expect(cl.Get(context.TODO(), req.NamespacedName, crd)).To(Succeed())
			Expect(meta.IsStatusConditionFalse(crd.Status.Conditions, brokerv1beta1.DeployedConditionType)).To(BeTrue())
		})
	})
})
