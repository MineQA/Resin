package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/state"
)

// RuleProfileSummary is the API response for listing rule profiles (without template YAML).
type RuleProfileSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// RuleProfileDetail is the API response for a single rule profile (includes template YAML).
type RuleProfileDetail struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	TemplateYAML string `json:"template_yaml"`
	Enabled      bool   `json:"enabled"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// ruleProfileToSummary converts a model.RuleProfile to a summary response.
func ruleProfileToSummary(p model.RuleProfile) RuleProfileSummary {
	return RuleProfileSummary{
		ID:        p.ID,
		Name:      p.Name,
		Enabled:   p.Enabled,
		CreatedAt: time.Unix(0, p.CreatedAtNs).UTC().Format(time.RFC3339Nano),
		UpdatedAt: time.Unix(0, p.UpdatedAtNs).UTC().Format(time.RFC3339Nano),
	}
}

// ruleProfileToDetail converts a model.RuleProfile to a detail response.
func ruleProfileToDetail(p model.RuleProfile) RuleProfileDetail {
	return RuleProfileDetail{
		ID:           p.ID,
		Name:         p.Name,
		TemplateYAML: p.TemplateYAML,
		Enabled:      p.Enabled,
		CreatedAt:    time.Unix(0, p.CreatedAtNs).UTC().Format(time.RFC3339Nano),
		UpdatedAt:    time.Unix(0, p.UpdatedAtNs).UTC().Format(time.RFC3339Nano),
	}
}

// ListRuleProfiles returns all rule profiles, optionally filtered by enabled status.
func (s *ControlPlaneService) ListRuleProfiles(enabled *bool) ([]RuleProfileSummary, error) {
	profiles, err := s.Engine.ListRuleProfiles(enabled)
	if err != nil {
		return nil, internal("list rule profiles", err)
	}
	result := make([]RuleProfileSummary, len(profiles))
	for i, p := range profiles {
		result[i] = ruleProfileToSummary(p)
	}
	return result, nil
}

// GetRuleProfile returns a single rule profile detail by ID.
func (s *ControlPlaneService) GetRuleProfile(id string) (*RuleProfileDetail, error) {
	p, err := s.Engine.GetRuleProfile(id)
	if err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return nil, notFound("rule profile not found")
		}
		return nil, internal("get rule profile", err)
	}
	resp := ruleProfileToDetail(*p)
	return &resp, nil
}

// CreateRuleProfileRequest holds the create rule profile request body.
type CreateRuleProfileRequest struct {
	Name         string `json:"name"`
	TemplateYAML string `json:"template_yaml"`
	Enabled      *bool  `json:"enabled,omitempty"`
}

// CreateRuleProfile creates a new rule profile after validation.
func (s *ControlPlaneService) CreateRuleProfile(req CreateRuleProfileRequest) (*RuleProfileDetail, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, invalidArg("name: must not be empty")
	}
	if len(name) > 128 {
		return nil, invalidArg("name: must be at most 128 characters")
	}
	if req.TemplateYAML == "" {
		return nil, invalidArg("template_yaml: must not be empty")
	}

	if err := ValidateRuleProfileTemplate(req.TemplateYAML); err != nil {
		return nil, invalidArg(err.Message)
	}

	now := time.Now().UnixNano()
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	p := model.RuleProfile{
		ID:           uuid.New().String(),
		Name:         name,
		TemplateYAML: req.TemplateYAML,
		Enabled:      enabled,
		CreatedAtNs:  now,
		UpdatedAtNs:  now,
	}

	if err := s.Engine.CreateRuleProfile(p); err != nil {
		if errors.Is(err, state.ErrConflict) {
			return nil, conflict("rule profile name already exists")
		}
		return nil, internal("create rule profile", err)
	}

	resp := ruleProfileToDetail(p)
	return &resp, nil
}

// UpdateRuleProfile patches a rule profile by ID with constrained fields:
// name, template_yaml, enabled. When template_yaml is changed, it is re-validated.
func (s *ControlPlaneService) UpdateRuleProfile(id string, patchJSON json.RawMessage) (*RuleProfileDetail, error) {
	patch, svcErr := parseMergePatch(patchJSON)
	if svcErr != nil {
		return nil, svcErr
	}

	allowed := map[string]bool{
		"name":          true,
		"template_yaml": true,
		"enabled":       true,
	}
	if svcErr := patch.validateFields(allowed, func(field string) string {
		return fmt.Sprintf("unknown field: %q; allowed: name, template_yaml, enabled", field)
	}); svcErr != nil {
		return nil, svcErr
	}

	// Fetch current profile to know which fields are being updated.
	current, err := s.Engine.GetRuleProfile(id)
	if err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return nil, notFound("rule profile not found")
		}
		return nil, internal("get rule profile for update", err)
	}

	name, nameSet, svcErr := patch.optionalNonEmptyString("name")
	if svcErr != nil {
		return nil, svcErr
	}
	if nameSet && len(name) > 128 {
		return nil, invalidArg("name: must be at most 128 characters")
	}

	templateYAML, templateSet, svcErr := patch.optionalString("template_yaml")
	if svcErr != nil {
		return nil, svcErr
	}

	// When template_yaml is set (even to empty), validate it.
	if templateSet {
		if templateYAML == "" {
			return nil, invalidArg("template_yaml: must not be empty")
		}
		if err := ValidateRuleProfileTemplate(templateYAML); err != nil {
			return nil, invalidArg(err.Message)
		}
	}

	enabledPtr, enabledSet, svcErr := patch.optionalBool("enabled")
	if svcErr != nil {
		return nil, svcErr
	}

	now := time.Now().UnixNano()

	// Apply changes to the current model.
	if nameSet {
		current.Name = name
	}
	if templateSet {
		current.TemplateYAML = templateYAML
	}
	if enabledSet {
		current.Enabled = enabledPtr
	}
	current.UpdatedAtNs = now

	// Compute arguments for repo UpdateRuleProfile.
	updateName := ""
	if nameSet {
		updateName = name
	}
	updateTemplateYAML := ""
	if templateSet {
		updateTemplateYAML = templateYAML
	}
	var updateEnabledPtr *bool
	if enabledSet {
		updateEnabledPtr = &enabledPtr
	}

	if err := s.Engine.UpdateRuleProfile(id, updateName, updateTemplateYAML, updateEnabledPtr, now); err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return nil, notFound("rule profile not found")
		}
		if errors.Is(err, state.ErrConflict) {
			return nil, conflict("rule profile name already exists")
		}
		return nil, internal("update rule profile", err)
	}

	resp := ruleProfileToDetail(*current)
	return &resp, nil
}

// GetRuleProfileForExport returns an enabled rule profile by ID for export use.
// Returns RULE_PROFILE_UNAVAILABLE when the profile is missing or disabled (unified).
// Defensively re-validates the template; corrupted data returns INTERNAL.
func (s *ControlPlaneService) GetRuleProfileForExport(id string) (*RuleProfileDetail, *ServiceError) {
	p, err := s.Engine.GetEnabledRuleProfile(id)
	if err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return nil, &ServiceError{Code: "RULE_PROFILE_UNAVAILABLE", Message: "rule profile is unavailable"}
		}
		return nil, internal("get rule profile for export", err)
	}
	if err := ValidateRuleProfileTemplate(p.TemplateYAML); err != nil {
		return nil, internal("corrupted rule profile template", fmt.Errorf("validate: %v", err))
	}
	resp := ruleProfileToDetail(*p)
	return &resp, nil
}

// DeleteRuleProfile removes a rule profile by ID.
func (s *ControlPlaneService) DeleteRuleProfile(id string) error {
	if err := s.Engine.DeleteRuleProfile(id); err != nil {
		if errors.Is(err, state.ErrNotFound) {
			return notFound("rule profile not found")
		}
		return internal("delete rule profile", err)
	}
	return nil
}
