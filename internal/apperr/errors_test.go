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

// REQ-001: new indexing-failure error codes.
func TestNewIndexingErrorCodes_AllDefined(t *testing.T) {
	cases := []struct {
		err  *Error
		code string
	}{
		{ErrUnsupportedFormat, "ERR_UNSUPPORTED_FORMAT"},
		{ErrExtractionFailed, "ERR_EXTRACTION_FAILED"},
		{ErrFileTooLarge, "ERR_FILE_TOO_LARGE"},
		{ErrFileUnreadable, "ERR_FILE_UNREADABLE"},
		{ErrEmbedCountMismatch, "ERR_EMBED_COUNT_MISMATCH"},
		{ErrHnswAdd, "ERR_HNSW_ADD"},
		{ErrStoreWrite, "ERR_STORE_WRITE"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.code, func(t *testing.T) {
			if tc.err == nil {
				t.Fatalf("%s is nil", tc.code)
			}
			if tc.err.Code != tc.code {
				t.Errorf("got code %q, want %q", tc.err.Code, tc.code)
			}
			if tc.err.Message == "" {
				t.Errorf("%s has empty Message", tc.code)
			}
		})
	}
}

// REQ-002: Classification values for every indexing-related code.
func TestClassify_KnownCodes(t *testing.T) {
	cases := []struct {
		err  error
		want Classification
	}{
		// Permanent
		{ErrUnsupportedFormat, ClassPermanent},
		{ErrExtractionFailed, ClassPermanent},
		{ErrFileTooLarge, ClassPermanent},
		{ErrFileUnreadable, ClassPermanent},
		{ErrEmbedCountMismatch, ClassPermanent},
		// TransientRetry
		{ErrEmbedFailed, ClassTransientRetry},
		{ErrHnswAdd, ClassTransientRetry},
		{ErrStoreWrite, ClassTransientRetry},
		// TransientWait
		{ErrRateLimited, ClassTransientWait},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.err.(*Error).Code, func(t *testing.T) {
			got := Classify(tc.err)
			if got != tc.want {
				t.Errorf("Classify(%s) = %q, want %q", tc.err.(*Error).Code, got, tc.want)
			}
		})
	}
}

// REQ-002: Classify works when err is wrapped via Wrap (errors.As traversal).
func TestClassify_WrappedErrors(t *testing.T) {
	inner := errors.New("disk full")
	wrapped := Wrap("ERR_STORE_WRITE", "store write failed", inner)
	got := Classify(wrapped)
	if got != ClassTransientRetry {
		t.Errorf("Classify(wrapped ERR_STORE_WRITE) = %q, want ClassTransientRetry", got)
	}
}

// REQ-004 / EDGE-013: raw (non-apperr) error treated as Permanent.
func TestClassify_RawErrorIsPermanent(t *testing.T) {
	raw := errors.New("some unexpected error")
	got := Classify(raw)
	if got != ClassPermanent {
		t.Errorf("Classify(raw error) = %q, want ClassPermanent", got)
	}
}

// REQ-002: nil is Permanent.
func TestClassify_NilIsPermanent(t *testing.T) {
	got := Classify(nil)
	if got != ClassPermanent {
		t.Errorf("Classify(nil) = %q, want ClassPermanent", got)
	}
}

// REQ-014: query-level error codes for fail-fast behaviour.
func TestQueryErrorCodes_AllDefined(t *testing.T) {
	cases := []struct {
		err  *Error
		code string
	}{
		{ErrQueryParseFailed, "ERR_QUERY_PARSE_FAILED"},
		{ErrQueryRateLimited, "ERR_QUERY_RATE_LIMITED"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.code, func(t *testing.T) {
			if tc.err == nil {
				t.Fatalf("%s is nil", tc.code)
			}
			if tc.err.Code != tc.code {
				t.Errorf("got code %q, want %q", tc.err.Code, tc.code)
			}
			if tc.err.Message == "" {
				t.Errorf("%s has empty Message", tc.code)
			}
		})
	}
}

// REQ-014: errors.Is works for query error codes when wrapped via Wrap.
func TestQueryErrorCodes_ErrorsIsWithWrap(t *testing.T) {
	wrappedParse := Wrap(ErrQueryParseFailed.Code, "llm call failed", nil)
	if !errors.Is(wrappedParse, ErrQueryParseFailed) {
		t.Error("errors.Is(Wrap(ErrQueryParseFailed.Code,...), ErrQueryParseFailed) returned false")
	}

	wrappedRate := Wrap(ErrQueryRateLimited.Code, "429 from gemini", nil)
	if !errors.Is(wrappedRate, ErrQueryRateLimited) {
		t.Error("errors.Is(Wrap(ErrQueryRateLimited.Code,...), ErrQueryRateLimited) returned false")
	}
}

// EDGE-013: unknown apperr code defaults to Permanent.
func TestClassify_UnknownCodeIsPermanent(t *testing.T) {
	unknown := Wrap("ERR_NOT_REGISTERED", "some future error", nil)
	got := Classify(unknown)
	if got != ClassPermanent {
		t.Errorf("Classify(unknown code) = %q, want ClassPermanent", got)
	}
}

// REQ-11 / REQ-12: filename-search error codes are registered with expected fields.
func TestFilenameSearchErrorCodes_AllDefined(t *testing.T) {
	cases := []struct {
		err  *Error
		code string
		msg  string
	}{
		{ErrFilenameSearchFailed, "ERR_FILENAME_SEARCH_FAILED", "filename search failed"},
		{ErrClassifierFailed, "ERR_CLASSIFIER_FAILED", "query classification failed"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.code, func(t *testing.T) {
			if tc.err == nil {
				t.Fatalf("%s is nil", tc.code)
			}
			if tc.err.Code != tc.code {
				t.Errorf("Code = %q, want %q", tc.err.Code, tc.code)
			}
			if tc.err.Message != tc.msg {
				t.Errorf("Message = %q, want %q", tc.err.Message, tc.msg)
			}
		})
	}
}

// REQ-11 / REQ-12: errors.Is works for filename-search codes when wrapped via Wrap.
func TestFilenameSearchErrorCodes_ErrorsIsWithWrap(t *testing.T) {
	wrappedFilename := Wrap(ErrFilenameSearchFailed.Code, "fuzzy search error", nil)
	if !errors.Is(wrappedFilename, ErrFilenameSearchFailed) {
		t.Error("errors.Is(Wrap(ErrFilenameSearchFailed.Code,...), ErrFilenameSearchFailed) returned false")
	}

	wrappedClassifier := Wrap(ErrClassifierFailed.Code, "llm classifier error", nil)
	if !errors.Is(wrappedClassifier, ErrClassifierFailed) {
		t.Error("errors.Is(Wrap(ErrClassifierFailed.Code,...), ErrClassifierFailed) returned false")
	}
}

// Regression: errors.Is / errors.As still work for new error vars.
func TestErrorsIs_As_Preserved(t *testing.T) {
	cause := errors.New("root cause")
	wrapped := Wrap("ERR_HNSW_ADD", "hnsw add failed", cause)

	// errors.Is should find the cause.
	if !errors.Is(wrapped, cause) {
		t.Error("errors.Is did not find wrapped cause through apperr.Error")
	}

	// errors.As should extract *Error.
	var appErr *Error
	if !errors.As(wrapped, &appErr) {
		t.Fatal("errors.As did not extract *apperr.Error")
	}
	if appErr.Code != "ERR_HNSW_ADD" {
		t.Errorf("extracted code = %q, want ERR_HNSW_ADD", appErr.Code)
	}
}
