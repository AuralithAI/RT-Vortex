package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/auth"
	"github.com/AuralithAI/rtvortex-server/internal/chat"
	"github.com/AuralithAI/rtvortex-server/internal/model"
)

// ── Chat Session Endpoints ──────────────────────────────────────────────────

// CreateChatSession creates a new chat session for a repository.
// POST /api/v1/repos/{repoID}/chat/sessions
func (h *Handler) CreateChatSession(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var body struct {
		Title    string `json:"title"`
		Model    string `json:"model"`
		Provider string `json:"provider"`
	}
	if err := readJSON(r, &body); err != nil {
		// Body is optional — defaults are fine.
		body.Title = ""
	}

	title := body.Title
	if title == "" {
		title = "New Chat"
	}

	session := &model.ChatSession{
		RepoID:   repoID,
		UserID:   userID,
		Title:    title,
		Model:    body.Model,
		Provider: body.Provider,
	}

	if err := h.ChatRepo.CreateSession(r.Context(), session); err != nil {
		slog.Error("failed to create chat session", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create chat session")
		return
	}

	writeJSON(w, http.StatusCreated, session)
}

// ListChatSessions returns all chat sessions for a repo + user.
// GET /api/v1/repos/{repoID}/chat/sessions
func (h *Handler) ListChatSessions(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	sessions, total, err := h.ChatRepo.ListSessions(r.Context(), repoID, userID, limit, offset)
	if err != nil {
		slog.Error("failed to list chat sessions", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list chat sessions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": sessions,
		"count":    len(sessions),
		"total":    total,
	})
}

// GetChatSession returns a single chat session.
// GET /api/v1/repos/{repoID}/chat/sessions/{sessionID}
func (h *Handler) GetChatSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	session, err := h.ChatRepo.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	// Verify ownership.
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok || session.UserID != userID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	writeJSON(w, http.StatusOK, session)
}

// UpdateChatSession updates a chat session (title).
// PUT /api/v1/repos/{repoID}/chat/sessions/{sessionID}
func (h *Handler) UpdateChatSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	// Verify ownership.
	session, err := h.ChatRepo.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok || session.UserID != userID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	var body struct {
		Title string `json:"title"`
	}
	if err := readJSON(r, &body); err != nil || body.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}

	if err := h.ChatRepo.UpdateSessionTitle(r.Context(), sessionID, body.Title); err != nil {
		slog.Error("failed to update chat session", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update session")
		return
	}

	session.Title = body.Title
	writeJSON(w, http.StatusOK, session)
}

// DeleteChatSession deletes a chat session and all its messages.
// DELETE /api/v1/repos/{repoID}/chat/sessions/{sessionID}
func (h *Handler) DeleteChatSession(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	// Verify ownership.
	session, err := h.ChatRepo.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok || session.UserID != userID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	if err := h.ChatRepo.DeleteSession(r.Context(), sessionID); err != nil {
		slog.Error("failed to delete chat session", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ── Chat Message Endpoints ──────────────────────────────────────────────────

// ListChatMessages returns messages in a chat session (paginated).
// GET /api/v1/repos/{repoID}/chat/sessions/{sessionID}/messages
func (h *Handler) ListChatMessages(w http.ResponseWriter, r *http.Request) {
	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	// Verify ownership.
	session, err := h.ChatRepo.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok || session.UserID != userID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	messages, total, err := h.ChatRepo.ListMessages(r.Context(), sessionID, limit, offset)
	if err != nil {
		slog.Error("failed to list chat messages", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list messages")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"messages": messages,
		"count":    len(messages),
		"total":    total,
	})
}

// SendChatMessage sends a message and streams the AI response via SSE.
// POST /api/v1/repos/{repoID}/chat/sessions/{sessionID}/messages
func (h *Handler) SendChatMessage(w http.ResponseWriter, r *http.Request) {
	repoID, err := uuid.Parse(chi.URLParam(r, "repoID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	sessionID, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid session ID")
		return
	}

	// Verify ownership.
	session, err := h.ChatRepo.GetSession(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok || session.UserID != userID {
		writeError(w, http.StatusForbidden, "access denied")
		return
	}

	var body struct {
		Content     string                 `json:"content"`
		Attachments []model.ChatAttachment `json:"attachments,omitempty"`
	}
	if err := readJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if body.Content == "" {
		writeError(w, http.StatusBadRequest, "message content is required")
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		slog.Error("streaming not supported by response writer")
		return
	}

	// Build request.
	req := chat.SendMessageRequest{
		SessionID:   sessionID,
		RepoID:      repoID,
		UserID:      userID,
		Content:     body.Content,
		Attachments: body.Attachments,
	}

	// Use a timeout context for long-running requests.
	ctx := r.Context()
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	// Stream events via SSE.
	_, err = h.ChatService.SendMessage(timeoutCtx, req, func(event model.ChatStreamEvent) {
		data, err := json.Marshal(event)
		if err != nil {
			slog.Error("failed to marshal chat event", "error", err)
			return
		}

		_, writeErr := fmt.Fprintf(w, "data: %s\n\n", data)
		if writeErr != nil {
			slog.Debug("SSE client disconnected", "error", writeErr)
			return
		}
		flusher.Flush()
	})

	if err != nil {
		slog.Error("chat message processing failed", "error", err)
		// Try to send error event (client may have disconnected).
		errEvent, _ := json.Marshal(model.ChatStreamEvent{Type: "error", Error: err.Error()})
		fmt.Fprintf(w, "data: %s\n\n", errEvent)
		flusher.Flush()
	}

	// Signal end of stream.
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
