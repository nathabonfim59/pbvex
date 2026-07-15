package scheduler

import "errors"

var (
	ErrJobNotFound                = errors.New("job not found")
	ErrJobNotCancelable           = errors.New("job cannot be canceled")
	ErrJobNotRetryable            = errors.New("job cannot be retried")
	ErrJobInvalidStatus           = errors.New("invalid job status filter")
	ErrDeploymentSnapshotNotFound = errors.New("deployment snapshot not found")
)

var errMaxAttemptsExceeded = errors.New("max attempts exceeded")
