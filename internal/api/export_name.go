package api

import (
	"strings"
)

// officiallyAssignedISO3166 contains the 249 officially assigned
// ISO 3166-1 alpha-2 country codes as lowercase keys.
// Source: https://www.iso.org/obp/ui/#search (ISO 3166 Maintenance Agency).
// Excluded: AA, EU, ZZ (exceptionally reserved / user-assigned).
var officiallyAssignedISO3166 map[string]bool

func init() {
	codes := []string{
		"ad", "ae", "af", "ag", "ai", "al", "am", "ao", "aq", "ar", "as", "at", "au", "aw", "ax", "az",
		"ba", "bb", "bd", "be", "bf", "bg", "bh", "bi", "bj", "bl", "bm", "bn", "bo", "bq", "br", "bs", "bt", "bv", "bw", "by", "bz",
		"ca", "cc", "cd", "cf", "cg", "ch", "ci", "ck", "cl", "cm", "cn", "co", "cr", "cu", "cv", "cw", "cx", "cy", "cz",
		"de", "dj", "dk", "dm", "do", "dz",
		"ec", "ee", "eg", "eh", "er", "es", "et",
		"fi", "fj", "fk", "fm", "fo", "fr",
		"ga", "gb", "gd", "ge", "gf", "gg", "gh", "gi", "gl", "gm", "gn", "gp", "gq", "gr", "gs", "gt", "gu", "gw", "gy",
		"hk", "hm", "hn", "hr", "ht", "hu",
		"id", "ie", "il", "im", "in", "io", "iq", "ir", "is", "it",
		"je", "jm", "jo", "jp",
		"ke", "kg", "kh", "ki", "km", "kn", "kp", "kr", "kw", "ky", "kz",
		"la", "lb", "lc", "li", "lk", "lr", "ls", "lt", "lu", "lv", "ly",
		"ma", "mc", "md", "me", "mf", "mg", "mh", "mk", "ml", "mm", "mn", "mo", "mp", "mq", "mr", "ms", "mt", "mu", "mv", "mw", "mx", "my", "mz",
		"na", "nc", "ne", "nf", "ng", "ni", "nl", "no", "np", "nr", "nu", "nz",
		"om",
		"pa", "pe", "pf", "pg", "ph", "pk", "pl", "pm", "pn", "pr", "ps", "pt", "pw", "py",
		"qa",
		"re", "ro", "rs", "ru", "rw",
		"sa", "sb", "sc", "sd", "se", "sg", "sh", "si", "sj", "sk", "sl", "sm", "sn", "so", "sr", "ss", "st", "sv", "sx", "sy", "sz",
		"tc", "td", "tf", "tg", "th", "tj", "tk", "tl", "tm", "tn", "to", "tr", "tt", "tv", "tw", "tz",
		"ua", "ug", "um", "us", "uy", "uz",
		"va", "vc", "ve", "vg", "vi", "vn", "vu",
		"wf", "ws",
		"ye", "yt",
		"za", "zm", "zw",
	}
	officiallyAssignedISO3166 = make(map[string]bool, len(codes))
	for _, code := range codes {
		officiallyAssignedISO3166[code] = true
	}
}

// isAssignedISO returns true if code is a lowercase officially assigned
// ISO 3166-1 alpha-2 code.
func isAssignedISO(code string) bool {
	return officiallyAssignedISO3166[code]
}

// parseLeafMarker attempts to extract a recognized country-code marker from the
// start of leaf. Returns the extracted lowercase code and the remainder text
// (everything after the marker and its separator). If no marker is found,
// returns ("", leaf).
func parseLeafMarker(leaf string) (code, remainder string) {
	if len(leaf) == 0 {
		return "", leaf
	}
	// Bracketed: [XX]...
	if leaf[0] == '[' && len(leaf) >= 4 {
		if isASCIIAlpha(leaf[1]) && isASCIIAlpha(leaf[2]) && leaf[3] == ']' {
			candidate := strings.ToLower(leaf[1:3])
			if isAssignedISO(candidate) {
				return candidate, leaf[4:]
			}
		}
	}
	// Bare: XX followed by -, _, or ASCII space.
	if len(leaf) >= 3 && isASCIIAlpha(leaf[0]) && isASCIIAlpha(leaf[1]) {
		sep := leaf[2]
		if sep == '-' || sep == '_' || sep == ' ' {
			candidate := strings.ToLower(leaf[:2])
			if isAssignedISO(candidate) {
				return candidate, leaf[3:]
			}
		}
	}
	return "", leaf
}

// isASCIIAlpha reports whether b is an ASCII letter.
func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// reconcileExportName reconciles the export name/tag with the detected egress
// region. If region is empty or not an officially assigned ISO 3166-1 alpha-2
// code, the original name is returned unchanged.
//
// The name is split at the last '/', preserving the entire prefix (including
// the '/') byte-for-byte. Only the leaf label after the last '/' is reconciled.
// Recognized markers (leading [XX] or bare XX followed by - _ or ASCII space)
// are canonicalized to "[XX] " when they match the region, or replaced with the
// correct canonical marker when they do not. Missing markers are prepended.
//
// If removing a recognized marker leaves an empty leaf, the complete original
// name is returned unchanged. Flags, full country names, and Chinese country
// names are treated as opaque ordinary text and receive the canonical prefix.
func reconcileExportName(name, region string) string {
	region = strings.ToLower(strings.TrimSpace(region))
	if !isAssignedISO(region) {
		return name
	}

	// Split at last '/', preserving the entire prefix including the '/'.
	var prefix, leaf string
	if slashIdx := strings.LastIndexByte(name, '/'); slashIdx >= 0 {
		prefix = name[:slashIdx+1]
		leaf = name[slashIdx+1:]
	} else {
		prefix = ""
		leaf = name
	}

	markerCode, remainder := parseLeafMarker(leaf)
	if markerCode != "" {
		// Removing a recognized marker that leaves an empty leaf returns the
		// original name unchanged.
		if remainder == "" {
			return name
		}
		// Trim exactly one leading space if present (canonical form includes
		// one space after the bracketed code).
		if len(remainder) > 0 && remainder[0] == ' ' {
			remainder = remainder[1:]
		}
	} else {
		remainder = leaf
	}

	return prefix + "[" + strings.ToUpper(region) + "] " + remainder
}
