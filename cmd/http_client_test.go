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
