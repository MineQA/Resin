package netutil

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	safeFetchTimeout     = 15 * time.Second
	safeFetchMaxBodySize = 512 * 1024 // 512 KiB
	safeFetchMaxRedirect = 3
	safeFetchReadLimit   = safeFetchMaxBodySize + 1
)

// ---------------------------------------------------------------------------
// Sentinel errors
// ---------------------------------------------------------------------------

// ErrBodyTooLarge is returned when the response body exceeds the maximum
// allowed size (512 KiB). The caller may use errors.Is to detect this case.
var ErrBodyTooLarge = errors.New("response body exceeds maximum allowed size")

// ---------------------------------------------------------------------------
// Injectable seams (resolver / dialer)
// ---------------------------------------------------------------------------

// Resolver resolves hostnames to IP addresses. The network argument is one
// of "ip", "ip4", or "ip6".
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// Dialer establishes TCP connections.
type Dialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
}

// ---------------------------------------------------------------------------
// Default implementations
// ---------------------------------------------------------------------------

type netResolver struct {
	inner *net.Resolver
}

func (r *netResolver) LookupNetIP(ctx context.Context, network, host string) ([]net.IP, error) {
	addrs, err := r.inner.LookupNetIP(ctx, network, host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, len(addrs))
	for i, a := range addrs {
		ips[i] = net.IP(a.AsSlice())
	}
	return ips, nil
}

type netDialer struct{}

func (d *netDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	var inner net.Dialer
	return inner.DialContext(ctx, network, addr)
}

// ---------------------------------------------------------------------------
// safeHTTPSFetcher  (internal; tests construct directly)
// ---------------------------------------------------------------------------

type safeHTTPSFetcher struct {
	resolver    Resolver
	dialer      Dialer
	tlsConfigFn func(hostname string) *tls.Config
}

// newSafeHTTPSFetcher returns a fetcher wired to the real system resolver,
// system dialer, and a default TLS config pinned to the given hostname.
func newSafeHTTPSFetcher() *safeHTTPSFetcher {
	return &safeHTTPSFetcher{
		resolver: &netResolver{inner: &net.Resolver{}},
		dialer:   &netDialer{},
		tlsConfigFn: func(hostname string) *tls.Config {
			return &tls.Config{ServerName: hostname}
		},
	}
}

var defaultFetcher = newSafeHTTPSFetcher()

// FetchSafeHTTPS fetches a URL with strict safety checks.
//
// Errors are deliberately non-sensitive: they indicate which rule was violated
// without leaking internal IP addresses or other details that could aid an
// attacker probing internal infrastructure.
func FetchSafeHTTPS(ctx context.Context, rawURL string) (body []byte, finalURL string, err error) {
	return defaultFetcher.fetch(ctx, rawURL)
}

// ---------------------------------------------------------------------------
// Public fetch entry-point
// ---------------------------------------------------------------------------

func (f *safeHTTPSFetcher) fetch(ctx context.Context, rawURL string) ([]byte, string, error) {
	// Hard cap at 15s total duration.  If the caller already provided a
	// tighter deadline that deadline is preserved (WithTimeout honours the
	// parent's earlier cancellation).  If the caller has a later deadline
	// (or none at all) the child context fires after 15s.
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, safeFetchTimeout)
	defer cancel()

	// Validate the initial URL before any I/O.
	reqURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, "", &NonRetryableError{Err: fmt.Errorf("invalid URL: %w", err)}
	}
	if err := f.validateRequestURL(reqURL); err != nil {
		return nil, "", err
	}

	transport := &safeTransport{
		resolver:    f.resolver,
		dialer:      f.dialer,
		tlsConfigFn: f.tlsConfigFn,
	}

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > safeFetchMaxRedirect {
				return fmt.Errorf("safe fetch: too many redirects")
			}
			if err := f.validateRequestURL(req.URL); err != nil {
				return err
			}
			return nil
		},
		Jar: nil, // no cookie jar
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", &NonRetryableError{Err: fmt.Errorf("creating request: %w", err)}
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("safe fetch: unexpected status %d", resp.StatusCode)
	}

	// Early rejection when Content-Length already exceeds the limit.
	if resp.ContentLength > safeFetchMaxBodySize {
		return nil, "", ErrBodyTooLarge
	}

	// Read body with limit. LimitReader stops after safeFetchReadLimit bytes,
	// so we can detect bodies that exceed the maximum.
	body, err := io.ReadAll(io.LimitReader(resp.Body, safeFetchReadLimit))
	if err != nil {
		return nil, "", fmt.Errorf("safe fetch: read body failed: %w", err)
	}
	if len(body) > safeFetchMaxBodySize {
		return nil, "", ErrBodyTooLarge
	}

	return body, resp.Request.URL.String(), nil
}

// ---------------------------------------------------------------------------
// URL validation
// ---------------------------------------------------------------------------

func (f *safeHTTPSFetcher) validateRequestURL(u *url.URL) error {
	if u == nil {
		return fmt.Errorf("safe fetch: nil URL")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("safe fetch: non-https scheme %q", u.Scheme)
	}
	if u.User != nil {
		return fmt.Errorf("safe fetch: URL contains userinfo")
	}
	if u.Fragment != "" {
		return fmt.Errorf("safe fetch: URL contains fragment")
	}
	host := u.Host
	if host == "" {
		return fmt.Errorf("safe fetch: empty host")
	}
	hostname := strings.Trim(u.Hostname(), "[]")
	if hostname == "" {
		return fmt.Errorf("safe fetch: empty hostname")
	}
	if strings.EqualFold(hostname, "localhost") || strings.HasPrefix(strings.ToLower(hostname), "localhost.") {
		return fmt.Errorf("safe fetch: localhost host")
	}
	// Validate IP literals at URL level.
	if ip := net.ParseIP(hostname); ip != nil {
		ip = normalizeIP(ip)
		if err := validateIP(ip); err != nil {
			return fmt.Errorf("safe fetch: unsafe IP literal %q: %w", hostname, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// safeTransport – custom http.RoundTripper
// ---------------------------------------------------------------------------

// safeTransport implements http.RoundTripper. Each RoundTrip call resolves
// the hostname itself, validates every resolved address, opens a fresh TCP
// connection to the first validated IP, performs a TLS handshake with the
// original hostname, and sends the HTTP request.  Connection lifecycle is
// tied to the response body: closing the body closes the TCP and TLS conns.
type safeTransport struct {
	resolver    Resolver
	dialer      Dialer
	tlsConfigFn func(hostname string) *tls.Config
}

func (t *safeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	u := req.URL

	hostname := strings.Trim(u.Hostname(), "[]")
	port := u.Port()
	if port == "" {
		port = "443"
	}

	// Resolve and validate.  This rejects the request if ANY resolved address
	// is loopback, private, link-local, multicast, unspecified, documentation,
	// or reserved.  IPv4-mapped IPv6 addresses are normalised to IPv4 first.
	dialIP, err := t.resolveAndValidate(ctx, hostname)
	if err != nil {
		return nil, err
	}

	dialAddr := net.JoinHostPort(dialIP.String(), port)

	conn, err := t.dialer.DialContext(ctx, "tcp", dialAddr)
	if err != nil {
		return nil, fmt.Errorf("safe fetch: dial failed: %w", err)
	}

	// TLS handshake with the original hostname (SNI / cert / Host header).
	tlsConf := t.tlsConfigFn(hostname)
	if tlsConf == nil {
		tlsConf = &tls.Config{}
	}
	if tlsConf.ServerName == "" {
		tlsConf.ServerName = hostname
	}
	tlsConn := tls.Client(conn, tlsConf)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("safe fetch: TLS handshake failed: %w", err)
	}

	// Set the connection deadline from the context so that all I/O during
	// RoundTrip (request write + header read) respects the deadline.
	if deadline, ok := ctx.Deadline(); ok {
		tlsConn.SetDeadline(deadline)
	}

	// Write HTTP request over TLS.
	if err := req.Write(tlsConn); err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("safe fetch: write request failed: %w", err)
	}

	// Read HTTP response.
	br := bufio.NewReader(tlsConn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("safe fetch: read response failed: %w", err)
	}

	// Wrap the response body with a watcher that propagates context
	// cancellation throughout the body read phase (not just header read).
	// This watcher lives until the body is closed (resp.Body.Close()),
	// fixing the prior bug where the watcher stopped at RoundTrip return.
	resp.Body = newResponseBody(resp.Body, tlsConn, conn, ctx)

	return resp, nil
}

func (t *safeTransport) resolveAndValidate(ctx context.Context, hostname string) (net.IP, error) {
	// IP literal – validate directly.
	if ip := net.ParseIP(hostname); ip != nil {
		ip = normalizeIP(ip)
		if err := validateIP(ip); err != nil {
			return nil, fmt.Errorf("safe fetch: %w", err)
		}
		return ip, nil
	}

	// Hostname – resolve and validate every returned address.
	ips, err := t.resolver.LookupNetIP(ctx, "ip", hostname)
	if err != nil {
		return nil, fmt.Errorf("safe fetch: DNS resolution failed: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("safe fetch: no addresses for %q", hostname)
	}

	var dialIP net.IP
	for _, ip := range ips {
		ip = normalizeIP(ip)
		if err := validateIP(ip); err != nil {
			return nil, fmt.Errorf("safe fetch: %w", err)
		}
		if dialIP == nil {
			dialIP = ip
		}
	}

	return dialIP, nil
}

// ---------------------------------------------------------------------------
// responseBody – body wrapper that watches for ctx cancellation and
// closes underlying connections when the body is closed.
// ---------------------------------------------------------------------------

// responseBody wraps an http.Response.Body and keeps a context-cancellation
// watcher alive until Close() is called.  This ensures that a caller who
// cancels during a slow body read gets a prompt error rather than waiting
// for the hard deadline.
type responseBody struct {
	io.ReadCloser // the original resp.Body from http.ReadResponse
	tlsConn       *tls.Conn
	rawConn       net.Conn
	ctx           context.Context
	stopCh        chan struct{}
	closeOnce     sync.Once
}

func newResponseBody(inner io.ReadCloser, tlsConn *tls.Conn, rawConn net.Conn, ctx context.Context) *responseBody {
	b := &responseBody{
		ReadCloser: inner,
		tlsConn:    tlsConn,
		rawConn:    rawConn,
		ctx:        ctx,
		stopCh:     make(chan struct{}),
	}

	// If the context has a deadline, set it on the TLS connection as a
	// safety net.
	if deadline, ok := ctx.Deadline(); ok {
		tlsConn.SetDeadline(deadline)
	}

	// Start a watcher that fires on context cancellation and immediately
	// fails blocked I/O.  The watcher stops when the body is closed.
	if ctx.Done() != nil {
		go func() {
			select {
			case <-ctx.Done():
				tlsConn.SetDeadline(time.Now())
			case <-b.stopCh:
			}
		}()
	}

	return b
}

// Close closes the underlying HTTP body and the TLS/TCP connections.
// It also signals the context-watcher goroutine to exit.
func (b *responseBody) Close() error {
	b.closeOnce.Do(func() {
		close(b.stopCh)
		b.tlsConn.Close()
		b.rawConn.Close()
	})
	return b.ReadCloser.Close()
}

// ---------------------------------------------------------------------------
// IP helpers
// ---------------------------------------------------------------------------

// normalizeIP converts IPv4-mapped IPv6 addresses (e.g. ::ffff:192.0.2.1)
// to their IPv4 form so that validation checks (private, loopback, etc.)
// recognise them correctly.
func normalizeIP(ip net.IP) net.IP {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4
	}
	return ip
}

// validateIP rejects addresses that must never be dialled.
// It fires on loopback, private, link-local unicast & multicast, multicast,
// unspecified, documentation, and reserved address ranges.
//
// Errors do not include the actual IP value to uphold the non-sensitive
// error contract (callers should not learn about internal infrastructure
// IPs through error messages).
func validateIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("nil IP")
	}
	if ip.IsLoopback() {
		return fmt.Errorf("loopback address")
	}
	if ip.IsPrivate() {
		return fmt.Errorf("private address")
	}
	if ip.IsLinkLocalUnicast() {
		return fmt.Errorf("link-local unicast address")
	}
	if ip.IsLinkLocalMulticast() {
		return fmt.Errorf("link-local multicast address")
	}
	if ip.IsMulticast() {
		return fmt.Errorf("multicast address")
	}
	if ip.IsUnspecified() {
		return fmt.Errorf("unspecified address")
	}
	if isDocumentationIP(ip) {
		return fmt.Errorf("documentation address")
	}
	if isReservedIP(ip) {
		return fmt.Errorf("reserved address")
	}
	return nil
}
