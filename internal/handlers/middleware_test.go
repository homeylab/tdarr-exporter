package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// TestRequestLogger_ForwardsResponseUnchanged verifies RequestLogger is
// transparent: it must not alter the status or body the wrapped handler
// produces, for both a default 200 and an explicit non-200 status.
func TestRequestLogger_ForwardsResponseUnchanged(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantStatus int
		wantBody   string
	}{
		{
			name: "implicit 200",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("ok-body"))
			},
			wantStatus: http.StatusOK,
			wantBody:   "ok-body",
		},
		{
			name: "explicit 404",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte("not-found-body"))
			},
			wantStatus: http.StatusNotFound,
			wantBody:   "not-found-body",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := RequestLogger(tc.handler)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/probe", nil)
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if rec.Body.String() != tc.wantBody {
				t.Fatalf("body = %q, want %q", rec.Body.String(), tc.wantBody)
			}
		})
	}
}

// TestRequestLogger_PanicStillLogsAccessLine verifies the access log fires even
// when the wrapped handler panics, by driving the real production composition
// Recovery(RequestLogger(h)) so the panic actually unwinds through both
// middlewares exactly as it would in production.
//
// This mutates the package-global log.Logger to capture output (matching the
// only precedent for capturing zerolog output in this repo, see
// internal/client/client_test.go and internal/collector/tdarr_test.go, both of
// which inject a logger rather than touch the global — RequestLogger has no
// injectable logger seam, so the global is the only capture point here).
// Deliberately NOT t.Parallel(): go test runs all non-parallel top-level tests
// to completion before any parallel-batch tests in this package resume
// concurrently, so this global mutation cannot race with the t.Parallel()
// tests elsewhere in this file as long as it stays non-parallel itself.
func TestRequestLogger_PanicStillLogsAccessLine(t *testing.T) {
	orig := log.Logger
	defer func() { log.Logger = orig }()

	var buf bytes.Buffer
	log.Logger = zerolog.New(&buf).Level(zerolog.DebugLevel)

	h := Recovery(RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom-access-log")
	})))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panics", nil)
	h.ServeHTTP(rec, req)

	logged := buf.String()
	if !strings.Contains(logged, "Incoming request") {
		t.Fatalf("access log missing after panic; buffer = %q", logged)
	}
	if !strings.Contains(logged, "/panics") {
		t.Fatalf("access log missing request_uri; buffer = %q", logged)
	}
}

// TestResponseRecorder_ImplicitStatusOnWrite verifies that calling Write
// without a prior WriteHeader records an implicit 200 and flips wrote to true.
func TestResponseRecorder_ImplicitStatusOnWrite(t *testing.T) {
	t.Parallel()

	underlying := httptest.NewRecorder()
	rec := &responseRecorder{ResponseWriter: underlying}

	if rec.wrote {
		t.Fatalf("wrote = true before any write, want false")
	}

	const body = "hello"
	n, err := rec.Write([]byte(body))
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len(body) {
		t.Fatalf("Write returned n = %d, want %d", n, len(body))
	}
	if !rec.wrote {
		t.Fatalf("wrote = false after Write, want true")
	}
	if rec.status != http.StatusOK {
		t.Fatalf("status = %d, want %d (implicit 200)", rec.status, http.StatusOK)
	}
	if underlying.Body.String() != body {
		t.Fatalf("underlying body = %q, want %q (Write not forwarded)", underlying.Body.String(), body)
	}
}

// TestResponseRecorder_DefaultStatusIsOK verifies a recorder from
// newResponseRecorder reports 200 before any write, matching net/http's implicit
// 200 for a handler that returns without calling WriteHeader.
func TestResponseRecorder_DefaultStatusIsOK(t *testing.T) {
	t.Parallel()

	rec := newResponseRecorder(httptest.NewRecorder())
	if rec.status != http.StatusOK {
		t.Fatalf("default status = %d, want %d", rec.status, http.StatusOK)
	}
	if rec.wrote {
		t.Fatalf("wrote = true before any write, want false")
	}
}

// TestResponseRecorder_ExplicitWriteHeader verifies WriteHeader records the
// given code, flips wrote to true, and forwards the call to the underlying
// writer.
func TestResponseRecorder_ExplicitWriteHeader(t *testing.T) {
	t.Parallel()

	underlying := httptest.NewRecorder()
	rec := &responseRecorder{ResponseWriter: underlying}

	rec.WriteHeader(http.StatusTeapot)

	if !rec.wrote {
		t.Fatalf("wrote = false after WriteHeader, want true")
	}
	if rec.status != http.StatusTeapot {
		t.Fatalf("status = %d, want %d", rec.status, http.StatusTeapot)
	}
	if underlying.Code != http.StatusTeapot {
		t.Fatalf("underlying recorder code = %d, want %d (WriteHeader not forwarded)", underlying.Code, http.StatusTeapot)
	}
}

// TestResponseRecorder_FirstStatusWins verifies that a second WriteHeader
// call does not overwrite the recorded status (mirrors stdlib's own
// superfluous-WriteHeader semantics, which the recorder must not suppress).
func TestResponseRecorder_FirstStatusWins(t *testing.T) {
	t.Parallel()

	underlying := httptest.NewRecorder()
	rec := &responseRecorder{ResponseWriter: underlying}

	rec.WriteHeader(http.StatusTeapot)
	rec.WriteHeader(http.StatusInternalServerError)

	if rec.status != http.StatusTeapot {
		t.Fatalf("status = %d, want %d (first WriteHeader call should win)", rec.status, http.StatusTeapot)
	}
}

// TestResponseRecorder_Unwrap verifies Unwrap returns the exact underlying
// writer instance, so http.ResponseController can reach Flush/Hijack/etc.
func TestResponseRecorder_Unwrap(t *testing.T) {
	t.Parallel()

	underlying := httptest.NewRecorder()
	rec := &responseRecorder{ResponseWriter: underlying}

	if got := rec.Unwrap(); got != http.ResponseWriter(underlying) {
		t.Fatalf("Unwrap() = %v, want the underlying writer %v", got, underlying)
	}
}

// TestRecovery_PanicWithoutWrite_Returns500 verifies a handler that panics
// before writing anything is converted into a 500 with an explanatory body.
func TestRecovery_PanicWithoutWrite_Returns500(t *testing.T) {
	t.Parallel()

	h := Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "Internal Server Error") {
		t.Fatalf("body = %q, want it to contain %q", rec.Body.String(), "Internal Server Error")
	}
}

// TestRecovery_PanicAfterWrite_DoesNotAppend500 verifies that once a handler
// has already started a response (status + body written), a subsequent panic
// must not append a 500 on top of it.
func TestRecovery_PanicAfterWrite_DoesNotAppend500(t *testing.T) {
	t.Parallel()

	const wantBody = "partial-body"
	h := Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(wantBody))
		panic("boom-after-write")
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (500 must not be appended after the response started)", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != wantBody {
		t.Fatalf("body = %q, want %q (unchanged)", rec.Body.String(), wantBody)
	}
	if strings.Contains(rec.Body.String(), "Internal Server Error") {
		t.Fatalf("body = %q, must not contain %q", rec.Body.String(), "Internal Server Error")
	}
}

// TestRecovery_PanicAfterWrite_ThroughRequestLogger exercises the real
// production composition Recovery(RequestLogger(h)): the inner RequestLogger
// recorder must forward the handler's write through to the outer Recovery
// recorder so Recovery sees wrote=true and suppresses the 500 append.
func TestRecovery_PanicAfterWrite_ThroughRequestLogger(t *testing.T) {
	t.Parallel()

	const wantBody = "partial-body"
	h := Recovery(RequestLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(wantBody))
		panic("boom-after-write")
	})))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (500 must not be appended through the double wrap)", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != wantBody {
		t.Fatalf("body = %q, want %q (unchanged)", rec.Body.String(), wantBody)
	}
	if strings.Contains(rec.Body.String(), "Internal Server Error") {
		t.Fatalf("body = %q, must not contain %q", rec.Body.String(), "Internal Server Error")
	}
}

// TestRecovery_ErrAbortHandlerRePanics verifies http.ErrAbortHandler is
// re-panicked rather than converted to a 500 — it is stdlib's sanctioned
// connection-abort signal and net/http itself handles it.
func TestRecovery_ErrAbortHandlerRePanics(t *testing.T) {
	t.Parallel()

	h := Recovery(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	rePanicked := false
	func() {
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			if p == http.ErrAbortHandler { //nolint:errorlint // sentinel identity check, matches middleware.go
				rePanicked = true
				return
			}
			t.Fatalf("recovered unexpected value: %v", p)
		}()
		h.ServeHTTP(rec, req)
	}()

	if !rePanicked {
		t.Fatalf("Recovery did not re-panic http.ErrAbortHandler")
	}
}
