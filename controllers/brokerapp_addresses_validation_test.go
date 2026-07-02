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
	"github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BrokerApp address list validation", func() {
	Context("when validating address declarations", func() {
		It("should reject duplicate address in both Addresses and SharedAddresses", func() {
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

			Expect(err).To(HaveOccurred())

			// Check it's a ValidationError with correct reason
			validErr, ok := err.(*ValidationError)
			Expect(ok).To(BeTrue(), "expected ValidationError")
			Expect(validErr.ConditionReason()).To(Equal(v1beta2.ValidConditionAddressTypeError))
			Expect(validErr.Message).To(ContainSubstring("cannot be both private and public"))
			Expect(validErr.Message).To(ContainSubstring("queue1"))
		})

		It("allows disjoint addresses", func() {
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

			Expect(err).NotTo(HaveOccurred())
		})

		It("allows empty SharedAddresses", func() {
			app := &v1beta2.BrokerApp{
				Spec: v1beta2.BrokerAppSpec{
					Addresses: []v1beta2.AddressType{NewAddressType("private1").Build()},
				},
			}

			reconciler := &BrokerAppInstanceReconciler{
				instance: app,
			}

			err := reconciler.validateAddressesDisjoint()

			Expect(err).NotTo(HaveOccurred())
		})

		It("allows empty Addresses", func() {
			app := &v1beta2.BrokerApp{
				Spec: v1beta2.BrokerAppSpec{
					SharedAddresses: []v1beta2.AddressType{NewAddressType("public1").Build()},
				},
			}

			reconciler := &BrokerAppInstanceReconciler{
				instance: app,
			}

			err := reconciler.validateAddressesDisjoint()

			Expect(err).NotTo(HaveOccurred())
		})
	})
})
