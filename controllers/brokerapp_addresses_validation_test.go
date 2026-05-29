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
)

func TestValidateAddressesDisjoint(t *testing.T) {
	t.Run("rejects duplicate address in both Addresses and SharedAddresses", func(t *testing.T) {
		appWithDuplicate := &v1beta2.BrokerApp{
			Spec: v1beta2.BrokerAppSpec{
				Addresses:       []v1beta2.AddressType{NewAddressType("queue1").Build()},
				SharedAddresses: []v1beta2.AddressType{NewAddressType("queue1").Build()}, // Duplicate!
			},
		}

		reconciler := &BrokerAppInstanceReconciler{
			instance: appWithDuplicate,
		}

		err := reconciler.validateAddressesDisjoint()

		assert.Error(t, err)

		// Check it's a ValidationError with correct reason
		validErr, ok := err.(*ValidationError)
		assert.True(t, ok, "expected ValidationError")
		assert.Equal(t, v1beta2.ValidConditionAddressTypeError, validErr.ConditionReason())
		assert.Contains(t, validErr.Message, "cannot be both private and public")
		assert.Contains(t, validErr.Message, "queue1")
	})

	t.Run("allows disjoint addresses", func(t *testing.T) {
		appDisjoint := &v1beta2.BrokerApp{
			Spec: v1beta2.BrokerAppSpec{
				Addresses:       []v1beta2.AddressType{NewAddressType("private1").Build()},
				SharedAddresses: []v1beta2.AddressType{NewAddressType("public1").Build()},
			},
		}

		reconciler := &BrokerAppInstanceReconciler{
			instance: appDisjoint,
		}

		err := reconciler.validateAddressesDisjoint()

		assert.NoError(t, err)
	})

	t.Run("allows empty SharedAddresses", func(t *testing.T) {
		app := &v1beta2.BrokerApp{
			Spec: v1beta2.BrokerAppSpec{
				Addresses: []v1beta2.AddressType{NewAddressType("private1").Build()},
			},
		}

		reconciler := &BrokerAppInstanceReconciler{
			instance: app,
		}

		err := reconciler.validateAddressesDisjoint()

		assert.NoError(t, err)
	})

	t.Run("allows empty Addresses", func(t *testing.T) {
		app := &v1beta2.BrokerApp{
			Spec: v1beta2.BrokerAppSpec{
				SharedAddresses: []v1beta2.AddressType{NewAddressType("public1").Build()},
			},
		}

		reconciler := &BrokerAppInstanceReconciler{
			instance: app,
		}

		err := reconciler.validateAddressesDisjoint()

		assert.NoError(t, err)
	})
}
