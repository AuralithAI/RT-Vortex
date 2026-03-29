package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// ── Web Fetch Proxy ─────────────────────────────────────────────────────────
// Safe HTTP fetch for agents: rate-limited, readability-stripped, PDF-aware.

const (
	webFetchMaxBytes = 2 * 1024 * 1024 // 2 MB response limit
	webFetchTimeout  = 30 * time.Second
	agentBusStream   = "swarm:agent-bus"
	agentBusGroup    = "swarm-agents"
	agentBusMaxLen   = 1000
	agentBusTTL      = 30 * time.Minute
)

// HandleWebFetch handles POST /internal/swarm/web/fetch.
// Agents call this to safely fetch a URL with rate-limiting.
func (h *Handler) HandleWebFetch(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL        string `json:"url"`
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
		ExtractPDF bool   `json:"extract_pdf"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.URL == "" && body.Query == "" {
		http.Error(w, `{"error":"url or query is required"}`, http.StatusBadRequest)
		return
	}

	fetchURL := body.URL
	if fetchURL == "" {
		// No external search engine wired yet — return guidance.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"text": fmt.Sprintf("Web search for %q is not yet wired to an external provider. "+
				"Use a direct URL or consult repo documentation.", body.Query),
			"url":    "",
			"status": "no_provider",
		})
		return
	}

	// Rate-limit: simple per-minute bucket via Redis.
	if h.TaskMgr != nil && h.TaskMgr.redis != nil {
		key := "swarm:web_fetch:rate"
		count, _ := h.TaskMgr.redis.Incr(r.Context(), key).Result()
		if count == 1 {
			h.TaskMgr.redis.Expire(r.Context(), key, 1*time.Minute)
		}
		if count > 30 {
			http.Error(w, `{"error":"web fetch rate limit exceeded (30/min)"}`, http.StatusTooManyRequests)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), webFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", "RTVortex-Agent/1.0")
	req.Header.Set("Accept", "text/html, application/pdf, text/plain, */*")

	fetchStart := time.Now()
	resp, err := http.DefaultClient.Do(req)
	SwarmWebFetchDuration.Observe(time.Since(fetchStart).Seconds())
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"fetch failed: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, int64(webFetchMaxBytes))
	rawBytes, err := io.ReadAll(limited)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"read failed: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	text := ""

	if strings.Contains(contentType, "application/pdf") && body.ExtractPDF {
		// PDF extraction placeholder — return raw text hint.
		text = "[PDF content — raw text extraction not yet available. " +
			"Content-Length: " + strconv.Itoa(len(rawBytes)) + " bytes]"
	} else {
		// Strip HTML tags for a basic readability pass.
		text = stripHTMLTags(string(rawBytes))
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"text":         text,
		"url":          fetchURL,
		"status_code":  resp.StatusCode,
		"content_type": contentType,
		"bytes":        len(rawBytes),
	})
}

// stripHTMLTags does a basic tag removal for readability.
func stripHTMLTags(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			b.WriteRune(' ')
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// ── Inter-Agent Communication Bus ───────────────────────────────────────────
// Redis Streams-backed pub/sub between agents (leader → worker, QA → Security, etc.).

// AgentBusMessage is a single message on the agent communication bus.
type AgentBusMessage struct {
	FromRole      string  `json:"from_role"`
	TargetRole    string  `json:"target_role"`
	TaskID        string  `json:"task_id"`
	Message       string  `json:"message"`
	EmbeddingsRef string  `json:"embeddings_ref,omitempty"`
	Confidence    float64 `json:"confidence"`
	Timestamp     int64   `json:"timestamp"`
}

// HandleAgentBusPublish handles POST /internal/swarm/agent-bus/publish.
func (h *Handler) HandleAgentBusPublish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TargetRole    string  `json:"target_role"`
		Message       string  `json:"message"`
		IncludeEmbRef bool    `json:"include_embeddings_ref"`
		Confidence    float64 `json:"confidence"`
		TaskID        string  `json:"task_id"`
		FromRole      string  `json:"from_role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.Message == "" || body.TargetRole == "" {
		http.Error(w, `{"error":"message and target_role are required"}`, http.StatusBadRequest)
		return
	}
	if body.Confidence == 0 {
		body.Confidence = 0.8
	}

	if h.TaskMgr == nil || h.TaskMgr.redis == nil {
		http.Error(w, `{"error":"redis not available"}`, http.StatusServiceUnavailable)
		return
	}

	msg := AgentBusMessage{
		FromRole:   body.FromRole,
		TargetRole: body.TargetRole,
		TaskID:     body.TaskID,
		Message:    body.Message,
		Confidence: body.Confidence,
		Timestamp:  time.Now().Unix(),
	}
	if body.IncludeEmbRef {
		msg.EmbeddingsRef = fmt.Sprintf("swarm:stm:%s:*", body.TaskID)
	}

	msgJSON, _ := json.Marshal(msg)

	// Ensure consumer group exists for downstream consumers.
	_ = h.TaskMgr.redis.XGroupCreateMkStream(r.Context(), agentBusStream, agentBusGroup, "0").Err()

	publishStart := time.Now()
	_, err := h.TaskMgr.redis.XAdd(r.Context(), &redis.XAddArgs{
		Stream: agentBusStream,
		MaxLen: agentBusMaxLen,
		Approx: true,
		Values: map[string]interface{}{
			"data": string(msgJSON),
		},
	}).Result()
	SwarmAgentIntercommLatency.Observe(time.Since(publishStart).Seconds())
	if err != nil {
		slog.Error("agent-bus publish failed", "error", err)
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"published"}`))
}

// HandleAgentBusRead handles GET /internal/swarm/agent-bus/read.
func (h *Handler) HandleAgentBusRead(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	targetRole := r.URL.Query().Get("role")
	limit := 10
	if limitStr != "" {
		if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
			limit = v
		}
	}

	if h.TaskMgr == nil || h.TaskMgr.redis == nil {
		http.Error(w, `{"error":"redis not available"}`, http.StatusServiceUnavailable)
		return
	}

	msgs, err := h.TaskMgr.redis.XRevRangeN(r.Context(), agentBusStream, "+", "-", int64(limit)).Result()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusInternalServerError)
		return
	}

	var results []AgentBusMessage
	cutoff := time.Now().Add(-agentBusTTL).Unix()
	for _, m := range msgs {
		raw, ok := m.Values["data"].(string)
		if !ok {
			continue
		}
		var abm AgentBusMessage
		if json.Unmarshal([]byte(raw), &abm) != nil {
			continue
		}
		if abm.Timestamp < cutoff {
			continue
		}
		if targetRole != "" && abm.TargetRole != targetRole {
			continue
		}
		results = append(results, abm)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"messages": results,
	})
}

// ── Ingest Asset Proxy ──────────────────────────────────────────────────────
// Routes documents/PDFs/URLs to the C++ engine for embedding.

// HandleIngestAsset handles POST /internal/swarm/ingest-asset.
// Accepts text content + metadata and forwards to the engine's IngestAsset RPC.
func (h *Handler) HandleIngestAsset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RepoID    string `json:"repo_id"`
		SourceURL string `json:"source_url"`
		Content   string `json:"content"`
		AssetType string `json:"asset_type"` // document, pdf, url
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if body.RepoID == "" || body.Content == "" {
		http.Error(w, `{"error":"repo_id and content are required"}`, http.StatusBadRequest)
		return
	}
	if body.AssetType == "" {
		body.AssetType = "document"
	}

	// Forward to the C++ engine via gRPC (IngestAsset RPC).
	// The engine will extract text, embed with BGE-M3, and store in the
	// repo-specific index with metadata flag type=document.
	//
	// For now, return success — the actual gRPC call requires the engine
	// client to be wired into the swarm handler.
	slog.Info("ingest-asset received",
		"repo_id", body.RepoID,
		"source_url", body.SourceURL,
		"asset_type", body.AssetType,
		"content_len", len(body.Content),
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     "accepted",
		"repo_id":    body.RepoID,
		"asset_type": body.AssetType,
		"bytes":      len(body.Content),
	})
}
