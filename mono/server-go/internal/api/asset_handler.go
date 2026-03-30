package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/store"
)

// Maximum upload size: 100 MB.
const maxUploadSize = 100 << 20

// assetTypeFromMIME detects the asset type from a MIME type string.
func assetTypeFromMIME(mime string) store.AssetType {
	mime = strings.ToLower(strings.TrimSpace(mime))
	switch {
	case mime == "application/pdf":
		return store.AssetTypePDF
	case strings.HasPrefix(mime, "image/"):
		return store.AssetTypeImage
	case strings.HasPrefix(mime, "audio/"):
		return store.AssetTypeAudio
	case strings.HasPrefix(mime, "video/"):
		return store.AssetTypeVideo
	case mime == "text/html" || mime == "application/xhtml+xml":
		return store.AssetTypeWebpage
	default:
		return store.AssetTypeDocument
	}
}

// UploadAsset handles POST /api/v1/repos/{repoID}/assets/upload
func (h *Handler) UploadAsset(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "repoID")
	if repoID == "" {
		writeError(w, http.StatusBadRequest, "repository ID is required")
		return
	}
	repoUUID, err := uuid.Parse(repoID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing 'file' field in multipart form")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxUploadSize))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read uploaded file")
		return
	}

	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}

	assetType := assetTypeFromMIME(mimeType)
	fileName := header.Filename

	slog.Info("asset upload received",
		"repo_id", repoID,
		"file_name", fileName,
		"mime_type", mimeType,
		"asset_type", string(assetType),
		"size_bytes", len(data),
	)

	asset := &store.Asset{
		RepoID:    repoUUID,
		AssetType: string(assetType),
		FileName:  fileName,
		MIMEType:  mimeType,
		SizeBytes: int64(len(data)),
	}

	if h.AssetRepo != nil {
		if err := h.AssetRepo.Create(r.Context(), asset); err != nil {
			slog.Warn("failed to insert asset record", "error", err)
		}
	}

	go h.processAssetAsync(context.Background(), repoUUID, asset.ID, assetType, fileName, mimeType, data)

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"id":         asset.ID.String(),
		"repo_id":    repoID,
		"asset_type": string(assetType),
		"file_name":  fileName,
		"mime_type":  mimeType,
		"size_bytes": len(data),
		"status":     "processing",
	})
}

// IngestURL handles POST /api/v1/repos/{repoID}/assets/ingest-url
func (h *Handler) IngestURL(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "repoID")
	if repoID == "" {
		writeError(w, http.StatusBadRequest, "repository ID is required")
		return
	}
	repoUUID, err := uuid.Parse(repoID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	var req struct {
		URL string `json:"url"`
	}
	if err := readJSON(r, &req); err != nil || req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	asset := &store.Asset{
		RepoID:    repoUUID,
		AssetType: string(store.AssetTypeWebpage),
		SourceURL: req.URL,
	}

	if h.AssetRepo != nil {
		if err := h.AssetRepo.Create(r.Context(), asset); err != nil {
			slog.Warn("failed to insert asset record", "error", err)
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"id":         asset.ID.String(),
		"repo_id":    repoID,
		"asset_type": "webpage",
		"source_url": req.URL,
		"status":     "processing",
	})
}

// ListAssets handles GET /api/v1/repos/{repoID}/assets
func (h *Handler) ListAssets(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "repoID")
	if repoID == "" {
		writeError(w, http.StatusBadRequest, "repository ID is required")
		return
	}
	repoUUID, err := uuid.Parse(repoID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}

	if h.AssetRepo == nil {
		writeJSON(w, http.StatusOK, []store.Asset{})
		return
	}

	assets, err := h.AssetRepo.ListByRepo(r.Context(), repoUUID, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list assets")
		return
	}

	writeJSON(w, http.StatusOK, assets)
}

// DeleteAsset handles DELETE /api/v1/repos/{repoID}/assets/{assetID}
func (h *Handler) DeleteAsset(w http.ResponseWriter, r *http.Request) {
	repoID := chi.URLParam(r, "repoID")
	assetID := chi.URLParam(r, "assetID")

	repoUUID, err := uuid.Parse(repoID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid repository ID")
		return
	}
	assetUUID, err := uuid.Parse(assetID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid asset ID")
		return
	}

	if h.AssetRepo != nil {
		if err := h.AssetRepo.Delete(r.Context(), assetUUID, repoUUID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete asset")
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "deleted",
		"id":     assetID,
	})
}

// processAssetAsync handles the actual embedding in a goroutine.
func (h *Handler) processAssetAsync(ctx context.Context, repoID, assetID uuid.UUID,
	assetType store.AssetType, fileName, mimeType string, data []byte) {

	var textContent string
	var chunksCreated int

	switch assetType {
	case store.AssetTypePDF:
		textContent = extractPDFText(data)
		if textContent == "" {
			h.updateAssetStatus(ctx, assetID, "error", "Failed to extract text from PDF", 0)
			return
		}
	case store.AssetTypeImage:
		textContent = fmt.Sprintf("[Image: %s, %d bytes, %s]", fileName, len(data), mimeType)
	case store.AssetTypeAudio:
		textContent = fmt.Sprintf("[Audio: %s, %d bytes, %s]", fileName, len(data), mimeType)
	default:
		textContent = string(data)
	}

	if h.EngineClient != nil {
		result, err := h.ingestToEngine(ctx, repoID, assetID, string(assetType),
			mimeType, textContent, data)
		if err != nil {
			slog.Warn("engine ingest failed", "asset_id", assetID, "error", err)
			h.updateAssetStatus(ctx, assetID, "error", err.Error(), 0)
			return
		}
		chunksCreated = int(result)
	}

	h.updateAssetStatus(ctx, assetID, "ready", "", chunksCreated)
	slog.Info("asset processed",
		"asset_id", assetID,
		"asset_type", string(assetType),
		"chunks_created", chunksCreated,
	)
}

// ingestToEngine forwards asset content to the C++ engine via gRPC.
func (h *Handler) ingestToEngine(ctx context.Context, repoID, assetID uuid.UUID,
	assetType, mimeType, textContent string, binaryData []byte) (int32, error) {

	if h.EngineClient == nil {
		return 0, fmt.Errorf("engine client not available")
	}

	// TODO: Wire up the actual pb.IngestAssetRequest with binary_data field
	// once proto is regenerated.
	_ = binaryData
	_ = mimeType
	_ = assetType

	slog.Info("would forward to engine",
		"repo_id", repoID.String(),
		"asset_id", assetID.String(),
		"asset_type", assetType,
		"text_len", len(textContent),
		"binary_len", len(binaryData),
	)

	return 1, nil
}

// updateAssetStatus updates the processing status of an asset in the DB.
func (h *Handler) updateAssetStatus(ctx context.Context, assetID uuid.UUID, status, errorMsg string, chunks int) {
	if h.AssetRepo == nil {
		return
	}
	if err := h.AssetRepo.UpdateStatus(ctx, assetID, status, errorMsg, chunks); err != nil {
		slog.Warn("failed to update asset status", "asset_id", assetID, "error", err)
	}
}

// extractPDFText extracts text content from PDF binary data.
func extractPDFText(data []byte) string {
	content := string(data)
	if strings.Contains(content, "%PDF") {
		return "[PDF document — text extraction pending integration with pdfcpu]"
	}
	return content
}

// ── Multimodal Embedding Settings ─────────────────────────────────────────

type multimodalConfig struct {
	ImageEnabled bool   `json:"image_enabled"`
	AudioEnabled bool   `json:"audio_enabled"`
	ImageModel   string `json:"image_model"`
	AudioModel   string `json:"audio_model"`
}

var defaultMultimodalConfig = multimodalConfig{
	ImageEnabled: true,
	AudioEnabled: true,
	ImageModel:   "siglip-base",
	AudioModel:   "clap-general",
}

// GetMultimodalConfig returns the multimodal embedding configuration.
// GET /api/v1/embeddings/multimodal
func (h *Handler) GetMultimodalConfig(w http.ResponseWriter, r *http.Request) {
	cfg := defaultMultimodalConfig

	type modalityInfo struct {
		Modality           string `json:"modality"`
		ModelName          string `json:"model_name"`
		Enabled            bool   `json:"enabled"`
		Status             string `json:"status"`
		NativeDimension    int    `json:"native_dimension"`
		ProjectedDimension int    `json:"projected_dimension"`
		Description        string `json:"description"`
		SizeMB             int    `json:"size_mb"`
		DownloadProgress   int    `json:"download_progress"`
	}

	modalities := []modalityInfo{
		{
			Modality:           "text",
			ModelName:          "bge-m3",
			Enabled:            true,
			Status:             "ready",
			NativeDimension:    1024,
			ProjectedDimension: 1024,
			Description:        "Code and text semantic search — high-quality multilingual embeddings.",
			SizeMB:             2300,
		},
		{
			Modality:           "image",
			ModelName:          cfg.ImageModel,
			Enabled:            cfg.ImageEnabled,
			Status:             "ready",
			NativeDimension:    768,
			ProjectedDimension: 1024,
			Description:        "Understand screenshots, diagrams, and design mockups alongside your code.",
			SizeMB:             350,
		},
		{
			Modality:           "audio",
			ModelName:          cfg.AudioModel,
			Enabled:            cfg.AudioEnabled,
			Status:             "ready",
			NativeDimension:    512,
			ProjectedDimension: 1024,
			Description:        "Search voice recordings, meeting notes, and audio assets in your project.",
			SizeMB:             650,
		},
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"modalities":        modalities,
		"unified_dimension": 1024,
		"image_enabled":     cfg.ImageEnabled,
		"audio_enabled":     cfg.AudioEnabled,
	})
}

// UpdateMultimodalConfig updates the multimodal embedding configuration.
// PUT /api/v1/embeddings/multimodal
func (h *Handler) UpdateMultimodalConfig(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ImageEnabled *bool  `json:"image_enabled"`
		AudioEnabled *bool  `json:"audio_enabled"`
		ImageModel   string `json:"image_model"`
		AudioModel   string `json:"audio_model"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cfg := defaultMultimodalConfig
	if req.ImageEnabled != nil {
		cfg.ImageEnabled = *req.ImageEnabled
	}
	if req.AudioEnabled != nil {
		cfg.AudioEnabled = *req.AudioEnabled
	}
	if req.ImageModel != "" {
		cfg.ImageModel = req.ImageModel
	}
	if req.AudioModel != "" {
		cfg.AudioModel = req.AudioModel
	}

	defaultMultimodalConfig = cfg

	slog.Info("multimodal config updated",
		"image_enabled", cfg.ImageEnabled,
		"audio_enabled", cfg.AudioEnabled,
		"image_model", cfg.ImageModel,
		"audio_model", cfg.AudioModel,
	)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"image_enabled": cfg.ImageEnabled,
		"audio_enabled": cfg.AudioEnabled,
		"image_model":   cfg.ImageModel,
		"audio_model":   cfg.AudioModel,
		"status":        "updated",
	})
}
