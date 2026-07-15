package probe

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"time"

	"github.com/Resinat/Resin/internal/node"
)

// ---------------------------------------------------------------------------
// Response-aware types (quality-specific, additive to existing Fetcher)
// ---------------------------------------------------------------------------

// FetchResponse is an already-consumed HTTP response snapshot for quality
// observation. Header must be cloned before the original response is closed.
//
// The caller owns Body bytes and the cloned Header map. FetchResponse does
// not expose a live response body or mutable transport-owned header map.
type FetchResponse struct {
	Body       []byte
	Latency    time.Duration
	StatusCode int
	Header     http.Header
	FinalURL   string
}

// ResponseFetcher is used only by quality checks that need response metadata.
// The existing Fetcher remains the compatibility contract for egress/latency.
//
// ResponseFetcher is additive — it coexists with Fetcher in ProbeConfig and
// ProbeManager. When nil, quality checks fall back to a legacy adapter that
// wraps the plain Fetcher with limited metadata confidence.
type ResponseFetcher func(hash node.Hash, url string) (FetchResponse, error)

// DirectResponseFetcher creates a ResponseFetcher that performs direct HTTP
// requests (not through a node outbound). This is mostly useful for tests or
// fallback wiring.
//
// It preserves the same timeout and latency semantics as DirectFetcher while
// also returning status code, cloned headers, and final URL.
//
// timeout is a closure that returns the current probe timeout.
func DirectResponseFetcher(timeout func() time.Duration) ResponseFetcher {
	transport := &http.Transport{
		// Disable redirect following for trace endpoint handled below.
	}

	return func(_ node.Hash, url string) (FetchResponse, error) {
		t := timeout()
		if t <= 0 {
			return FetchResponse{}, fmt.Errorf("probe: invalid timeout %v", t)
		}

		ctx, cancel := context.WithTimeout(context.Background(), t)
		defer cancel()

		var tlsStart, tlsDone, firstByte time.Time
		trace := &httptrace.ClientTrace{
			TLSHandshakeStart:    func() { tlsStart = time.Now() },
			TLSHandshakeDone:     func(_ tls.ConnectionState, _ error) { tlsDone = time.Now() },
			GotFirstResponseByte: func() { firstByte = time.Now() },
		}

		req, err := http.NewRequestWithContext(
			httptrace.WithClientTrace(ctx, trace),
			http.MethodGet, url, nil,
		)
		if err != nil {
			return FetchResponse{}, fmt.Errorf("probe: create request: %w", err)
		}

		requestStart := time.Now()
		resp, err := transport.RoundTrip(req)
		if err != nil {
			return FetchResponse{}, fmt.Errorf("probe: do request: %w", err)
		}
		requestDone := time.Now()
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return FetchResponse{}, fmt.Errorf("probe: read body: %w", err)
		}

		// Prefer TLS handshake latency. If there is no handshake event (for
		// example HTTP/plaintext or connection reuse), fall back to request RTT.
		latency := requestDone.Sub(requestStart)
		if !tlsStart.IsZero() && !tlsDone.IsZero() && tlsDone.After(tlsStart) {
			latency = tlsDone.Sub(tlsStart)
		} else if !firstByte.IsZero() && firstByte.After(requestStart) {
			latency = firstByte.Sub(requestStart)
		}
		if latency <= 0 {
			latency = time.Nanosecond
		}

		finalURL := url
		if resp.Request != nil && resp.Request.URL != nil {
			finalURL = resp.Request.URL.String()
		}

		return FetchResponse{
			Body:       body,
			Latency:    latency,
			StatusCode: resp.StatusCode,
			Header:     resp.Header.Clone(),
			FinalURL:   finalURL,
		}, nil
	}
}

// LegacyResponseFetcher wraps a plain Fetcher into a ResponseFetcher with
// limited metadata confidence. The resulting FetchResponse has StatusCode=0,
// a nil Header, and empty FinalURL because the plain Fetcher contract does
// not expose HTTP metadata.
//
// Body challenge detection may still classify challenges via body patterns,
// making this adapter suitable for legacy code paths where the underlying
// transport provides the response body but not headers or status.
func LegacyResponseFetcher(fetcher Fetcher) ResponseFetcher {
	return func(hash node.Hash, url string) (FetchResponse, error) {
		body, latency, err := fetcher(hash, url)
		if err != nil {
			return FetchResponse{}, err
		}
		return FetchResponse{
			Body:    body,
			Latency: latency,
			// StatusCode=0, Header=nil, FinalURL="" — metadata poor.
		}, nil
	}
}

// ---------------------------------------------------------------------------
// Original Fetcher (unchanged — legacy egress/latency contract)
// ---------------------------------------------------------------------------

// DirectFetcher creates a Fetcher that performs direct HTTP requests
// (not through a node outbound). This is mostly useful for tests or
// fallback wiring.
//
// timeout is a closure that returns the current probe timeout.
func DirectFetcher(timeout func() time.Duration) Fetcher {
	transport := &http.Transport{
		// Disable redirect following for trace endpoint handled below.
	}

	return func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		t := timeout()
		if t <= 0 {
			return nil, 0, fmt.Errorf("probe: invalid timeout %v", t)
		}

		ctx, cancel := context.WithTimeout(context.Background(), t)
		defer cancel()

		var tlsStart, tlsDone, firstByte time.Time
		trace := &httptrace.ClientTrace{
			TLSHandshakeStart:    func() { tlsStart = time.Now() },
			TLSHandshakeDone:     func(_ tls.ConnectionState, _ error) { tlsDone = time.Now() },
			GotFirstResponseByte: func() { firstByte = time.Now() },
		}

		req, err := http.NewRequestWithContext(
			httptrace.WithClientTrace(ctx, trace),
			http.MethodGet, url, nil,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("probe: create request: %w", err)
		}

		requestStart := time.Now()
		resp, err := transport.RoundTrip(req)
		if err != nil {
			return nil, 0, fmt.Errorf("probe: do request: %w", err)
		}
		requestDone := time.Now()
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, 0, fmt.Errorf("probe: read body: %w", err)
		}

		// Prefer TLS handshake latency. If there is no handshake event (for
		// example HTTP/plaintext or connection reuse), fall back to request RTT.
		latency := requestDone.Sub(requestStart)
		if !tlsStart.IsZero() && !tlsDone.IsZero() && tlsDone.After(tlsStart) {
			latency = tlsDone.Sub(tlsStart)
		} else if !firstByte.IsZero() && firstByte.After(requestStart) {
			latency = firstByte.Sub(requestStart)
		}
		if latency <= 0 {
			latency = time.Nanosecond
		}

		return body, latency, nil
	}
}
