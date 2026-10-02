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
	"errors"
	"fmt"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("condition errors", func() {

	Context("NewValidationError", func() {
		It("creates error with reason and message", Label(unitLabel), func() {
			reason := broker.ValidConditionInvalidResourceName

			err := NewValidationError(reason, "invalid resource name")

			Expect(err).NotTo(BeNil())
			Expect(err.ConditionReason()).To(Equal(reason))
			Expect(err.Message).To(Equal("invalid resource name"))
			Expect(err.Error()).To(Equal("invalid resource name"))
		})

		It("supports formatted message", Label(unitLabel), func() {
			reason := broker.ValidConditionAddressTypeError

			err := NewValidationError(reason, "address '%s' has invalid type", "my-address")

			Expect(err).NotTo(BeNil())
			Expect(err.ConditionReason()).To(Equal(reason))
			Expect(err.Message).To(Equal("address 'my-address' has invalid type"))
		})
	})

	Context("NewTransientError", func() {
		It("creates error with reason and message", Label(unitLabel), func() {
			reason := broker.DeployedConditionNoMatchingServiceReason

			err := NewTransientError(reason, "no matching services available")

			Expect(err).NotTo(BeNil())
			Expect(err.ConditionReason()).To(Equal(reason))
			Expect(err.Message).To(Equal("no matching services available"))
			Expect(err.Error()).To(Equal("no matching services available"))
		})

		It("supports formatted message", Label(unitLabel), func() {
			reason := broker.DeployedConditionNoServiceCapacityReason

			err := NewTransientError(reason, "no service with capacity: required 1Gi, available 512Mi")

			Expect(err).NotTo(BeNil())
			Expect(err.ConditionReason()).To(Equal(reason))
			Expect(err.Message).To(Equal("no service with capacity: required 1Gi, available 512Mi"))
		})
	})

	Context("TransientErrorWithCause", func() {
		It("wraps underlying error", Label(unitLabel), func() {
			cause := errors.New("API server unavailable")
			reason := broker.DeployedConditionCrudKindErrorReason

			err := NewTransientErrorWithCause(reason, "failed to create resource", cause)

			Expect(err).NotTo(BeNil())
			Expect(err.ConditionReason()).To(Equal(reason))
			Expect(err.Error()).To(ContainSubstring("failed to create resource"))
			Expect(err.Error()).To(ContainSubstring("API server unavailable"))
			Expect(errors.Unwrap(err)).To(Equal(cause))
		})

		It("works without cause", Label(unitLabel), func() {
			reason := broker.DeployedConditionNoMatchingServiceReason

			err := NewTransientError(reason, "no matching services")

			Expect(err).NotTo(BeNil())
			Expect(err.Error()).To(Equal("no matching services"))
			Expect(errors.Unwrap(err)).To(BeNil())
		})
	})

	Context("ValidateResourceName", func() {
		It("returns ValidationError for invalid resource name", Label(unitLabel), func() {
			invalidName := "invalid/name"

			err := ValidateResourceName(invalidName)

			Expect(err).To(HaveOccurred())
			validErr, ok := err.(*ValidationError)
			Expect(ok).To(BeTrue(), "expected ValidationError")
			Expect(validErr.ConditionReason()).To(Equal(broker.ValidConditionInvalidResourceName))
			Expect(validErr.Message).To(ContainSubstring("invalid"))
		})

		It("returns nil for valid resource name", Label(unitLabel), func() {
			validName := "valid-name-123"

			err := ValidateResourceName(validName)

			Expect(err).NotTo(HaveOccurred())
		})

		Context("validates common invalid patterns", func() {
			for _, name := range []string{
				"name/with/slashes",
				"name/../with-parent-ref",
				".starts-with-dot",
			} {
				It(name, Label(unitLabel), func() {
					err := ValidateResourceName(name)
					Expect(err).To(HaveOccurred(), "expected error for name: %s", name)

					validErr, ok := err.(*ValidationError)
					Expect(ok).To(BeTrue(), "expected ValidationError")
					Expect(validErr.ConditionReason()).To(Equal(broker.ValidConditionInvalidResourceName))
				})
			}
		})
	})

	Context("error type checking", func() {
		It("can distinguish ValidationError", Label(unitLabel), func() {
			var err error = NewValidationError(broker.ValidConditionInvalidResourceName, "test")

			_, isValidation := err.(*ValidationError)
			_, isTransient := err.(*TransientError)

			Expect(isValidation).To(BeTrue())
			Expect(isTransient).To(BeFalse())
		})

		It("can distinguish TransientError", Label(unitLabel), func() {
			var err error = NewTransientError(broker.DeployedConditionNoMatchingServiceReason, "test")

			_, isValidation := err.(*ValidationError)
			_, isTransient := err.(*TransientError)

			Expect(isValidation).To(BeFalse())
			Expect(isTransient).To(BeTrue())
		})

		It("regular errors are neither", Label(unitLabel), func() {
			err := fmt.Errorf("regular error")

			_, isValidation := err.(*ValidationError)
			_, isTransient := err.(*TransientError)

			Expect(isValidation).To(BeFalse())
			Expect(isTransient).To(BeFalse())
		})
	})
})
