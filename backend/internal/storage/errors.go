package storage

import "errors"

// ErrorCode mirrors the deploy error code set for protocol consistency.
type ErrorCode string

const (
	ErrorCodeBadRequest     ErrorCode = "bad_request"
	ErrorCodeNotFound       ErrorCode = "not_found"
	ErrorCodeUnauthorized   ErrorCode = "unauthorized"
	ErrorCodeForbidden      ErrorCode = "forbidden"
	ErrorCodeInternal       ErrorCode = "internal"
	ErrorCodeUploadExpired  ErrorCode = "upload_expired"
	ErrorCodeUploadConsumed ErrorCode = "upload_consumed"
	ErrorCodeUploadTooLarge ErrorCode = "upload_too_large"
	ErrorCodeInvalidContent ErrorCode = "invalid_content"
	ErrorCodeStorageFull    ErrorCode = "storage_full"
	ErrorCodeUploadPending  ErrorCode = "upload_pending"
)

// UploadError is a typed validation error for storage uploads.
type UploadError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *UploadError) Error() string { return e.Message }
func (e *UploadError) Unwrap() error { return e.Err }

var (
	ErrStorageNotFound       = errors.New("storage file not found")
	ErrStorageDeleted        = errors.New("storage file already deleted")
	ErrInvalidStorageID      = errors.New("invalid storage id")
	ErrTokenExpired          = errors.New("upload token expired")
	ErrTokenConsumed         = errors.New("upload token already consumed")
	ErrTokenNotFound         = errors.New("upload token not found")
	ErrTokenClaimFailed      = errors.New("upload token claim failed")
	ErrUploadTooLarge        = errors.New("upload exceeds maximum allowed size")
	ErrContentTypeNotAllowed = errors.New("content type not allowed")
	ErrMalformedContentType  = errors.New("malformed content type")
	ErrInvalidFilename       = errors.New("invalid filename")
	ErrURLTampered           = errors.New("signed url is invalid or tampered")
	ErrURLExpired            = errors.New("signed url expired")
	ErrURLForbidden          = errors.New("signed url does not match caller")
	ErrStorageDataLost       = errors.New("storage file data lost")
	ErrReservationLost       = errors.New("storage upload reservation lost")
)
