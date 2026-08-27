// Package cmd implements the Anvil CLI commands.
//
// ── Shared Timed HTTP Clients (TD-008) ──────────────────────────────
//
// The update and adapter install flows (cmd/update.go,
// cmd/adapter_binary.go, cmd/standard_shared.go) talk to GitHub and
// standard release repositories for release metadata and binary
// downloads. Every request goes through one of the two shared clients
// defined here:
//
//   - httpClient (httpGet): short-lived metadata requests — the latest
//     release lookup and SHA256SUMS.txt fetches. It carries an explicit
//     per-request timeout (60s default) so a stalled network surfaces a
//     clear timeout error instead of hanging the command indefinitely
//     (TD-008 §4).
//
//   - downloadClient (httpGetDownload): large binary and content
//     downloads (the CLI binary, adapter binaries, standard release
//     content up to 1 GiB). A total per-request timeout is WRONG for
//     these: on a slow-but-working connection the body read alone can
//     legitimately exceed 60s, and http.Client.Timeout cancels the whole
//     request mid-body with "context deadline exceeded ... while reading
//     body". Instead the download client bounds only the connection
//     phase (dial, TLS handshake, response headers) via the transport,
//     and the body read is bounded per-read by an idle timeout
//     (idleTimeoutBody): a download that keeps delivering data runs as
//     long as it needs, while a stalled connection fails within the idle
//     window with a clear timeout error. This preserves TD-008's
//     contract — no request can hang indefinitely, stalls surface as
//     explicit timeouts — without cutting off slow-but-progressing
//     downloads.
//
// Reference: TD-008, TS-007-034, TS-007-036, TS-007-037
package cmd

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

const (
	// httpDefaultTimeout is the default per-request timeout for the
	// short-lived metadata requests issued by the update and adapter
	// install flows (TD-008 §4: 30-60 seconds per request).
	httpDefaultTimeout = 60 * time.Second

	// EnvHTTPTimeout overrides the per-request HTTP timeout with a Go
	// duration (e.g. "30s"). Values that are unset, invalid, or
	// non-positive fall back to httpDefaultTimeout.
	EnvHTTPTimeout = "ANVIL_HTTP_TIMEOUT"

	// httpDefaultDownloadIdleTimeout is the default stall timeout for
	// binary and content downloads: a read that delivers no data for
	// this long fails with a clear timeout error. It is an IDLE bound —
	// every read that returns data re-arms the timer — so a slow but
	// progressing download is never cut off, while a stalled connection
	// surfaces within the idle window instead of hanging forever
	// (TD-008 §4).
	httpDefaultDownloadIdleTimeout = 30 * time.Second

	// EnvDownloadIdleTimeout overrides the download idle timeout with a
	// Go duration (e.g. "2m"). Values that are unset, invalid, or
	// non-positive fall back to httpDefaultDownloadIdleTimeout.
	EnvDownloadIdleTimeout = "ANVIL_DOWNLOAD_IDLE_TIMEOUT"

	// Connection-phase bounds for the download client (and the standard
	// content client built on the same transport). These cover the parts
	// of a request a healthy server answers quickly — establishing the
	// connection, the TLS handshake, and the response headers — so a
	// dead or blackholed server still fails fast. The body phase is
	// bounded separately by idleTimeoutBody.
	downloadDialTimeout           = 10 * time.Second
	downloadTLSHandshakeTimeout   = 10 * time.Second
	downloadResponseHeaderTimeout = 30 * time.Second
)

// httpClient is the shared HTTP client for short-lived metadata
// requests (latest-release lookup, checksum fetches) in the update and
// adapter install flows. It is a package-level seam: tests swap it (or
// its Timeout) to exercise timeout behavior without waiting on the
// production default deadline.
var httpClient = newHTTPClient()

// newHTTPClient builds an HTTP client carrying the configured per-request
// timeout. The timeout is resolved from EnvHTTPTimeout once at process
// start (see httpClientTimeout) and applies to the whole request,
// including the response body read — appropriate for the small metadata
// payloads this client serves.
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

// ── Download Client (large binary and content fetches) ──────────────

// downloadClient is the shared HTTP client for large binary and content
// downloads (the CLI binary during 'anvil update', adapter binaries, and
// standard release content). It deliberately carries NO total
// per-request timeout: a slow-but-progressing download must be allowed
// to run past any fixed deadline (a 13 MB CLI binary on a slow link
// regularly exceeds the 60s metadata timeout — the reported
// "context deadline exceeded ... while reading body" failure). Stalls
// are bounded instead by the transport's connection-phase timeouts and
// by the idle timeout on the body read (see httpGetDownload and
// idleTimeoutBody), so no request can hang indefinitely. It is a
// package-level seam: tests swap it to point at local test servers.
var downloadClient = newDownloadClient()

// newBoundedTransport builds the shared transport for download-style
// clients: the standard-library defaults with explicit connection-phase
// bounds — dial, TLS handshake, and response headers — so a dead or
// blackholed server fails fast while the (unbounded) body phase is left
// to the idle-timeout reader.
func newBoundedTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: downloadDialTimeout}).DialContext
	transport.TLSHandshakeTimeout = downloadTLSHandshakeTimeout
	transport.ResponseHeaderTimeout = downloadResponseHeaderTimeout
	return transport
}

// newDownloadClient builds the shared download client: the bounded
// transport and no total per-request timeout (the body is bounded by the
// idle timeout applied in httpGetDownload).
func newDownloadClient() *http.Client {
	return &http.Client{Transport: newBoundedTransport()}
}

// httpGetDownload performs a GET request through the download client and
// returns the response with its body wrapped in an idle-timeout reader:
// the download is bounded by activity, not by a total deadline (see
// idleTimeoutBody). Connection-phase failures (dial, TLS handshake,
// response headers) surface as their native, descriptive net/http
// errors; body stalls surface as "download timed out: no data received
// for ..." wrapping os.ErrDeadlineExceeded.
//
// The URL is intentionally variable: callers build it from constant
// templates and the runtime platform, never from user input.
func httpGetDownload(url string) (*http.Response, error) {
	resp, err := downloadClient.Get(url) //nolint:gosec // G107: template-built URLs, not user input
	if err != nil {
		return nil, err
	}
	resp.Body = newIdleTimeoutBody(resp.Body, downloadIdleTimeout())
	return resp, nil
}

// downloadIdleTimeout returns the body-read stall timeout: the value of
// EnvDownloadIdleTimeout when set and parseable as a positive duration,
// otherwise httpDefaultDownloadIdleTimeout.
func downloadIdleTimeout() time.Duration {
	raw, ok := os.LookupEnv(EnvDownloadIdleTimeout)
	if !ok {
		return httpDefaultDownloadIdleTimeout
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		return httpDefaultDownloadIdleTimeout
	}
	return timeout
}

// ── Idle-Timeout Body (TD-008, download stall detection) ────────────

// idleTimeoutBody wraps an http response body so a read that delivers no
// data for the configured idle window fails with a clear timeout error,
// while a slow-but-progressing download is never cut off: every read
// that returns data re-arms the timer, and only a read that stays empty
// for the full window trips it.
//
// The stall is implemented by closing the underlying body from the
// timer callback — a blocked Read on an http response body returns an
// error as soon as the body is closed — so the wrapped read surfaces a
// deterministic timeout instead of hanging. The error wraps
// os.ErrDeadlineExceeded, which isTimeout detects, so timeout handling
// downstream (TD-008 §9) treats it like every other deadline.
type idleTimeoutBody struct {
	body    io.ReadCloser
	timeout time.Duration

	mu       sync.Mutex
	timer    *time.Timer
	closed   bool
	timedOut bool
}

// newIdleTimeoutBody wraps body with the given idle timeout. The timer
// starts immediately; the first read that returns data re-arms it.
func newIdleTimeoutBody(body io.ReadCloser, timeout time.Duration) *idleTimeoutBody {
	b := &idleTimeoutBody{body: body, timeout: timeout}
	b.timer = time.AfterFunc(timeout, b.stall)
	return b
}

// stall fires when no data has arrived within the idle window. It marks
// the wrapper timed out and closes the underlying body, which unblocks a
// blocked Read. It is idempotent: the guard flag makes a late second
// firing (timer re-armed around the same moment) a no-op.
func (b *idleTimeoutBody) stall() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	b.timedOut = true
	_ = b.body.Close()
}

// Read re-arms the idle timer, reads from the underlying body, and maps
// a timer-induced close into the clear timeout error. Errors raised by
// the underlying body for other reasons pass through unchanged.
func (b *idleTimeoutBody) Read(p []byte) (int, error) {
	b.timer.Reset(b.timeout)
	n, err := b.body.Read(p)
	if err != nil {
		b.mu.Lock()
		defer b.mu.Unlock()
		if b.timedOut {
			return 0, b.timeoutError()
		}
		b.timer.Stop()
	}
	return n, err
}

// Close stops the stall timer and closes the underlying body. Closing
// an already-timed-out body is a no-op on the underlying close result.
func (b *idleTimeoutBody) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	b.timer.Stop()
	return b.body.Close()
}

// timeoutError renders the stall as an actionable timeout error. The
// idle window is named so the user can distinguish a dead connection
// from a slow one and raise ANVIL_DOWNLOAD_IDLE_TIMEOUT when their
// network needs a longer window. os.ErrDeadlineExceeded stays reachable
// via errors.Is for isTimeout.
func (b *idleTimeoutBody) timeoutError() error {
	return fmt.Errorf(
		"download timed out: no data received for %s (slow network? raise ANVIL_DOWNLOAD_IDLE_TIMEOUT, e.g. ANVIL_DOWNLOAD_IDLE_TIMEOUT=2m): %w",
		b.timeout, os.ErrDeadlineExceeded)
}

// httpError wraps an HTTP client error, distinguishing a timeout from
// other network failures with a clear message naming the timeout of the
// metadata client (TD-008 §9). Non-timeout errors are returned unchanged
// so their original context is preserved.
func httpError(err error) error {
	return httpErrorWithTimeout(httpClient.Timeout, err)
}

// httpErrorWithTimeout is the parameterized form of httpError: it names
// the timeout that actually bound the request, which differs per client
// (httpClient.Timeout for metadata requests, downloadResponseHeaderTimeout
// for transport-phase failures of the download client).
func httpErrorWithTimeout(timeout time.Duration, err error) error {
	if isTimeout(err) {
		return fmt.Errorf("request timed out after %s: %w", timeout, err)
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
