// Package cmd implements the Anvil CLI commands.
//
// Tests for the shared timed HTTP client (TD-008): every request issued
// by the update and adapter install flows must carry an explicit timeout
// and surface timeout failures with a clear, distinguishable message.
// The tests use httptest servers only — no real network.
//
// Reference: TD-008
package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// TestHTTPClient_HasExplicitTimeout verifies the shared client always
// carries a positive per-request timeout, so no update or adapter
// install request can hang indefinitely (TD-008 Validation Checklist:
// all HTTP call sites use a client with an explicit timeout).
func TestHTTPClient_HasExplicitTimeout(t *testing.T) {
	if httpClient.Timeout <= 0 {
		t.Errorf("httpClient.Timeout = %v, want a positive timeout", httpClient.Timeout)
	}
}

// TestDownloadClient_NoTotalTimeout_BoundedTransport verifies the
// download client carries NO total per-request timeout — a slow but
// progressing binary download must never be cut off by a fixed deadline
// (the reported "context deadline exceeded ... while reading body"
// failure on a 13 MB CLI binary) — while its transport still bounds the
// connection phase (dial, TLS handshake, response headers) and the body
// read is bounded by the idle timeout (idleTimeoutBody), so no request
// can hang indefinitely (TD-008).
func TestDownloadClient_NoTotalTimeout_BoundedTransport(t *testing.T) {
	if downloadClient.Timeout != 0 {
		t.Errorf("downloadClient.Timeout = %v, want 0 (no total deadline; the body is bounded by the idle timeout)", downloadClient.Timeout)
	}

	transport, ok := downloadClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("downloadClient.Transport = %T, want *http.Transport", downloadClient.Transport)
	}
	if transport.DialContext == nil {
		t.Error("downloadClient transport has no dial timeout bound")
	}
	if transport.TLSHandshakeTimeout <= 0 {
		t.Errorf("downloadClient TLSHandshakeTimeout = %v, want a positive bound", transport.TLSHandshakeTimeout)
	}
	if transport.ResponseHeaderTimeout <= 0 {
		t.Errorf("downloadClient ResponseHeaderTimeout = %v, want a positive bound", transport.ResponseHeaderTimeout)
	}
}

// TestHTTPGetDownload_Success verifies a normal download request returns
// the response and body through the download client with the body
// wrapped in the idle-timeout reader.
func TestHTTPGetDownload_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "payload")
	}))
	defer srv.Close()

	resp, err := httpGetDownload(srv.URL)
	if err != nil {
		t.Fatalf("httpGetDownload: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "payload" {
		t.Errorf("body = %q, want %q", body, "payload")
	}
}

// TestIdleTimeoutBody_StallsMidBody verifies that a download whose
// transfer STOPS mid-body — headers delivered, then silence — fails
// with a clear timeout error within the idle window, instead of hanging
// or being cut off only by a total deadline (TD-008 §4: a stalled
// network surfaces a clear timeout error).
func TestIdleTimeoutBody_StallsMidBody(t *testing.T) {
	t.Setenv(EnvDownloadIdleTimeout, "100ms")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2048")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("partial"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		time.Sleep(2 * time.Second)
		_, _ = w.Write(make([]byte, 2041))
	}))
	defer srv.Close()

	resp, err := httpGetDownload(srv.URL)
	if err != nil {
		t.Fatalf("httpGetDownload: %v", err)
	}
	defer resp.Body.Close()

	_, err = io.ReadAll(resp.Body)
	if err == nil {
		t.Fatal("expected a timeout error from a mid-body stall, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("stall error should mention the timeout, got: %v", err)
	}
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Error("stall error should wrap os.ErrDeadlineExceeded for isTimeout")
	}
}

// TestIdleTimeoutBody_SlowButProgressing is the regression test for the
// reported "anvil update" failure: a download that keeps delivering data
// — but with gaps that would trip any fixed total deadline — must be
// allowed to complete. The server trickles a chunk every 100ms for
// ~1.5s while the idle window is 150ms: a TOTAL timeout of 150ms would
// fail at the first gap, but the idle timeout re-arms on every read, so
// the transfer completes.
func TestIdleTimeoutBody_SlowButProgressing(t *testing.T) {
	t.Setenv(EnvDownloadIdleTimeout, "150ms")

	chunk := []byte("0123456789abcdef")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for i := 0; i < 10; i++ {
			_, _ = w.Write(chunk)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			time.Sleep(100 * time.Millisecond)
		}
	}))
	defer srv.Close()

	resp, err := httpGetDownload(srv.URL)
	if err != nil {
		t.Fatalf("httpGetDownload: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("slow-but-progressing download must complete, got: %v", err)
	}
	if want := 10 * len(chunk); len(body) != want {
		t.Errorf("body length = %d, want %d", len(body), want)
	}
}

// TestDownloadIdleTimeout_Default verifies the fallback idle timeout
// applies when ANVIL_DOWNLOAD_IDLE_TIMEOUT is unset, empty, invalid,
// zero, or negative.
func TestDownloadIdleTimeout_Default(t *testing.T) {
	t.Setenv(EnvDownloadIdleTimeout, "")
	if got := downloadIdleTimeout(); got != httpDefaultDownloadIdleTimeout {
		t.Errorf("empty override: downloadIdleTimeout() = %v, want %v", got, httpDefaultDownloadIdleTimeout)
	}

	t.Setenv(EnvDownloadIdleTimeout, "not-a-duration")
	if got := downloadIdleTimeout(); got != httpDefaultDownloadIdleTimeout {
		t.Errorf("invalid override: downloadIdleTimeout() = %v, want %v", got, httpDefaultDownloadIdleTimeout)
	}

	t.Setenv(EnvDownloadIdleTimeout, "0s")
	if got := downloadIdleTimeout(); got != httpDefaultDownloadIdleTimeout {
		t.Errorf("zero override: downloadIdleTimeout() = %v, want %v", got, httpDefaultDownloadIdleTimeout)
	}

	t.Setenv(EnvDownloadIdleTimeout, "-5s")
	if got := downloadIdleTimeout(); got != httpDefaultDownloadIdleTimeout {
		t.Errorf("negative override: downloadIdleTimeout() = %v, want %v", got, httpDefaultDownloadIdleTimeout)
	}
}

// TestDownloadIdleTimeout_EnvOverride verifies ANVIL_DOWNLOAD_IDLE_TIMEOUT
// is honored when set to a valid positive duration.
func TestDownloadIdleTimeout_EnvOverride(t *testing.T) {
	t.Setenv(EnvDownloadIdleTimeout, "2m")
	if got := downloadIdleTimeout(); got != 2*time.Minute {
		t.Errorf("downloadIdleTimeout() = %v, want 2m", got)
	}
}

// TestHTTPClientTimeout_Default verifies the fallback timeout applies
// when ANVIL_HTTP_TIMEOUT is unset, empty, invalid, zero, or negative
// (TD-008 §4: 30-60 seconds per request).
func TestHTTPClientTimeout_Default(t *testing.T) {
	t.Setenv(EnvHTTPTimeout, "")
	if got := httpClientTimeout(); got != httpDefaultTimeout {
		t.Errorf("empty override: httpClientTimeout() = %v, want %v", got, httpDefaultTimeout)
	}

	t.Setenv(EnvHTTPTimeout, "not-a-duration")
	if got := httpClientTimeout(); got != httpDefaultTimeout {
		t.Errorf("invalid override: httpClientTimeout() = %v, want %v", got, httpDefaultTimeout)
	}

	t.Setenv(EnvHTTPTimeout, "0s")
	if got := httpClientTimeout(); got != httpDefaultTimeout {
		t.Errorf("zero override: httpClientTimeout() = %v, want %v", got, httpDefaultTimeout)
	}

	t.Setenv(EnvHTTPTimeout, "-5s")
	if got := httpClientTimeout(); got != httpDefaultTimeout {
		t.Errorf("negative override: httpClientTimeout() = %v, want %v", got, httpDefaultTimeout)
	}
}

// TestHTTPClientTimeout_EnvOverride verifies ANVIL_HTTP_TIMEOUT is
// honored when set to a valid positive duration, and that newHTTPClient
// applies it (TD-008 §11: timeout configurable via an environment
// variable).
func TestHTTPClientTimeout_EnvOverride(t *testing.T) {
	t.Setenv(EnvHTTPTimeout, "30s")
	if got := httpClientTimeout(); got != 30*time.Second {
		t.Errorf("httpClientTimeout() = %v, want 30s", got)
	}

	client := newHTTPClient()
	if client.Timeout != 30*time.Second {
		t.Errorf("newHTTPClient().Timeout = %v, want 30s", client.Timeout)
	}
}

// TestHTTPGet_Success verifies a normal request returns the response and
// body through the shared client.
func TestHTTPGet_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	defer srv.Close()

	resp, err := httpGet(srv.URL)
	if err != nil {
		t.Fatalf("httpGet: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want %q", body, "ok")
	}
}

// TestHTTPGet_NonOKStatusReturned verifies non-2xx statuses are returned
// to the caller, not turned into errors — status handling stays at the
// call sites (same contract as http.Get).
func TestHTTPGet_NonOKStatusReturned(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	resp, err := httpGet(srv.URL)
	if err != nil {
		t.Fatalf("httpGet: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

// TestHTTPGet_TimesOutOnStalledServer verifies the shared client's
// timeout converts a stalled connection into a clear timeout error
// instead of hanging (TD-008 §4, §9). The timeout is shortened for the
// test; production uses httpDefaultTimeout.
func TestHTTPGet_TimesOutOnStalledServer(t *testing.T) {
	orig := httpClient
	httpClient = &http.Client{Timeout: 100 * time.Millisecond}
	t.Cleanup(func() { httpClient = orig })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	_, err := httpGet(srv.URL)
	if err == nil {
		t.Fatal("expected a timeout error from a stalled server, got nil")
	}
	if !strings.Contains(err.Error(), "timed out after 100ms") {
		t.Errorf("timeout error should name the timeout, got: %v", err)
	}
}

// TestHTTPError_TimeoutMapped verifies httpError maps a timeout to a
// clear message while preserving the original error for errors.Is, and
// leaves non-timeout errors unchanged.
func TestHTTPError_TimeoutMapped(t *testing.T) {
	// Non-timeout errors pass through untouched.
	plain := errors.New("connection refused")
	if got := httpError(plain); got != plain {
		t.Errorf("httpError(non-timeout) = %v, want unchanged", got)
	}

	// os.ErrDeadlineExceeded is mapped to a message naming the timeout
	// and stays reachable via errors.Is.
	mapped := httpError(os.ErrDeadlineExceeded)
	if !strings.Contains(mapped.Error(), "timed out") {
		t.Errorf("httpError(deadline) should mention the timeout, got: %v", mapped)
	}
	if !errors.Is(mapped, os.ErrDeadlineExceeded) {
		t.Error("httpError(deadline) should wrap the original error for errors.Is")
	}
}

// TestHTTPErrorWithTimeout_NamesGivenTimeout verifies the parameterized
// form names the timeout that actually bound the request — the download
// client's transport bounds and the idle window differ from the metadata
// client's total timeout, so the message must reflect the right one.
func TestHTTPErrorWithTimeout_NamesGivenTimeout(t *testing.T) {
	mapped := httpErrorWithTimeout(30*time.Second, os.ErrDeadlineExceeded)
	if !strings.Contains(mapped.Error(), "timed out after 30s") {
		t.Errorf("httpErrorWithTimeout should name the given timeout, got: %v", mapped)
	}
	if !errors.Is(mapped, os.ErrDeadlineExceeded) {
		t.Error("httpErrorWithTimeout should wrap the original error for errors.Is")
	}

	plain := errors.New("connection refused")
	if got := httpErrorWithTimeout(30*time.Second, plain); got != plain {
		t.Errorf("httpErrorWithTimeout(non-timeout) = %v, want unchanged", got)
	}
}
