package api

import (
	"net/http"

	"github.com/Resinat/Resin/internal/service"
)

// ---------------------------------------------------------------------------
// Node action: POST /api/v1/nodes/{hash}/actions/probe-quality
// ---------------------------------------------------------------------------

// HandleNodeActionProbeQuality returns a handler for probe-quality on a single
// node. Unlike proxy-check, this endpoint does not accept a request body — it
// always uses the current runtime quality profile, options, and scoring policy.
// It is a thin wrapper over ProbeQualitySync.
func HandleNodeActionProbeQuality(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hash := PathParam(r, "hash")

		result, err := cp.CheckProbeQuality(hash)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	}
}

// ---------------------------------------------------------------------------
// Trigger all: POST /api/v1/proxy-check/actions/trigger-all
// ---------------------------------------------------------------------------

// HandleTriggerAllQualityProbes triggers an async quality sweep across all
// eligible nodes. Returns 202 with candidate_count and coalesced fields.
// Returns an error (not 202) when the queue rejects the task.
func HandleTriggerAllQualityProbes(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := cp.TriggerAllQualityProbes()
		if err != nil {
			// Any error here (incl. queue rejection) is returned as an error
			// response, NOT 202 — the API contract guarantees 202 only on
			// successful acceptance.
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusAccepted, result)
	}
}

// ---------------------------------------------------------------------------
// Node action: POST /api/v1/nodes/{hash}/actions/proxy-check
// ---------------------------------------------------------------------------

// HandleNodeActionProxyCheck returns a handler for proxy-check on a single node.
// Request body is optional; when present it may contain {profile, options}.
func HandleNodeActionProxyCheck(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hash := PathParam(r, "hash")

		var req service.ProxyCheckRequest
		// Body is optional — only decode when present.
		if r.Body != nil && r.Header.Get("Content-Type") != "" {
			if err := DecodeBody(r, &req); err != nil {
				writeDecodeBodyError(w, err)
				return
			}
		}

		result, err := cp.CheckProxyCheck(hash, req)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, result)
	}
}

// ---------------------------------------------------------------------------
// Batch tasks: POST /api/v1/proxy-check/tasks
//              GET  /api/v1/proxy-check/tasks/{id}
// ---------------------------------------------------------------------------

// HandleCreateProxyCheckTask returns a handler for creating a batch proxy check task.
func HandleCreateProxyCheckTask(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req service.ProxyCheckBatchRequest
		if err := DecodeBody(r, &req); err != nil {
			writeDecodeBodyError(w, err)
			return
		}

		task, err := cp.CreateProxyCheckBatchTask(req)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, task)
	}
}

// HandleGetProxyCheckTask returns a handler for retrieving a batch proxy check task.
func HandleGetProxyCheckTask(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := PathParam(r, "id")
		task, err := cp.GetProxyCheckTask(id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, task)
	}
}
