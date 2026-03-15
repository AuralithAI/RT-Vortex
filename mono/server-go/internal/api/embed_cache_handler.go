package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// GetEmbedCache looks up a cached embedding vector.
// GET /internal/engine/embed-cache/{repoID}/{chunkHash}
func (h *Handler) GetEmbedCache(w http.ResponseWriter, r *http.Request) {
	if h.EmbedCache == nil {
		http.Error(w, "embed cache not configured", http.StatusServiceUnavailable)
		return
	}

	repoID := chi.URLParam(r, "repoID")
	chunkHash := chi.URLParam(r, "chunkHash")

	vec, err := h.EmbedCache.Get(r.Context(), repoID, chunkHash)
	if err != nil {
		http.Error(w, "cache error", http.StatusInternalServerError)
		return
	}
	if vec == nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"embedding": vec,
	})
}

// PutEmbedCache stores an embedding vector.
// PUT /internal/engine/embed-cache/{repoID}/{chunkHash}
func (h *Handler) PutEmbedCache(w http.ResponseWriter, r *http.Request) {
	if h.EmbedCache == nil {
		http.Error(w, "embed cache not configured", http.StatusServiceUnavailable)
		return
	}

	repoID := chi.URLParam(r, "repoID")
	chunkHash := chi.URLParam(r, "chunkHash")

	var body struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	if err := h.EmbedCache.Put(r.Context(), repoID, chunkHash, body.Embedding); err != nil {
		http.Error(w, "cache write error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
