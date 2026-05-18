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
	"testing"
	"time"

	"github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta1"
	"github.com/arkmq-org/arkmq-org-broker-operator/v2/pkg/utils/common"
	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
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

func TestDeleteExistingAddress(t *testing.T) {
	testDeleteExistingAddress(t, false)
}

func TestDeleteExistingAddressWithRemoveFromBrokerOnDelete(t *testing.T) {
	testDeleteExistingAddress(t, true)
}

func testDeleteExistingAddress(t *testing.T, removeFromBrokerOnDelete bool) {

	var result ctrl.Result
	var err error

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
			} else {
				return apierrors.NewNotFound(schema.GroupResource{}, "")
			}
		},
	}
	fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

	r := NewActiveMQArtemisAddressReconciler(fakeClient, nil, logr.New(log.NullLogSink{}))

	result, err = r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test-namespace", Name: "test-name"}})

	assert.Nil(t, err)
	assert.False(t, result.Requeue)
	assert.Equal(t, common.GetReconcileResyncPeriod(), result.RequeueAfter)

	addressExists = false

	result, err = r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test-namespace", Name: "test-name"}})

	assert.Nil(t, err)
	assert.False(t, result.Requeue)
	assert.Equal(t, time.Duration(0), result.RequeueAfter)
}

func TestDeleteAddressWithNotFoundError(t *testing.T) {

	interceptorFuncs := interceptor.Funcs{
		Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			return apierrors.NewNotFound(schema.GroupResource{}, "")
		},
	}
	fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

	r := NewActiveMQArtemisAddressReconciler(fakeClient, nil, logr.New(log.NullLogSink{}))

	result, err := r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test-namespace", Name: "test-name"}})

	assert.Nil(t, err)
	assert.False(t, result.Requeue)
	assert.Equal(t, time.Duration(0), result.RequeueAfter)
}

func TestDeleteAddressWithInternalError(t *testing.T) {

	internalError := apierrors.NewInternalError(errors.New("internal-error"))

	interceptorFuncs := interceptor.Funcs{
		Get: func(ctx context.Context, client client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			return internalError
		},
	}
	fakeClient := fake.NewClientBuilder().WithInterceptorFuncs(interceptorFuncs).Build()

	r := NewActiveMQArtemisAddressReconciler(fakeClient, nil, logr.New(log.NullLogSink{}))

	result, err := r.Reconcile(context.TODO(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "test-namespace", Name: "test-name"}})

	assert.Equal(t, internalError, err)
	assert.False(t, result.Requeue)
	assert.Equal(t, time.Duration(0), result.RequeueAfter)
}
