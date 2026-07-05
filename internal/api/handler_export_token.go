package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Resinat/Resin/internal/node"
	"github.com/Resinat/Resin/internal/service"
)

// HandleListExportTokens returns a handler for GET /api/v1/export-tokens.
func HandleListExportTokens(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokens, err := cp.ListExportTokens()
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, tokens)
	}
}

// HandleCreateExportToken returns a handler for POST /api/v1/export-tokens.
func HandleCreateExportToken(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if err := DecodeBody(r, &body); err != nil {
			writeDecodeBodyError(w, err)
			return
		}
		resp, err := cp.CreateExportToken(body.Name)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, resp)
	}
}

// HandleDeleteExportToken returns a handler for DELETE /api/v1/export-tokens/{id}.
func HandleDeleteExportToken(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := PathParam(r, "id")
		if err := cp.DeleteExportToken(id); err != nil {
			writeServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ExportOutbound is a single outbound entry in the export response.
type ExportOutbound struct {
	Tag  string          `json:"tag"`
	Type string          `json:"type"`
	Raw  json.RawMessage `json:"-"`
}

// MarshalJSON implements json.Marshaler for ExportOutbound.
// It produces {"tag":"...","type":"...", ...all other fields from Raw...}.
func (o ExportOutbound) MarshalJSON() ([]byte, error) {
	// Start with the raw options as a base map.
	if len(o.Raw) == 0 {
		return json.Marshal(map[string]any{
			"tag":  o.Tag,
			"type": o.Type,
		})
	}
	var base map[string]any
	if err := json.Unmarshal(o.Raw, &base); err != nil {
		return json.Marshal(map[string]any{
			"tag":  o.Tag,
			"type": o.Type,
		})
	}
	base["tag"] = o.Tag
	// Ensure "type" is consistent (RawOptions should already have type).
	if t, ok := base["type"].(string); ok {
		_ = t
	} else {
		base["type"] = o.Type
	}
	return json.Marshal(base)
}

// exportSingBoxResponse is the sing-box config response for GET /api/v1/node-pool/export.
type exportSingBoxResponse struct {
	Outbounds []ExportOutbound `json:"outbounds"`
}

// exportTokenHeaderPrefix is the User-Agent prefix for export token auth.
const exportTokenHeaderPrefix = "ResinExport/"

// extractExportToken extracts an export token from the request, trying in order:
//  1. Authorization: Bearer <token>
//  2. ?export_token=<token>
//  3. User-Agent: ResinExport/<token>
func extractExportToken(r *http.Request) string {
	if auth := r.Header.Get("Authorization"); auth != "" {
		const bearerPrefix = "Bearer "
		if strings.HasPrefix(auth, bearerPrefix) {
			return auth[len(bearerPrefix):]
		}
	}
	if token := r.URL.Query().Get("export_token"); token != "" {
		return token
	}
	if ua := r.Header.Get("User-Agent"); ua != "" {
		if strings.HasPrefix(ua, exportTokenHeaderPrefix) {
			return ua[len(exportTokenHeaderPrefix):]
		}
	}
	return ""
}

// HandleNodePoolExport returns a handler for GET /api/v1/node-pool/export.
// This endpoint does NOT require admin auth; it validates via export token.
func HandleNodePoolExport(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// --- Authentication via export token ---
		rawToken := extractExportToken(r)
		if !cp.ValidateExportToken(rawToken) {
			WriteError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid or missing export token")
			return
		}

		// --- Format ---
		format := r.URL.Query().Get("format")
		if format == "" {
			format = "sing-box"
		}
		if format != "sing-box" {
			WriteError(w, http.StatusBadRequest, "INVALID_ARGUMENT", "format: only 'sing-box' is supported")
			return
		}

		// --- Filters ---
		q := r.URL.Query()
		filters := service.NodeFilters{}

		platformID, ok := parseOptionalUUIDQuery(w, r, "platform_id", "platform_id")
		if !ok {
			return
		}
		filters.PlatformID = platformID

		subscriptionID, ok := parseOptionalUUIDQuery(w, r, "subscription_id", "subscription_id")
		if !ok {
			return
		}
		filters.SubscriptionID = subscriptionID

		if v := q.Get("region"); v != "" {
			filters.Region = &v
		}
		if v := q.Get("egress_ip"); v != "" {
			filters.EgressIP = &v
		}
		if v := strings.TrimSpace(q.Get("tag_keyword")); v != "" {
			filters.TagKeyword = &v
		}

		circuitOpen, ok := parseBoolQueryOrWriteInvalid(w, r, "circuit_open")
		if !ok {
			return
		}
		filters.CircuitOpen = circuitOpen

		hasOutbound, ok := parseBoolQueryOrWriteInvalid(w, r, "has_outbound")
		if !ok {
			return
		}
		filters.HasOutbound = hasOutbound

		enabled, ok := parseBoolQueryOrWriteInvalid(w, r, "enabled")
		if !ok {
			return
		}
		filters.Enabled = enabled

		// Default routable=true for export endpoint.
		routable := true
		if v := q.Get("routable"); v != "" {
			routableBool, ok := parseBoolQueryOrWriteInvalid(w, r, "routable")
			if !ok {
				return
			}
			if routableBool != nil {
				routable = *routableBool
			}
		}
		filters.Routable = &routable

		if v := q.Get("probed_since"); v != "" {
			parsedTime, err := time.Parse(time.RFC3339Nano, v)
			if err != nil {
				writeInvalidArgument(w, "probed_since: invalid RFC3339 timestamp")
				return
			}
			filters.ProbedSince = &parsedTime
		}

		// --- Fetch nodes ---
		nodes, err := cp.ListNodes(filters)
		if err != nil {
			writeServiceError(w, err)
			return
		}

		// --- Pagination (export defaults: limit=10000) ---
		pg := Pagination{Limit: 10000}
		if v := q.Get("limit"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 0 {
				writeInvalidArgument(w, "limit: must be a non-negative integer")
				return
			}
			if n > maxPageLimit {
				writeInvalidArgument(w, "limit: must be <= 100000")
				return
			}
			if n > 0 {
				pg.Limit = n
			}
		}
		if v := q.Get("offset"); v != "" {
			offset, err := strconv.Atoi(v)
			if err != nil || offset < 0 {
				writeInvalidArgument(w, "offset: must be a non-negative integer")
				return
			}
			pg.Offset = offset
		}

		// Build outbounds from node entries.
		var outbounds []ExportOutbound
		for _, ns := range nodes {
			h, err := node.ParseHex(ns.NodeHash)
			if err != nil {
				continue
			}
			entry, ok := cp.Pool.GetEntry(h)
			if !ok {
				continue
			}
			// Deep-copy RawOptions to avoid mutating the pool entry.
			rawCopy := make(json.RawMessage, len(entry.RawOptions))
			copy(rawCopy, entry.RawOptions)

			// Determine display tag.
			tag := ns.DisplayTag
			if tag == "" && len(ns.Tags) > 0 {
				tag = ns.Tags[0].Tag
			}
			// If still empty, use node hash prefix.
			if tag == "" && ns.NodeHash != "" {
				prefix := ns.NodeHash
				if len(prefix) > 12 {
					prefix = prefix[:12]
				}
				tag = prefix
			}

			// Extract "type" from raw options for convenience.
			outboundType := ""
			var rawMap map[string]any
			if err := json.Unmarshal(rawCopy, &rawMap); err == nil {
				if t, ok := rawMap["type"].(string); ok {
					outboundType = t
				}
			}

			outbounds = append(outbounds, ExportOutbound{
				Tag:  tag,
				Type: outboundType,
				Raw:  rawCopy,
			})
		}

		// Apply pagination.
		page := PaginateSlice(outbounds, pg)

		// sing-box format: return {"outbounds": [...]} with no wrapper metadata.
		WriteJSON(w, http.StatusOK, exportSingBoxResponse{
			Outbounds: page,
		})
	}
}
