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

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	artemis_client "github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/artemis"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/jolokia_client"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/selectors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("activemqartemis controller unit", func() {

	Context("validate", func() {
		It("rejects reserved labels in resource templates", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				Spec: v1beta2.BrokerClusterSpec{
					ResourceTemplates: []v1beta2.ResourceTemplate{
						{
							Labels: map[string]string{selectors.LabelAppKey: "myAppKey"},
						},
					},
				},
			}

			namer := MakeNamers(cr)

			r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false, nil)
			ri := NewBrokerClusterReconcilerImpl(cr, r)

			valid, retry := ri.validate(cr, k8sClient, *namer)

			Expect(valid).To(BeFalse())
			Expect(retry).To(BeFalse())

			Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, v1beta2.ValidConditionType)).To(BeTrue())

			condition := meta.FindStatusCondition(cr.Status.Conditions, v1beta2.ValidConditionType)
			Expect(condition.Reason).To(Equal(v1beta2.ValidConditionFailedReservedLabelReason))
			Expect(strings.Contains(condition.Message, "Templates[0]")).To(BeTrue())
		})

		It("rejects duplicate broker properties", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				Spec: v1beta2.BrokerClusterSpec{
					BrokerProperties: []string{
						"min=X",
						"min=y",
					},
				},
			}

			namer := MakeNamers(cr)

			r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false, nil)
			ri := NewBrokerClusterReconcilerImpl(cr, r)

			valid, retry := ri.validate(cr, k8sClient, *namer)

			Expect(valid).To(BeFalse())
			Expect(retry).To(BeFalse())

			Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, v1beta2.ValidConditionType)).To(BeTrue())

			condition := meta.FindStatusCondition(cr.Status.Conditions, v1beta2.ValidConditionType)
			Expect(condition.Reason).To(Equal(v1beta2.ValidConditionFailedDuplicateBrokerPropertiesKey))
			Expect(strings.Contains(condition.Message, "min")).To(BeTrue())
		})

		It("rejects duplicate broker properties on first equals", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				Spec: v1beta2.BrokerClusterSpec{
					BrokerProperties: []string{
						"nameWith\\=equals_not_matched=X",
						"nameWith\\=equals_not_matched=Y",
					},
				},
			}

			namer := MakeNamers(cr)

			r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false, nil)
			ri := NewBrokerClusterReconcilerImpl(cr, r)

			valid, retry := ri.validate(cr, k8sClient, *namer)

			Expect(valid).To(BeFalse())
			Expect(retry).To(BeFalse())

			Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, v1beta2.ValidConditionType)).To(BeTrue())

			condition := meta.FindStatusCondition(cr.Status.Conditions, v1beta2.ValidConditionType)
			Expect(condition.Reason).To(Equal(v1beta2.ValidConditionFailedDuplicateBrokerPropertiesKey))
			Expect(strings.Contains(condition.Message, "nameWith")).To(BeTrue())
		})

		It("accepts distinct broker properties with escaped equals", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				Spec: v1beta2.BrokerClusterSpec{
					BrokerProperties: []string{
						"nameWith\\=equals_A_not_matched=X",
						"nameWith\\=equals_B_not_matched=Y",
					},
				},
			}

			namer := MakeNamers(cr)

			r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false, nil)
			ri := NewBrokerClusterReconcilerImpl(cr, r)

			valid, retry := ri.validate(cr, k8sClient, *namer)

			Expect(valid).To(BeTrue())
			Expect(retry).To(BeFalse())

			Expect(meta.IsStatusConditionTrue(cr.Status.Conditions, brokerv1beta1.ValidConditionType)).To(BeTrue())
		})
	})

	Context("status checks", func() {
		It("caches pod status check result", Label(unitLabel), func() {
			replicas := int32(1)
			cr := &v1beta2.BrokerCluster{
				ObjectMeta: v1.ObjectMeta{
					Name:      "broker",
					Namespace: "some-ns",
				},
				Spec: v1beta2.BrokerClusterSpec{
					DeploymentPlan: v1beta2.DeploymentPlanType{
						Size: &replicas,
					},
				},
				Status: v1beta2.BrokerClusterStatus{
					DeploymentPlanSize: replicas,
				},
			}

			r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false, nil)
			ri := NewBrokerClusterReconcilerImpl(cr, r)

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

			cl := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

			valid := ri.CheckStatus(cr, cl, checkOk)
			Expect(valid).NotTo(BeNil())
			Expect(valid.Error()).To(ContainSubstring("Waiting for"))
			Expect(times).To(Equal(1))

			valid = ri.CheckStatus(cr, cl, checkOk)
			Expect(valid).NotTo(BeNil())
			Expect(valid.Error()).To(ContainSubstring("Waiting for"))

			Expect(times).To(Equal(1))
		})

		It("caches jolokia status result", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				ObjectMeta: v1.ObjectMeta{Name: "a"},
				Spec:       v1beta2.BrokerClusterSpec{},
			}

			r := NewBrokerClusterReconciler(&NillCluster{}, ctrl.Log, isOpenshift, false, nil)
			ri := NewBrokerClusterReconcilerImpl(cr, r)

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

			valid = ri.CheckStatusFromJolokia(&jolokia_client.JkInfo{Artemis: a, IP: "IP", Ordinal: "0"}, checkOk)
			Expect(valid).NotTo(BeNil())
			Expect(strings.Contains(valid.Error(), "AttributeNotFoundException")).To(BeTrue())
		})
	})

	Context("extra volume mounts", func() {
		It("returns empty when no extra volumes", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				Spec: v1beta2.BrokerClusterSpec{},
			}

			volumeMounts := MakeExtraVolumeMounts(cr)
			Expect(volumeMounts).To(BeEmpty())
		})

		It("creates mounts for extra volumes", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				Spec: v1beta2.BrokerClusterSpec{
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

		It("uses mount override when specified", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				Spec: v1beta2.BrokerClusterSpec{
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

		It("creates mounts for extra volume claim templates", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				Spec: v1beta2.BrokerClusterSpec{
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

		It("creates mounts for both extra volumes and claims", Label(unitLabel), func() {
			cr := &v1beta2.BrokerCluster{
				Spec: v1beta2.BrokerClusterSpec{
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

	It("requeues on not ready", Label(unitLabel), func() {
		s := runtime.NewScheme()
		_ = brokerv1beta1.AddToScheme(s)
		_ = corev1.AddToScheme(s)
		_ = appsv1.AddToScheme(s)
		_ = netv1.AddToScheme(s)

		crd := &brokerv1beta1.ActiveMQArtemis{
			ObjectMeta: v1.ObjectMeta{
				Name:      "test-broker",
				Namespace: "default",
			},
			Spec: brokerv1beta1.ActiveMQArtemisSpec{},
		}

		cl := fake.NewClientBuilder().WithScheme(s).WithObjects(crd).WithStatusSubresource(crd).Build()

		r := NewActiveMQArtemisReconciler(&NillCluster{}, ctrl.Log, false, false)
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

		Expect(cl.Get(context.TODO(), req.NamespacedName, crd)).NotTo(HaveOccurred())
		Expect(meta.IsStatusConditionFalse(crd.Status.Conditions, brokerv1beta1.DeployedConditionType)).To(BeTrue())
	})
})
