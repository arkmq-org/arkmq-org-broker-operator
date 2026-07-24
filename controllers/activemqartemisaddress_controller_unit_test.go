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
	"errors"
	"reflect"
	"time"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("ActiveMQArtemisAddress Controller", func() {

	Context("when deleting an existing address", func() {
		It("should reconcile successfully without removeFromBrokerOnDelete", func() {
			testDeleteExistingAddressGinkgo(false)
		})

		It("should reconcile successfully with removeFromBrokerOnDelete", func() {
			testDeleteExistingAddressGinkgo(true)
		})
	})

	Context("when address is not found", func() {
		It("should return no error and no requeue", func() {
			interceptorFuncs := interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					return apierrors.NewNotFound(schema.GroupResource{}, "")
				},
			}
			fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

			r := NewActiveMQArtemisAddressReconciler(fakeClient, nil, logr.New(log.NullLogSink{}))

			result, err := r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test-namespace", Name: "test-name"}})

			Expect(err).To(BeNil())
			Expect(result.Requeue).To(BeFalse())
			Expect(result.RequeueAfter).To(Equal(time.Duration(0)))
		})
	})

	Context("when an internal error occurs", func() {
		It("should return the error", func() {
			internalError := apierrors.NewInternalError(errors.New("internal-error"))

			interceptorFuncs := interceptor.Funcs{
				Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					return internalError
				},
			}
			fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

			r := NewActiveMQArtemisAddressReconciler(fakeClient, nil, logr.New(log.NullLogSink{}))

			result, err := r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test-namespace", Name: "test-name"}})

			Expect(err).To(Equal(internalError))
			Expect(result.Requeue).To(BeFalse())
			Expect(result.RequeueAfter).To(Equal(time.Duration(0)))
		})
	})
})

func testDeleteExistingAddressGinkgo(removeFromBrokerOnDelete bool) {
	addressExists := true
	interceptorFuncs := interceptor.Funcs{
		Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			if addressExists && key.Name == "test-name" && reflect.TypeOf(obj) == reflect.TypeOf(&v1beta1.ActiveMQArtemisAddress{}) {
				v := obj.(*v1beta1.ActiveMQArtemisAddress)
				v.ObjectMeta = v1.ObjectMeta{
					Namespace: key.Namespace,
					Name:      key.Name,
				}
				v.Spec = v1beta1.ActiveMQArtemisAddressSpec{
					RemoveFromBrokerOnDelete: removeFromBrokerOnDelete,
				}
				return nil
			}
			return apierrors.NewNotFound(schema.GroupResource{}, "")
		},
	}
	fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

	r := NewActiveMQArtemisAddressReconciler(fakeClient, nil, logr.New(log.NullLogSink{}))

	result, err := r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test-namespace", Name: "test-name"}})

	Expect(err).To(BeNil())
	Expect(result.Requeue).To(BeFalse())
	Expect(result.RequeueAfter).To(Equal(common.GetReconcileResyncPeriod()))

	addressExists = false

	result, err = r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test-namespace", Name: "test-name"}})

	Expect(err).To(BeNil())
	Expect(result.Requeue).To(BeFalse())
	Expect(result.RequeueAfter).To(Equal(time.Duration(0)))
}
