package probe

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Resinat/Resin/internal/node"
)

func TestDirectFetcher_HTTPFallbackLatencyNonZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	fetcher := DirectFetcher(func() time.Duration { return time.Second })
	body, latency, err := fetcher(node.Zero, srv.URL)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if string(body) != "ok" {
		t.Fatalf("body: got %q, want %q", string(body), "ok")
	}
	if latency <= 0 {
		t.Fatalf("latency should be > 0, got %v", latency)
	}
}

func TestDirectResponseFetcher_NonZeroLatency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Millisecond)
		_, _ = w.Write([]byte("response"))
	}))
	defer srv.Close()

	fetcher := DirectResponseFetcher(func() time.Duration { return time.Second })
	resp, err := fetcher(node.Zero, srv.URL)
	if err != nil {
		t.Fatalf("DirectResponseFetcher failed: %v", err)
	}
	if string(resp.Body) != "response" {
		t.Fatalf("body: got %q, want %q", string(resp.Body), "response")
	}
	if resp.Latency <= 0 {
		t.Fatalf("latency should be > 0, got %v", resp.Latency)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status code: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if resp.Header == nil {
		t.Fatal("header should not be nil")
	}
	if resp.FinalURL == "" {
		t.Fatal("final URL should not be empty")
	}
}

func TestDirectResponseFetcher_StatusAndHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Server", "cloudflare")
		w.Header().Set("CF-Ray", "abc123")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("challenge"))
	}))
	defer srv.Close()

	fetcher := DirectResponseFetcher(func() time.Duration { return time.Second })
	resp, err := fetcher(node.Zero, srv.URL)
	if err != nil {
		t.Fatalf("DirectResponseFetcher failed: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status code: got %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
	if resp.Header.Get("Server") != "cloudflare" {
		t.Fatalf("Server header: got %q, want %q", resp.Header.Get("Server"), "cloudflare")
	}
	if resp.Header.Get("CF-Ray") != "abc123" {
		t.Fatalf("CF-Ray header: got %q, want %q", resp.Header.Get("CF-Ray"), "abc123")
	}
	if string(resp.Body) != "challenge" {
		t.Fatalf("body: got %q, want %q", string(resp.Body), "challenge")
	}
}

func TestDirectResponseFetcher_TimeoutError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte("too late"))
	}))
	defer srv.Close()

	fetcher := DirectResponseFetcher(func() time.Duration { return 10 * time.Millisecond })
	_, err := fetcher(node.Zero, srv.URL)
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestDirectResponseFetcher_InvalidTimeout(t *testing.T) {
	fetcher := DirectResponseFetcher(func() time.Duration { return 0 })
	_, err := fetcher(node.Zero, "http://example.com")
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
}

func TestLegacyResponseFetcher_WrapsBodyAndLatency(t *testing.T) {
	plain := Fetcher(func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		return []byte("hello"), 42 * time.Millisecond, nil
	})

	adapter := LegacyResponseFetcher(plain)
	resp, err := adapter(node.Zero, "http://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(resp.Body) != "hello" {
		t.Fatalf("body: got %q, want %q", string(resp.Body), "hello")
	}
	if resp.Latency != 42*time.Millisecond {
		t.Fatalf("latency: got %v, want %v", resp.Latency, 42*time.Millisecond)
	}
	// Metadata-poor fields.
	if resp.StatusCode != 0 {
		t.Fatalf("expected StatusCode=0 (metadata-poor), got %d", resp.StatusCode)
	}
	if resp.Header != nil {
		t.Fatal("expected Header=nil (metadata-poor)")
	}
	if resp.FinalURL != "" {
		t.Fatalf("expected empty FinalURL (metadata-poor), got %q", resp.FinalURL)
	}
}

func TestLegacyResponseFetcher_PropagatesError(t *testing.T) {
	plain := Fetcher(func(_ node.Hash, url string) ([]byte, time.Duration, error) {
		return nil, 0, fmt.Errorf("network error")
	})

	adapter := LegacyResponseFetcher(plain)
	_, err := adapter(node.Zero, "http://example.com")
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}
