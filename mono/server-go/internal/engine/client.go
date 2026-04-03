// Package engine provides the gRPC client for communicating with the RTVortex C++ engine.
//
// It wraps every RPC defined in engine.proto and converts between Go domain
// types and protobuf messages.
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/AuralithAI/rtvortex-server/internal/engine/pb"
)

// SearchCacheTTL controls how long cached search results stay valid.
const SearchCacheTTL = 60 * time.Second

// ── Client wraps Pool and exposes typed RPC methods ─────────────────────────

// Client is a high-level gRPC client for the RTVortex C++ engine.
// Each public method maps 1:1 to an RPC in engine.proto's EngineService.
type Client struct {
	pool           *Pool
	defaultTimeout time.Duration

	searchCache sync.Map
	searchGroup singleflight.Group
}

type cachedSearch struct {
	result    *SearchResult
	expiresAt time.Time
}

// NewClient creates an engine client backed by the connection pool.
func NewClient(pool *Pool) *Client {
	return &Client{
		pool:           pool,
		defaultTimeout: 120 * time.Second,
	}
}

// stub returns a fresh EngineService client from the pool.
func (c *Client) stub() pb.EngineServiceClient {
	return pb.NewEngineServiceClient(c.pool.GetConn())
}

// ctx returns a context with the default timeout if the parent has no deadline.
func (c *Client) ctx(parent context.Context) (context.Context, context.CancelFunc) {
	if _, ok := parent.Deadline(); ok {
		return parent, func() {}
	}
	return context.WithTimeout(parent, c.defaultTimeout)
}

// ── Health & Diagnostics ────────────────────────────────────────────────────

// HealthStatus is the response from the engine health check.
type HealthStatus struct {
	Healthy             bool
	Version             string
	UptimeSeconds       uint64
	Components          map[string]string
	MetricsEnabled      bool
	ActiveMetricStreams uint32
}

// HealthCheck calls the engine's HealthCheck RPC.
func (c *Client) HealthCheck(ctx context.Context) (*HealthStatus, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()

	resp, err := c.stub().HealthCheck(ctx, &pb.HealthCheckRequest{})
	if err != nil {
		return nil, fmt.Errorf("engine health check: %w", err)
	}
	return &HealthStatus{
		Healthy:             resp.Healthy,
		Version:             resp.Version,
		UptimeSeconds:       resp.UptimeSeconds,
		Components:          resp.Components,
		MetricsEnabled:      resp.MetricsEnabled,
		ActiveMetricStreams: resp.ActiveMetricStreams,
	}, nil
}

// IsHealthy returns true if the engine responds to health check.
func (c *Client) IsHealthy(ctx context.Context) bool {
	hs, err := c.HealthCheck(ctx)
	if err != nil {
		slog.Debug("engine health check failed", "error", err)
		return false
	}
	return hs.Healthy
}

// DiagnosticsResult is the response from the engine diagnostics RPC.
type DiagnosticsResult struct {
	Memory  *MemoryStats
	Indices []IndexInfo
	Config  map[string]string
}

// MemoryStats mirrors pb.MemoryStats.
type MemoryStats struct {
	HeapUsedBytes  uint64
	HeapTotalBytes uint64
	RSSBytes       uint64
}

// IndexInfo mirrors pb.IndexInfo.
type IndexInfo struct {
	RepoID      string
	SizeBytes   uint64
	LastUpdated string
	IsLoaded    bool
}

// GetDiagnostics calls the engine's GetDiagnostics RPC.
func (c *Client) GetDiagnostics(ctx context.Context, includeMemory, includeIndices bool) (*DiagnosticsResult, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()

	resp, err := c.stub().GetDiagnostics(ctx, &pb.DiagnosticsRequest{
		IncludeMemory:  includeMemory,
		IncludeIndices: includeIndices,
	})
	if err != nil {
		return nil, fmt.Errorf("engine diagnostics: %w", err)
	}

	result := &DiagnosticsResult{
		Config: resp.Config,
	}
	if resp.Memory != nil {
		result.Memory = &MemoryStats{
			HeapUsedBytes:  resp.Memory.HeapUsedBytes,
			HeapTotalBytes: resp.Memory.HeapTotalBytes,
			RSSBytes:       resp.Memory.RssBytes,
		}
	}
	for _, idx := range resp.Indices {
		result.Indices = append(result.Indices, IndexInfo{
			RepoID:      idx.RepoId,
			SizeBytes:   idx.SizeBytes,
			LastUpdated: idx.LastUpdated,
			IsLoaded:    idx.IsLoaded,
		})
	}
	return result, nil
}

// ── Storage Configuration ───────────────────────────────────────────────────

// StorageConfig holds the storage backend configuration pushed to the engine.
type StorageConfig struct {
	Provider    string
	BasePath    string
	Bucket      string
	Region      string
	EndpointURL string
	AccessKey   string
	SecretKey   string
	UseSSL      bool
	VerifySSL   bool
	TimeoutMs   int32
	MaxRetries  int32

	// ServerCallbackURL is the Go API server's own base URL (e.g. "http://localhost:8080").
	// The C++ engine uses this to call back into the Go server for Redis embed-cache
	// proxy, webhook delivery, etc. Computed at startup from cfg.Server.
	ServerCallbackURL string
}

// PushStorageConfig sends storage configuration to the C++ engine.
// Called once at server startup to configure the storage backend.
func (c *Client) PushStorageConfig(ctx context.Context, cfg StorageConfig) error {
	ctx, cancel := c.ctx(ctx)
	defer cancel()

	providerMap := map[string]pb.StorageProvider{
		"local":  pb.StorageProvider_STORAGE_PROVIDER_LOCAL,
		"aws":    pb.StorageProvider_STORAGE_PROVIDER_AWS,
		"gcp":    pb.StorageProvider_STORAGE_PROVIDER_GCP,
		"azure":  pb.StorageProvider_STORAGE_PROVIDER_AZURE,
		"oci":    pb.StorageProvider_STORAGE_PROVIDER_OCI,
		"minio":  pb.StorageProvider_STORAGE_PROVIDER_MINIO,
		"custom": pb.StorageProvider_STORAGE_PROVIDER_CUSTOM,
	}
	provider, ok := providerMap[cfg.Provider]
	if !ok {
		provider = pb.StorageProvider_STORAGE_PROVIDER_LOCAL
	}

	resp, err := c.stub().ConfigureStorage(ctx, &pb.StorageConfigRequest{
		Provider:          provider,
		BasePath:          cfg.BasePath,
		Bucket:            cfg.Bucket,
		Region:            cfg.Region,
		EndpointUrl:       cfg.EndpointURL,
		AccessKey:         cfg.AccessKey,
		SecretKey:         cfg.SecretKey,
		UseSsl:            cfg.UseSSL,
		VerifySsl:         cfg.VerifySSL,
		TimeoutMs:         cfg.TimeoutMs,
		MaxRetries:        cfg.MaxRetries,
		ServerCallbackUrl: cfg.ServerCallbackURL,
	})
	if err != nil {
		return fmt.Errorf("push storage config: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("engine rejected storage config: %s", resp.Message)
	}
	slog.Info("storage config pushed to engine",
		"provider", resp.ActiveProvider,
		"server_callback_url", cfg.ServerCallbackURL,
	)
	return nil
}

// ── Indexing Operations ─────────────────────────────────────────────────────

// IndexConfig holds indexing parameters sent to the engine.
type IndexConfig struct {
	MaxFileSizeKB       uint32
	ChunkSize           uint32
	ChunkOverlap        uint32
	EnableASTChunking   bool
	ExcludePatterns     []string
	IncludeLanguages    []string
	EmbeddingEndpoint   string
	EmbeddingDimensions uint32
	EmbeddingProvider   string // "LOCAL_ONNX", "HTTP", "CUSTOM"
	EmbeddingModel      string // e.g. "text-embedding-3-small"
	EmbeddingAPIKey     string // runtime API key — never persisted
	CloneToken          string // VCS clone token for authenticated git clone — never persisted
	IndexAction         string // "index" (default), "reindex" (skip git), "reclone" (force fresh clone)
	TargetBranch        string // branch to checkout before indexing (empty = default)
}

// IndexResult is the outcome of an indexing operation.
type IndexResult struct {
	Success bool
	Message string
	Stats   *IndexStats
}

// IndexStats holds statistics about an index.
type IndexStats struct {
	RepoID          string
	IndexVersion    string
	TotalFiles      uint64
	IndexedFiles    uint64
	TotalChunks     uint64
	TotalSymbols    uint64
	IndexSizeBytes  uint64
	LastUpdated     string
	IsComplete      bool
	FilesByLanguage map[string]uint64
}

// IndexRepository triggers a full repository indexing on the C++ engine.
func (c *Client) IndexRepository(ctx context.Context, repoID, repoPath string, cfg IndexConfig) (*IndexResult, error) {
	// Caller controls the deadline (service.go sets 24h for full index).

	resp, err := c.stub().IndexRepository(ctx, &pb.IndexRequest{
		RepoId:   repoID,
		RepoPath: repoPath,
		Config: &pb.IndexConfig{
			MaxFileSizeKb:       cfg.MaxFileSizeKB,
			ChunkSize:           cfg.ChunkSize,
			ChunkOverlap:        cfg.ChunkOverlap,
			EnableAstChunking:   cfg.EnableASTChunking,
			ExcludePatterns:     cfg.ExcludePatterns,
			IncludeLanguages:    cfg.IncludeLanguages,
			EmbeddingEndpoint:   cfg.EmbeddingEndpoint,
			EmbeddingDimensions: cfg.EmbeddingDimensions,
			EmbeddingProvider:   cfg.EmbeddingProvider,
			EmbeddingModel:      cfg.EmbeddingModel,
			EmbeddingApiKey:     cfg.EmbeddingAPIKey,
			CloneToken:          cfg.CloneToken,
			IndexAction:         cfg.IndexAction,
			TargetBranch:        cfg.TargetBranch,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("index repository: %w", err)
	}
	return convertIndexResponse(resp), nil
}

// IndexProgressUpdate is a progress event received from the engine stream.
type IndexProgressUpdate struct {
	RepoID         string
	Progress       int    // 0-100
	Phase          string // "queued", "cloning", "scanning", "chunking", "embedding", "finalizing", "completed", "failed"
	FilesProcessed uint64
	FilesTotal     uint64
	CurrentFile    string
	ETASeconds     int64 // -1 = unknown
	Done           bool
	Success        bool
	Error          string
	FinalStats     *IndexStats
}

// ProgressFunc is called for each progress update from the engine.
type ProgressFunc func(update IndexProgressUpdate)

// IndexRepositoryStream triggers indexing and streams progress updates.
// The onProgress callback is invoked for each update from the engine.
// This method blocks until indexing completes or fails.
func (c *Client) IndexRepositoryStream(ctx context.Context, repoID, repoPath string, cfg IndexConfig, onProgress ProgressFunc) (*IndexResult, error) {
	// Caller controls the deadline (service.go sets 24h for full index).

	stream, err := c.stub().IndexRepositoryStream(ctx, &pb.IndexRequest{
		RepoId:   repoID,
		RepoPath: repoPath,
		Config: &pb.IndexConfig{
			MaxFileSizeKb:       cfg.MaxFileSizeKB,
			ChunkSize:           cfg.ChunkSize,
			ChunkOverlap:        cfg.ChunkOverlap,
			EnableAstChunking:   cfg.EnableASTChunking,
			ExcludePatterns:     cfg.ExcludePatterns,
			IncludeLanguages:    cfg.IncludeLanguages,
			EmbeddingEndpoint:   cfg.EmbeddingEndpoint,
			EmbeddingDimensions: cfg.EmbeddingDimensions,
			EmbeddingProvider:   cfg.EmbeddingProvider,
			EmbeddingModel:      cfg.EmbeddingModel,
			EmbeddingApiKey:     cfg.EmbeddingAPIKey,
			CloneToken:          cfg.CloneToken,
			IndexAction:         cfg.IndexAction,
			TargetBranch:        cfg.TargetBranch,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("index repository stream: %w", err)
	}

	var lastUpdate *pb.IndexProgressUpdate
	for {
		update, err := stream.Recv()
		if err != nil {
			// Stream ended unexpectedly
			if lastUpdate != nil && lastUpdate.Done {
				break // normal completion
			}
			return nil, fmt.Errorf("index stream recv: %w", err)
		}
		lastUpdate = update

		// Build the Go-side progress update
		pu := IndexProgressUpdate{
			RepoID:         update.RepoId,
			Progress:       int(update.Progress),
			Phase:          update.Phase,
			FilesProcessed: update.FilesProcessed,
			FilesTotal:     update.FilesTotal,
			CurrentFile:    update.CurrentFile,
			ETASeconds:     update.EtaSeconds,
			Done:           update.Done,
			Success:        update.Success,
			Error:          update.Error,
		}
		if update.FinalStats != nil {
			pu.FinalStats = convertStats(update.FinalStats)
		}

		if onProgress != nil {
			onProgress(pu)
		}

		if update.Done {
			if update.Success {
				return &IndexResult{
					Success: true,
					Message: "Index completed successfully",
					Stats:   pu.FinalStats,
				}, nil
			}
			return &IndexResult{
				Success: false,
				Message: update.Error,
			}, fmt.Errorf("engine indexing failed: %s", update.Error)
		}
	}

	return &IndexResult{Success: false, Message: "stream ended unexpectedly"}, fmt.Errorf("index stream ended unexpectedly")
}

// IncrementalIndex updates an existing index with changed files.
func (c *Client) IncrementalIndex(ctx context.Context, repoID string, changedFiles []string, baseCommit, headCommit string) (*IndexResult, error) {
	// Caller controls the deadline (service.go sets 2h for incremental).

	resp, err := c.stub().IncrementalIndex(ctx, &pb.IncrementalIndexRequest{
		RepoId:       repoID,
		ChangedFiles: changedFiles,
		BaseCommit:   baseCommit,
		HeadCommit:   headCommit,
	})
	if err != nil {
		return nil, fmt.Errorf("incremental index: %w", err)
	}
	return convertIndexResponse(resp), nil
}

// GetIndexStats returns statistics for a repository's index.
func (c *Client) GetIndexStats(ctx context.Context, repoID string) (*IndexStats, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()

	resp, err := c.stub().GetIndexStats(ctx, &pb.IndexStatsRequest{RepoId: repoID})
	if err != nil {
		if s, ok := status.FromError(err); ok && s.Code() == codes.NotFound {
			return nil, nil // No index exists — not an error
		}
		return nil, fmt.Errorf("get index stats: %w", err)
	}
	if !resp.Found {
		return nil, nil
	}
	return convertStats(resp.Stats), nil
}

// DeleteIndex removes a repository's index from the engine.
func (c *Client) DeleteIndex(ctx context.Context, repoID string) error {
	ctx, cancel := c.ctx(ctx)
	defer cancel()

	resp, err := c.stub().DeleteIndex(ctx, &pb.DeleteIndexRequest{RepoId: repoID})
	if err != nil {
		return fmt.Errorf("delete index: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("engine refused to delete index: %s", resp.Message)
	}
	slog.Info("index deleted", "repo_id", repoID)
	return nil
}

// ── Search / Retrieval ──────────────────────────────────────────────────────

// SearchConfig holds search parameters.
type SearchConfig struct {
	TopK             uint32
	LexicalWeight    float32
	VectorWeight     float32
	GraphExpandDepth uint32
	FileFilters      []string
	LanguageFilters  []string
}

// ContextChunk is a code chunk returned by the engine.
type ContextChunk struct {
	ID             string
	FilePath       string
	StartLine      uint32
	EndLine        uint32
	Content        string
	Language       string
	Symbols        []string
	RelevanceScore float32
	ChunkType      string
}

// SearchResult holds the result of a search query.
type SearchResult struct {
	Chunks  []ContextChunk
	Metrics *SearchMetrics
}

// SearchMetrics contains timing and hit statistics.
type SearchMetrics struct {
	TotalCandidates uint32
	LexicalHits     uint32
	VectorHits      uint32
	GraphHits       uint32
	SearchTimeMs    uint64
}

// Search queries the engine's hybrid retrieval system (lexical + vector + graph).
func (c *Client) Search(ctx context.Context, repoID, query string, touchedSymbols []string, cfg SearchConfig) (*SearchResult, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()

	resp, err := c.stub().Search(ctx, &pb.SearchRequest{
		RepoId:         repoID,
		Query:          query,
		TouchedSymbols: touchedSymbols,
		Config: &pb.SearchConfig{
			TopK:             cfg.TopK,
			LexicalWeight:    cfg.LexicalWeight,
			VectorWeight:     cfg.VectorWeight,
			GraphExpandDepth: cfg.GraphExpandDepth,
			FileFilters:      cfg.FileFilters,
			LanguageFilters:  cfg.LanguageFilters,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	result := &SearchResult{}
	for _, chunk := range resp.Chunks {
		result.Chunks = append(result.Chunks, convertChunk(chunk))
	}
	if resp.Metrics != nil {
		result.Metrics = &SearchMetrics{
			TotalCandidates: resp.Metrics.TotalCandidates,
			LexicalHits:     resp.Metrics.LexicalHits,
			VectorHits:      resp.Metrics.VectorHits,
			GraphHits:       resp.Metrics.GraphHits,
			SearchTimeMs:    resp.Metrics.SearchTimeMs,
		}
	}
	return result, nil
}

// SearchCached wraps Search with a TTL cache and singleflight deduplication.
// Concurrent identical queries share a single gRPC call; repeated queries
// within SearchCacheTTL return cached results without any gRPC round-trip.
func (c *Client) SearchCached(ctx context.Context, repoID, query string, touchedSymbols []string, cfg SearchConfig) (*SearchResult, error) {
	key := searchCacheKey(repoID, query)

	if cached, ok := c.searchCache.Load(key); ok {
		entry := cached.(cachedSearch)
		if time.Now().Before(entry.expiresAt) {
			return entry.result, nil
		}
		c.searchCache.Delete(key)
	}

	val, err, _ := c.searchGroup.Do(key, func() (interface{}, error) {
		return c.Search(ctx, repoID, query, touchedSymbols, cfg)
	})
	if err != nil {
		return nil, err
	}

	result := val.(*SearchResult)
	c.searchCache.Store(key, cachedSearch{
		result:    result,
		expiresAt: time.Now().Add(SearchCacheTTL),
	})
	return result, nil
}

func searchCacheKey(repoID, query string) string {
	h := sha256.Sum256([]byte(repoID + "\x00" + query))
	return hex.EncodeToString(h[:16]) // 128-bit key — collision-safe
}

// ── Review Context Building ─────────────────────────────────────────────────

// TouchedSymbol represents a symbol affected by a diff.
type TouchedSymbol struct {
	Name          string
	QualifiedName string
	Kind          string // function, class, variable, etc.
	FilePath      string
	Line          uint32
	ChangeType    string // added, modified, deleted
	EndLine       uint32
	Callers       []string
	Callees       []string
}

// ContextPack is the full context package for a review, combining diff,
// retrieved chunks, symbol analysis, and heuristic warnings.
type ContextPack struct {
	RepoID              string
	PRTitle             string
	PRDescription       string
	Diff                string
	ContextChunks       []ContextChunk
	TouchedSymbols      []TouchedSymbol
	HeuristicWarnings   []string
	TotalTokensEstimate uint64
}

// BuildReviewContext calls the engine to build a complete context pack for a PR review.
// This is the main entry point for the review pipeline's engine interaction.
func (c *Client) BuildReviewContext(ctx context.Context, repoID, diff, prTitle, prDescription string, maxChunks uint32) (*ContextPack, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	resp, err := c.stub().BuildReviewContext(ctx, &pb.ReviewContextRequest{
		RepoId:           repoID,
		Diff:             diff,
		PrTitle:          prTitle,
		PrDescription:    prDescription,
		MaxContextChunks: maxChunks,
	})
	if err != nil {
		return nil, fmt.Errorf("build review context: %w", err)
	}

	pack := resp.ContextPack
	if pack == nil {
		return nil, fmt.Errorf("engine returned nil context pack")
	}

	result := &ContextPack{
		RepoID:              pack.RepoId,
		PRTitle:             pack.PrTitle,
		PRDescription:       pack.PrDescription,
		Diff:                pack.Diff,
		HeuristicWarnings:   pack.HeuristicWarnings,
		TotalTokensEstimate: pack.TotalTokensEstimate,
	}
	for _, ch := range pack.ContextChunks {
		result.ContextChunks = append(result.ContextChunks, convertChunk(ch))
	}
	for _, ts := range pack.TouchedSymbols {
		result.TouchedSymbols = append(result.TouchedSymbols, convertTouchedSymbol(ts))
	}
	return result, nil
}

// ── PR Embedding Progress Streaming ─────────────────────────────────────────

// PREmbedProgressUpdate is a progress event received from the engine stream
// during PR diff embedding.
type PREmbedProgressUpdate struct {
	RepoID         string
	PRNumber       int
	Progress       int    // 0-100
	Phase          string // "parsing_diff", "resolving_symbols", "building_graph", "embedding_chunks", "finalizing"
	FilesProcessed uint32
	FilesTotal     uint32
	CurrentFile    string
	ETASeconds     int64 // -1 = unknown
	Done           bool
	Success        bool
	Error          string
	ContextPack    *ContextPack
}

// PREmbedProgressFunc is called for each progress update during streaming PR embedding.
type PREmbedProgressFunc func(update PREmbedProgressUpdate)

// BuildReviewContextStream triggers PR embedding and streams progress updates.
// The onProgress callback is invoked for each update from the engine.
// This method blocks until embedding completes or fails.
func (c *Client) BuildReviewContextStream(
	ctx context.Context,
	repoID, diff, prTitle, prDescription string,
	maxChunks uint32,
	onProgress PREmbedProgressFunc,
) (*ContextPack, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	stream, err := c.stub().BuildReviewContextStream(ctx, &pb.ReviewContextRequest{
		RepoId:           repoID,
		Diff:             diff,
		PrTitle:          prTitle,
		PrDescription:    prDescription,
		MaxContextChunks: maxChunks,
	})
	if err != nil {
		// Fall back to unary call if streaming is not supported
		if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
			slog.Debug("engine does not support BuildReviewContextStream, falling back to unary")
			return c.BuildReviewContext(ctx, repoID, diff, prTitle, prDescription, maxChunks)
		}
		return nil, fmt.Errorf("build review context stream: %w", err)
	}

	var lastUpdate *pb.PREmbedProgressUpdate
	for {
		update, err := stream.Recv()
		if err != nil {
			// The C++ engine may not implement this streaming RPC yet.
			// With server-streaming, Unimplemented arrives on the first Recv(), not on stream creation.
			if s, ok := status.FromError(err); ok && s.Code() == codes.Unimplemented {
				slog.Debug("engine does not support BuildReviewContextStream (recv), falling back to unary")
				return c.BuildReviewContext(ctx, repoID, diff, prTitle, prDescription, maxChunks)
			}
			if lastUpdate != nil && lastUpdate.Done {
				break // normal completion
			}
			return nil, fmt.Errorf("embed stream recv: %w", err)
		}
		lastUpdate = update

		pu := PREmbedProgressUpdate{
			RepoID:         update.RepoId,
			PRNumber:       int(update.PrNumber),
			Progress:       int(update.Progress),
			Phase:          update.Phase,
			FilesProcessed: update.FilesProcessed,
			FilesTotal:     update.FilesTotal,
			CurrentFile:    update.CurrentFile,
			ETASeconds:     update.EtaSeconds,
			Done:           update.Done,
			Success:        update.Success,
			Error:          update.Error,
		}
		if update.ContextPack != nil {
			pack := update.ContextPack
			cp := &ContextPack{
				RepoID:              pack.RepoId,
				PRTitle:             pack.PrTitle,
				PRDescription:       pack.PrDescription,
				Diff:                pack.Diff,
				HeuristicWarnings:   pack.HeuristicWarnings,
				TotalTokensEstimate: pack.TotalTokensEstimate,
			}
			for _, ch := range pack.ContextChunks {
				cp.ContextChunks = append(cp.ContextChunks, convertChunk(ch))
			}
			for _, ts := range pack.TouchedSymbols {
				cp.TouchedSymbols = append(cp.TouchedSymbols, convertTouchedSymbol(ts))
			}
			pu.ContextPack = cp
		}

		if onProgress != nil {
			onProgress(pu)
		}

		if update.Done {
			if update.Success && pu.ContextPack != nil {
				return pu.ContextPack, nil
			}
			if !update.Success {
				return nil, fmt.Errorf("engine PR embedding failed: %s", update.Error)
			}
		}
	}

	return nil, fmt.Errorf("embed stream ended unexpectedly")
}

// ── Heuristics ──────────────────────────────────────────────────────────────

// HeuristicFinding is a heuristic check result from the engine.
type HeuristicFinding struct {
	ID         string
	Category   string
	Severity   string
	Confidence float32
	FilePath   string
	Line       uint32
	EndLine    uint32
	Message    string
	Suggestion string
	Evidence   string
	RuleID     string
	RuleName   string
}

// RunHeuristics executes heuristic checks on a diff.
func (c *Client) RunHeuristics(ctx context.Context, diff string, enabledChecks []string) ([]HeuristicFinding, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()

	resp, err := c.stub().RunHeuristics(ctx, &pb.HeuristicsRequest{
		Diff:          diff,
		EnabledChecks: enabledChecks,
	})
	if err != nil {
		return nil, fmt.Errorf("run heuristics: %w", err)
	}

	var findings []HeuristicFinding
	for _, f := range resp.Findings {
		findings = append(findings, convertHeuristicFinding(f))
	}
	return findings, nil
}

// ── Converters (protobuf → Go domain types) ─────────────────────────────────

func convertIndexResponse(resp *pb.IndexResponse) *IndexResult {
	result := &IndexResult{
		Success: resp.Success,
		Message: resp.Message,
	}
	if resp.Stats != nil {
		result.Stats = convertStats(resp.Stats)
	}
	return result
}

func convertStats(s *pb.IndexStats) *IndexStats {
	if s == nil {
		return nil
	}
	return &IndexStats{
		RepoID:          s.RepoId,
		IndexVersion:    s.IndexVersion,
		TotalFiles:      s.TotalFiles,
		IndexedFiles:    s.IndexedFiles,
		TotalChunks:     s.TotalChunks,
		TotalSymbols:    s.TotalSymbols,
		IndexSizeBytes:  s.IndexSizeBytes,
		LastUpdated:     s.LastUpdated,
		IsComplete:      s.IsComplete,
		FilesByLanguage: s.FilesByLanguage,
	}
}

func convertChunk(c *pb.ContextChunk) ContextChunk {
	return ContextChunk{
		ID:             c.Id,
		FilePath:       c.FilePath,
		StartLine:      c.StartLine,
		EndLine:        c.EndLine,
		Content:        c.Content,
		Language:       c.Language,
		Symbols:        c.Symbols,
		RelevanceScore: c.RelevanceScore,
		ChunkType:      c.ChunkType,
	}
}

func convertTouchedSymbol(ts *pb.TouchedSymbol) TouchedSymbol {
	return TouchedSymbol{
		Name:          ts.Name,
		QualifiedName: ts.QualifiedName,
		Kind:          ts.Kind,
		FilePath:      ts.FilePath,
		Line:          ts.Line,
		ChangeType:    ts.ChangeType,
		EndLine:       ts.EndLine,
		Callers:       ts.Callers,
		Callees:       ts.Callees,
	}
}

func convertHeuristicFinding(f *pb.HeuristicFinding) HeuristicFinding {
	categoryMap := map[pb.CheckCategory]string{
		pb.CheckCategory_CATEGORY_SECURITY:      "security",
		pb.CheckCategory_CATEGORY_PERFORMANCE:   "performance",
		pb.CheckCategory_CATEGORY_RELIABILITY:   "reliability",
		pb.CheckCategory_CATEGORY_STYLE:         "style",
		pb.CheckCategory_CATEGORY_ARCHITECTURE:  "architecture",
		pb.CheckCategory_CATEGORY_TESTING:       "testing",
		pb.CheckCategory_CATEGORY_DOCUMENTATION: "documentation",
	}
	severityMap := map[pb.Severity]string{
		pb.Severity_SEVERITY_INFO:     "info",
		pb.Severity_SEVERITY_WARNING:  "warning",
		pb.Severity_SEVERITY_ERROR:    "error",
		pb.Severity_SEVERITY_CRITICAL: "critical",
	}

	cat := categoryMap[f.Category]
	if cat == "" {
		cat = "unspecified"
	}
	sev := severityMap[f.Severity]
	if sev == "" {
		sev = "info"
	}

	return HeuristicFinding{
		ID:         f.Id,
		Category:   cat,
		Severity:   sev,
		Confidence: f.Confidence,
		FilePath:   f.FilePath,
		Line:       f.Line,
		EndLine:    f.EndLine,
		Message:    f.Message,
		Suggestion: f.Suggestion,
		Evidence:   f.Evidence,
		RuleID:     f.RuleId,
		RuleName:   f.RuleName,
	}
}

// ── Embedding Statistics ────────────────────────────────────────────────────

// EmbedStats holds embedding subsystem health and performance metrics.
type EmbedStats struct {
	ActiveModel          string  `json:"active_model"`
	EmbeddingDimension   uint32  `json:"embedding_dimension"`
	BackendType          string  `json:"backend_type"`
	TotalChunks          uint64  `json:"total_chunks"`
	TotalVectors         uint64  `json:"total_vectors"`
	IndexSizeBytes       uint64  `json:"index_size_bytes"`
	KGNodes              uint64  `json:"kg_nodes"`
	KGEdges              uint64  `json:"kg_edges"`
	KGEnabled            bool    `json:"kg_enabled"`
	MerkleCachedFiles    uint64  `json:"merkle_cached_files"`
	MerkleCacheHitRate   float64 `json:"merkle_cache_hit_rate"`
	AvgEmbedLatencyMs    float64 `json:"avg_embed_latency_ms"`
	AvgSearchLatencyMs   float64 `json:"avg_search_latency_ms"`
	TotalQueries         uint64  `json:"total_queries"`
	EmbedCacheSize       uint64  `json:"embed_cache_size"`
	EmbedCacheHitRate    float64 `json:"embed_cache_hit_rate"`
	LLMAvoidsRate        float64 `json:"llm_avoided_rate"`
	AvgConfidenceScore   float64 `json:"avg_confidence_score"`
	LLMAvoidsCount       uint64  `json:"llm_avoided_count"`
	LLMUsedCount         uint64  `json:"llm_used_count"`
	AvgGraphExpansionMs  float64 `json:"avg_graph_expansion_ms"`
	AvgGraphExpandChunks float64 `json:"avg_graph_expanded_chunks"`
	ModelSwapsTotal      uint64  `json:"model_swaps_total"`
	MultiVectorEnabled   bool    `json:"multi_vector_enabled"`
	CoarseDimension      uint32  `json:"coarse_dimension"`
	FineDimension        uint32  `json:"fine_dimension"`
	CoarseIndexVectors   uint64  `json:"coarse_index_vectors"`
	FineIndexVectors     uint64  `json:"fine_index_vectors"`
}

// GetEmbedStats retrieves embedding statistics from the C++ engine.
func (c *Client) GetEmbedStats(ctx context.Context, repoID string) (*EmbedStats, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()

	resp, err := c.stub().GetEmbedStats(ctx, &pb.EmbedStatsRequest{
		RepoId: repoID,
	})
	if err != nil {
		return nil, fmt.Errorf("get embed stats: %w", err)
	}

	return &EmbedStats{
		ActiveModel:          resp.ActiveModel,
		EmbeddingDimension:   resp.EmbeddingDimension,
		BackendType:          resp.BackendType,
		TotalChunks:          resp.TotalChunks,
		TotalVectors:         resp.TotalVectors,
		IndexSizeBytes:       resp.IndexSizeBytes,
		KGNodes:              resp.KgNodes,
		KGEdges:              resp.KgEdges,
		KGEnabled:            resp.KgEnabled,
		MerkleCachedFiles:    resp.MerkleCachedFiles,
		MerkleCacheHitRate:   resp.MerkleCacheHitRate,
		AvgEmbedLatencyMs:    resp.AvgEmbedLatencyMs,
		AvgSearchLatencyMs:   resp.AvgSearchLatencyMs,
		TotalQueries:         resp.TotalQueries,
		EmbedCacheSize:       resp.EmbedCacheSize,
		EmbedCacheHitRate:    resp.EmbedCacheHitRate,
		LLMAvoidsRate:        resp.LlmAvoidedRate,
		AvgConfidenceScore:   resp.AvgConfidenceScore,
		LLMAvoidsCount:       resp.LlmAvoidedCount,
		LLMUsedCount:         resp.LlmUsedCount,
		AvgGraphExpansionMs:  resp.AvgGraphExpansionMs,
		AvgGraphExpandChunks: resp.AvgGraphExpandedChunks,
		ModelSwapsTotal:      resp.ModelSwapsTotal,
		MultiVectorEnabled:   resp.MultiVectorEnabled,
		CoarseDimension:      resp.CoarseDimension,
		FineDimension:        resp.FineDimension,
		CoarseIndexVectors:   resp.CoarseIndexVectors,
		FineIndexVectors:     resp.FineIndexVectors,
	}, nil
}

// ── Multimodal Embedding Operations ─────────────────────────────────────────

// IngestAssetResult holds the response from IngestAsset RPC.
type IngestAssetResult struct {
	ChunksCreated int32  `json:"chunks_created"`
	Status        string `json:"status"`
	AssetID       string `json:"asset_id"`
}

// IngestAsset sends content or binary data to the C++ engine for embedding.
// For text assets, the engine chunks and embeds the text.
// For image/audio assets, the engine runs ONNX inference (SigLIP/CLAP).
func (c *Client) IngestAsset(ctx context.Context, repoID, assetType, mimeType,
	fileName, sourceURL, textContent string, binaryData []byte) (*IngestAssetResult, error) {

	ctx, cancel := c.ctx(ctx)
	defer cancel()

	resp, err := c.stub().IngestAsset(ctx, &pb.IngestAssetRequest{
		RepoId:     repoID,
		AssetType:  assetType,
		MimeType:   mimeType,
		FileName:   fileName,
		SourceUrl:  sourceURL,
		Content:    textContent,
		BinaryData: binaryData,
	})
	if err != nil {
		return nil, fmt.Errorf("ingest asset: %w", err)
	}

	return &IngestAssetResult{
		ChunksCreated: resp.ChunksCreated,
		Status:        resp.Status,
		AssetID:       resp.AssetId,
	}, nil
}

// MultimodalModalityConfig describes one modality slot from the C++ engine.
type MultimodalModalityConfig struct {
	Modality           string `json:"modality"`
	ModelName          string `json:"model_name"`
	Enabled            bool   `json:"enabled"`
	Status             string `json:"status"`
	Dimension          uint32 `json:"dimension"`
	ProjectedDimension uint32 `json:"projected_dimension"`
	DownloadProgress   int32  `json:"download_progress"`
}

// MultimodalConfigResult is the full multimodal config from the C++ engine.
type MultimodalConfigResult struct {
	Modalities       []MultimodalModalityConfig `json:"modalities"`
	UnifiedDimension uint32                     `json:"unified_dimension"`
	ImageEnabled     bool                       `json:"image_enabled"`
	AudioEnabled     bool                       `json:"audio_enabled"`
}

// GetMultimodalConfig queries the C++ engine for live multimodal model status.
func (c *Client) GetMultimodalConfig(ctx context.Context) (*MultimodalConfigResult, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()

	resp, err := c.stub().GetMultimodalConfig(ctx, &pb.GetMultimodalConfigRequest{})
	if err != nil {
		return nil, fmt.Errorf("get multimodal config: %w", err)
	}

	result := &MultimodalConfigResult{
		UnifiedDimension: resp.UnifiedDimension,
		ImageEnabled:     resp.ImageEnabled,
		AudioEnabled:     resp.AudioEnabled,
	}
	for _, m := range resp.Modalities {
		result.Modalities = append(result.Modalities, MultimodalModalityConfig{
			Modality:           m.Modality,
			ModelName:          m.ModelName,
			Enabled:            m.Enabled,
			Status:             m.Status,
			Dimension:          m.Dimension,
			ProjectedDimension: m.ProjectedDimension,
			DownloadProgress:   m.DownloadProgress,
		})
	}
	return result, nil
}

// ConfigureMultimodalResult is the response from ConfigureMultimodal RPC.
type ConfigureMultimodalResult struct {
	Success      bool     `json:"success"`
	Message      string   `json:"message"`
	LoadedModels []string `json:"loaded_models"`
}

// ConfigureMultimodal enables/disables image and audio modalities in the C++ engine.
func (c *Client) ConfigureMultimodal(ctx context.Context, enableImage, enableAudio bool) (*ConfigureMultimodalResult, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()

	resp, err := c.stub().ConfigureMultimodal(ctx, &pb.ConfigureMultimodalRequest{
		EnableImage: enableImage,
		EnableAudio: enableAudio,
	})
	if err != nil {
		return nil, fmt.Errorf("configure multimodal: %w", err)
	}

	return &ConfigureMultimodalResult{
		Success:      resp.Success,
		Message:      resp.Message,
		LoadedModels: resp.LoadedModels,
	}, nil
}

// ── Intra-Repo File Map (Knowledge Graph) ───────────────────────────────────

// KGNode represents a node in the knowledge graph.
type KGNode struct {
	ID       string `json:"id"`
	NodeType string `json:"node_type"`
	Name     string `json:"name"`
	FilePath string `json:"file_path"`
	Language string `json:"language"`
	RepoID   string `json:"repo_id"`
	Metadata string `json:"metadata"`
}

// KGEdge represents an edge in the knowledge graph.
type KGEdge struct {
	ID       int64   `json:"id"`
	SrcID    string  `json:"src_id"`
	DstID    string  `json:"dst_id"`
	EdgeType string  `json:"edge_type"`
	Weight   float32 `json:"weight"`
	RepoID   string  `json:"repo_id"`
}

// RepoFileMap is the intra-repo file dependency graph.
type RepoFileMap struct {
	Nodes      []KGNode `json:"nodes"`
	Edges      []KGEdge `json:"edges"`
	TotalNodes uint32   `json:"total_nodes"`
	TotalEdges uint32   `json:"total_edges"`
	Truncated  bool     `json:"truncated"`
}

// GetRepoFileMap retrieves the intra-repo file/symbol dependency map from the KG.
func (c *Client) GetRepoFileMap(ctx context.Context, repoID string, nodeTypes, edgeTypes []string, maxNodes uint32) (*RepoFileMap, error) {
	ctx, cancel := c.ctx(ctx)
	defer cancel()

	resp, err := c.stub().GetRepoFileMap(ctx, &pb.RepoFileMapRequest{
		RepoId:    repoID,
		NodeTypes: nodeTypes,
		EdgeTypes: edgeTypes,
		MaxNodes:  maxNodes,
	})
	if err != nil {
		return nil, fmt.Errorf("get repo file map: %w", err)
	}

	result := &RepoFileMap{
		TotalNodes: resp.TotalNodes,
		TotalEdges: resp.TotalEdges,
		Truncated:  resp.Truncated,
	}
	for _, n := range resp.Nodes {
		result.Nodes = append(result.Nodes, KGNode{
			ID:       n.Id,
			NodeType: n.NodeType,
			Name:     n.Name,
			FilePath: n.FilePath,
			Language: n.Language,
			RepoID:   n.RepoId,
			Metadata: n.Metadata,
		})
	}
	for _, e := range resp.Edges {
		result.Edges = append(result.Edges, KGEdge{
			ID:       e.Id,
			SrcID:    e.SrcId,
			DstID:    e.DstId,
			EdgeType: e.EdgeType,
			Weight:   e.Weight,
			RepoID:   e.RepoId,
		})
	}
	return result, nil
}
