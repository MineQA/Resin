package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// minimalValidINI is a well-formed ACL4SSR [custom] section for testing.
const minimalValidINI = `[custom]
custom_proxy_group=Test` + "`select`" + `.*
ruleset=Test,https://example.com/r.list
ruleset=Final,[]FINAL
`

// mockACL4SSRFetcher returns canned data. It is an ACL4SSRFetcher.
func mockACL4SSRFetcher(data string, err error) ACL4SSRFetcher {
	return func(_ context.Context, _ string) ([]byte, string, error) {
		if err != nil {
			return nil, "", err
		}
		return []byte(data), "", nil
	}
}

func TestPreviewACL4SSRConversion_INIContentSuccess(t *testing.T) {
	cp := &ControlPlaneService{}
	resp, svcErr := cp.PreviewACL4SSRConversion(context.Background(), ACL4SSRPreviewRequest{
		INIContent: strPtr(minimalValidINI),
	})
	if svcErr != nil {
		t.Fatalf("unexpected error: %v", svcErr)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// Core conversion fields.
	if resp.TemplateYAML == "" {
		t.Fatal("expected non-empty template_yaml")
	}
	if resp.GroupCount != 1 {
		t.Errorf("expected 1 group, got %d", resp.GroupCount)
	}
	if resp.ProviderCount != 1 {
		t.Errorf("expected 1 provider, got %d", resp.ProviderCount)
	}
	if resp.RuleCount != 2 {
		t.Errorf("expected 2 rules, got %d", resp.RuleCount)
	}

	// Source attribution for user-provided content — genuinely neutral,
	// no false provenance claim.
	if resp.Source.Name != "User-provided content" {
		t.Errorf("source.name = %q, want %q", resp.Source.Name, "User-provided content")
	}
	if resp.Source.URL != "" {
		t.Errorf("source.url should be empty for user content, got %q", resp.Source.URL)
	}
	if resp.Source.License != "Unknown / user-provided" {
		t.Errorf("source.license = %q, want %q", resp.Source.License, "Unknown / user-provided")
	}
	if resp.Attribution == "" {
		t.Fatal("expected non-empty attribution")
	}
	// Must not falsely claim ACL4SSR provenance or CC-BY-SA-4.0 for generic input.
	if strings.Contains(resp.Attribution, "ACL4SSR/ACL4SSR") {
		t.Error("attribution must not claim ACL4SSR/ACL4SSR provenance for user-provided content")
	}
	// The generic attribution should still mention ACL4SSR in the conditional
	// ("if derived from ACL4SSR") but that's acceptable — it is a neutral
	// advisory, not a provenance claim.  We just check we don't *assert* it.
	if !strings.Contains(resp.Attribution, "User-provided content") {
		t.Error("attribution should start with 'User-provided content'")
	}
}

func TestPreviewACL4SSRConversion_SourceSuccess(t *testing.T) {
	cp := &ControlPlaneService{
		ACL4SSRFetcher: mockACL4SSRFetcher(minimalValidINI, nil),
	}
	resp, svcErr := cp.PreviewACL4SSRConversion(context.Background(), ACL4SSRPreviewRequest{
		SourceID: strPtr("acl4ssr-online-full"),
	})
	if svcErr != nil {
		t.Fatalf("unexpected error: %v", svcErr)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// Core conversion fields.
	if resp.TemplateYAML == "" {
		t.Fatal("expected non-empty template_yaml")
	}
	if resp.GroupCount != 1 {
		t.Errorf("expected 1 group, got %d", resp.GroupCount)
	}

	// Source attribution for ACL4SSR/ACL4SSR.
	if resp.Source.Name != "ACL4SSR/ACL4SSR" {
		t.Errorf("source.name = %q, want %q", resp.Source.Name, "ACL4SSR/ACL4SSR")
	}
	if resp.Source.URL != acl4ssrKnownSources["acl4ssr-online-full"] {
		t.Errorf("source.url = %q, want %q", resp.Source.URL, acl4ssrKnownSources["acl4ssr-online-full"])
	}
	if resp.Source.License != "CC-BY-SA-4.0" {
		t.Errorf("source.license = %q, want %q", resp.Source.License, "CC-BY-SA-4.0")
	}
	if resp.Attribution == "" {
		t.Fatal("expected non-empty attribution")
	}
	if !strings.Contains(resp.Attribution, "CC-BY-SA-4.0") {
		t.Error("attribution should mention CC-BY-SA-4.0")
	}
}

func TestPreviewACL4SSRConversion_INIContentWhitespaceOnly(t *testing.T) {
	cp := &ControlPlaneService{}
	_, svcErr := cp.PreviewACL4SSRConversion(context.Background(), ACL4SSRPreviewRequest{
		INIContent: strPtr("   \n\t  "),
	})
	if svcErr == nil {
		t.Fatal("expected error for whitespace-only content")
	}
	if !strings.Contains(svcErr.Message, "must not be empty") {
		t.Errorf("unexpected error message: %v", svcErr)
	}
}

func TestPreviewACL4SSRConversion_BothFieldsProvided(t *testing.T) {
	cp := &ControlPlaneService{}
	_, svcErr := cp.PreviewACL4SSRConversion(context.Background(), ACL4SSRPreviewRequest{
		INIContent: strPtr(minimalValidINI),
		SourceID:   strPtr("acl4ssr-online-full"),
	})
	if svcErr == nil {
		t.Fatal("expected error for both fields")
	}
	if !strings.Contains(svcErr.Message, "exactly one") {
		t.Errorf("unexpected message: %v", svcErr)
	}
}

func TestPreviewACL4SSRConversion_NeitherField(t *testing.T) {
	cp := &ControlPlaneService{}
	_, svcErr := cp.PreviewACL4SSRConversion(context.Background(), ACL4SSRPreviewRequest{})
	if svcErr == nil {
		t.Fatal("expected error for neither field")
	}
	if !strings.Contains(svcErr.Message, "exactly one") {
		t.Errorf("unexpected message: %v", svcErr)
	}
}

func TestPreviewACL4SSRConversion_UnknownSourceID(t *testing.T) {
	cp := &ControlPlaneService{}
	_, svcErr := cp.PreviewACL4SSRConversion(context.Background(), ACL4SSRPreviewRequest{
		SourceID: strPtr("nonexistent-source"),
	})
	if svcErr == nil {
		t.Fatal("expected error for unknown source")
	}
	if !strings.Contains(svcErr.Message, "unknown source") {
		t.Errorf("unexpected message: %v", svcErr)
	}
}

func TestPreviewACL4SSRConversion_EmptySourceID(t *testing.T) {
	cp := &ControlPlaneService{}
	_, svcErr := cp.PreviewACL4SSRConversion(context.Background(), ACL4SSRPreviewRequest{
		SourceID: strPtr(""),
	})
	if svcErr == nil {
		t.Fatal("expected error for empty source_id")
	}
}

func TestPreviewACL4SSRConversion_FetcherNotConfigured(t *testing.T) {
	// ACL4SSRFetcher is nil; source_id should fail.
	cp := &ControlPlaneService{}
	_, svcErr := cp.PreviewACL4SSRConversion(context.Background(), ACL4SSRPreviewRequest{
		SourceID: strPtr("acl4ssr-online-full"),
	})
	if svcErr == nil {
		t.Fatal("expected error when fetcher is not configured")
	}
	if svcErr.Code != "UNAVAILABLE" {
		t.Errorf("expected UNAVAILABLE code, got %q", svcErr.Code)
	}
}

func TestPreviewACL4SSRConversion_FetchFailure(t *testing.T) {
	cp := &ControlPlaneService{
		ACL4SSRFetcher: mockACL4SSRFetcher("", errors.New("connection refused")),
	}
	_, svcErr := cp.PreviewACL4SSRConversion(context.Background(), ACL4SSRPreviewRequest{
		SourceID: strPtr("acl4ssr-online-full"),
	})
	if svcErr == nil {
		t.Fatal("expected error on fetch failure")
	}
	if svcErr.Code != "UNAVAILABLE" {
		t.Errorf("expected UNAVAILABLE code, got %q", svcErr.Code)
	}
}

func TestPreviewACL4SSRConversion_ConverterError(t *testing.T) {
	cp := &ControlPlaneService{}
	_, svcErr := cp.PreviewACL4SSRConversion(context.Background(), ACL4SSRPreviewRequest{
		INIContent: strPtr("[custom]\n"),
	})
	if svcErr == nil {
		t.Fatal("expected converter error for incomplete input")
	}
	if svcErr.Code != "INVALID_ARGUMENT" {
		t.Errorf("expected INVALID_ARGUMENT, got %q", svcErr.Code)
	}
}

func TestPreviewACL4SSRConversion_DoesNotPersist(t *testing.T) {
	// Verify no state change by creating a service with a real engine
	// and checking that list remains empty after preview.
	engine := newTestEngine(t)
	cp := &ControlPlaneService{Engine: engine}

	resp, svcErr := cp.PreviewACL4SSRConversion(context.Background(), ACL4SSRPreviewRequest{
		INIContent: strPtr(minimalValidINI),
	})
	if svcErr != nil {
		t.Fatalf("unexpected error: %v", svcErr)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	// List rule profiles — must still be empty.
	profiles, err := cp.ListRuleProfiles(nil)
	if err != nil {
		t.Fatalf("ListRuleProfiles: %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("expected 0 persisted profiles after preview, got %d", len(profiles))
	}
}

func TestPreviewACL4SSRConversion_SourceExactAllowlistedURL(t *testing.T) {
	// Verify the allowlisted URL is exactly the expected one.
	expectedURL := "https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/refs/heads/master/Clash/config/ACL4SSR_Online_Full.ini"
	gotURL := acl4ssrKnownSources["acl4ssr-online-full"]
	if gotURL != expectedURL {
		t.Errorf("allowlisted URL mismatch:\ngot:  %q\nwant: %q", gotURL, expectedURL)
	}
}
