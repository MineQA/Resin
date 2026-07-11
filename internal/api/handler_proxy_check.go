package api

import (
	"net/http"

	"github.com/Resinat/Resin/internal/service"
)

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
