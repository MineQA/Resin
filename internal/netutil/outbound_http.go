package netutil

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"time"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
)

const defaultOutboundUserAgent = "Resin/1.0"

type ConnLifecycleOp uint8

const (
	ConnLifecycleOpen ConnLifecycleOp = iota
	ConnLifecycleClose
)

// OutboundHTTPOptions controls outbound-backed HTTP execution behavior.
type OutboundHTTPOptions struct {
	// RequireStatusOK enforces HTTP 200 status; otherwise any status is accepted.
	RequireStatusOK bool
	// UserAgent overrides the request User-Agent when non-empty.
	UserAgent string
	// OnConnLifecycle is called with open/close lifecycle events to track connection
	// lifecycle for metrics. Set by probe callers to count outbound connections;
	// left nil for download callers (GeoIP, subscription) to exclude from stats.
	OnConnLifecycle func(op ConnLifecycleOp)
	// CheckRedirect is an optional redirect-URL validation callback. When non-nil,
	// it is called before following each redirect. The callback may reject redirects
	// to unsafe or malformed URLs by returning a non-nil error.
	// When nil, the default http.Client redirect policy applies (up to 10 redirects).
	// This is used by quality checks (ResponseFetcher) to enforce URL-shape safety;
	// legacy egress/latency probes leave this nil for backward compatibility.
	CheckRedirect func(req *http.Request, via []*http.Request) error
}

// HTTPGetResponse holds the full response metadata from an outbound HTTP GET.
// Header is cloned before the original response is closed; the caller owns all
// fields and may read Body safely.
type HTTPGetResponse struct {
	Body       []byte
	Latency    time.Duration
	StatusCode int
	Header     http.Header
	FinalURL   string
}

// HTTPGetViaOutboundWithResponse executes an HTTP GET through the provided
// outbound and returns the full response snapshot. It is the metadata-rich
// counterpart of HTTPGetViaOutbound.
//
// The returned HTTPGetResponse owns its body bytes and a cloned header map.
// The caller may read Body, Latency, StatusCode, Header, and FinalURL without
// lifetime concerns.
//
// Timeout and cancellation are controlled solely by ctx.
func HTTPGetViaOutboundWithResponse(
	ctx context.Context,
	outbound adapter.Outbound,
	url string,
	opts OutboundHTTPOptions,
) (HTTPGetResponse, error) {
	if outbound == nil {
		return HTTPGetResponse{}, fmt.Errorf("outbound fetch: outbound is nil")
	}

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := outbound.DialContext(ctx, network, M.ParseSocksaddr(addr))
			if err != nil {
				return nil, err
			}
			if opts.OnConnLifecycle != nil {
				opts.OnConnLifecycle(ConnLifecycleOpen)
				return &connCloseHook{Conn: conn, onClose: func() { opts.OnConnLifecycle(ConnLifecycleClose) }}, nil
			}
			return conn, nil
		},
		DisableKeepAlives: true,
		ForceAttemptHTTP2: true,
	}

	client := &http.Client{
		Transport:     transport,
		CheckRedirect: opts.CheckRedirect,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return HTTPGetResponse{}, err
	}

	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = defaultOutboundUserAgent
	}
	req.Header.Set("User-Agent", userAgent)

	var start time.Time
	var latency time.Duration
	trace := &httptrace.ClientTrace{
		TLSHandshakeStart: func() { start = time.Now() },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err == nil {
				latency = time.Since(start)
			}
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(ctx, trace))

	resp, err := client.Do(req)
	if err != nil {
		return HTTPGetResponse{}, err
	}
	defer resp.Body.Close()

	if opts.RequireStatusOK && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return HTTPGetResponse{
			Body:       body,
			Latency:    latency,
			StatusCode: resp.StatusCode,
			Header:     resp.Header.Clone(),
			FinalURL:   resp.Request.URL.String(),
		}, fmt.Errorf("outbound fetch: unexpected status %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return HTTPGetResponse{
			Body:       nil,
			Latency:    latency,
			StatusCode: resp.StatusCode,
			Header:     resp.Header.Clone(),
			FinalURL:   resp.Request.URL.String(),
		}, err
	}

	return HTTPGetResponse{
		Body:       body,
		Latency:    latency,
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		FinalURL:   resp.Request.URL.String(),
	}, nil
}

// HTTPGetViaOutbound executes an HTTP GET through the provided outbound.
// Timeout and cancellation are controlled solely by ctx.
//
// Deprecated: Prefer HTTPGetViaOutboundWithResponse for new code that needs
// response metadata. This wrapper exists for legacy callers (egress/latency
// probes) that only need body, latency, and error.
func HTTPGetViaOutbound(
	ctx context.Context,
	outbound adapter.Outbound,
	url string,
	opts OutboundHTTPOptions,
) ([]byte, time.Duration, error) {
	resp, err := HTTPGetViaOutboundWithResponse(ctx, outbound, url, opts)
	if err != nil {
		return resp.Body, resp.Latency, err
	}
	return resp.Body, resp.Latency, nil
}

// connCloseHook wraps a net.Conn and calls onClose exactly once on Close.
type connCloseHook struct {
	net.Conn
	onClose   func()
	closeOnce sync.Once
	closeErr  error
}

func (c *connCloseHook) Close() error {
	c.closeOnce.Do(func() {
		if c.onClose != nil {
			c.onClose()
		}
		c.closeErr = c.Conn.Close()
	})
	return c.closeErr
}

// SafeRedirectPolicy returns an http.Client CheckRedirect function that
// validates each redirect URL against basic shape-safety rules:
// absolute HTTPS, non-empty host, no userinfo, no fragment, no localhost.
//
// This is used by quality check (ResponseFetcher) HTTP callers to close
// the redirect-safety gap custom-target redirects. Legacy egress/latency
// probes do not use this policy and retain default redirect behavior.
func SafeRedirectPolicy(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("redirect loop: too many redirects")
	}
	redirectURL := req.URL
	if redirectURL == nil {
		return fmt.Errorf("redirect with nil URL")
	}
	if !strings.EqualFold(redirectURL.Scheme, "https") {
		return fmt.Errorf("redirect to non-https scheme %q", redirectURL.Scheme)
	}
	if redirectURL.User != nil && redirectURL.User.String() != "" {
		return fmt.Errorf("redirect contains userinfo")
	}
	if redirectURL.Fragment != "" {
		return fmt.Errorf("redirect contains fragment")
	}
	host := strings.ToLower(redirectURL.Host)
	if host == "" {
		return fmt.Errorf("redirect to empty host")
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return fmt.Errorf("redirect to empty hostname")
	}
	if host == "localhost" || strings.HasPrefix(host, "localhost.") {
		return fmt.Errorf("redirect to localhost")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
			isDocumentationIP(ip) || isReservedIP(ip) {
			return fmt.Errorf("redirect to unsafe IP %q", redirectURL.Host)
		}
	}
	return nil
}
