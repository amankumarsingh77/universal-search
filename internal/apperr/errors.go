// Package apperr defines a typed application error with stable codes
// that the frontend can translate into user-facing messages.
package apperr

import "errors"

// Error is a typed application error carrying a stable code and optional cause.
type Error struct {
	Code    string
	Message string
	Cause   error
}

// Error formats the error as "message: cause" when a cause is present.
func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// Unwrap returns the underlying cause, enabling errors.Is / errors.As traversal.
func (e *Error) Unwrap() error { return e.Cause }

// Is reports whether e matches the target error by code when both are *Error.
// This allows errors.Is(wrappedErr, apperr.ErrRateLimited) to return true even
// when wrappedErr was created with Wrap (different pointer, same code).
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Code == t.Code
}

// Wrap constructs an Error with the given code, human-readable message, and underlying cause.
func Wrap(code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

// New constructs an Error with no underlying cause.
func New(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Stable error codes surfaced to the frontend.
var (
	ErrModelMismatch   = &Error{Code: "ERR_MODEL_MISMATCH", Message: "index was built with a different embedding model — reindex required"}
	ErrEmbedFailed     = &Error{Code: "ERR_EMBED_FAILED", Message: "embedding request failed"}
	ErrFolderDenied    = &Error{Code: "ERR_FOLDER_DENIED", Message: "folder cannot be accessed"}
	ErrStoreLocked     = &Error{Code: "ERR_STORE_LOCKED", Message: "database is locked"}
	ErrConfigInvalid   = &Error{Code: "ERR_CONFIG_INVALID", Message: "configuration is invalid"}
	ErrMigrationFailed = &Error{Code: "ERR_MIGRATION_FAILED", Message: "database migration failed"}
	ErrRateLimited     = &Error{Code: "ERR_RATE_LIMITED", Message: "rate limited by embedding provider"}
	ErrInternal        = &Error{Code: "ERR_INTERNAL", Message: "internal error"}

	// Query-understanding error codes (REQ-014).
	ErrQueryParseFailed = &Error{Code: "ERR_QUERY_PARSE_FAILED", Message: "query understanding failed"}
	ErrQueryRateLimited = &Error{Code: "ERR_QUERY_RATE_LIMITED", Message: "rate limited while parsing query"}
)

// Indexing-failure error codes (REQ-001).
var (
	ErrUnsupportedFormat  = &Error{Code: "ERR_UNSUPPORTED_FORMAT", Message: "file format is not supported for indexing"}
	ErrExtractionFailed   = &Error{Code: "ERR_EXTRACTION_FAILED", Message: "text extraction from file failed"}
	ErrFileTooLarge       = &Error{Code: "ERR_FILE_TOO_LARGE", Message: "file exceeds the maximum size for indexing"}
	ErrFileUnreadable     = &Error{Code: "ERR_FILE_UNREADABLE", Message: "file could not be read"}
	ErrEmbedCountMismatch = &Error{Code: "ERR_EMBED_COUNT_MISMATCH", Message: "embedding response count does not match chunk count"}
	ErrHnswAdd            = &Error{Code: "ERR_HNSW_ADD", Message: "failed to add vector to HNSW index"}
	ErrStoreWrite         = &Error{Code: "ERR_STORE_WRITE", Message: "failed to write to metadata store"}
)

// Classification describes how the retry coordinator should handle a failure.
type Classification string

const (
	// ClassPermanent means the failure will not resolve with retries (e.g. unsupported format).
	ClassPermanent Classification = "Permanent"
	// ClassTransientRetry means the failure may resolve after a short backoff retry.
	ClassTransientRetry Classification = "TransientRetry"
	// ClassTransientWait means the failure requires waiting on a rate-limit unpause before retrying.
	ClassTransientWait Classification = "TransientWait"
)

// classifications maps error codes to their retry classification (REQ-002).
var classifications = map[string]Classification{
	"ERR_UNSUPPORTED_FORMAT":   ClassPermanent,
	"ERR_EXTRACTION_FAILED":    ClassPermanent,
	"ERR_FILE_TOO_LARGE":       ClassPermanent,
	"ERR_FILE_UNREADABLE":      ClassPermanent,
	"ERR_EMBED_COUNT_MISMATCH": ClassPermanent,
	"ERR_EMBED_FAILED":         ClassTransientRetry,
	"ERR_HNSW_ADD":             ClassTransientRetry,
	"ERR_STORE_WRITE":          ClassTransientRetry,
	"ERR_RATE_LIMITED":         ClassTransientWait,
}

// Classify returns the Classification for err.
//
// If err is nil, not an *Error, or carries an unregistered code, ClassPermanent
// is returned — raw unwrapped errors from indexFile are treated as permanent
// failures per REQ-004 / EDGE-013.
func Classify(err error) Classification {
	if err == nil {
		return ClassPermanent
	}
	var appErr *Error
	if !errors.As(err, &appErr) {
		return ClassPermanent
	}
	if c, ok := classifications[appErr.Code]; ok {
		return c
	}
	return ClassPermanent
}
