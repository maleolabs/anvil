// Package cmd implements the Anvil CLI commands.
//
// ── Shared Timed HTTP Client (TD-008) ───────────────────────────────
//
// The update and adapter install flows (cmd/update.go,
// cmd/adapter_binary.go) talk to GitHub for release metadata and binary
// downloads. Every request goes through the shared timed client defined
// here: httpGet wraps httpClient, which carries an explicit per-request
// timeout so a stalled network surfaces a clear timeout error instead of
// hanging the command indefinitely (TD-008 §4).
//
// Reference: TD-008, TS-007-034, TS-007-036, TS-007-037
package cmd

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"
)

const (
	// httpDefaultTimeout is the default per-request timeout for every
	// HTTP request issued by the update and adapter install flows
	// (TD-008 §4: 30-60 seconds per request).
	httpDefaultTimeout = 60 * time.Second

	// EnvHTTPTimeout overrides the per-request HTTP timeout with a Go
	// duration (e.g. "30s"). Values that are unset, invalid, or
	// non-positive fall back to httpDefaultTimeout.
	EnvHTTPTimeout = "ANVIL_HTTP_TIMEOUT"
)

// httpClient is the shared HTTP client used by every request in the
// update and adapter install flows. It is a package-level seam: tests
// swap it (or its Timeout) to exercise timeout behavior without waiting
// on the production default deadline.
var httpClient = newHTTPClient()

// newHTTPClient builds an HTTP client carrying the configured per-request
// timeout. The timeout is resolved from EnvHTTPTimeout once at process
// start (see httpClientTimeout) and applies to the whole request,
// including the response body read.
func newHTTPClient() *http.Client {
	return &http.Client{Timeout: httpClientTimeout()}
}

// httpClientTimeout returns the per-request HTTP timeout: the value of
// EnvHTTPTimeout when set and parseable as a positive duration, otherwise
// httpDefaultTimeout.
func httpClientTimeout() time.Duration {
	raw, ok := os.LookupEnv(EnvHTTPTimeout)
	if !ok {
		return httpDefaultTimeout
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		return httpDefaultTimeout
	}
	return timeout
}

// httpGet performs a GET request through the shared timed client and
// returns the response. Non-2xx statuses are not errors at this layer —
// callers inspect resp.StatusCode, matching the previous http.Get
// behavior. A request that exceeds the client timeout is surfaced as an
// explicit timeout error instead of hanging forever (TD-008 §4, §9).
//
// The URL is intentionally variable: callers build it from constant
// templates and the runtime platform, never from user input.
func httpGet(url string) (*http.Response, error) {
	resp, err := httpClient.Get(url) //nolint:gosec // G107: template-built URLs, not user input
	if err != nil {
		return nil, httpError(err)
	}
	return resp, nil
}

// httpError wraps an HTTP client error, distinguishing a timeout from
// other network failures with a clear message naming the timeout
// (TD-008 §9). Non-timeout errors are returned unchanged so their
// original context is preserved.
func httpError(err error) error {
	if isTimeout(err) {
		return fmt.Errorf("request timed out after %s: %w", httpClient.Timeout, err)
	}
	return err
}

// isTimeout reports whether err is a deadline exceeded or a network
// timeout. http.Client.Timeout surfaces as a net.Error with Timeout()
// true or as os.ErrDeadlineExceeded, depending on the Go version.
func isTimeout(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
