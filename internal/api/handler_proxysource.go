package api

import (
	"net/http"

	"github.com/Resinat/Resin/internal/service"
)

// ---------------------------------------------------------------------------
// Proxy sources
// ---------------------------------------------------------------------------

// HandleListProxySources returns all built-in proxy sources.
// GET /api/v1/proxy-sources
func HandleListProxySources() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, service.BuiltInProxySources)
	}
}

// HandleFetchProxySources fetches one or all proxy sources and returns
// extracted proxy candidates.
// POST /api/v1/proxy-sources/fetch
func HandleFetchProxySources(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req service.FetchSourcesRequest
		if err := DecodeBody(r, &req); err != nil {
			writeDecodeBodyError(w, err)
			return
		}

		result, err := cp.FetchProxySources(req)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	}
}

// ---------------------------------------------------------------------------
// Proxy check import
// ---------------------------------------------------------------------------

// HandleProxyCheckImport imports caller-provided proxies into the node pool
// under an ephemeral subscription. Does not run proxy checks.
// Request must include confirm_checked=true to confirm proxies were reviewed.
// POST /api/v1/proxy-check/import
func HandleProxyCheckImport(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req service.ImportProxiesRequest
		if err := DecodeBody(r, &req); err != nil {
			writeDecodeBodyError(w, err)
			return
		}

		result, err := cp.ImportProxies(req)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	}
}
