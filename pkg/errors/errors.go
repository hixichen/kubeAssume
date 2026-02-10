// Package errors provides domain-specific error types for KubeAssume,
// following Kubernetes and CNCF ecosystem conventions.
package errors

import (
	"errors"
	"fmt"
)

// ErrorCode represents a machine-readable error code.
type ErrorCode string

const (
	// CodeConfig indicates a configuration error (not retryable).
	CodeConfig ErrorCode = "ConfigError"
	// CodeFetch indicates an OIDC metadata fetch error (retryable).
	CodeFetch ErrorCode = "FetchError"
	// CodePublish indicates a publish error (retryable).
	CodePublish ErrorCode = "PublishError"
	// CodeRotation indicates a rotation state error (retryable).
	CodeRotation ErrorCode = "RotationError"
	// CodeValidation indicates a validation error (not retryable).
	CodeValidation ErrorCode = "ValidationError"
	// CodeNotFound indicates a resource was not found (not retryable).
	CodeNotFound ErrorCode = "NotFoundError"
	// CodePermission indicates insufficient permissions (not retryable).
	CodePermission ErrorCode = "PermissionError"
	// CodeInternal indicates an unexpected internal error (retryable).
	CodeInternal ErrorCode = "InternalError"
)

// retryableCodes contains the set of error codes that indicate retryable errors.
var retryableCodes = map[ErrorCode]bool{
	CodeFetch:    true,
	CodePublish:  true,
	CodeRotation: true,
	CodeInternal: true,
}

// KubeAssumeError is the base error type for all KubeAssume errors.
type KubeAssumeError struct {
	Code      ErrorCode
	Component string
	Message   string
	Err       error
}

func (e *KubeAssumeError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %s: %v", e.Code, e.Component, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s: %s", e.Code, e.Component, e.Message)
}

func (e *KubeAssumeError) Unwrap() error {
	return e.Err
}

// New creates a new KubeAssumeError.
func New(code ErrorCode, component, message string, err error) *KubeAssumeError {
	return &KubeAssumeError{
		Code:      code,
		Component: component,
		Message:   message,
		Err:       err,
	}
}

// --- Constructors for each error category ---

// NewConfigError creates a configuration error.
func NewConfigError(component, message string, err error) *KubeAssumeError {
	return New(CodeConfig, component, message, err)
}

// NewFetchError creates a fetch error.
func NewFetchError(component, message string, err error) *KubeAssumeError {
	return New(CodeFetch, component, message, err)
}

// NewPublishError creates a publish error.
func NewPublishError(component, message string, err error) *KubeAssumeError {
	return New(CodePublish, component, message, err)
}

// NewRotationError creates a rotation state error.
func NewRotationError(component, message string, err error) *KubeAssumeError {
	return New(CodeRotation, component, message, err)
}

// NewValidationError creates a validation error.
func NewValidationError(component, message string, err error) *KubeAssumeError {
	return New(CodeValidation, component, message, err)
}

// NewNotFoundError creates a not-found error.
func NewNotFoundError(component, message string, err error) *KubeAssumeError {
	return New(CodeNotFound, component, message, err)
}

// NewPermissionError creates a permission error.
func NewPermissionError(component, message string, err error) *KubeAssumeError {
	return New(CodePermission, component, message, err)
}

// NewInternalError creates an internal error.
func NewInternalError(component, message string, err error) *KubeAssumeError {
	return New(CodeInternal, component, message, err)
}

// --- Type checking helpers ---

// AsKubeAssumeError extracts a KubeAssumeError from the error chain.
func AsKubeAssumeError(err error) (*KubeAssumeError, bool) {
	var kaErr *KubeAssumeError
	if errors.As(err, &kaErr) {
		return kaErr, true
	}
	return nil, false
}

// IsCode checks if an error in the chain has the given error code.
func IsCode(err error, code ErrorCode) bool {
	kaErr, ok := AsKubeAssumeError(err)
	if !ok {
		return false
	}
	return kaErr.Code == code
}

// IsConfigError checks if the error is a config error.
func IsConfigError(err error) bool { return IsCode(err, CodeConfig) }

// IsFetchError checks if the error is a fetch error.
func IsFetchError(err error) bool { return IsCode(err, CodeFetch) }

// IsPublishError checks if the error is a publish error.
func IsPublishError(err error) bool { return IsCode(err, CodePublish) }

// IsRotationError checks if the error is a rotation error.
func IsRotationError(err error) bool { return IsCode(err, CodeRotation) }

// IsValidationError checks if the error is a validation error.
func IsValidationError(err error) bool { return IsCode(err, CodeValidation) }

// IsNotFoundError checks if the error is a not-found error.
func IsNotFoundError(err error) bool { return IsCode(err, CodeNotFound) }

// IsPermissionError checks if the error is a permission error.
func IsPermissionError(err error) bool { return IsCode(err, CodePermission) }

// IsRetryable checks if the error is retryable based on its code.
func IsRetryable(err error) bool {
	kaErr, ok := AsKubeAssumeError(err)
	if !ok {
		return false
	}
	return retryableCodes[kaErr.Code]
}

// GetCode returns the error code, or empty string if not a KubeAssumeError.
func GetCode(err error) ErrorCode {
	kaErr, ok := AsKubeAssumeError(err)
	if !ok {
		return ""
	}
	return kaErr.Code
}

// GetComponent returns the component name, or empty string if not a KubeAssumeError.
func GetComponent(err error) string {
	kaErr, ok := AsKubeAssumeError(err)
	if !ok {
		return ""
	}
	return kaErr.Component
}
