package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/Resinat/Resin/internal/service"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Naming mode
// ---------------------------------------------------------------------------

type clashNamingMode int

const (
	clashNamingLegacy clashNamingMode = iota
	clashNamingProfile
)

// ---------------------------------------------------------------------------
// writeClashWithProfile writes a full Clash YAML document with template injection.
// ---------------------------------------------------------------------------

func writeClashWithProfile(w http.ResponseWriter, outbounds []ExportOutbound, cp *service.ControlPlaneService, profileID string) {
	profile, svcErr := cp.GetRuleProfileForExport(profileID)
	if svcErr != nil {
		writeServiceError(w, svcErr)
		return
	}

	proxies, err := buildClashProxies(outbounds, clashNamingProfile)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", err.Error())
		return
	}

	// Parse template preserving YAML structure.
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(profile.TemplateYAML), &doc); err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "corrupted template")
		return
	}

	// Inject proxies into the top-level mapping.
	injectClashProxies(&doc, proxies)

	out, err := yaml.Marshal(&doc)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "INTERNAL", "failed to encode YAML")
		return
	}

	w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// ---------------------------------------------------------------------------
// buildClashProxies returns the proxy list for Clash output.
// ---------------------------------------------------------------------------

func buildClashProxies(outbounds []ExportOutbound, mode clashNamingMode) ([]map[string]any, error) {
	switch mode {
	case clashNamingLegacy:
		var proxies []map[string]any
		for _, o := range outbounds {
			if p := outboundToClashProxy(o); p != nil {
				proxies = append(proxies, p)
			}
		}
		if proxies == nil {
			proxies = []map[string]any{}
		}
		return proxies, nil

	case clashNamingProfile:
		return buildClashProxiesProfile(outbounds)

	default:
		return nil, fmt.Errorf("unknown clash naming mode: %d", mode)
	}
}

// ---------------------------------------------------------------------------
// Profile-mode proxy building
// ---------------------------------------------------------------------------

// profileProxy holds a converted proxy plus identity info for dedup.
type profileProxy struct {
	proxy    map[string]any
	nodeHash string
	name     string // profile-assigned name before dedup
}

func buildClashProxiesProfile(outbounds []ExportOutbound) ([]map[string]any, error) {
	var proxies []profileProxy

	for _, o := range outbounds {
		profileName := profileNodeName(o.BaseTag, o.Region)

		// Copy outbound with profile-assigned name for conversion.
		profileOutbound := o
		profileOutbound.Tag = profileName

		p := outboundToClashProxy(profileOutbound)
		if p == nil {
			continue // unsupported type, skip
		}

		proxies = append(proxies, profileProxy{
			proxy:    p,
			nodeHash: o.NodeHash,
			name:     profileName,
		})
	}

	if err := dedupProfileProxies(proxies); err != nil {
		return nil, err
	}

	result := make([]map[string]any, len(proxies))
	for i, pi := range proxies {
		result[i] = pi.proxy
	}
	if result == nil {
		result = []map[string]any{}
	}
	return result, nil
}

// ---------------------------------------------------------------------------
// Profile node naming
// ---------------------------------------------------------------------------

// profileNodeName computes the strict profile-mode display name.
// It preserves the path prefix, strips any existing country marker from the
// leaf, and applies [CC] or [??] based on region. When stripping the marker
// leaves an empty remainder, the original leaf is used as fallback.
func profileNodeName(name, region string) string {
	region = strings.ToLower(strings.TrimSpace(region))

	// Split at last '/', preserving the entire prefix including the '/'.
	var prefix, leaf string
	if slashIdx := strings.LastIndexByte(name, '/'); slashIdx >= 0 {
		prefix = name[:slashIdx+1]
		leaf = name[slashIdx+1:]
	} else {
		prefix = ""
		leaf = name
	}

	// Parse existing marker using shared helper.
	markerCode, remainder := parseLeafMarker(leaf)
	if markerCode != "" {
		// Strip marker and exactly one leading space if present.
		if len(remainder) > 0 && remainder[0] == ' ' {
			remainder = remainder[1:]
		}
	} else {
		remainder = leaf
	}

	// Fallback when stripping the marker leaves nothing.
	if remainder == "" {
		remainder = leaf
	}

	// Apply region marker.
	if isAssignedISO(region) {
		return prefix + "[" + strings.ToUpper(region) + "] " + remainder
	}
	return prefix + "[??] " + remainder
}

// ---------------------------------------------------------------------------
// Proxy name dedup for profile mode
// ---------------------------------------------------------------------------

// dedupProfileProxies resolves name collisions among profile-mode proxies.
// Only colliding names receive a hash suffix (" #<hex>"). Non-colliding names
// are left unchanged. Suffix starts at 8 hex chars and extends to 12, then
// to the full hash (32 chars) if needed. Missing node hash on a colliding
// proxy returns an error.
func dedupProfileProxies(proxies []profileProxy) error {
	// Group indices by name.
	nameGroups := make(map[string][]int)
	for i, p := range proxies {
		nameGroups[p.name] = append(nameGroups[p.name], i)
	}

	// Track all names that have been assigned (including unchanged non-colliding
	// names and deduped colliding names).
	taken := make(map[string]bool)

	// First pass: claim all non-colliding names.
	var collidingNames []string
	for name, indices := range nameGroups {
		if len(indices) == 1 {
			taken[name] = true
		} else {
			collidingNames = append(collidingNames, name)
		}
	}

	// Sort collision groups for deterministic output.
	sort.Strings(collidingNames)

	// Second pass: resolve collisions group by group.
	for _, name := range collidingNames {
		indices := nameGroups[name]

		// Sort within the group by hash for deterministic behavior.
		sort.Slice(indices, func(i, j int) bool {
			return proxies[indices[i]].nodeHash < proxies[indices[j]].nodeHash
		})

		for _, idx := range indices {
			hash := proxies[idx].nodeHash
			if hash == "" {
				return fmt.Errorf("INTERNAL: proxy name collision unresolvable for %q: missing node hash", name)
			}

			resolved := false
			for _, length := range []int{8, 12, 32} {
				if length > len(hash) {
					continue
				}
				suffix := hash[:length]
				candidate := name + " #" + suffix
				if !taken[candidate] {
					taken[candidate] = true
					proxies[idx].proxy["name"] = candidate
					resolved = true
					break
				}
			}
			if !resolved {
				return fmt.Errorf("INTERNAL: proxy name collision unresolvable for %q hash=%s", name, hash)
			}
		}
	}

	return nil
}

// ---------------------------------------------------------------------------
// Template injection
// ---------------------------------------------------------------------------

// injectClashProxies replaces or adds the top-level "proxies" key in the YAML
// document node with the given proxy list. It preserves other keys/comments.
//
// When an existing "proxies" value node carries a YAML anchor (&name), the
// anchor is preserved on the updated node so that any alias (*name) elsewhere
// in the document still resolves to the injected sequence. The node is mutated
// in-place — the pointer is never replaced.
func injectClashProxies(doc *yaml.Node, proxies []map[string]any) {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return
	}
	mapping := doc.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return
	}

	// Marshal proxies to a YAML sequence node.
	proxySeq := marshalProxySequence(proxies)
	if proxySeq == nil {
		return
	}

	// Find and replace existing "proxies" key-value pair, or append.
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Kind == yaml.ScalarNode && mapping.Content[i].Value == "proxies" {
			existing := mapping.Content[i+1]

			// Preserve anchor and comment fields from the original node so
			// that YAML aliases elsewhere in the document still resolve to
			// this node and comments are not silently dropped.
			anchor := existing.Anchor
			headComment := existing.HeadComment
			lineComment := existing.LineComment
			footComment := existing.FootComment

			existing.Kind = proxySeq.Kind
			existing.Style = proxySeq.Style
			existing.Tag = proxySeq.Tag
			existing.Value = proxySeq.Value
			existing.Content = proxySeq.Content
			existing.Alias = nil
			existing.Anchor = anchor
			existing.HeadComment = headComment
			existing.LineComment = lineComment
			existing.FootComment = footComment
			return
		}
	}

	// Not found — append new key-value pair.
	keyNode := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: "proxies",
		Tag:   "!!str",
	}
	mapping.Content = append(mapping.Content, keyNode, proxySeq)
}

// marshalProxySequence marshals a proxy list to a yaml.Node sequence.
func marshalProxySequence(proxies []map[string]any) *yaml.Node {
	data, err := yaml.Marshal(proxies)
	if err != nil {
		return nil
	}
	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return nil
	}
	// node is DocumentNode; Content[0] is the SequenceNode.
	if node.Kind != yaml.DocumentNode || len(node.Content) == 0 {
		return nil
	}
	return node.Content[0]
}
