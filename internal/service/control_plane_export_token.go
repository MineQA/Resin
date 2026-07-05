package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/Resinat/Resin/internal/model"
	"github.com/Resinat/Resin/internal/state"
)

// ExportTokenResponse is the API response for an export token (without the raw token).
type ExportTokenResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TokenPrefix string `json:"token_prefix"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at"`
	LastUsedAt  string `json:"last_used_at"`
}

// ExportTokenCreatedResponse includes the plaintext token (only returned on creation).
type ExportTokenCreatedResponse struct {
	ExportTokenResponse
	Token string `json:"token"`
}

// exportTokenToResponse converts a model.ExportToken to an ExportTokenResponse.
func exportTokenToResponse(t model.ExportToken) ExportTokenResponse {
	createdAt := time.Unix(0, t.CreatedAtNs).UTC().Format(time.RFC3339Nano)
	lastUsedAt := ""
	if t.LastUsedAtNs > 0 {
		lastUsedAt = time.Unix(0, t.LastUsedAtNs).UTC().Format(time.RFC3339Nano)
	}
	return ExportTokenResponse{
		ID:          t.ID,
		Name:        t.Name,
		TokenPrefix: t.TokenPrefix,
		Enabled:     t.Enabled,
		CreatedAt:   createdAt,
		LastUsedAt:  lastUsedAt,
	}
}

// generateHighEntropyToken creates a cryptographically random token
// (base64url-encoded, no padding) and returns the raw token, its SHA-256 hex hash,
// and a prefix (first 8 chars) for display.
func generateHighEntropyToken() (rawToken string, hashHex string, prefix string, err error) {
	b := make([]byte, 32) // 256 bits
	if _, err := rand.Read(b); err != nil {
		return "", "", "", fmt.Errorf("generate token: %w", err)
	}
	rawToken = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(rawToken))
	hashHex = hex.EncodeToString(sum[:])
	prefix = rawToken[:8]
	return
}

// ListExportTokens returns all export tokens without exposing the raw token.
func (s *ControlPlaneService) ListExportTokens() ([]ExportTokenResponse, error) {
	tokens, err := s.Engine.ListExportTokens()
	if err != nil {
		return nil, internal("list export tokens", err)
	}
	result := make([]ExportTokenResponse, len(tokens))
	for i, t := range tokens {
		result[i] = exportTokenToResponse(t)
	}
	return result, nil
}

// CreateExportToken generates a high-entropy token, stores the SHA-256 hash,
// and returns metadata along with the plaintext token (one-time exposure).
func (s *ControlPlaneService) CreateExportToken(name string) (*ExportTokenCreatedResponse, error) {
	if name == "" {
		return nil, invalidArg("name: must not be empty")
	}

	rawToken, hashHex, prefix, err := generateHighEntropyToken()
	if err != nil {
		return nil, internal("generate export token", err)
	}

	now := time.Now().UnixNano()
	t := model.ExportToken{
		ID:           uuid.New().String(),
		Name:         name,
		TokenHash:    hashHex,
		TokenPrefix:  prefix,
		Enabled:      true,
		CreatedAtNs:  now,
		LastUsedAtNs: 0,
	}

	if err := s.Engine.CreateExportToken(t); err != nil {
		return nil, internal("persist export token", err)
	}

	resp := ExportTokenCreatedResponse{
		ExportTokenResponse: exportTokenToResponse(t),
		Token:               rawToken,
	}
	return &resp, nil
}

// DeleteExportToken removes an export token by ID.
func (s *ControlPlaneService) DeleteExportToken(id string) error {
	if err := s.Engine.DeleteExportToken(id); err != nil {
		if err == state.ErrNotFound {
			return notFound("export token not found")
		}
		return internal("delete export token", err)
	}
	return nil
}

// ValidateExportToken hashes the raw token, looks up an enabled matching export token,
// updates last_used_at_ns, and returns true if valid.
func (s *ControlPlaneService) ValidateExportToken(rawToken string) bool {
	if rawToken == "" {
		return false
	}
	sum := sha256.Sum256([]byte(rawToken))
	hashHex := hex.EncodeToString(sum[:])

	t, err := s.Engine.FindEnabledExportTokenByHash(hashHex)
	if err != nil || t == nil {
		return false
	}

	// Touch last_used_at_ns best-effort (non-blocking for export path).
	_ = s.Engine.TouchExportTokenLastUsed(t.ID, time.Now().UnixNano())
	return true
}
