// Package cloudflare defines canonical Cloudflare status tokens and helpers
// used across API, service, platform, and probe layers. This package has zero
// internal dependencies, making it safe for all packages to import without
// creating cycles.
package cloudflare

import "fmt"

// AllCanonicalStatuses is the complete set of accepted canonical Cloudflare
// status tokens in deterministic display order. "unchecked" is last.
// This slice is used for persisted array ordering and response stability.
var AllCanonicalStatuses = []string{
	"js_challenge",
	"captcha_challenge",
	"block",
	"challenge",
	"ng",
	"clean",
	"not_detected",
	StatusUnchecked,
}

// StatusUnchecked is the display form for empty/legacy cloudflare status.
const StatusUnchecked = "unchecked"

// IsValidStatus returns true for any recognized canonical Cloudflare status
// token, including the empty persisted form and explicit "unchecked" token.
func IsValidStatus(s string) bool {
	switch s {
	case "clean", "not_detected",
		"js_challenge", "captcha_challenge", "block",
		"challenge", "ng", StatusUnchecked, "":
		return true
	default:
		return false
	}
}

// IsValidExplicitStatus returns true for a non-empty recognized canonical
// Cloudflare status token. Used for API query and platform filter validation.
// Empty means "no filter" and must not be explicit, while "unchecked" is the
// public filter token for a legacy empty persisted status.
func IsValidExplicitStatus(s string) bool {
	if s == "" {
		return false
	}
	return IsValidStatus(s)
}

// NormalizeSet normalizes each token to its canonical string form,
// deduplicates, and produces output in AllCanonicalStatuses order. Empty
// strings are rejected. Returns an error if any token is not recognized.
func NormalizeSet(tokens []string) ([]string, error) {
	seen := make(map[string]struct{}, len(tokens))
	for i, t := range tokens {
		if t == "" {
			return nil, fmt.Errorf("quality_cloudflare_status[%d]: empty token not allowed", i)
		}
		if !IsValidExplicitStatus(t) {
			return nil, fmt.Errorf("quality_cloudflare_status[%d]: unknown status %q", i, t)
		}
		seen[t] = struct{}{}
	}
	// Output in canonical order.
	out := make([]string, 0, len(seen))
	for _, c := range AllCanonicalStatuses {
		if _, ok := seen[c]; ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// NormalizeForDisplay returns the display form of a Cloudflare status.
// Empty/legacy statuses are normalized to "unchecked".
func NormalizeForDisplay(s string) string {
	if s == "" {
		return StatusUnchecked
	}
	return s
}
