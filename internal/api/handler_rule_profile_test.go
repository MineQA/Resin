package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// --- helpers ---

func mustCreateRuleProfile(t *testing.T, srv *Server, name, templateYAML string) map[string]any {
	t.Helper()
	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/rule-profiles", map[string]any{
		"name":          name,
		"template_yaml": templateYAML,
	}, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create rule profile: status=%d body=%s", rec.Code, rec.Body.String())
	}
	return decodeJSONMap(t, rec)
}

// --- List ---

func TestRuleProfileList_Empty(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodGet, "/api/v1/rule-profiles", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list []any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rec.Body.String())
	}
	if len(list) != 0 {
		t.Fatalf("expected empty list, got %d", len(list))
	}
}

func TestRuleProfileList_AfterCreate(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	mustCreateRuleProfile(t, srv, "Alpha", "rules:\n  - MATCH,Proxy\n")
	mustCreateRuleProfile(t, srv, "Beta", "rules:\n  - MATCH,Proxy\n")

	rec := doJSONRequest(t, srv, http.MethodGet, "/api/v1/rule-profiles", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal: %v body=%s", err, rec.Body.String())
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
	// Summary must not contain template_yaml.
	for _, item := range list {
		if _, ok := item["template_yaml"]; ok {
			t.Fatal("summary must not include template_yaml")
		}
		if _, ok := item["id"]; !ok {
			t.Fatal("summary missing id")
		}
	}
	// Order by name: Alpha before Beta.
	if list[0]["name"] != "Alpha" {
		t.Fatalf("list[0].name = %v, want Alpha", list[0]["name"])
	}
}

func TestRuleProfileList_EnabledFilter(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	mustCreateRuleProfile(t, srv, "Enabled One", "rules:\n  - MATCH,Proxy\n")
	// Create a disabled one.
	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/rule-profiles", map[string]any{
		"name":          "Disabled One",
		"template_yaml": "rules:\n  - MATCH,Proxy\n",
		"enabled":       false,
	}, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create disabled: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Only enabled.
	rec = doJSONRequest(t, srv, http.MethodGet, "/api/v1/rule-profiles?enabled=true", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var list []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0]["name"] != "Enabled One" {
		t.Fatalf("expected 1 enabled, got %d: %s", len(list), rec.Body.String())
	}

	// Only disabled.
	rec = doJSONRequest(t, srv, http.MethodGet, "/api/v1/rule-profiles?enabled=false", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 1 || list[0]["name"] != "Disabled One" {
		t.Fatalf("expected 1 disabled, got %d: %s", len(list), rec.Body.String())
	}
}

func TestRuleProfileList_Unauthenticated(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodGet, "/api/v1/rule-profiles", nil, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// --- Create ---

func TestRuleProfileCreate_Success(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	body := mustCreateRuleProfile(t, srv, "New Profile", "rules:\n  - MATCH,Proxy\n")
	if body["id"] == "" {
		t.Fatal("missing id")
	}
	if body["name"] != "New Profile" {
		t.Fatalf("name = %v", body["name"])
	}
	if body["template_yaml"] != "rules:\n  - MATCH,Proxy\n" {
		t.Fatalf("template_yaml mismatch: %q", body["template_yaml"])
	}
	if body["enabled"] != true {
		t.Fatal("expected enabled=true")
	}
	if body["created_at"] == "" {
		t.Fatal("missing created_at")
	}
	if body["updated_at"] == "" {
		t.Fatal("missing updated_at")
	}
}

func TestRuleProfileCreate_ExplicitDisabled(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/rule-profiles", map[string]any{
		"name":          "Disabled Profile",
		"template_yaml": "rules:\n  - MATCH,Proxy\n",
		"enabled":       false,
	}, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeJSONMap(t, rec)
	if body["enabled"] != false {
		t.Fatal("expected enabled=false")
	}
}

func TestRuleProfileCreate_DuplicateName(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	mustCreateRuleProfile(t, srv, "My Profile", "rules:\n  - MATCH,Proxy\n")
	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/rule-profiles", map[string]any{
		"name":          "my profile", // different case
		"template_yaml": "rules:\n  - MATCH,Proxy\n",
	}, true)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "CONFLICT")
}

func TestRuleProfileCreate_InvalidYAML(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/rule-profiles", map[string]any{
		"name":          "Bad",
		"template_yaml": "{bad",
	}, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileCreate_EmptyName(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/rule-profiles", map[string]any{
		"name":          "",
		"template_yaml": "rules:\n  - MATCH,Proxy\n",
	}, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileCreate_EmptyTemplate(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/rule-profiles", map[string]any{
		"name":          "No Template",
		"template_yaml": "",
	}, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileCreate_MissingTemplate(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/rule-profiles", map[string]any{
		"name": "Missing Template",
	}, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileCreate_Unauthenticated(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/rule-profiles", map[string]any{
		"name":          "No Auth",
		"template_yaml": "rules:\n  - MATCH,Proxy\n",
	}, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// --- Get ---

func TestRuleProfileGet_Success(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	created := mustCreateRuleProfile(t, srv, "Detail Test", "rules:\n  - MATCH,DIRECT\n")

	rec := doJSONRequest(t, srv, http.MethodGet, "/api/v1/rule-profiles/"+created["id"].(string), nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeJSONMap(t, rec)
	if body["name"] != "Detail Test" {
		t.Fatalf("name = %v", body["name"])
	}
	// Detail must include template_yaml.
	if body["template_yaml"] != "rules:\n  - MATCH,DIRECT\n" {
		t.Fatalf("template_yaml mismatch: %q", body["template_yaml"])
	}
}

func TestRuleProfileGet_NotFound(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodGet, "/api/v1/rule-profiles/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeffff", nil, true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileGet_InvalidUUID(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodGet, "/api/v1/rule-profiles/not-a-uuid", nil, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// --- Update (PATCH) ---

func TestRuleProfileUpdate_Name(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	created := mustCreateRuleProfile(t, srv, "Old Name", "rules:\n  - MATCH,Proxy\n")

	rec := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/"+created["id"].(string),
		`{"name":"New Name"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeJSONMap(t, rec)
	if body["name"] != "New Name" {
		t.Fatalf("name = %v", body["name"])
	}
	if body["template_yaml"] != "rules:\n  - MATCH,Proxy\n" {
		t.Fatal("template_yaml should not change")
	}
}

func TestRuleProfileUpdate_Enabled(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	created := mustCreateRuleProfile(t, srv, "Toggle", "rules:\n  - MATCH,Proxy\n")

	rec := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/"+created["id"].(string),
		`{"enabled":false}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeJSONMap(t, rec)
	if body["enabled"] != false {
		t.Fatal("expected disabled")
	}

	// Toggle back.
	rec = doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/"+created["id"].(string),
		`{"enabled":true}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body = decodeJSONMap(t, rec)
	if body["enabled"] != true {
		t.Fatal("expected enabled")
	}
}

func TestRuleProfileUpdate_Template(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	created := mustCreateRuleProfile(t, srv, "Update Template", "rules:\n  - MATCH,Proxy\n")

	newTemplate := "rules:\n  - DOMAIN-SUFFIX,example.com,Proxy\n  - MATCH,Proxy\n"
	rec := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/"+created["id"].(string),
		map[string]any{"template_yaml": newTemplate}, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := decodeJSONMap(t, rec)
	if body["template_yaml"] != newTemplate {
		t.Fatalf("template mismatch:\ngot:  %q\nwant: %q", body["template_yaml"], newTemplate)
	}
}

func TestRuleProfileUpdate_NotFound(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeffff",
		`{"name":"New Name"}`, true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileUpdate_InvalidUUID(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/not-a-uuid",
		`{"name":"New Name"}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileUpdate_UnknownField(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	created := mustCreateRuleProfile(t, srv, "Strict", "rules:\n  - MATCH,Proxy\n")
	rec := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/"+created["id"].(string),
		`{"bogus":true}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileUpdate_NullField(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	created := mustCreateRuleProfile(t, srv, "NoNull", "rules:\n  - MATCH,Proxy\n")
	rec := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/"+created["id"].(string),
		`{"name":null}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileUpdate_EmptyPatch(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	created := mustCreateRuleProfile(t, srv, "Empty", "rules:\n  - MATCH,Proxy\n")
	rec := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/"+created["id"].(string),
		`{}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileUpdate_InvalidTemplate(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	created := mustCreateRuleProfile(t, srv, "Bad Template", "rules:\n  - MATCH,Proxy\n")
	rec := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/"+created["id"].(string),
		`{"template_yaml":"bad: ["}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileUpdate_DuplicateName(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	mustCreateRuleProfile(t, srv, "First", "rules:\n  - MATCH,Proxy\n")
	second := mustCreateRuleProfile(t, srv, "Second", "rules:\n  - MATCH,Proxy\n")

	rec := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/"+second["id"].(string),
		`{"name":"first"}`, true)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
	assertErrorCode(t, rec, "CONFLICT")
}

func TestRuleProfileUpdate_Unauthenticated(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/some-id",
		`{"name":"No Auth"}`, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// --- Delete ---

func TestRuleProfileDelete_Success(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	created := mustCreateRuleProfile(t, srv, "Delete Me", "rules:\n  - MATCH,Proxy\n")

	rec := doJSONRequest(t, srv, http.MethodDelete, "/api/v1/rule-profiles/"+created["id"].(string), nil, true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}

	// Verify deleted.
	rec = doJSONRequest(t, srv, http.MethodGet, "/api/v1/rule-profiles/"+created["id"].(string), nil, true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec.Code)
	}
}

func TestRuleProfileDelete_NotFound(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodDelete, "/api/v1/rule-profiles/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeffff", nil, true)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileDelete_InvalidUUID(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodDelete, "/api/v1/rule-profiles/not-a-uuid", nil, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileDelete_Unauthenticated(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodDelete, "/api/v1/rule-profiles/some-id", nil, false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

// --- Edge cases ---

func TestRuleProfileList_BadEnabledQuery(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	rec := doJSONRequest(t, srv, http.MethodGet, "/api/v1/rule-profiles?enabled=maybe", nil, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid enabled value, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileCreate_LongName(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	longName := strings.Repeat("a", 129)
	rec := doJSONRequest(t, srv, http.MethodPost, "/api/v1/rule-profiles", map[string]any{
		"name":          longName,
		"template_yaml": "rules:\n  - MATCH,Proxy\n",
	}, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for long name, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleProfileUpdate_EmptyTemplate(t *testing.T) {
	srv, _, _ := newControlPlaneTestServer(t)
	created := mustCreateRuleProfile(t, srv, "Empty Template Test", "rules:\n  - MATCH,Proxy\n")
	rec := doJSONRequest(t, srv, http.MethodPatch, "/api/v1/rule-profiles/"+created["id"].(string),
		`{"template_yaml":""}`, true)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty template, got %d body=%s", rec.Code, rec.Body.String())
	}
}
