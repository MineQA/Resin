package api

import (
	"encoding/json"
	"net/http"

	"github.com/Resinat/Resin/internal/service"
)

// HandleListRuleProfiles returns a handler for GET /api/v1/rule-profiles.
func HandleListRuleProfiles(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		enabled, err := ParseBoolQuery(r, "enabled")
		if err != nil {
			writeInvalidArgument(w, err.Error())
			return
		}

		profiles, err := cp.ListRuleProfiles(enabled)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, profiles)
	}
}

// HandleCreateRuleProfile returns a handler for POST /api/v1/rule-profiles.
func HandleCreateRuleProfile(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name         string `json:"name"`
			TemplateYAML string `json:"template_yaml"`
			Enabled      *bool  `json:"enabled,omitempty"`
		}
		if err := DecodeBody(r, &body); err != nil {
			writeDecodeBodyError(w, err)
			return
		}

		resp, err := cp.CreateRuleProfile(service.CreateRuleProfileRequest{
			Name:         body.Name,
			TemplateYAML: body.TemplateYAML,
			Enabled:      body.Enabled,
		})
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusCreated, resp)
	}
}

// HandleGetRuleProfile returns a handler for GET /api/v1/rule-profiles/{id}.
func HandleGetRuleProfile(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := PathParam(r, "id")
		if !ValidateUUID(id) {
			writeInvalidArgument(w, "id: must be a valid UUID")
			return
		}

		resp, err := cp.GetRuleProfile(id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, resp)
	}
}

// HandleUpdateRuleProfile returns a handler for PATCH /api/v1/rule-profiles/{id}.
func HandleUpdateRuleProfile(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := PathParam(r, "id")
		if !ValidateUUID(id) {
			writeInvalidArgument(w, "id: must be a valid UUID")
			return
		}

		body, ok := readRawBodyOrWriteInvalid(w, r)
		if !ok {
			return
		}

		resp, err := cp.UpdateRuleProfile(id, json.RawMessage(body))
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, resp)
	}
}

// HandleACL4SSRPreview returns a handler for POST /api/v1/rule-profiles/acl4ssr/preview.
// It converts an ACL4SSR [custom] INI (inline or from a known source) to a
// preview Clash/Mihomo YAML template without persisting anything.
func HandleACL4SSRPreview(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body service.ACL4SSRPreviewRequest
		if err := DecodeBody(r, &body); err != nil {
			writeDecodeBodyError(w, err)
			return
		}

		resp, err := cp.PreviewACL4SSRConversion(r.Context(), body)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		WriteJSON(w, http.StatusOK, resp)
	}
}

// HandleDeleteRuleProfile returns a handler for DELETE /api/v1/rule-profiles/{id}.
func HandleDeleteRuleProfile(cp *service.ControlPlaneService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := PathParam(r, "id")
		if !ValidateUUID(id) {
			writeInvalidArgument(w, "id: must be a valid UUID")
			return
		}

		if err := cp.DeleteRuleProfile(id); err != nil {
			writeServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
