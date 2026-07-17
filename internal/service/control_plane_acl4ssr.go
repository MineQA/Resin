package service

import (
	"context"
	"fmt"
	"strings"
)

// ACL4SSRPreviewRequest is the JSON request body for
// POST /api/v1/rule-profiles/acl4ssr/preview.
// Exactly one of INIContent or SourceID must be non-nil and non-empty.
type ACL4SSRPreviewRequest struct {
	INIContent *string `json:"ini_content,omitempty"`
	SourceID   *string `json:"source_id,omitempty"`
}

// ACL4SSRSourceAttribution holds source metadata returned in the preview response.
type ACL4SSRSourceAttribution struct {
	Name    string `json:"name"`
	URL     string `json:"url,omitempty"`
	License string `json:"license"`
}

// ACL4SSRPreviewResponse is the JSON response for the preview endpoint.
type ACL4SSRPreviewResponse struct {
	*ACL4SSRConversionResult
	Source      ACL4SSRSourceAttribution `json:"source"`
	Attribution string                   `json:"attribution"`
}

// ACL4SSRFetcher fetches an HTTPS URL and returns the response body and final
// URL after redirects. Production code should wire
// ControlPlaneService.ACL4SSRFetcher (e.g. with netutil.FetchSafeHTTPS).
// When nil, preview with source_id returns an internal error.
type ACL4SSRFetcher func(ctx context.Context, url string) ([]byte, string, error)

// acl4ssrKnownSources maps supported source IDs to their canonical download
// URLs. Only the exact IDs listed here are accepted.
var acl4ssrKnownSources = map[string]string{
	"acl4ssr-online-full": "https://raw.githubusercontent.com/ACL4SSR/ACL4SSR/refs/heads/master/Clash/config/ACL4SSR_Online_Full.ini",
}

// PreviewACL4SSRConversion converts ACL4SSR [custom] INI content (provided
// inline or fetched from a known source) into a preview Clash/Mihomo YAML
// template. The result is never persisted — this is a dry-run preview.
//
// Validation rules:
//   - Exactly one of INIContent or SourceID must be non-nil and non-empty.
//   - source_id must be one of the known sources in acl4ssrKnownSources.
//   - ini_content must be non-empty after trimming whitespace.
//   - Unknown JSON fields are rejected by the caller (DecodeBody).
func (s *ControlPlaneService) PreviewACL4SSRConversion(ctx context.Context, req ACL4SSRPreviewRequest) (*ACL4SSRPreviewResponse, *ServiceError) {
	// --- Validate: exactly one non-empty field ---
	hasINI := req.INIContent != nil
	hasSource := req.SourceID != nil
	if hasINI == hasSource {
		return nil, invalidArg("exactly one of 'ini_content' or 'source_id' must be provided")
	}

	var (
		iniContent  string
		source      ACL4SSRSourceAttribution
		attribution string
	)

	if hasSource {
		sourceID := strings.TrimSpace(*req.SourceID)
		if sourceID == "" {
			return nil, invalidArg("source_id: must not be empty")
		}
		url, ok := acl4ssrKnownSources[sourceID]
		if !ok {
			return nil, invalidArg(fmt.Sprintf("source_id: unknown source %q", sourceID))
		}

		// Fetch via injectable fetcher or configured default.
		fetcher := s.acl4ssrFetcher()
		data, _, err := fetcher(ctx, url)
		if err != nil {
			return nil, &ServiceError{
				Code:    "UNAVAILABLE",
				Message: "failed to fetch source content",
				Err:     err,
			}
		}
		iniContent = string(data)

		// Fixed source attribution — accurately names the upstream project
		// without claiming a pinned commit.
		source = ACL4SSRSourceAttribution{
			Name:    "ACL4SSR/ACL4SSR",
			URL:     url,
			License: "CC-BY-SA-4.0",
		}
		attribution = fmt.Sprintf(
			"Source: ACL4SSR/ACL4SSR (%s) — CC-BY-SA-4.0. "+
				"This output is a Resin conversion; the upstream content "+
				"is provided under its original license terms.",
			url,
		)
	} else {
		iniContent = *req.INIContent
		if strings.TrimSpace(iniContent) == "" {
			return nil, invalidArg("ini_content: must not be empty or whitespace-only")
		}

		// User-provided content gets a genuinely neutral notice: no false
		// provenance claim, no implied "All rights reserved", no specific
		// license attribution.  Resin does not determine ownership or
		// license terms; if derived from ACL4SSR, the upstream project
		// provides its content under CC-BY-SA-4.0.
		source = ACL4SSRSourceAttribution{
			Name:    "User-provided content",
			License: "Unknown / user-provided",
		}
		attribution = "User-provided content. Resin does not determine the " +
			"ownership or license of this content. The user should review " +
			"the original source terms. If this content is derived from " +
			"ACL4SSR, the upstream ACL4SSR content is provided under " +
			"CC-BY-SA-4.0."
	}

	// --- Convert via Phase 1 converter ---
	result, svcErr := ConvertACL4SSRCustomINI(iniContent)
	if svcErr != nil {
		return nil, svcErr
	}

	return &ACL4SSRPreviewResponse{
		ACL4SSRConversionResult: result,
		Source:                  source,
		Attribution:             attribution,
	}, nil
}

// acl4ssrFetcher returns the configured fetcher or a safe default that returns
// an error. Production code should wire ControlPlaneService.ACL4SSRFetcher
// (e.g. in cmd/resin/app_runtime.go with netutil.FetchSafeHTTPS).
func (s *ControlPlaneService) acl4ssrFetcher() ACL4SSRFetcher {
	if s.ACL4SSRFetcher != nil {
		return s.ACL4SSRFetcher
	}
	return func(_ context.Context, _ string) ([]byte, string, error) {
		return nil, "", fmt.Errorf("ACL4SSR fetcher not configured; wire ControlPlaneService.ACL4SSRFetcher")
	}
}
