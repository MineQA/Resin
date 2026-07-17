package service

import (
	"encoding/json"
	"testing"

	"github.com/Resinat/Resin/internal/state"
)

// newTestEngine creates a fresh StateEngine backed by a temp state.db.
func newTestEngine(t *testing.T) *state.StateEngine {
	t.Helper()
	engine, closer, err := state.PersistenceBootstrap(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("PersistenceBootstrap: %v", err)
	}
	t.Cleanup(func() { _ = closer.Close() })
	return engine
}

func newTestRuleProfileService(t *testing.T) *ControlPlaneService {
	t.Helper()
	return &ControlPlaneService{
		Engine: newTestEngine(t),
	}
}

func validTemplate() string {
	return "rules:\n  - MATCH,Proxy\n"
}

func TestCreateRuleProfile_Success(t *testing.T) {
	cp := newTestRuleProfileService(t)
	enabled := true
	resp, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "My Profile",
		TemplateYAML: validTemplate(),
		Enabled:      &enabled,
	})
	if err != nil {
		t.Fatalf("CreateRuleProfile: %v", err)
	}
	if resp.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if resp.Name != "My Profile" {
		t.Fatalf("name = %q, want %q", resp.Name, "My Profile")
	}
	if resp.TemplateYAML != validTemplate() {
		t.Fatalf("template mismatch")
	}
	if !resp.Enabled {
		t.Fatal("expected enabled")
	}
	if resp.CreatedAt == "" {
		t.Fatal("expected created_at")
	}
	if resp.UpdatedAt == "" {
		t.Fatal("expected updated_at")
	}
}

func TestCreateRuleProfile_DefaultEnabled(t *testing.T) {
	cp := newTestRuleProfileService(t)
	resp, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Default Enabled",
		TemplateYAML: validTemplate(),
	})
	if err != nil {
		t.Fatalf("CreateRuleProfile: %v", err)
	}
	if !resp.Enabled {
		t.Fatal("expected enabled by default")
	}
}

func TestCreateRuleProfile_Disabled(t *testing.T) {
	cp := newTestRuleProfileService(t)
	enabled := false
	resp, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Disabled",
		TemplateYAML: validTemplate(),
		Enabled:      &enabled,
	})
	if err != nil {
		t.Fatalf("CreateRuleProfile: %v", err)
	}
	if resp.Enabled {
		t.Fatal("expected disabled")
	}
}

func TestCreateRuleProfile_EmptyName(t *testing.T) {
	cp := newTestRuleProfileService(t)
	_, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "  ",
		TemplateYAML: validTemplate(),
	})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestCreateRuleProfile_NameTooLong(t *testing.T) {
	cp := newTestRuleProfileService(t)
	name := ""
	for i := 0; i < 129; i++ {
		name += "a"
	}
	_, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         name,
		TemplateYAML: validTemplate(),
	})
	if err == nil {
		t.Fatal("expected error for name > 128 chars")
	}
}

func TestCreateRuleProfile_EmptyTemplate(t *testing.T) {
	cp := newTestRuleProfileService(t)
	_, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Empty Template",
		TemplateYAML: "",
	})
	if err == nil {
		t.Fatal("expected error for empty template")
	}
}

func TestCreateRuleProfile_InvalidTemplate(t *testing.T) {
	cp := newTestRuleProfileService(t)
	_, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Bad Template",
		TemplateYAML: "{bad",
	})
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}

func TestCreateRuleProfile_DuplicateName(t *testing.T) {
	cp := newTestRuleProfileService(t)
	_, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Unique Name",
		TemplateYAML: validTemplate(),
	})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err = cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "unique name", // different case
		TemplateYAML: validTemplate(),
	})
	if err == nil {
		t.Fatal("expected error for duplicate name (case-insensitive)")
	}
}

func TestGetRuleProfile_Success(t *testing.T) {
	cp := newTestRuleProfileService(t)
	created, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Get Me",
		TemplateYAML: validTemplate(),
	})
	if err != nil {
		t.Fatalf("CreateRuleProfile: %v", err)
	}
	got, err := cp.GetRuleProfile(created.ID)
	if err != nil {
		t.Fatalf("GetRuleProfile: %v", err)
	}
	if got.Name != "Get Me" {
		t.Fatalf("name = %q", got.Name)
	}
	if got.TemplateYAML != validTemplate() {
		t.Fatal("template mismatch")
	}
}

func TestGetRuleProfile_NotFound(t *testing.T) {
	cp := newTestRuleProfileService(t)
	_, err := cp.GetRuleProfile("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

func TestListRuleProfiles(t *testing.T) {
	cp := newTestRuleProfileService(t)
	_, _ = cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "B Profile",
		TemplateYAML: validTemplate(),
	})
	_, _ = cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "A Profile",
		TemplateYAML: validTemplate(),
	})

	all, err := cp.ListRuleProfiles(nil)
	if err != nil {
		t.Fatalf("ListRuleProfiles: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
	// Order by name (case-insensitive): "A Profile" before "B Profile"
	if all[0].Name != "A Profile" {
		t.Fatalf("all[0].Name = %q, want 'A Profile'", all[0].Name)
	}
	// Summary should not include template_yaml (struct fields are intentional).
}

func TestListRuleProfiles_EnabledFilter(t *testing.T) {
	cp := newTestRuleProfileService(t)
	enabled := true
	disabled := false
	_, _ = cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Enabled Profile",
		TemplateYAML: validTemplate(),
		Enabled:      &enabled,
	})
	_, _ = cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Disabled Profile",
		TemplateYAML: validTemplate(),
		Enabled:      &disabled,
	})

	onlyEnabled, err := cp.ListRuleProfiles(boolPtrForService(true))
	if err != nil {
		t.Fatalf("ListRuleProfiles enabled=true: %v", err)
	}
	if len(onlyEnabled) != 1 || onlyEnabled[0].Name != "Enabled Profile" {
		t.Fatalf("expected 1 enabled profile, got %d", len(onlyEnabled))
	}

	onlyDisabled, err := cp.ListRuleProfiles(boolPtrForService(false))
	if err != nil {
		t.Fatalf("ListRuleProfiles enabled=false: %v", err)
	}
	if len(onlyDisabled) != 1 || onlyDisabled[0].Name != "Disabled Profile" {
		t.Fatalf("expected 1 disabled profile, got %d", len(onlyDisabled))
	}
}

func boolPtrForService(v bool) *bool { return &v }

func TestUpdateRuleProfile_Name(t *testing.T) {
	cp := newTestRuleProfileService(t)
	created, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Original Name",
		TemplateYAML: validTemplate(),
	})
	if err != nil {
		t.Fatalf("CreateRuleProfile: %v", err)
	}

	updated, err := cp.UpdateRuleProfile(created.ID, json.RawMessage(`{"name":"Updated Name"}`))
	if err != nil {
		t.Fatalf("UpdateRuleProfile: %v", err)
	}
	if updated.Name != "Updated Name" {
		t.Fatalf("name = %q, want %q", updated.Name, "Updated Name")
	}
	if updated.TemplateYAML != validTemplate() {
		t.Fatal("template_yaml should not change")
	}
}

func TestUpdateRuleProfile_Enabled(t *testing.T) {
	cp := newTestRuleProfileService(t)
	created, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Toggle Me",
		TemplateYAML: validTemplate(),
	})
	if err != nil {
		t.Fatalf("CreateRuleProfile: %v", err)
	}

	updated, err := cp.UpdateRuleProfile(created.ID, json.RawMessage(`{"enabled":false}`))
	if err != nil {
		t.Fatalf("UpdateRuleProfile enabled=false: %v", err)
	}
	if updated.Enabled {
		t.Fatal("expected disabled")
	}

	updated2, err := cp.UpdateRuleProfile(created.ID, json.RawMessage(`{"enabled":true}`))
	if err != nil {
		t.Fatalf("UpdateRuleProfile enabled=true: %v", err)
	}
	if !updated2.Enabled {
		t.Fatal("expected enabled")
	}
}

func TestUpdateRuleProfile_Template(t *testing.T) {
	cp := newTestRuleProfileService(t)
	created, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Update Template",
		TemplateYAML: validTemplate(),
	})
	if err != nil {
		t.Fatalf("CreateRuleProfile: %v", err)
	}

	newTemplate := "rules:\n  - DOMAIN-SUFFIX,example.com,Proxy\n  - MATCH,Proxy\n"
	updated, err := cp.UpdateRuleProfile(created.ID, json.RawMessage(`{"template_yaml":"`+jsonEscape(newTemplate)+`"}`))
	if err != nil {
		t.Fatalf("UpdateRuleProfile template: %v", err)
	}
	if updated.TemplateYAML != newTemplate {
		t.Fatalf("template mismatch:\ngot:  %q\nwant: %q", updated.TemplateYAML, newTemplate)
	}
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}

func TestUpdateRuleProfile_DuplicateName(t *testing.T) {
	cp := newTestRuleProfileService(t)
	_, _ = cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "First",
		TemplateYAML: validTemplate(),
	})
	second, _ := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Second",
		TemplateYAML: validTemplate(),
	})

	_, err := cp.UpdateRuleProfile(second.ID, json.RawMessage(`{"name":"first"}`))
	if err == nil {
		t.Fatal("expected error for duplicate name (case-insensitive)")
	}
}

func TestUpdateRuleProfile_NotFound(t *testing.T) {
	cp := newTestRuleProfileService(t)
	_, err := cp.UpdateRuleProfile("nonexistent-id", json.RawMessage(`{"name":"New Name"}`))
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

func TestUpdateRuleProfile_InvalidTemplate(t *testing.T) {
	cp := newTestRuleProfileService(t)
	created, _ := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Valid Profile",
		TemplateYAML: validTemplate(),
	})
	_, err := cp.UpdateRuleProfile(created.ID, json.RawMessage(`{"template_yaml":"bad: ["}`))
	if err == nil {
		t.Fatal("expected error for invalid template")
	}
}

func TestUpdateRuleProfile_EmptyTemplate(t *testing.T) {
	cp := newTestRuleProfileService(t)
	created, _ := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Valid Profile",
		TemplateYAML: validTemplate(),
	})
	_, err := cp.UpdateRuleProfile(created.ID, json.RawMessage(`{"template_yaml":""}`))
	if err == nil {
		t.Fatal("expected error for empty template")
	}
}

func TestUpdateRuleProfile_UnknownField(t *testing.T) {
	cp := newTestRuleProfileService(t)
	created, _ := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Valid Profile",
		TemplateYAML: validTemplate(),
	})
	_, err := cp.UpdateRuleProfile(created.ID, json.RawMessage(`{"bogus":true}`))
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestUpdateRuleProfile_NullField(t *testing.T) {
	cp := newTestRuleProfileService(t)
	created, _ := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Valid Profile",
		TemplateYAML: validTemplate(),
	})
	_, err := cp.UpdateRuleProfile(created.ID, json.RawMessage(`{"name":null}`))
	if err == nil {
		t.Fatal("expected error for null field")
	}
}

func TestUpdateRuleProfile_NameTooLong(t *testing.T) {
	cp := newTestRuleProfileService(t)
	created, _ := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Valid Profile",
		TemplateYAML: validTemplate(),
	})
	name := ""
	for i := 0; i < 129; i++ {
		name += "a"
	}
	patch, _ := json.Marshal(map[string]string{"name": name})
	_, err := cp.UpdateRuleProfile(created.ID, patch)
	if err == nil {
		t.Fatal("expected error for name > 128 chars")
	}
}

func TestDeleteRuleProfile_Success(t *testing.T) {
	cp := newTestRuleProfileService(t)
	created, _ := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "Delete Me",
		TemplateYAML: validTemplate(),
	})
	if err := cp.DeleteRuleProfile(created.ID); err != nil {
		t.Fatalf("DeleteRuleProfile: %v", err)
	}
	// Should not be found after delete.
	_, err := cp.GetRuleProfile(created.ID)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteRuleProfile_NotFound(t *testing.T) {
	cp := newTestRuleProfileService(t)
	err := cp.DeleteRuleProfile("nonexistent-id")
	if err == nil {
		t.Fatal("expected error for not found")
	}
}

func TestCreateRuleProfile_NameTrimmed(t *testing.T) {
	cp := newTestRuleProfileService(t)
	resp, err := cp.CreateRuleProfile(CreateRuleProfileRequest{
		Name:         "  Spaced Name  ",
		TemplateYAML: validTemplate(),
	})
	if err != nil {
		t.Fatalf("CreateRuleProfile: %v", err)
	}
	if resp.Name != "Spaced Name" {
		t.Fatalf("name = %q, want %q", resp.Name, "Spaced Name")
	}
}
