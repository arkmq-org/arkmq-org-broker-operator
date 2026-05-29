/*
Copyright 2026.

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
	"fmt"

	broker "github.com/arkmq-org/arkmq-org-broker-operator/api/v1beta2"
	"github.com/arkmq-org/arkmq-org-broker-operator/pkg/utils/common"
)

// ValidationError represents a spec validation error.
// User must fix the spec - no auto-retry until spec changes (new generation).
type ValidationError struct {
	Reason  string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func (e *ValidationError) ConditionReason() string {
	return e.Reason
}

// NewValidationError creates a new ValidationError with the given reason and message
func NewValidationError(reason string, format string, args ...interface{}) *ValidationError {
	return &ValidationError{
		Reason:  reason,
		Message: fmt.Sprintf(format, args...),
	}
}

// TransientError represents a runtime issue that may resolve on retry.
// Used for: capacity issues, routing issues, API errors, network failures.
// Controller-runtime will retry these with exponential backoff.
type TransientError struct {
	Reason  string
	Message string
	Cause   error // Optional: wrapped underlying error
}

func (e *TransientError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *TransientError) ConditionReason() string {
	return e.Reason
}

func (e *TransientError) Unwrap() error {
	return e.Cause
}

// NewTransientError creates a new TransientError with the given reason and message
func NewTransientError(reason, message string) *TransientError {
	return &TransientError{
		Reason:  reason,
		Message: message,
	}
}

// NewTransientErrorWithCause creates a new TransientError with a cause
func NewTransientErrorWithCause(reason, message string, cause error) *TransientError {
	return &TransientError{
		Reason:  reason,
		Message: message,
		Cause:   cause,
	}
}

// ValidateResourceName validates a resource name and returns a ValidationError if invalid
func ValidateResourceName(resourceName string) error {
	err := common.ValidateResourceName(resourceName)
	if err != nil {
		return NewValidationError(
			broker.ValidConditionInvalidResourceName,
			"invalid resource name: %v", err)
	}
	return nil
}
