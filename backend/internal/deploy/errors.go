package deploy

import (
	"errors"
	"fmt"
)

type UploadValidationError struct {
	Code ErrorCode
	Err  error
}

func (e *UploadValidationError) Error() string { return e.Err.Error() }
func (e *UploadValidationError) Unwrap() error { return e.Err }

// ValueSizeError is returned when a wire value exceeds a configured size limit.
type ValueSizeError struct {
	Label string
	Limit int64
}

func (e *ValueSizeError) Error() string {
	return fmt.Sprintf("%s exceeds configured limit", e.Label)
}

var (
	ErrDeploymentNotFound = errors.New("deployment not found")
	ErrFunctionNotFound   = errors.New("function not found")
	ErrActiveNotFound     = errors.New("no active deployment")
	ErrAlreadyActive      = errors.New("deployment is already active")
	ErrInvalidBundle      = errors.New("bundle failed validation")
	ErrInvalidManifest    = errors.New("manifest failed validation")
	ErrActivationFailed   = errors.New("failed to activate deployment")
	ErrForbidden          = errors.New("forbidden")
	ErrPinUnderflow       = errors.New("pin count underflow")
)
