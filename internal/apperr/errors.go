// Package apperr defines a typed application error with stable codes
// that the frontend can translate into user-facing messages.
package apperr

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
)
