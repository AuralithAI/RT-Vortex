package api

import (
	"context"
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/store"
)

// Maximum upload / download size: 100 MB.
const maxUploadSize = 100 << 20

// Maximum text batch size sent in a single gRPC call (50 KB).
// The C++ engine chunks each batch further (~1500 chars), so 50 KB ≈ 33 chunks
// per RPC — well within safe memory bounds even with 3 ONNX models loaded.
const maxBatchBytes = 50 * 1024

// assetDataDir is the directory where uploaded asset binaries are persisted
// so they can be served back for previews (thumbnails, audio players, etc.).
var assetDataDir = filepath.Join(os.Getenv("RT_HOME"), "data", "assets")

func init() {
	if d := os.Getenv("RT_HOME"); d == "" {
		assetDataDir = filepath.Join(".", "data", "assets")
	}
	_ = os.MkdirAll(assetDataDir, 0o755)
}

// saveAssetFile persists the raw binary to disk so it can be served later.
func saveAssetFile(assetID uuid.UUID, data []byte) {
	fp := filepath.Join(assetDataDir, assetID.String())
	if err := os.WriteFile(fp, data, 0o644); err != nil {
		slog.Warn("failed to persist asset file", "asset_id", assetID, "error", err)
	}
}

// generateThumbnailDataURI creates a small JPEG data URI from image bytes.
// Returns empty string on failure. Max 200×200 px.
func generateThumbnailDataURI(data []byte) string {
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}

	// Compute thumbnail dimensions (max 200px, preserve aspect ratio).
	bounds := src.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	maxDim := 200
	if w <= maxDim && h <= maxDim {
		// Already small enough — encode directly.
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 70}); err != nil {
			return ""
		}
		return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	}

	if w >= h {
		h = h * maxDim / w
		w = maxDim
	} else {
		w = w * maxDim / h
		h = maxDim
	}

	// Simple nearest-neighbor resize using SubImage sampling.
	thumb := image.NewRGBA(image.Rect(0, 0, w, h))
	srcW, srcH := bounds.Dx(), bounds.Dy()
	for y := 0; y < h; y++ {
		srcY := bounds.Min.Y + y*srcH/h
		for x := 0; x < w; x++ {
			srcX := bounds.Min.X + x*srcW/w
			thumb.Set(x, y, src.At(srcX, srcY))
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: 70}); err != nil {
		return ""
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// ── HTML text extraction ────────────────────────────────────────────────────

var (
	reHTMLTag     = regexp.MustCompile(`<[^>]*>`)
	reHTMLComment = regexp.MustCompile(`<!--[\s\S]*?-->`)
	reScript      = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle       = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reWhitespace  = regexp.MustCompile(`\s{3,}`)
)

// stripHTML extracts readable text from HTML content by removing scripts,
// styles, tags, and excess whitespace.  Good enough for indexing — we don't
// need a full DOM parser.
func stripHTML(html string) string {
	s := reScript.ReplaceAllString(html, " ")
	s = reStyle.ReplaceAllString(s, " ")
	s = reHTMLComment.ReplaceAllString(s, " ")
	s = reHTMLTag.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	s = strings.ReplaceAll(s, "&#39;", "'")
	s = reWhitespace.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

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

	// Persist binary to disk so we can serve it for previews.
	go saveAssetFile(asset.ID, data)

	// For images, generate a base64 thumbnail and store in metadata.
	var thumbnailURI string
	if assetType == store.AssetTypeImage {
		thumbnailURI = generateThumbnailDataURI(data)
		if thumbnailURI != "" && h.AssetRepo != nil {
			_ = h.AssetRepo.UpdateMetadata(context.Background(), asset.ID,
				fmt.Sprintf(`{"thumbnail":"%s"}`, thumbnailURI))
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"id":         asset.ID.String(),
		"repo_id":    repoID,
		"asset_type": string(assetType),
		"file_name":  fileName,
		"mime_type":  mimeType,
		"size_bytes": len(data),
		"status":     "processing",
		"thumbnail":  thumbnailURI,
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

	slog.Info("url ingest queued",
		"repo_id", repoID,
		"asset_id", asset.ID.String(),
		"url", req.URL,
	)

	// Fetch the URL content and process in the background.
	go h.fetchAndProcessURL(context.Background(), repoUUID, asset.ID, req.URL)

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"id":         asset.ID.String(),
		"repo_id":    repoID,
		"asset_type": "webpage",
		"source_url": req.URL,
		"status":     "processing",
	})
}

// fetchAndProcessURL downloads content from a URL, detects its type, updates
// the asset record with real metadata, and forwards it for embedding.
func (h *Handler) fetchAndProcessURL(ctx context.Context, repoID, assetID uuid.UUID, rawURL string) {
	// ── 1. HTTP GET with timeout ───────────────────────────────────────────
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(rawURL)
	if err != nil {
		slog.Warn("url fetch failed", "url", rawURL, "error", err)
		h.updateAssetStatus(ctx, assetID, "error", fmt.Sprintf("fetch failed: %v", err), 0)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		msg := fmt.Sprintf("HTTP %d from %s", resp.StatusCode, rawURL)
		slog.Warn("url fetch non-200", "url", rawURL, "status", resp.StatusCode)
		h.updateAssetStatus(ctx, assetID, "error", msg, 0)
		return
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxUploadSize))
	if err != nil {
		slog.Warn("url read failed", "url", rawURL, "error", err)
		h.updateAssetStatus(ctx, assetID, "error", fmt.Sprintf("read failed: %v", err), 0)
		return
	}

	if len(data) == 0 {
		h.updateAssetStatus(ctx, assetID, "error", "empty response from URL", 0)
		return
	}

	// ── 2. Detect MIME type and derive asset type ──────────────────────────
	mimeType := resp.Header.Get("Content-Type")
	if mimeType == "" || mimeType == "application/octet-stream" {
		mimeType = http.DetectContentType(data)
	}
	// Strip charset / params: "text/html; charset=utf-8" → "text/html"
	if idx := strings.Index(mimeType, ";"); idx > 0 {
		mimeType = strings.TrimSpace(mimeType[:idx])
	}

	assetType := assetTypeFromMIME(mimeType)

	// Derive a filename from the URL path
	fileName := path.Base(rawURL)
	if fileName == "" || fileName == "/" || fileName == "." {
		fileName = "webpage"
	}

	slog.Info("url fetched",
		"url", rawURL,
		"asset_id", assetID.String(),
		"mime_type", mimeType,
		"asset_type", string(assetType),
		"size_bytes", len(data),
	)

	// ── 3. Update DB record with real metadata ─────────────────────────────
	if h.AssetRepo != nil {
		if err := h.AssetRepo.UpdateMeta(ctx, assetID, fileName, mimeType, string(assetType), int64(len(data))); err != nil {
			slog.Warn("failed to update asset meta", "asset_id", assetID, "error", err)
		}
	}

	// ── 4. Persist binary to disk for preview serving ──────────────────────
	go saveAssetFile(assetID, data)

	// ── 5. Forward to the engine for embedding ─────────────────────────────
	h.processAssetAsync(ctx, repoID, assetID, assetType, fileName, mimeType, data)
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

	// Clean up the persisted file from disk.
	go os.Remove(filepath.Join(assetDataDir, assetUUID.String()))

	writeJSON(w, http.StatusOK, map[string]string{
		"status": "deleted",
		"id":     assetID,
	})
}

// ServeAssetContent serves the raw binary of a stored asset from disk,
// allowing the UI to render image thumbnails, audio players, and PDF embeds.
func (h *Handler) ServeAssetContent(w http.ResponseWriter, r *http.Request) {
	assetID := chi.URLParam(r, "assetID")
	assetUUID, err := uuid.Parse(assetID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid asset ID")
		return
	}

	// Look up the asset to get MIME type for the Content-Type header.
	var mimeType string
	if h.AssetRepo != nil {
		asset, err := h.AssetRepo.GetByID(r.Context(), assetUUID)
		if err != nil || asset == nil {
			writeError(w, http.StatusNotFound, "asset not found")
			return
		}
		mimeType = asset.MIMEType
	}

	fp := filepath.Join(assetDataDir, assetUUID.String())
	data, err := os.ReadFile(fp)
	if err != nil {
		writeError(w, http.StatusNotFound, "asset content not available")
		return
	}

	if mimeType == "" {
		mimeType = http.DetectContentType(data)
	}

	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// chunkText splits large text into batches of at most maxBatchBytes,
// breaking at paragraph ("\n\n") or sentence (". ") boundaries when possible.
func chunkText(text string, maxBytes int) []string {
	if len(text) <= maxBytes {
		return []string{text}
	}

	var batches []string
	for len(text) > 0 {
		end := maxBytes
		if end > len(text) {
			end = len(text)
		}

		if end < len(text) {
			// Try paragraph boundary
			if idx := strings.LastIndex(text[:end], "\n\n"); idx > maxBytes/3 {
				end = idx + 2
			} else if idx := strings.LastIndex(text[:end], ". "); idx > maxBytes/3 {
				// Try sentence boundary
				end = idx + 2
			} else if idx := strings.LastIndex(text[:end], "\n"); idx > maxBytes/3 {
				// Try line boundary
				end = idx + 1
			}
		}

		batch := strings.TrimSpace(text[:end])
		if batch != "" {
			batches = append(batches, batch)
		}
		text = text[end:]
	}
	return batches
}

// processAssetAsync handles the actual embedding in a goroutine.
func (h *Handler) processAssetAsync(ctx context.Context, repoID, assetID uuid.UUID,
	assetType store.AssetType, fileName, mimeType string, data []byte) {

	var textContent string
	var binaryPayload []byte // only non-nil for image/audio
	var chunksCreated int

	switch assetType {
	case store.AssetTypePDF:
		textContent = extractPDFText(data)
		if textContent == "" {
			h.updateAssetStatus(ctx, assetID, "error", "Failed to extract text from PDF", 0)
			return
		}

	case store.AssetTypeImage:
		// Image: send as binary for ONNX inference, short text placeholder
		textContent = fmt.Sprintf("[Image: %s, %d bytes, %s]", fileName, len(data), mimeType)
		binaryPayload = data

	case store.AssetTypeAudio:
		// Audio: send as binary for ONNX inference, short text placeholder
		textContent = fmt.Sprintf("[Audio: %s, %d bytes, %s]", fileName, len(data), mimeType)
		binaryPayload = data

	case store.AssetTypeWebpage:
		// Webpage: strip HTML to clean text, don't send binary
		raw := string(data)
		if strings.Contains(mimeType, "html") || strings.Contains(raw[:min(512, len(raw))], "<html") {
			textContent = stripHTML(raw)
		} else {
			textContent = raw
		}
		if textContent == "" {
			h.updateAssetStatus(ctx, assetID, "error", "no readable text extracted from URL", 0)
			return
		}

	default:
		textContent = string(data)
	}

	if h.EngineClient == nil {
		h.updateAssetStatus(ctx, assetID, "error", "engine client not available", 0)
		return
	}

	// ── Binary path (image/audio): single RPC, no batching needed ───────
	if binaryPayload != nil {
		result, err := h.ingestToEngine(ctx, repoID, assetID, string(assetType),
			mimeType, textContent, binaryPayload)
		if err != nil {
			slog.Warn("engine ingest failed", "asset_id", assetID, "error", err)
			h.updateAssetStatus(ctx, assetID, "error", err.Error(), 0)
			return
		}
		chunksCreated = int(result)
		h.updateAssetStatus(ctx, assetID, "ready", "", chunksCreated)
		slog.Info("asset processed (binary)",
			"asset_id", assetID,
			"asset_type", string(assetType),
			"chunks_created", chunksCreated,
		)
		return
	}

	// ── Text path: split into batches and send sequentially ─────────────
	batches := chunkText(textContent, maxBatchBytes)

	slog.Info("processing text asset in batches",
		"asset_id", assetID,
		"asset_type", string(assetType),
		"total_text_len", len(textContent),
		"batches", len(batches),
	)

	for i, batch := range batches {
		result, err := h.ingestToEngine(ctx, repoID, assetID, string(assetType),
			mimeType, batch, nil)
		if err != nil {
			slog.Warn("engine ingest failed for batch",
				"asset_id", assetID,
				"batch", fmt.Sprintf("%d/%d", i+1, len(batches)),
				"error", err,
			)
			h.updateAssetStatus(ctx, assetID, "error",
				fmt.Sprintf("batch %d/%d failed: %s", i+1, len(batches), err.Error()), chunksCreated)
			return
		}
		chunksCreated += int(result)

		slog.Debug("batch ingested",
			"asset_id", assetID,
			"batch", fmt.Sprintf("%d/%d", i+1, len(batches)),
			"batch_len", len(batch),
			"batch_chunks", result,
			"total_chunks", chunksCreated,
		)
	}

	h.updateAssetStatus(ctx, assetID, "ready", "", chunksCreated)
	slog.Info("asset processed (text)",
		"asset_id", assetID,
		"asset_type", string(assetType),
		"text_len", len(textContent),
		"batches", len(batches),
		"chunks_created", chunksCreated,
	)
}

// ingestToEngine forwards asset content to the C++ engine via gRPC.
func (h *Handler) ingestToEngine(ctx context.Context, repoID, assetID uuid.UUID,
	assetType, mimeType, textContent string, binaryData []byte) (int32, error) {

	if h.EngineClient == nil {
		return 0, fmt.Errorf("engine client not available")
	}

	result, err := h.EngineClient.IngestAsset(
		ctx,
		repoID.String(),
		assetType,
		mimeType,
		assetID.String(), // fileName
		"",               // sourceURL
		textContent,
		binaryData,
	)
	if err != nil {
		return 0, fmt.Errorf("engine IngestAsset RPC: %w", err)
	}

	slog.Info("engine ingest completed",
		"repo_id", repoID.String(),
		"asset_id", assetID.String(),
		"asset_type", assetType,
		"chunks_created", result.ChunksCreated,
		"status", result.Status,
	)

	return result.ChunksCreated, nil
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
//
// Queries the C++ engine for live model status; falls back to in-memory defaults
// if the engine is unreachable.
func (h *Handler) GetMultimodalConfig(w http.ResponseWriter, r *http.Request) {
	// Try to get live status from the C++ engine
	if h.EngineClient != nil {
		cfg, err := h.EngineClient.GetMultimodalConfig(r.Context())
		if err == nil {
			// Enrich engine response with description and size metadata
			type modalityInfo struct {
				Modality           string `json:"modality"`
				ModelName          string `json:"model_name"`
				Enabled            bool   `json:"enabled"`
				Status             string `json:"status"`
				NativeDimension    uint32 `json:"native_dimension"`
				ProjectedDimension uint32 `json:"projected_dimension"`
				Description        string `json:"description"`
				SizeMB             int    `json:"size_mb"`
				DownloadProgress   int32  `json:"download_progress"`
			}

			descriptions := map[string]struct {
				desc   string
				sizeMB int
			}{
				"text":  {"Code and text semantic search — high-quality multilingual embeddings.", 2300},
				"image": {"Understand screenshots, diagrams, and design mockups alongside your code.", 350},
				"audio": {"Search voice recordings, meeting notes, and audio assets in your project.", 650},
			}

			var modalities []modalityInfo
			for _, m := range cfg.Modalities {
				info := descriptions[m.Modality]
				modalities = append(modalities, modalityInfo{
					Modality:           m.Modality,
					ModelName:          m.ModelName,
					Enabled:            m.Enabled,
					Status:             m.Status,
					NativeDimension:    m.Dimension,
					ProjectedDimension: m.ProjectedDimension,
					Description:        info.desc,
					SizeMB:             info.sizeMB,
					DownloadProgress:   m.DownloadProgress,
				})
			}

			writeJSON(w, http.StatusOK, map[string]interface{}{
				"modalities":        modalities,
				"unified_dimension": cfg.UnifiedDimension,
				"image_enabled":     cfg.ImageEnabled,
				"audio_enabled":     cfg.AudioEnabled,
				"source":            "engine",
			})
			return
		}
		slog.Warn("failed to get multimodal config from engine, using defaults", "error", err)
	}

	// Fallback: return in-memory defaults
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
			Status:             "unknown",
			NativeDimension:    768,
			ProjectedDimension: 1024,
			Description:        "Understand screenshots, diagrams, and design mockups alongside your code.",
			SizeMB:             350,
		},
		{
			Modality:           "audio",
			ModelName:          cfg.AudioModel,
			Enabled:            cfg.AudioEnabled,
			Status:             "unknown",
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
		"source":            "fallback",
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

	// Forward to C++ engine to enable/disable ONNX sessions
	if h.EngineClient != nil {
		result, err := h.EngineClient.ConfigureMultimodal(r.Context(), cfg.ImageEnabled, cfg.AudioEnabled)
		if err != nil {
			slog.Warn("engine ConfigureMultimodal failed", "error", err)
		} else {
			slog.Info("engine multimodal config updated",
				"success", result.Success,
				"loaded_models", result.LoadedModels,
			)
		}
	}

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
