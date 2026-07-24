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

var _ = Describe("Condition Error", func() {
	Context("when creating a new error", func() {
		It("should create a new error with the given reason and message", func() {
			reason := broker.ValidConditionInvalidResourceName
			message := "invalid resource name"
			err := NewValidationError(reason, "%s", message)
			Expect(err).To(HaveOccurred())
			Expect(err.ConditionReason()).To(Equal(reason))
			Expect(err.Error()).To(Equal(message))
		})

		It("should support formatted message", func() {
			reason := broker.ValidConditionAddressTypeError
			err := NewValidationError(reason, "address '%s' has invalid type", "my-address")
			Expect(err).To(HaveOccurred())
			Expect(err.ConditionReason()).To(Equal(reason))
			Expect(err.Error()).To(Equal("address 'my-address' has invalid type"))
		})
	})
})

var _ = Describe("TransientError", func() {
	Context("when creating a new TransientError", func() {
		It("should create a new error with the given reason and message", func() {
			reason := broker.ValidConditionInvalidResourceName
			message := "invalid resource name"
			err := NewTransientError(reason, message)
			Expect(err).To(HaveOccurred())
			Expect(err.ConditionReason()).To(Equal(reason))
			Expect(err.Error()).To(Equal(message))
		})

		It("should support formatted message", func() {
			reason := broker.ValidConditionAddressTypeError
			err := NewTransientError(reason, fmt.Sprintf("address 'my-address' has invalid type"))
			Expect(err).To(HaveOccurred())
			Expect(err.ConditionReason()).To(Equal(reason))
			Expect(err.Error()).To(Equal("address 'my-address' has invalid type"))
		})
	})
})

var _ = Describe("TransientError with cause", func() {
	Context("when creating a TransientError that wraps another error", func() {
		It("should wrap underlying error", func() {
			cause := errors.New("API server unavailable")
			reason := broker.DeployedConditionCrudKindErrorReason

			err := NewTransientErrorWithCause(reason, "failed to create resource", cause)

			Expect(err).To(HaveOccurred())
			Expect(err.ConditionReason()).To(Equal(reason))
			Expect(err.Error()).To(ContainSubstring("failed to create resource"))
			Expect(err.Error()).To(ContainSubstring("API server unavailable"))
			Expect(errors.Unwrap(err)).To(Equal(cause))
		})
		It("should work without cause", func() {
			reason := broker.DeployedConditionNoMatchingServiceReason

			err := NewTransientError(reason, "no matching services")

			Expect(err).To(HaveOccurred())
			Expect(err.ConditionReason()).To(Equal(reason))
			Expect(err.Error()).To(Equal("no matching services"))
			Expect(errors.Unwrap(err)).To(BeNil())
		})
	})
})

var _ = Describe("ValidateResourceName", func() {
	Context("when validating a resource name", func() {
		It("should return a ValidationError for an invalid resource name", func() {
			invalidName := "invalid/name"

			err := ValidateResourceName(invalidName)

			Expect(err).To(HaveOccurred())

		})
	})
	It("should return nil for a valid resource name", func() {
		validName := "valid-name"
		err := ValidateResourceName(validName)
		Expect(err).To(BeNil())
	})
})

var _ = Describe("error type checking", func() {
	Context("when checking error types", func() {
		It("should correctly identify ValidationError", func() {
			var err error = NewValidationError(broker.ValidConditionInvalidResourceName, "invalid resource name")
			_, ok := err.(*ValidationError)
			Expect(ok).To(BeTrue())
		})
		It("should correctly identify TransientError", func() {
			var err error = NewTransientError(broker.DeployedConditionNoMatchingServiceReason, "no matching services")
			_, ok := err.(*TransientError)
			Expect(ok).To(BeTrue())
		})
		It("should implement error interface", func() {
			err := NewValidationError(broker.ValidConditionInvalidResourceName, "invalid resource name")
			var _ error = err
			Expect(err.Error()).To(Equal("invalid resource name"))
		})
	})
})
