package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/AuralithAI/rtvortex-server/internal/llm"
)

// StreamLLMCompletion handles SSE streaming of LLM responses.
// POST /api/v1/llm/stream
//
// The request body is a standard CompletionRequest. The response is a stream
// of Server-Sent Events where each event contains a JSON StreamChunk.
//
//	data: {"content":"Hello","done":false}
//	data: {"content":" world","done":false}
//	data: {"content":"","done":true,"finish_reason":"stop","usage":{...}}
func (h *Handler) StreamLLMCompletion(w http.ResponseWriter, r *http.Request) {
	var req llm.CompletionRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages array is required")
		return
	}

	// Get the streaming channel from the registry (falls back to non-streaming).
	ch, err := h.LLMRegistry.StreamComplete(r.Context(), &req)
	if err != nil {
		slog.Error("stream complete failed", "error", err)
		writeError(w, http.StatusBadGateway, "LLM streaming error: "+err.Error())
		return
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	for chunk := range ch {
		data, err := json.Marshal(chunk)
		if err != nil {
			slog.Error("failed to marshal stream chunk", "error", err)
			continue
		}

		_, writeErr := fmt.Fprintf(w, "data: %s\n\n", data)
		if writeErr != nil {
			// Client disconnected.
			slog.Debug("SSE client disconnected", "error", writeErr)
			return
		}
		flusher.Flush()
	}

	// Send final SSE event to signal end of stream.
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
