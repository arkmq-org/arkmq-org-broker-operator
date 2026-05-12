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

package controllers

import (
	"testing"

	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/stretchr/testify/assert"

	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestValidateAddressesDisjoint(t *testing.T) {
	t.Run("rejects duplicate address in both Addresses and SharedAddresses", func(t *testing.T) {
		appWithDuplicate := &v1beta2.BrokerApp{
			Spec: v1beta2.BrokerAppSpec{
				Addresses:       []v1beta2.AddressType{{Address: "queue1"}},
				SharedAddresses: []v1beta2.AddressType{{Address: "queue1"}}, // Duplicate!
			},
		}
		status := &v1beta2.BrokerAppStatus{
			Conditions: []v1.Condition{},
		}

		reconciler := &BrokerAppInstanceReconciler{
			instance: appWithDuplicate,
			status:   status,
		}

		err := reconciler.validateAddressesDisjoint()

		assert.Error(t, err)
		assert.Len(t, status.Conditions, 1)

		validCond := meta.FindStatusCondition(status.Conditions, v1beta2.ValidConditionType)
		assert.NotNil(t, validCond)
		assert.Equal(t, v1.ConditionFalse, validCond.Status)
		assert.Equal(t, v1beta2.ValidConditionAddressTypeError, validCond.Reason)
		assert.Contains(t, validCond.Message, "cannot be both private and public")
		assert.Contains(t, validCond.Message, "queue1")
	})

	t.Run("allows disjoint addresses", func(t *testing.T) {
		appDisjoint := &v1beta2.BrokerApp{
			Spec: v1beta2.BrokerAppSpec{
				Addresses:       []v1beta2.AddressType{{Address: "private1"}},
				SharedAddresses: []v1beta2.AddressType{{Address: "public1"}},
			},
		}
		status := &v1beta2.BrokerAppStatus{
			Conditions: []v1.Condition{},
		}

		reconciler := &BrokerAppInstanceReconciler{
			instance: appDisjoint,
			status:   status,
		}

		err := reconciler.validateAddressesDisjoint()

		assert.NoError(t, err)
		assert.Len(t, status.Conditions, 0)
	})

	t.Run("allows empty SharedAddresses", func(t *testing.T) {
		app := &v1beta2.BrokerApp{
			Spec: v1beta2.BrokerAppSpec{
				Addresses: []v1beta2.AddressType{{Address: "private1"}},
			},
		}
		status := &v1beta2.BrokerAppStatus{
			Conditions: []v1.Condition{},
		}

		reconciler := &BrokerAppInstanceReconciler{
			instance: app,
			status:   status,
		}

		err := reconciler.validateAddressesDisjoint()

		assert.NoError(t, err)
		assert.Len(t, status.Conditions, 0)
	})

	t.Run("allows empty Addresses", func(t *testing.T) {
		app := &v1beta2.BrokerApp{
			Spec: v1beta2.BrokerAppSpec{
				SharedAddresses: []v1beta2.AddressType{{Address: "public1"}},
			},
		}
		status := &v1beta2.BrokerAppStatus{
			Conditions: []v1.Condition{},
		}

		reconciler := &BrokerAppInstanceReconciler{
			instance: app,
			status:   status,
		}

		err := reconciler.validateAddressesDisjoint()

		assert.NoError(t, err)
		assert.Len(t, status.Conditions, 0)
	})
}
