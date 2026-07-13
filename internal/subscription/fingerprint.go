package subscription

import (
	"encoding/hex"
	"strings"
)

// ClashFingerprintPolicy controls handling of Clash `fingerprint`
// (Mihomo full-certificate SHA-256 pin) values during subscription parsing.
type ClashFingerprintPolicy int

const (
	// ClashFingerprintReject rejects any node that carries a Clash certificate
	// fingerprint. This is the default and safest policy.
	ClashFingerprintReject ClashFingerprintPolicy = iota
	// ClashFingerprintDropSafe omits the fingerprint when skip-cert-verify is
	// not true (standard CA/hostname verification still applies; self-signed
	// may fail). If skip-cert-verify resolves true the node is rejected as
	// unsafe.
	ClashFingerprintDropSafe
	// ClashFingerprintDropAlways omits the fingerprint unconditionally and
	// accepts the node. A warning is emitted; when skip-cert-verify is true
	// the warning is elevated to explicitly flag the MITM risk.
	ClashFingerprintDropAlways
)

// String returns a human-readable policy name.
func (p ClashFingerprintPolicy) String() string {
	switch p {
	case ClashFingerprintReject:
		return "reject"
	case ClashFingerprintDropSafe:
		return "drop_safe"
	case ClashFingerprintDropAlways:
		return "drop_always"
	default:
		return "reject"
	}
}

// ParseClashFingerprintPolicy parses a policy string. Returns
// ClashFingerprintReject for unknown or empty values.
func ParseClashFingerprintPolicy(s string) ClashFingerprintPolicy {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "drop_safe":
		return ClashFingerprintDropSafe
	case "drop_always":
		return ClashFingerprintDropAlways
	default:
		return ClashFingerprintReject
	}
}

// Diagnostic codes for Clash certificate fingerprint handling and related
// unsupported features. These are stable identifiers; consumers must not
// depend on the message text.
const (
	// Clash fingerprint value is a known browser TLS fingerprint profile
	// name (chrome, firefox, safari, ios, android, edge, 360, qq, random,
	// randomized) rather than a hex SHA-256 certificate fingerprint.
	ClashFingerprintBrowserName = "CLASH_FINGERPRINT_BROWSER_NAME"

	// Clash fingerprint value is not a valid hex-encoded SHA-256 digest
	// (not a browser name, but fails to decode to exactly 32 bytes).
	ClashFingerprintInvalid = "CLASH_FINGERPRINT_INVALID"

	// Node rejected because it contains a valid Clash certificate fingerprint
	// and the active policy is reject (default).
	ClashCertFingerprintUnsupported = "CLASH_CERTIFICATE_FINGERPRINT_UNSUPPORTED"

	// Node rejected under drop_safe policy because skip-cert-verify is true,
	// making it unsafe to drop the certificate pin.
	ClashFingerprintUnsafeDrop = "CLASH_FINGERPRINT_UNSAFE_DROP"

	// Node accepted under drop_safe policy; the Clash certificate fingerprint
	// was successfully omitted. Standard CA/hostname verification still
	// applies; self-signed nodes may fail.
	ClashFingerprintDropSafeWarning = "CLASH_FINGERPRINT_DROP_SAFE"

	// Node accepted under drop_always policy; the Clash certificate fingerprint
	// was omitted. Standard CA/hostname verification still applies.
	ClashFingerprintDropAlwaysWarning = "CLASH_FINGERPRINT_DROP_ALWAYS"

	// Node accepted under drop_always policy but skip-cert-verify is true,
	// meaning no certificate verification will take place (MITM risk).
	ClashFingerprintDropAlwaysUnsafe = "CLASH_FINGERPRINT_DROP_ALWAYS_UNSAFE"

	// HY2 URI contains pinSHA256 which is not supported by the current
	// sing-box version (v1.12.21).
	HY2PinSHA256Unsupported = "HY2_PIN_SHA256_UNSUPPORTED"
)

// knownClashFingerprintBrowserNames are Clash TLS fingerprint profile names
// that are NOT valid hex certificate fingerprints. When a user writes
// `fingerprint: chrome` in Clash they almost certainly mean uTLS, but Mihomo
// maps it differently; we reject it here so callers do not conflate it.
// Keep sync'd with the names list in the test helper.
var knownClashFingerprintBrowserNames = map[string]struct{}{
	"chrome":     {},
	"firefox":    {},
	"safari":     {},
	"ios":        {},
	"android":    {},
	"edge":       {},
	"360":        {},
	"qq":         {},
	"random":     {},
	"randomized": {},
}

// validateClashFingerprint validates a Clash `fingerprint` value.
//
// It performs the following steps in order:
//  1. TrimSpace.
//  2. Reject known browser profile names → CLASH_FINGERPRINT_BROWSER_NAME.
//  3. Remove colons only.
//  4. Hex decode → CLASH_FINGERPRINT_INVALID on failure.
//  5. Require exactly 32 bytes → CLASH_FINGERPRINT_INVALID.
//
// Returns the decoded bytes and an empty diagnostic on success, or nil and a
// non-empty diagnostic on failure. The raw fingerprint value is never included
// in any returned diagnostic message.
func validateClashFingerprint(raw string) ([]byte, string) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, ""
	}

	lower := strings.ToLower(value)
	if _, ok := knownClashFingerprintBrowserNames[lower]; ok {
		return nil, ClashFingerprintBrowserName
	}

	cleaned := strings.ReplaceAll(value, ":", "")
	decoded, err := hex.DecodeString(cleaned)
	if err != nil {
		return nil, ClashFingerprintInvalid
	}
	if len(decoded) != 32 {
		return nil, ClashFingerprintInvalid
	}
	return decoded, ""
}

// applyClashFingerprintPolicy checks a Clash proxy for a non-empty `fingerprint`
// (cert SHA-256 pin), validates it, and applies the configured policy.
//
// This is the Clash‑only entry point called at the proxy boundary
// (parseClashProxies) so that Surge/QX and URI paths are never affected.
//
// Returns false when the node must be rejected. Diagnostics are recorded
// on ctx when non‑nil. All policies are fail‑closed: a node with an
// unrecognised or rejected fingerprint is never accepted.
func applyClashFingerprintPolicy(proxy map[string]any, ctx *parseCtx) bool {
	clashFP := strings.TrimSpace(getString(proxy, "fingerprint"))
	if clashFP == "" {
		return true
	}

	_, diagCode := validateClashFingerprint(clashFP)
	if diagCode != "" {
		// Browser name or malformed — always reject.
		if ctx != nil {
			switch diagCode {
			case ClashFingerprintBrowserName:
				ctx.rejectNode(getProxyTag(proxy), ClashFingerprintBrowserName,
					"Clash fingerprint is a browser TLS profile name, not a certificate SHA-256 pin; use client-fingerprint instead")
			default:
				ctx.rejectNode(getProxyTag(proxy), ClashFingerprintInvalid,
					"Clash fingerprint is not a valid hex-encoded SHA-256 certificate fingerprint")
			}
		}
		return false
	}

	// Valid cert SHA-256 fingerprint — apply policy.
	policy := ClashFingerprintReject
	if ctx != nil {
		policy = ctx.policy
	}
	skipVerify, _ := getBool(proxy, "skip-cert-verify", "insecure", "allowInsecure")

	switch policy {
	case ClashFingerprintReject:
		if ctx != nil {
			ctx.rejectNode(getProxyTag(proxy), ClashCertFingerprintUnsupported,
				"Node contains a Clash certificate fingerprint which is not supported by this version")
		}
		return false

	case ClashFingerprintDropSafe:
		if skipVerify {
			if ctx != nil {
				ctx.rejectNode(getProxyTag(proxy), ClashFingerprintUnsafeDrop,
					"Cannot safely drop Clash certificate fingerprint: skip-cert-verify is enabled")
			}
			return false
		}
		if ctx != nil {
			ctx.warnNode(getProxyTag(proxy), ClashFingerprintDropSafeWarning,
				"Clash certificate fingerprint omitted; standard CA/hostname verification still applies, self-signed nodes may fail")
		}

	case ClashFingerprintDropAlways:
		if skipVerify {
			if ctx != nil {
				ctx.warnNode(getProxyTag(proxy), ClashFingerprintDropAlwaysUnsafe,
					"Clash certificate fingerprint omitted with skip-cert-verify=true: no server authentication (MITM risk)")
			}
		} else {
			if ctx != nil {
				ctx.warnNode(getProxyTag(proxy), ClashFingerprintDropAlwaysWarning,
					"Clash certificate fingerprint omitted; standard CA/hostname verification still applies")
			}
		}
	}

	return true
}

// ParseOptions controls detailed subscription parsing behavior.
// The zero value provides safe defaults (fingerprint policy = reject).
type ParseOptions struct {
	// ClashFingerprintPolicy controls how Clash `fingerprint` (cert SHA-256
	// pin) values are handled. Default is ClashFingerprintReject.
	ClashFingerprintPolicy ClashFingerprintPolicy
}

// ParseDetailResult is the complete result of a detailed parse operation.
type ParseDetailResult struct {
	// Nodes contains successfully parsed and accepted outbound nodes.
	Nodes []ParsedNode
	// Rejected contains nodes that were rejected during parsing.
	Rejected []RejectedNode
	// Warnings contains non-fatal conditions on accepted nodes.
	Warnings []ParseWarning
}

// RejectedNode describes a node that was rejected during subscription parsing.
type RejectedNode struct {
	// Code is a stable diagnostic code (one of the CLASH_* / HY2_* constants).
	Code string
	// Message is a human-readable explanation safe for display.
	Message string
	// Tag is the node's original tag/name, if available.
	Tag string
}

// ParseWarning describes a non-fatal condition on an accepted node.
type ParseWarning struct {
	// Code is a stable diagnostic code.
	Code string
	// Message is a human-readable explanation safe for display.
	Message string
	// Tag is the affected node tag/name.
	Tag string
}

// parseCtx carries per-parse options and accumulates diagnostics.
type parseCtx struct {
	policy   ClashFingerprintPolicy
	rejected []RejectedNode
	warnings []ParseWarning
}

func newParseCtx(opts *ParseOptions) *parseCtx {
	if opts == nil {
		return &parseCtx{policy: ClashFingerprintReject}
	}
	return &parseCtx{policy: opts.ClashFingerprintPolicy}
}

func (ctx *parseCtx) rejectNode(tag, code, msg string) {
	if ctx == nil {
		return
	}
	ctx.rejected = append(ctx.rejected, RejectedNode{Code: code, Message: msg, Tag: tag})
}

func (ctx *parseCtx) warnNode(tag, code, msg string) {
	if ctx == nil {
		return
	}
	ctx.warnings = append(ctx.warnings, ParseWarning{Code: code, Message: msg, Tag: tag})
}

func (ctx *parseCtx) collect() *ParseDetailResult {
	if ctx == nil {
		return &ParseDetailResult{}
	}
	return &ParseDetailResult{
		Nodes:    nil, // filled by caller
		Rejected: ctx.rejected,
		Warnings: ctx.warnings,
	}
}

// clashProcessFingerprint applies `client-fingerprint`/`client_fingerprint`
// from a Clash proxy as uTLS on the tls map.
//
// This is called only from protocol‑specific Clash paths that already support
// the client‑fingerprint → utls mapping (VMess, VLESS, Hysteria, Hysteria2).
// The cert‑fingerprint (`fingerprint`) policy is already handled at the Clash
// proxy boundary by applyClashFingerprintPolicy, so this function never
// inspects or validates cert‑pin values.
func clashProcessFingerprint(tls map[string]any, proxy map[string]any, _ *parseCtx) bool {
	clientFP := strings.TrimSpace(firstNonEmpty(
		getString(proxy, "client-fingerprint"),
		getString(proxy, "client_fingerprint"),
	))
	if clientFP != "" {
		tls["utls"] = map[string]any{
			"enabled":     true,
			"fingerprint": clientFP,
		}
	}
	return true
}

// getProxyTag extracts the name/tag from a Clash proxy map.
func getProxyTag(proxy map[string]any) string {
	return strings.TrimSpace(firstNonEmpty(
		getString(proxy, "name"),
		getString(proxy, "tag"),
	))
}
