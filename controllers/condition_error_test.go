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
	"testing"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/v2/api/v1beta2"
	"github.com/stretchr/testify/assert"
)

func TestNewValidationError(t *testing.T) {
	t.Run("creates error with reason and message", func(t *testing.T) {
		reason := broker.ValidConditionInvalidResourceName

		err := NewValidationError(reason, "invalid resource name")

		assert.NotNil(t, err)
		assert.Equal(t, reason, err.ConditionReason())
		assert.Equal(t, "invalid resource name", err.Message)
		assert.Equal(t, "invalid resource name", err.Error())
	})

	t.Run("supports formatted message", func(t *testing.T) {
		reason := broker.ValidConditionAddressTypeError

		err := NewValidationError(reason, "address '%s' has invalid type", "my-address")

		assert.NotNil(t, err)
		assert.Equal(t, reason, err.ConditionReason())
		assert.Equal(t, "address 'my-address' has invalid type", err.Message)
	})
}

func TestNewTransientError(t *testing.T) {
	t.Run("creates error with reason and message", func(t *testing.T) {
		reason := broker.DeployedConditionNoMatchingServiceReason

		err := NewTransientError(reason, "no matching services available")

		assert.NotNil(t, err)
		assert.Equal(t, reason, err.ConditionReason())
		assert.Equal(t, "no matching services available", err.Message)
		assert.Equal(t, "no matching services available", err.Error())
	})

	t.Run("supports formatted message", func(t *testing.T) {
		reason := broker.DeployedConditionNoServiceCapacityReason

		err := NewTransientError(reason, "no service with capacity: required 1Gi, available 512Mi")

		assert.NotNil(t, err)
		assert.Equal(t, reason, err.ConditionReason())
		assert.Equal(t, "no service with capacity: required 1Gi, available 512Mi", err.Message)
	})
}

func TestTransientErrorWithCause(t *testing.T) {
	t.Run("wraps underlying error", func(t *testing.T) {
		cause := errors.New("API server unavailable")
		reason := broker.DeployedConditionCrudKindErrorReason

		err := NewTransientErrorWithCause(reason, "failed to create resource", cause)

		assert.NotNil(t, err)
		assert.Equal(t, reason, err.ConditionReason())
		assert.Contains(t, err.Error(), "failed to create resource")
		assert.Contains(t, err.Error(), "API server unavailable")
		assert.Equal(t, cause, errors.Unwrap(err))
	})

	t.Run("works without cause", func(t *testing.T) {
		reason := broker.DeployedConditionNoMatchingServiceReason

		err := NewTransientError(reason, "no matching services")

		assert.NotNil(t, err)
		assert.Equal(t, "no matching services", err.Error())
		assert.Nil(t, errors.Unwrap(err))
	})
}

func TestValidateResourceName(t *testing.T) {
	t.Run("returns ValidationError for invalid resource name", func(t *testing.T) {
		invalidName := "invalid/name"

		err := ValidateResourceName(invalidName)

		assert.Error(t, err)
		validErr, ok := err.(*ValidationError)
		assert.True(t, ok, "expected ValidationError")
		assert.Equal(t, broker.ValidConditionInvalidResourceName, validErr.ConditionReason())
		assert.Contains(t, validErr.Message, "invalid")
	})

	t.Run("returns nil for valid resource name", func(t *testing.T) {
		validName := "valid-name-123"

		err := ValidateResourceName(validName)

		assert.NoError(t, err)
	})

	t.Run("validates common invalid patterns", func(t *testing.T) {
		invalidNames := []string{
			"name/with/slashes",
			"name/../with-parent-ref",
			".starts-with-dot",
		}

		for _, name := range invalidNames {
			t.Run(name, func(t *testing.T) {
				err := ValidateResourceName(name)
				assert.Error(t, err, "expected error for name: %s", name)

				validErr, ok := err.(*ValidationError)
				assert.True(t, ok, "expected ValidationError")
				assert.Equal(t, broker.ValidConditionInvalidResourceName, validErr.ConditionReason())
			})
		}
	})
}

func TestErrorTypeChecking(t *testing.T) {
	t.Run("can distinguish ValidationError", func(t *testing.T) {
		var err error = NewValidationError(broker.ValidConditionInvalidResourceName, "test")

		_, isValidation := err.(*ValidationError)
		_, isTransient := err.(*TransientError)

		assert.True(t, isValidation)
		assert.False(t, isTransient)
	})

	t.Run("can distinguish TransientError", func(t *testing.T) {
		var err error = NewTransientError(broker.DeployedConditionNoMatchingServiceReason, "test")

		_, isValidation := err.(*ValidationError)
		_, isTransient := err.(*TransientError)

		assert.False(t, isValidation)
		assert.True(t, isTransient)
	})

	t.Run("regular errors are neither", func(t *testing.T) {
		var err error = fmt.Errorf("regular error")

		_, isValidation := err.(*ValidationError)
		_, isTransient := err.(*TransientError)

		assert.False(t, isValidation)
		assert.False(t, isTransient)
	})
}
