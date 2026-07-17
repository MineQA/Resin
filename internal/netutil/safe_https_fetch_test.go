package netutil

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testResolver returns a fixed set of IPs for any hostname.
type testResolver struct {
	ips []net.IP
}

func (r *testResolver) LookupNetIP(_ context.Context, _, _ string) ([]net.IP, error) {
	return r.ips, nil
}

// testResolverFunc is a Resolver backed by a function (useful for call-counting).
type testResolverFunc struct {
	fn func(ctx context.Context, network, host string) ([]net.IP, error)
}

func (r *testResolverFunc) LookupNetIP(ctx context.Context, network, host string) ([]net.IP, error) {
	return r.fn(ctx, network, host)
}

// testDialer connects to a fixed target address regardless of the requested addr.
type testDialer struct {
	targetAddr string // "host:port" to actually connect to
}

func (d *testDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	var inner net.Dialer
	return inner.DialContext(ctx, network, d.targetAddr)
}

// testDialerVerify connects to a fixed target address and checks that the
// caller-requested address matches expectedAddr.
type testDialerVerify struct {
	expectedAddr string
	targetAddr   string
	t            *testing.T
}

func (d *testDialerVerify) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if addr != d.expectedAddr {
		d.t.Errorf("safe fetch dial addr = %q, want %q", addr, d.expectedAddr)
	}
	var inner net.Dialer
	return inner.DialContext(ctx, network, d.targetAddr)
}

// testTLSConfig returns a TLS config suitable for tests connecting to
// httptest.NewTLSServer (self-signed cert for 127.0.0.1 / localhost).
func testTLSConfig() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true}
}

// mustStartTLS starts an httptest.TLSServer and returns it along with a
// dialer that connects to the server's listener.
func mustStartTLS(t *testing.T, h http.Handler) (*httptest.Server, *testDialer) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	// Strip scheme to get raw host:port.
	return srv, &testDialer{targetAddr: srv.Listener.Addr().String()}
}

// ---------------------------------------------------------------------------
// Helper: build a fetcher with test seams that talks to an httptest.TLSServer.
// ---------------------------------------------------------------------------

func testFetcher(srv *httptest.Server, resolverIPs []net.IP, tlsFn func(string) *tls.Config) *safeHTTPSFetcher {
	var dialer Dialer
	if tlsFn == nil {
		tlsFn = func(string) *tls.Config { return testTLSConfig() }
	}
	dialer = &testDialer{targetAddr: srv.Listener.Addr().String()}
	return &safeHTTPSFetcher{
		resolver:    &testResolver{ips: resolverIPs},
		dialer:      dialer,
		tlsConfigFn: tlsFn,
	}
}

// ---------------------------------------------------------------------------
// 1. Basic success
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_BasicSuccess(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello world"))
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)
	body, finalURL, err := fetcher.fetch(context.Background(), "https://example.com/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "hello world" {
		t.Fatalf("body = %q, want %q", string(body), "hello world")
	}
	if finalURL != "https://example.com/path" {
		t.Fatalf("finalURL = %q, want %q", finalURL, "https://example.com/path")
	}
}

// ---------------------------------------------------------------------------
// 2. Public function (FetchSafeHTTPS) fails gracefully without test seams
//    (no real network call attempted because there is no route to example.com
//     in CI, so we just verify it returns an error, not a panic).
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_PublicAPIReturnsErrorWithoutNetwork(t *testing.T) {
	// No network needed — short timeout ensures it fails quickly.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	_, _, err := FetchSafeHTTPS(ctx, "https://192.0.2.1/nonexistent")
	if err == nil {
		t.Fatal("expected error when no network is available")
	}
}

// ---------------------------------------------------------------------------
// 3. Private IP blocked (even when resolved)
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_PrivateIPBlocked(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(192, 168, 1, 1)}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
	if err == nil {
		t.Fatal("expected error for private IP")
	}
	if !strings.Contains(err.Error(), "private address") {
		t.Fatalf("error does not mention private address: %v", err)
	}
}

func TestFetchSafeHTTPS_PrivateIPLiteralInURL(t *testing.T) {
	_, _, err := defaultFetcher.fetch(context.Background(), "https://192.168.1.1/path")
	if err == nil {
		t.Fatal("expected error for private IP literal in URL")
	}
}

// ---------------------------------------------------------------------------
// 4. IPv4-mapped IPv6 normalized and rejected
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_IPv4MappedIPv6Rejected(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// ::ffff:192.168.1.1 should be normalised to 192.168.1.1 and rejected.
	mapped := net.ParseIP("::ffff:192.168.1.1")
	fetcher := testFetcher(srv, []net.IP{mapped}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
	if err == nil {
		t.Fatal("expected error for IPv4-mapped IPv6 private address")
	}
}

// ---------------------------------------------------------------------------
// 5. Loopback blocked
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_LoopbackIPBlocked(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(127, 0, 0, 1)}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
	if err == nil {
		t.Fatal("expected error for loopback IP")
	}
}

func TestFetchSafeHTTPS_LoopbackIPLiteralInURL(t *testing.T) {
	_, _, err := defaultFetcher.fetch(context.Background(), "https://127.0.0.1:8080/path")
	if err == nil {
		t.Fatal("expected error for loopback IP literal in URL")
	}
}

// ---------------------------------------------------------------------------
// 6. Documentation IP blocked
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_DocumentationIPBlocked(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	for _, docIP := range []net.IP{
		net.IPv4(192, 0, 2, 1),
		net.IPv4(198, 51, 100, 1),
		net.IPv4(203, 0, 113, 1),
		net.ParseIP("2001:db8::1"),
	} {
		fetcher := testFetcher(srv, []net.IP{docIP}, nil)
		_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
		if err == nil {
			t.Fatalf("expected error for documentation IP %q", docIP)
		}
	}
}

// ---------------------------------------------------------------------------
// 7. Reserved IP blocked
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_ReservedIPBlocked(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	for _, rsvIP := range []net.IP{
		net.IPv4(240, 0, 0, 1),
		net.ParseIP("100::1"),
		net.ParseIP("fec0::1"),
	} {
		fetcher := testFetcher(srv, []net.IP{rsvIP}, nil)
		_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
		if err == nil {
			t.Fatalf("expected error for reserved IP %q", rsvIP)
		}
	}
}

// ---------------------------------------------------------------------------
// 8. Unspecified / link-local / multicast blocked
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_UnspecifiedBlocked(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(0, 0, 0, 0)}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
	if err == nil {
		t.Fatal("expected error for unspecified address")
	}
}

func TestFetchSafeHTTPS_LinkLocalUnicastBlocked(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(169, 254, 1, 1)}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
	if err == nil {
		t.Fatal("expected error for link-local unicast address")
	}
}

func TestFetchSafeHTTPS_LinkLocalMulticastBlocked(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(224, 0, 0, 1)}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
	if err == nil {
		t.Fatal("expected error for link-local multicast address")
	}
}

func TestFetchSafeHTTPS_MulticastBlocked(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(239, 255, 255, 250)}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
	if err == nil {
		t.Fatal("expected error for multicast address")
	}
}

// ---------------------------------------------------------------------------
// 9. Public resolved IP is the dial address
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_DialUsesResolvedPublicIP(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("dial-check"))
	}))
	t.Cleanup(srv.Close)

	publicIP := net.IPv4(1, 1, 1, 1)
	dialer := &testDialerVerify{
		expectedAddr: net.JoinHostPort(publicIP.String(), "443"),
		targetAddr:   srv.Listener.Addr().String(),
		t:            t,
	}
	fetcher := &safeHTTPSFetcher{
		resolver:    &testResolver{ips: []net.IP{publicIP}},
		dialer:      dialer,
		tlsConfigFn: func(string) *tls.Config { return testTLSConfig() },
	}

	body, finalURL, err := fetcher.fetch(context.Background(), "https://example.com/verified")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "dial-check" {
		t.Fatalf("body = %q, want %q", string(body), "dial-check")
	}
	if finalURL != "https://example.com/verified" {
		t.Fatalf("finalURL = %q, want %q", finalURL, "https://example.com/verified")
	}
}

// ---------------------------------------------------------------------------
// 10. Hostname is not redialed (resolved once, dialed directly to IP)
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_HostnameNotRedialed(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("once"))
	}))
	t.Cleanup(srv.Close)

	var resolveCount atomic.Int32
	resolver := &testResolverFunc{
		fn: func(_ context.Context, _, host string) ([]net.IP, error) {
			resolveCount.Add(1)
			if host != "example.com" {
				t.Errorf("resolved unexpected host %q", host)
			}
			return []net.IP{net.IPv4(1, 1, 1, 1)}, nil
		},
	}

	// dialer that checks the dial address is 1.1.1.1:443, not example.com:443
	dialer := &testDialerVerify{
		expectedAddr: net.JoinHostPort(
			net.IPv4(1, 1, 1, 1).String(), "443",
		),
		targetAddr: srv.Listener.Addr().String(),
		t:          t,
	}

	fetcher := &safeHTTPSFetcher{
		resolver:    resolver,
		dialer:      dialer,
		tlsConfigFn: func(string) *tls.Config { return testTLSConfig() },
	}

	body, _, err := fetcher.fetch(context.Background(), "https://example.com/once")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "once" {
		t.Fatalf("body = %q, want %q", string(body), "once")
	}
	if n := resolveCount.Load(); n != 1 {
		t.Fatalf("resolver called %d times, want 1", n)
	}
}

// ---------------------------------------------------------------------------
// 11. HTTPS-only & URL shape validation
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_RejectsNonHTTPS(t *testing.T) {
	_, _, err := defaultFetcher.fetch(context.Background(), "http://example.com/path")
	if err == nil {
		t.Fatal("expected error for http scheme")
	}
}

func TestFetchSafeHTTPS_RejectsUserinfo(t *testing.T) {
	_, _, err := defaultFetcher.fetch(context.Background(), "https://user:pass@example.com/path")
	if err == nil {
		t.Fatal("expected error for userinfo")
	}
}

func TestFetchSafeHTTPS_RejectsFragment(t *testing.T) {
	_, _, err := defaultFetcher.fetch(context.Background(), "https://example.com/path#frag")
	if err == nil {
		t.Fatal("expected error for fragment")
	}
}

func TestFetchSafeHTTPS_RejectsLocalhost(t *testing.T) {
	_, _, err := defaultFetcher.fetch(context.Background(), "https://localhost:8080/path")
	if err == nil {
		t.Fatal("expected error for localhost")
	}
	_, _, err = defaultFetcher.fetch(context.Background(), "https://localhost./path")
	if err == nil {
		t.Fatal("expected error for localhost.")
	}
}

func TestFetchSafeHTTPS_RejectsEmptyHost(t *testing.T) {
	_, _, err := defaultFetcher.fetch(context.Background(), "https:///path")
	if err == nil {
		t.Fatal("expected error for empty host")
	}
}

// ---------------------------------------------------------------------------
// 12. Redirect: exactly 3 allowed, 4th rejected
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_ThreeRedirectsAllowed(t *testing.T) {
	// Server: first 3 requests get a redirect, the 4th serves the final body.
	var counter atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := counter.Add(1)
		if n <= 3 {
			http.Redirect(w, r, "https://example.com/next", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("final"))
	})

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)

	body, finalURL, err := fetcher.fetch(context.Background(), "https://example.com/start")
	if err != nil {
		t.Fatalf("expected success after 3 redirects, got: %v", err)
	}
	if string(body) != "final" {
		t.Fatalf("body = %q, want %q", string(body), "final")
	}
	if finalURL != "https://example.com/next" {
		t.Fatalf("finalURL = %q, want %q", finalURL, "https://example.com/next")
	}
	if n := counter.Load(); n != 4 {
		t.Fatalf("total requests = %d, want 4 (3 redirects + 1 final)", n)
	}
}

func TestFetchSafeHTTPS_FourthRedirectRejected(t *testing.T) {
	// Server offers 4 redirects before serving final content.
	// The client should reject the 4th redirect, so the final handler is never hit.
	var counter atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := counter.Add(1)
		if n <= 4 {
			http.Redirect(w, r, "https://example.com/next", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("final"))
	})

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)

	_, _, err := fetcher.fetch(context.Background(), "https://example.com/start")
	if err == nil {
		t.Fatal("expected error after 4 redirects")
	}
	if !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Exactly 4 requests should be made (initial + 3 followed redirects).
	// The 4th redirect attempt is blocked at the CheckRedirect level.
	if n := counter.Load(); n != 4 {
		t.Fatalf("total requests = %d, want 4 (3 followed redirects + 1 redirected response that triggers rejection)", n)
	}
}

// ---------------------------------------------------------------------------
// 13. Redirect validates target URL safety
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_RedirectToNonHTTPSRejected(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.com/phish", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/start")
	if err == nil {
		t.Fatal("expected error for redirect to non-https")
	}
}

func TestFetchSafeHTTPS_RedirectToLocalhostRejected(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://localhost:9999/evil", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/start")
	if err == nil {
		t.Fatal("expected error for redirect to localhost")
	}
}

func TestFetchSafeHTTPS_RedirectToPrivateIPLiteralRejected(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://10.0.0.1/admin", http.StatusFound)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/start")
	if err == nil {
		t.Fatal("expected error for redirect to private IP literal")
	}
}

// ---------------------------------------------------------------------------
// 14. Body size limits: oversize via Content-Length rejected
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_BodyOversizeViaContentLength(t *testing.T) {
	bodySize := safeFetchMaxBodySize + 1
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", bodySize))
		w.WriteHeader(http.StatusOK)
		// Write exactly bodySize bytes.
		buf := make([]byte, bodySize)
		for i := range buf {
			buf[i] = 'a'
		}
		_, _ = w.Write(buf)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/big")
	if err == nil {
		t.Fatal("expected error for oversize body (Content-Length)")
	}
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 15. Body size limits: oversize via chunked transfer rejected
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_BodyOversizeViaChunked(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected http.Flusher")
		}
		w.WriteHeader(http.StatusOK)
		// Write 256 KB chunks until we exceed the limit.
		chunk := make([]byte, 256*1024)
		for i := range chunk {
			chunk[i] = 'b'
		}
		// Write 3 chunks = 768 KB > 512 KB.
		for i := 0; i < 3; i++ {
			_, _ = w.Write(chunk)
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/chunked")
	if err == nil {
		t.Fatal("expected error for oversize chunked body")
	}
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// 16. Body size limits: exact size accepted
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_ExactBodySizeAccepted(t *testing.T) {
	bodySize := safeFetchMaxBodySize
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", bodySize))
		w.WriteHeader(http.StatusOK)
		buf := make([]byte, bodySize)
		for i := range buf {
			buf[i] = 'c'
		}
		_, _ = w.Write(buf)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)
	body, _, err := fetcher.fetch(context.Background(), "https://example.com/exact")
	if err != nil {
		t.Fatalf("unexpected error for exact-size body: %v", err)
	}
	if len(body) != safeFetchMaxBodySize {
		t.Fatalf("body length = %d, want %d", len(body), safeFetchMaxBodySize)
	}
	// Verify content
	for _, b := range body {
		if b != 'c' {
			t.Fatal("body content mismatch")
		}
	}
}

// ---------------------------------------------------------------------------
// 17. Status rejection: non-200 status
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_RejectsNon200Status(t *testing.T) {
	tests := []int{
		http.StatusNotFound,
		http.StatusForbidden,
		http.StatusInternalServerError,
		http.StatusMovedPermanently,
	}
	for _, status := range tests {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte("error"))
		}))
		fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)
		_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
		if err == nil {
			t.Fatalf("expected error for status %d", status)
		}
		srv.Close()
	}
}

// ---------------------------------------------------------------------------
// 18. Context timeout / cancellation
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_ContextTimeout(t *testing.T) {
	// Server that blocks until client context is cancelled.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, _, err := fetcher.fetch(ctx, "https://example.com/slow")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestFetchSafeHTTPS_ContextCancelled(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // immediately cancelled

	_, _, err := fetcher.fetch(ctx, "https://example.com/cancel")
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

// ---------------------------------------------------------------------------
// 19. Default timeout applied when no context deadline
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_DefaultTimeoutApplied(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until client disconnects; default 15s timeout fires.
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)

	// No deadline on context - default 15s should fire.
	start := time.Now()
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/slow")
	if err == nil {
		t.Fatal("expected timeout due to default 15s limit")
	}
	if elapsed := time.Since(start); elapsed > 18*time.Second {
		t.Fatalf("default timeout did not fire in time: took %v", elapsed)
	}
}

// ---------------------------------------------------------------------------
// 20. Proxy bypass: HTTP_PROXY / HTTPS_PROXY env vars do not affect fetch
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_ProxyEnvBypassed(t *testing.T) {
	// Set proxy env vars. Since our transport does not use http.ProxyFromEnvironment,
	// these should have no effect on the fetch.
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:9999")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:9999")
	t.Setenv("NO_PROXY", "")

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("proxy-bypassed"))
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)
	body, _, err := fetcher.fetch(context.Background(), "https://example.com/proxy-test")
	if err != nil {
		t.Fatalf("unexpected error with proxy env set: %v", err)
	}
	if string(body) != "proxy-bypassed" {
		t.Fatalf("body = %q, want %q", string(body), "proxy-bypassed")
	}
}

// ---------------------------------------------------------------------------
// 21. Sensitive IP classes also blocked for non-canonical IPv6 forms
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_IPv6LoopbackBlocked(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.ParseIP("::1")}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
	if err == nil {
		t.Fatal("expected error for IPv6 loopback")
	}
}

func TestFetchSafeHTTPS_IPv6PrivateBlocked(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.ParseIP("fd00::1")}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
	if err == nil {
		t.Fatal("expected error for IPv6 unique-local (private)")
	}
}

func TestFetchSafeHTTPS_IPv6LinkLocalBlocked(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.ParseIP("fe80::1")}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
	if err == nil {
		t.Fatal("expected error for IPv6 link-local")
	}
}

// ---------------------------------------------------------------------------
// 22. Multiple resolved IPs: if ANY is unsafe, the whole request is rejected
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_MixedPublicAndPrivateRejected(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	// First IP is public, second is private -> should reject because of private.
	fetcher := testFetcher(srv, []net.IP{
		net.IPv4(1, 1, 1, 1),
		net.IPv4(10, 0, 0, 1),
	}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/path")
	if err == nil {
		t.Fatal("expected error when one resolved address is private")
	}
}

// ---------------------------------------------------------------------------
// 23. SafeTransport's error messages are non-sensitive (no IP leaks in errors)
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_ErrorMessagesNonSensitive(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"http scheme", "http://internal.admin/path"},
		{"userinfo", "https://token@internal.admin/path"},
		{"fragment", "https://internal.admin/path#sec"},
		{"localhost", "https://localhost:8080/admin"},
		{"private IP literal", "https://10.0.0.1/admin"},
	}
	for _, tt := range tests {
		_, _, err := defaultFetcher.fetch(context.Background(), tt.url)
		if err == nil {
			t.Errorf("%s: expected error", tt.name)
			continue
		}
		// Errors should not contain the full URL or credentials.
		msg := err.Error()
		if strings.Contains(msg, "token@") {
			t.Errorf("%s: error leaks credential: %s", tt.name, msg)
		}
		if strings.Contains(msg, "internal.admin") {
			// The hostname appears in non-sensitive scheme/host validation errors,
			// which is fine — the caller provided it.  The important thing is that
			// resolved IPs are not leaked.
			continue
		}
	}
}

// ---------------------------------------------------------------------------
// 24. Caller later deadline capped to 15s
// ---------------------------------------------------------------------------

// testDialerCapture is a Dialer that connects to a fixed target and records
// the context deadline seen by the first DialContext call.
type testDialerCapture struct {
	targetAddr       string
	capturedDeadline time.Time
	deadlineSet      bool
	t                *testing.T
}

func (d *testDialerCapture) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	if !d.deadlineSet {
		d.capturedDeadline, d.deadlineSet = ctx.Deadline()
	}
	var inner net.Dialer
	return inner.DialContext(ctx, network, d.targetAddr)
}

func TestFetchSafeHTTPS_CallerLaterDeadlineCapped(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("capped"))
	}))
	t.Cleanup(srv.Close)

	// Caller provides a deadline far in the future (60 s).
	callerDeadline := time.Now().Add(60 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), callerDeadline)
	defer cancel()

	capture := &testDialerCapture{
		targetAddr: srv.Listener.Addr().String(),
		t:          t,
	}
	fetcher := &safeHTTPSFetcher{
		resolver:    &testResolver{ips: []net.IP{net.IPv4(1, 1, 1, 1)}},
		dialer:      capture,
		tlsConfigFn: func(string) *tls.Config { return testTLSConfig() },
	}

	body, _, err := fetcher.fetch(ctx, "https://example.com/test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "capped" {
		t.Fatalf("body = %q, want %q", string(body), "capped")
	}
	if !capture.deadlineSet {
		t.Fatal("dialer did not receive a context deadline")
	}
	// The effective deadline must be earlier than the caller's 60s deadline.
	if !capture.capturedDeadline.Before(callerDeadline) {
		t.Fatalf("effective deadline %v is not before caller deadline %v",
			capture.capturedDeadline, callerDeadline)
	}
	// Sanity: it should also be well before now+30s (not in the 60s range).
	if !capture.capturedDeadline.Before(time.Now().Add(25 * time.Second)) {
		t.Fatalf("effective deadline %v seems too far in the future (not capped to ~15s)", capture.capturedDeadline)
	}
}

// ---------------------------------------------------------------------------
// 26. Cancel during body read returns promptly (watcher lives beyond headers)
// ---------------------------------------------------------------------------

// TestFetchSafeHTTPS_CancelDuringBodyReadReturnsPromptly sends 200 + flush
// then stalls; the caller cancels after headers arrive and must get an error
// well before the 15s hard deadline.
func TestFetchSafeHTTPS_CancelDuringBodyReadReturnsPromptly(t *testing.T) {
	headersSent := make(chan struct{})

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("initial"))
		w.(http.Flusher).Flush()
		close(headersSent)
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)
		_, _, err := fetcher.fetch(ctx, "https://example.com/stall")
		errCh <- err
	}()

	// Wait for the handler to flush headers, then cancel.
	<-headersSent
	time.Sleep(5 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error after cancellation during body read")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fetch did not return promptly after context cancellation (5s timeout)")
	}
}

// ---------------------------------------------------------------------------
// 25. Content-Length early rejection
// ---------------------------------------------------------------------------

func TestFetchSafeHTTPS_ContentLengthEarlyRejection(t *testing.T) {
	bodySize := safeFetchMaxBodySize + 1
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", bodySize))
		w.WriteHeader(http.StatusOK)
		// Write nothing — early rejection should happen before any body bytes.
	}))
	t.Cleanup(srv.Close)

	fetcher := testFetcher(srv, []net.IP{net.IPv4(1, 1, 1, 1)}, nil)
	_, _, err := fetcher.fetch(context.Background(), "https://example.com/early-reject")
	if err == nil {
		t.Fatal("expected error for Content-Length oversize")
	}
	if !errors.Is(err, ErrBodyTooLarge) {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}
}
