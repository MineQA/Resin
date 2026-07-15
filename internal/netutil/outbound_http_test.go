package netutil

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Resinat/Resin/internal/testutil"
)

func TestHTTPGetViaOutbound_RequireStatusOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	ob, err := (&testutil.StubOutboundBuilder{}).Build(nil)
	if err != nil {
		t.Fatalf("build outbound: %v", err)
	}
	_, _, err = HTTPGetViaOutbound(context.Background(), ob, srv.URL, OutboundHTTPOptions{
		RequireStatusOK: true,
	})
	if err == nil {
		t.Fatal("expected non-200 status to return error")
	}
	if !strings.Contains(err.Error(), "unexpected status 404") {
		t.Fatalf("expected status error, got: %v", err)
	}
}

func TestHTTPGetViaOutbound_AllowNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("probe-body"))
	}))
	defer srv.Close()

	ob, err := (&testutil.StubOutboundBuilder{}).Build(nil)
	if err != nil {
		t.Fatalf("build outbound: %v", err)
	}
	body, _, err := HTTPGetViaOutbound(context.Background(), ob, srv.URL, OutboundHTTPOptions{
		RequireStatusOK: false,
	})
	if err != nil {
		t.Fatalf("expected non-200 response to pass through, got: %v", err)
	}
	if string(body) != "probe-body" {
		t.Fatalf("unexpected body %q", string(body))
	}
}

func TestHTTPGetViaOutboundWithResponse_StatusCodeAndHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("CF-Ray", "abc-123-def")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("challenge page"))
	}))
	defer srv.Close()

	ob, err := (&testutil.StubOutboundBuilder{}).Build(nil)
	if err != nil {
		t.Fatalf("build outbound: %v", err)
	}

	resp, err := HTTPGetViaOutboundWithResponse(context.Background(), ob, srv.URL, OutboundHTTPOptions{
		RequireStatusOK: false,
	})
	if err != nil {
		t.Fatalf("HTTPGetViaOutboundWithResponse: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("StatusCode = %d, want 503", resp.StatusCode)
	}
	if resp.Header.Get("Server") != "cloudflare" {
		t.Fatalf("Server header = %q, want 'cloudflare'", resp.Header.Get("Server"))
	}
	if resp.Header.Get("CF-Ray") != "abc-123-def" {
		t.Fatalf("CF-Ray header = %q, want 'abc-123-def'", resp.Header.Get("CF-Ray"))
	}
	if string(resp.Body) != "challenge page" {
		t.Fatalf("Body = %q, want 'challenge page'", string(resp.Body))
	}
	if resp.FinalURL == "" {
		t.Fatal("FinalURL should not be empty")
	}
}

func TestHTTPGetViaOutboundWithResponse_FinalURL(t *testing.T) {
	// Verify FinalURL captures any redirect target.
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		_, _ = w.Write([]byte("final"))
	}))
	defer redirectTarget.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer redirector.Close()

	ob, err := (&testutil.StubOutboundBuilder{}).Build(nil)
	if err != nil {
		t.Fatalf("build outbound: %v", err)
	}

	resp, err := HTTPGetViaOutboundWithResponse(context.Background(), ob, redirector.URL, OutboundHTTPOptions{
		RequireStatusOK: false,
	})
	if err != nil {
		t.Fatalf("HTTPGetViaOutboundWithResponse: %v", err)
	}
	if resp.FinalURL == "" {
		t.Fatal("FinalURL should not be empty")
	}
	if string(resp.Body) != "final" {
		t.Fatalf("Body = %q, want 'final' (should follow redirect)", string(resp.Body))
	}
}

func TestHTTPGetViaOutbound_PreservesRedirectBehavior(t *testing.T) {
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("final"))
	}))
	defer redirectTarget.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer redirector.Close()

	ob, err := (&testutil.StubOutboundBuilder{}).Build(nil)
	if err != nil {
		t.Fatalf("build outbound: %v", err)
	}

	body, _, err := HTTPGetViaOutbound(context.Background(), ob, redirector.URL, OutboundHTTPOptions{
		RequireStatusOK: false,
	})
	if err != nil {
		t.Fatalf("HTTPGetViaOutbound: %v", err)
	}
	if string(body) != "final" {
		t.Fatalf("Body = %q, want 'final'", string(body))
	}
}

func TestHTTPGetViaOutboundWithResponse_RequireStatusOKError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("not found"))
	}))
	defer srv.Close()

	ob, err := (&testutil.StubOutboundBuilder{}).Build(nil)
	if err != nil {
		t.Fatalf("build outbound: %v", err)
	}
	resp, err := HTTPGetViaOutboundWithResponse(context.Background(), ob, srv.URL, OutboundHTTPOptions{
		RequireStatusOK: true,
	})
	if err == nil {
		t.Fatal("expected non-200 status to return error")
	}
	// Even on error, the response should have body and status.
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("StatusCode = %d, want 404 even on error", resp.StatusCode)
	}
	if string(resp.Body) != "not found" {
		t.Fatalf("Body = %q, want 'not found'", string(resp.Body))
	}
}

func TestConnCloseHook_CloseIsIdempotentAndConcurrentSafe(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	var onCloseCount atomic.Int32
	hook := &connCloseHook{
		Conn: client,
		onClose: func() {
			onCloseCount.Add(1)
		},
	}

	const closers = 32
	var wg sync.WaitGroup
	wg.Add(closers)
	for i := 0; i < closers; i++ {
		go func() {
			defer wg.Done()
			_ = hook.Close()
		}()
	}
	wg.Wait()

	if got := onCloseCount.Load(); got != 1 {
		t.Fatalf("onClose called %d times, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// SafeRedirectPolicy tests
// ---------------------------------------------------------------------------

func TestSafeRedirectPolicy_RejectsNonHTTPS(t *testing.T) {
	err := SafeRedirectPolicy(&http.Request{URL: mustParseURL("http://example.com")}, nil)
	if err == nil {
		t.Fatal("expected error for non-https redirect")
	}
}

func TestSafeRedirectPolicy_RejectsUserinfo(t *testing.T) {
	err := SafeRedirectPolicy(&http.Request{URL: mustParseURL("https://user:pass@example.com")}, nil)
	if err == nil {
		t.Fatal("expected error for userinfo in redirect")
	}
}

func TestSafeRedirectPolicy_RejectsFragment(t *testing.T) {
	err := SafeRedirectPolicy(&http.Request{URL: mustParseURL("https://example.com/path#frag")}, nil)
	if err == nil {
		t.Fatal("expected error for fragment in redirect")
	}
}

func TestSafeRedirectPolicy_RejectsLocalhost(t *testing.T) {
	err := SafeRedirectPolicy(&http.Request{URL: mustParseURL("https://localhost:8080/path")}, nil)
	if err == nil {
		t.Fatal("expected error for localhost redirect")
	}
}

func TestSafeRedirectPolicy_RejectsEmptyHost(t *testing.T) {
	err := SafeRedirectPolicy(&http.Request{URL: mustParseURL("https:///path")}, nil)
	if err == nil {
		t.Fatal("expected error for empty host")
	}
}

func TestSafeRedirectPolicy_RejectsTooManyRedirects(t *testing.T) {
	via := make([]*http.Request, 10)
	err := SafeRedirectPolicy(&http.Request{URL: mustParseURL("https://example.com")}, via)
	if err == nil {
		t.Fatal("expected error for too many redirects")
	}
	if !strings.Contains(err.Error(), "redirect loop") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSafeRedirectPolicy_AcceptsHTTPS(t *testing.T) {
	err := SafeRedirectPolicy(&http.Request{URL: mustParseURL("https://example.com/path?q=1")}, nil)
	if err != nil {
		t.Fatalf("unexpected error for valid https redirect: %v", err)
	}
}

func TestSafeRedirectPolicy_RejectsPrivateIP(t *testing.T) {
	err := SafeRedirectPolicy(&http.Request{URL: mustParseURL("https://192.168.1.1/admin")}, nil)
	if err == nil {
		t.Fatal("expected error for private IP redirect")
	}
}

func TestSafeRedirectPolicy_RejectsLoopbackIP(t *testing.T) {
	err := SafeRedirectPolicy(&http.Request{URL: mustParseURL("https://127.0.0.1:8080")}, nil)
	if err == nil {
		t.Fatal("expected error for loopback IP redirect")
	}
}

func TestSafeRedirectPolicy_RejectsDocumentationIPv4(t *testing.T) {
	tests := []string{
		"https://192.0.2.1/path",
		"https://198.51.100.1/path",
		"https://203.0.113.1/path",
	}
	for _, u := range tests {
		err := SafeRedirectPolicy(&http.Request{URL: mustParseURL(u)}, nil)
		if err == nil {
			t.Fatalf("expected error for documentation IP redirect %q", u)
		}
	}
}

func TestSafeRedirectPolicy_RejectsDocumentationIPv6(t *testing.T) {
	err := SafeRedirectPolicy(&http.Request{URL: mustParseURL("https://[2001:db8::1]/path")}, nil)
	if err == nil {
		t.Fatal("expected error for documentation IPv6 redirect")
	}
}

func TestSafeRedirectPolicy_RejectsReservedIPv4(t *testing.T) {
	err := SafeRedirectPolicy(&http.Request{URL: mustParseURL("https://240.0.0.1/path")}, nil)
	if err == nil {
		t.Fatal("expected error for reserved IPv4 redirect")
	}
}

func TestSafeRedirectPolicy_RejectsReservedIPv6(t *testing.T) {
	tests := []string{
		"https://[100::1]/path",
		"https://[fec0::1]/path",
	}
	for _, u := range tests {
		err := SafeRedirectPolicy(&http.Request{URL: mustParseURL(u)}, nil)
		if err == nil {
			t.Fatalf("expected error for reserved IPv6 redirect %q", u)
		}
	}
}

func TestHTTPGetViaOutboundWithResponse_SafeRedirectBlockedViaPolicy(t *testing.T) {
	// Verify that SafeRedirectPolicy wired into HTTP client prevents redirect
	// to unsafe targets. Since TLS test servers require client cert trust and
	// the stub outbound doesn't configure InsecureSkipVerify, we test the
	// CheckRedirect wiring by intercepting the redirect policy directly.
	// The SafeRedirectPolicy itself is thoroughly unit-tested above.
	// This functional test proves the CheckRedirect field is plumbed through.
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://localhost:9999/evil", http.StatusFound)
	}))
	defer redirector.Close()

	ob, err := (&testutil.StubOutboundBuilder{}).Build(nil)
	if err != nil {
		t.Fatalf("build outbound: %v", err)
	}

	// The CheckRedirect callback blocks before the TLS connection, so the
	// HTTP origin server is fine — the error comes from URL-shape validation.
	_, err = HTTPGetViaOutboundWithResponse(context.Background(), ob, redirector.URL, OutboundHTTPOptions{
		RequireStatusOK: false,
		CheckRedirect:   SafeRedirectPolicy,
	})
	if err == nil {
		t.Fatal("expected error for blocked redirect to localhost")
	}
	if !strings.Contains(err.Error(), "localhost") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// mustParseURL parses a URL string or panics.
func mustParseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
