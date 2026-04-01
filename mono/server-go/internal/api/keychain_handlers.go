package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
)

// ── Request / Response Types ────────────────────────────────────────────────

type keychainInitResponse struct {
	RecoveryPhrase string `json:"recovery_phrase"`
}

type keychainStatusResponse struct {
	Initialized bool `json:"initialized"`
	KeyVersion  int  `json:"key_version"`
	SecretCount int  `json:"secret_count"`
}

type keychainPutSecretRequest struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Category string `json:"category,omitempty"`
	Metadata string `json:"metadata,omitempty"`
}

type keychainSecretResponse struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type keychainSecretListEntry struct {
	Name      string `json:"name"`
	Version   int64  `json:"version"`
	Category  string `json:"category,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

type keychainRecoverRequest struct {
	RecoveryPhrase string `json:"recovery_phrase"`
}

// ── Handlers ────────────────────────────────────────────────────────────────

// InitKeychain creates a new keychain for the authenticated user.
// POST /api/v1/keychain/init
func (h *Handler) InitKeychain(w http.ResponseWriter, r *http.Request) {
	if h.KeychainService == nil {
		writeError(w, http.StatusServiceUnavailable, "keychain service not configured")
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	result, err := h.KeychainService.InitForUser(r.Context(), userID)
	if err != nil {
		slog.Warn("keychain init failed", "user_id", userID, "error", err)
		writeError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, keychainInitResponse{
		RecoveryPhrase: result.RecoveryPhrase,
	})
}

// GetKeychainStatus returns whether the user has an initialized keychain.
// GET /api/v1/keychain/status
func (h *Handler) GetKeychainStatus(w http.ResponseWriter, r *http.Request) {
	if h.KeychainService == nil {
		writeError(w, http.StatusServiceUnavailable, "keychain service not configured")
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Try to load the keychain metadata to check initialization and key version.
	kc, kcErr := h.KeychainService.GetKeychain(r.Context(), userID)
	if kcErr != nil {
		writeJSON(w, http.StatusOK, keychainStatusResponse{Initialized: false})
		return
	}

	versions, err := h.KeychainService.ListSecretNames(r.Context(), userID)
	if err != nil {
		writeJSON(w, http.StatusOK, keychainStatusResponse{
			Initialized: true,
			KeyVersion:  kc.KeyVersion,
		})
		return
	}

	count := 0
	for _, v := range versions {
		if v.Name != "__master_key__" {
			count++
		}
	}

	writeJSON(w, http.StatusOK, keychainStatusResponse{
		Initialized: true,
		KeyVersion:  kc.KeyVersion,
		SecretCount: count,
	})
}

// PutKeychainSecret stores a secret in the user's keychain.
// PUT /api/v1/keychain/secrets
func (h *Handler) PutKeychainSecret(w http.ResponseWriter, r *http.Request) {
	if h.KeychainService == nil {
		writeError(w, http.StatusServiceUnavailable, "keychain service not configured")
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req keychainPutSecretRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.Name == "" || req.Value == "" {
		writeError(w, http.StatusBadRequest, "name and value are required")
		return
	}

	category := req.Category
	if category == "" {
		category = "custom"
	}
	metadata := req.Metadata
	if metadata == "" {
		metadata = "{}"
	}

	if err := h.KeychainService.PutSecret(r.Context(), userID, req.Name, category, []byte(req.Value), metadata); err != nil {
		slog.Warn("keychain put secret failed", "user_id", userID, "name", req.Name, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to store secret")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "stored"})
}

// GetKeychainSecret retrieves a decrypted secret from the user's keychain.
// GET /api/v1/keychain/secrets/{name}
func (h *Handler) GetKeychainSecret(w http.ResponseWriter, r *http.Request) {
	if h.KeychainService == nil {
		writeError(w, http.StatusServiceUnavailable, "keychain service not configured")
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name query parameter is required")
		return
	}

	plaintext, err := h.KeychainService.GetSecret(r.Context(), userID, name)
	if err != nil {
		writeError(w, http.StatusNotFound, "secret not found")
		return
	}

	writeJSON(w, http.StatusOK, keychainSecretResponse{
		Name:  name,
		Value: string(plaintext),
	})
}

// ListKeychainSecrets returns metadata for all of the user's secrets.
// GET /api/v1/keychain/secrets
func (h *Handler) ListKeychainSecrets(w http.ResponseWriter, r *http.Request) {
	if h.KeychainService == nil {
		writeError(w, http.StatusServiceUnavailable, "keychain service not configured")
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	versions, err := h.KeychainService.ListSecretNames(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list secrets")
		return
	}

	entries := make([]keychainSecretListEntry, 0, len(versions))
	for _, v := range versions {
		if v.Name == "__master_key__" {
			continue
		}
		entries = append(entries, keychainSecretListEntry{
			Name:      v.Name,
			Version:   v.Version,
			Category:  v.Category,
			UpdatedAt: v.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	writeJSON(w, http.StatusOK, entries)
}

// DeleteKeychainSecret removes a secret from the user's keychain.
// DELETE /api/v1/keychain/secrets
func (h *Handler) DeleteKeychainSecret(w http.ResponseWriter, r *http.Request) {
	if h.KeychainService == nil {
		writeError(w, http.StatusServiceUnavailable, "keychain service not configured")
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, http.StatusBadRequest, "name query parameter is required")
		return
	}

	if err := h.KeychainService.DeleteSecret(r.Context(), userID, name); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete secret")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// RotateKeychainKeys triggers a key rotation for the user's keychain.
// POST /api/v1/keychain/rotate
func (h *Handler) RotateKeychainKeys(w http.ResponseWriter, r *http.Request) {
	if h.KeychainService == nil {
		writeError(w, http.StatusServiceUnavailable, "keychain service not configured")
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	if err := h.KeychainService.RotateKeys(r.Context(), userID); err != nil {
		slog.Warn("keychain key rotation failed", "user_id", userID, "error", err)
		writeError(w, http.StatusInternalServerError, "key rotation failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "rotated"})
}

// RecoverKeychain recovers a keychain using the BIP39 recovery phrase.
// POST /api/v1/keychain/recover
func (h *Handler) RecoverKeychain(w http.ResponseWriter, r *http.Request) {
	if h.KeychainService == nil {
		writeError(w, http.StatusServiceUnavailable, "keychain service not configured")
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req keychainRecoverRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if req.RecoveryPhrase == "" {
		writeError(w, http.StatusBadRequest, "recovery_phrase is required")
		return
	}

	if err := h.KeychainService.RecoverFromPhrase(r.Context(), userID, req.RecoveryPhrase); err != nil {
		slog.Warn("keychain recovery failed", "user_id", userID, "error", err)
		writeError(w, http.StatusForbidden, "recovery failed — phrase may be incorrect")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "recovered"})
}

// ── Audit Log ───────────────────────────────────────────────────────────────

type keychainAuditLogEntry struct {
	ID         string `json:"id"`
	Action     string `json:"action"`
	SecretName string `json:"secret_name,omitempty"`
	IPAddr     string `json:"ip_addr,omitempty"`
	UserAgent  string `json:"user_agent,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// ListKeychainAuditLog returns recent audit events for the user's keychain.
// GET /api/v1/keychain/audit
func (h *Handler) ListKeychainAuditLog(w http.ResponseWriter, r *http.Request) {
	if h.KeychainService == nil {
		writeError(w, http.StatusServiceUnavailable, "keychain service not configured")
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	limit := 50
	if ls := r.URL.Query().Get("limit"); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil && n > 0 {
			limit = n
		}
	}

	entries, err := h.KeychainService.ListAuditLog(r.Context(), userID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list audit log")
		return
	}

	out := make([]keychainAuditLogEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, keychainAuditLogEntry{
			ID:         e.ID.String(),
			Action:     e.Action,
			SecretName: e.SecretName,
			IPAddr:     e.IPAddr,
			UserAgent:  e.UserAgent,
			CreatedAt:  e.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	writeJSON(w, http.StatusOK, out)
}