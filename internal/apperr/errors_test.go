package apperr

import (
	"errors"
	"testing"
)

func TestError_ImplementsErrorInterface(t *testing.T) {
	var _ error = (*Error)(nil)
}

func TestError_MessageIncludesCause(t *testing.T) {
	cause := errors.New("underlying failure")
	e := Wrap("ERR_TEST", "wrapping message", cause)
	want := "wrapping message: underlying failure"
	if e.Error() != want {
		t.Errorf("Error() = %q, want %q", e.Error(), want)
	}
}

func TestError_MessageWithoutCause(t *testing.T) {
	e := New("ERR_TEST", "bare message")
	if e.Error() != "bare message" {
		t.Errorf("Error() = %q, want %q", e.Error(), "bare message")
	}
}

func TestError_UnwrapReturnsCause(t *testing.T) {
	cause := errors.New("root cause")
	e := Wrap("ERR_TEST", "msg", cause)
	if errors.Unwrap(e) != cause {
		t.Errorf("Unwrap did not return cause")
	}
}

func TestError_ErrorsIsFindsCause(t *testing.T) {
	cause := errors.New("sentinel")
	e := Wrap("ERR_X", "msg", cause)
	if !errors.Is(e, cause) {
		t.Errorf("errors.Is did not find wrapped cause")
	}
}

// REF-063: stable error codes vocabulary.
func TestStableErrorCodes_AllDefined(t *testing.T) {
	cases := []struct {
		err  *Error
		code string
	}{
		{ErrModelMismatch, "ERR_MODEL_MISMATCH"},
		{ErrEmbedFailed, "ERR_EMBED_FAILED"},
		{ErrFolderDenied, "ERR_FOLDER_DENIED"},
		{ErrStoreLocked, "ERR_STORE_LOCKED"},
		{ErrConfigInvalid, "ERR_CONFIG_INVALID"},
		{ErrMigrationFailed, "ERR_MIGRATION_FAILED"},
		{ErrRateLimited, "ERR_RATE_LIMITED"},
		{ErrInternal, "ERR_INTERNAL"},
	}
	for _, tc := range cases {
		if tc.err == nil {
			t.Errorf("%s is nil", tc.code)
			continue
		}
		if tc.err.Code != tc.code {
			t.Errorf("got code %q, want %q", tc.err.Code, tc.code)
		}
		if tc.err.Message == "" {
			t.Errorf("%s has empty Message", tc.code)
		}
	}
}
