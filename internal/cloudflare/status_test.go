package cloudflare

import (
	"testing"
)

func TestIsValidStatus(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"clean", true},
		{"not_detected", true},
		{"js_challenge", true},
		{"captcha_challenge", true},
		{"block", true},
		{"challenge", true},
		{"ng", true},
		{"", true}, // empty = unchecked/legacy persisted form
		{"unchecked", true},
		{"unknown", false},
	}
	for _, tt := range tests {
		got := IsValidStatus(tt.s)
		if got != tt.want {
			t.Errorf("IsValidStatus(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestIsValidExplicitStatus(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"clean", true},
		{"not_detected", true},
		{"js_challenge", true},
		{"captcha_challenge", true},
		{"block", true},
		{"challenge", true},
		{"ng", true},
		{"unchecked", true},
		{"", false}, // empty not allowed as explicit
		{"unknown", false},
	}
	for _, tt := range tests {
		got := IsValidExplicitStatus(tt.s)
		if got != tt.want {
			t.Errorf("IsValidExplicitStatus(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestNormalizeSet(t *testing.T) {
	// Normal flow: deduplicates, orders canonically, rejects unknowns.
	t.Run("valid set orders canonically", func(t *testing.T) {
		out, err := NormalizeSet([]string{"ng", "clean", "block", "ng"})
		if err != nil {
			t.Fatalf("NormalizeSet: %v", err)
		}
		want := []string{"block", "ng", "clean"}
		if !equalSlice(out, want) {
			t.Fatalf("got %v, want %v", out, want)
		}
	})

	t.Run("orders correctly for js_challenge and captcha_challenge", func(t *testing.T) {
		out, err := NormalizeSet([]string{"clean", "captcha_challenge", "js_challenge", "challenge"})
		if err != nil {
			t.Fatalf("NormalizeSet: %v", err)
		}
		want := []string{"js_challenge", "captcha_challenge", "challenge", "clean"}
		if !equalSlice(out, want) {
			t.Fatalf("got %v, want %v", out, want)
		}
	})

	t.Run("rejects unknown token", func(t *testing.T) {
		_, err := NormalizeSet([]string{"clean", "bogus"})
		if err == nil {
			t.Fatal("expected error for unknown status")
		}
	})

	t.Run("rejects empty explicit token", func(t *testing.T) {
		_, err := NormalizeSet([]string{""})
		if err == nil {
			t.Fatal("expected error for empty explicit token")
		}
	})

	t.Run("nil input returns empty", func(t *testing.T) {
		out, err := NormalizeSet(nil)
		if err != nil {
			t.Fatalf("NormalizeSet(nil): %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected empty, got %v", out)
		}
	})

	t.Run("empty input returns empty", func(t *testing.T) {
		out, err := NormalizeSet([]string{})
		if err != nil {
			t.Fatalf("NormalizeSet([]): %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected empty, got %v", out)
		}
	})

	t.Run("deduplicates", func(t *testing.T) {
		out, err := NormalizeSet([]string{"clean", "clean", "not_detected", "clean"})
		if err != nil {
			t.Fatalf("NormalizeSet: %v", err)
		}
		want := []string{"clean", "not_detected"}
		if !equalSlice(out, want) {
			t.Fatalf("got %v, want %v", out, want)
		}
	})

	t.Run("accepts explicit unchecked", func(t *testing.T) {
		out, err := NormalizeSet([]string{"unchecked", "clean"})
		if err != nil {
			t.Fatalf("NormalizeSet: %v", err)
		}
		want := []string{"clean", "unchecked"}
		if !equalSlice(out, want) {
			t.Fatalf("got %v, want %v", out, want)
		}
	})
}

func TestNormalizeForDisplay(t *testing.T) {
	tests := []struct {
		s    string
		want string
	}{
		{"clean", "clean"},
		{"not_detected", "not_detected"},
		{"js_challenge", "js_challenge"},
		{"block", "block"},
		{"", "unchecked"},
	}
	for _, tt := range tests {
		got := NormalizeForDisplay(tt.s)
		if got != tt.want {
			t.Errorf("NormalizeForDisplay(%q) = %q, want %q", tt.s, got, tt.want)
		}
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
