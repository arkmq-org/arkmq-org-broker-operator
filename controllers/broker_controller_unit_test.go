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

	brokerv1beta1 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	v1beta2 "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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

var _ = Describe("broker controller", func() {

	AfterEach(func() {
		common.UnsetOperatorNameSpace()
	})

	Context("secret validation", func() {

		It("should error when the required secret is not found", func() {
			cr := &v1beta2.Broker{
				ObjectMeta: v1.ObjectMeta{Name: "a"},
				Spec:       v1beta2.BrokerSpec{},
			}

			namer := MakeNamersForBroker(cr)

			r := NewBrokerReconciler(&NillCluster{}, ctrl.Log, isOpenshift)
			ri := NewBrokerReconcilerImpl(cr, r)

			interceptorFuncs := interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
				},
			}

			common.SetOperatorNameSpace("test")

			fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

			err := ri.Process(cr, *namer, fakeClient, nil)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})

		It("should require each secret in turn before marking valid", func() {
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

			fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

			By("missing operator cert secret")
			valid, retry := ri.validate(cr, fakeClient)
			Expect(valid).To(BeFalse())
			Expect(retry).To(BeTrue())
			Expect(meta.IsStatusConditionFalse(cr.Status.Conditions, brokerv1beta1.ValidConditionType)).To(BeTrue())
			condition := meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
			Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionMissingResourcesReason))
			Expect(condition.Message).To(ContainSubstring(common.DefaultOperatorCertSecretName))

			By("providing operator cert secret — now missing CA secret")
			fakeSecrets[common.DefaultOperatorCertSecretName] = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCertSecretName},
			}
			valid, retry = ri.validate(cr, fakeClient)
			Expect(valid).To(BeFalse())
			Expect(retry).To(BeTrue())
			condition = meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
			Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionMissingResourcesReason))
			Expect(condition.Message).To(ContainSubstring(common.DefaultOperatorCASecretName))

			By("providing CA secret — now missing operand cert secret")
			fakeSecrets[common.DefaultOperatorCASecretName] = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperatorCASecretName},
			}
			valid, retry = ri.validate(cr, fakeClient)
			Expect(valid).To(BeFalse())
			Expect(retry).To(BeTrue())
			condition = meta.FindStatusCondition(cr.Status.Conditions, brokerv1beta1.ValidConditionType)
			Expect(condition.Reason).To(Equal(brokerv1beta1.ValidConditionMissingResourcesReason))
			Expect(condition.Message).To(ContainSubstring(common.DefaultOperandCertSecretName))

			By("providing all secrets — should be valid")
			fakeSecrets[common.DefaultOperandCertSecretName] = &corev1.Secret{
				ObjectMeta: v1.ObjectMeta{Name: common.DefaultOperandCertSecretName},
			}
			valid, retry = ri.validate(cr, fakeClient)
			Expect(valid).To(BeTrue())
			Expect(retry).To(BeFalse())
			Expect(meta.IsStatusConditionTrue(cr.Status.Conditions, brokerv1beta1.ValidConditionType)).To(BeTrue())
		})
	})
})
