package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/redis/go-redis/v9"

	"github.com/AuralithAI/rtvortex-server/internal/config"
	"github.com/AuralithAI/rtvortex-server/internal/store"
	"github.com/AuralithAI/rtvortex-server/internal/vault"
)

var (
	mcpCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "rtvortex",
		Subsystem: "mcp",
		Name:      "calls_total",
		Help:      "Total MCP provider calls by provider and status.",
	}, []string{"provider", "action", "status"})

	mcpLatencySeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "rtvortex",
		Subsystem: "mcp",
		Name:      "call_duration_seconds",
		Help:      "MCP provider call latency in seconds.",
		Buckets:   []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
	}, []string{"provider", "action"})

	mcpActiveConnections = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "rtvortex",
		Subsystem: "mcp",
		Name:      "active_connections",
		Help:      "Number of active MCP connections by provider.",
	}, []string{"provider"})
)

const (
	tokenCachePrefix = "mcp:token:"
	tokenCacheTTL    = 10 * time.Minute
	refreshInterval  = 5 * time.Minute
	refreshThreshold = 15 * time.Minute
)

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|secret|token|api[_-]?key|access[_-]?token|refresh[_-]?token|authorization)\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)(bearer|basic)\s+[A-Za-z0-9\-._~+/]+=*`),
	regexp.MustCompile(`\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z]{2,}\b`),
	regexp.MustCompile(`\b\d{3}[-.]?\d{3}[-.]?\d{4}\b`),
	regexp.MustCompile(`xox[bpras]-[0-9A-Za-z\-]+`),
}

type Service struct {
	repo     *store.MCPRepository
	registry *ProviderRegistry
	vault    *vault.FileVault
	rdb      *redis.Client
	cfg      config.MCPConfig
}

func NewService(
	repo *store.MCPRepository,
	registry *ProviderRegistry,
	v *vault.FileVault,
	rdb *redis.Client,
	cfg config.MCPConfig,
) *Service {
	return &Service{
		repo:     repo,
		registry: registry,
		vault:    v,
		rdb:      rdb,
		cfg:      cfg,
	}
}

type ExecuteRequest struct {
	UserID   uuid.UUID
	OrgID    *uuid.UUID
	Provider string
	Action   string
	Params   map[string]interface{}
	AgentID  string
	TaskID   string
}

func (s *Service) Execute(ctx context.Context, req ExecuteRequest) (*Result, error) {
	if !s.cfg.Enabled {
		return nil, fmt.Errorf("MCP integrations are disabled")
	}

	if !s.isProviderAllowed(req.Provider) {
		return nil, fmt.Errorf("provider %q is not in the allowed list", req.Provider)
	}

	provider, ok := s.registry.Get(req.Provider)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", req.Provider)
	}

	action := s.findAction(provider, req.Action)
	if action == nil {
		return nil, fmt.Errorf("unknown action %q for provider %q", req.Action, req.Provider)
	}

	if err := s.validateParams(action, req.Params); err != nil {
		return nil, err
	}

	if s.cfg.MaxCallsPerTask > 0 && req.TaskID != "" {
		count, err := s.repo.CountCallsForTask(ctx, req.TaskID)
		if err != nil {
			slog.Warn("mcp: failed to count task calls", "error", err, "task_id", req.TaskID)
		} else if count >= s.cfg.MaxCallsPerTask {
			s.logCall(ctx, uuid.Nil, req, "rate_limited", 0, fmt.Sprintf("task call limit reached (%d/%d)", count, s.cfg.MaxCallsPerTask))
			return nil, fmt.Errorf("task %s has reached the MCP call limit (%d)", req.TaskID, s.cfg.MaxCallsPerTask)
		}
	}

	if err := s.registry.CheckCircuitBreaker(req.Provider); err != nil {
		s.logCall(ctx, uuid.Nil, req, "error", 0, err.Error())
		return nil, err
	}

	if err := s.registry.CheckRateLimit(ctx, req.Provider); err != nil {
		s.logCall(ctx, uuid.Nil, req, "rate_limited", 0, err.Error())
		return nil, err
	}

	conn, err := s.repo.FindActiveConnection(ctx, req.UserID, req.OrgID, req.Provider)
	if err != nil {
		return nil, fmt.Errorf("no active %s connection found for user", req.Provider)
	}

	token, err := s.resolveToken(ctx, conn)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve token for %s: %w", req.Provider, err)
	}

	callCtx := ctx
	if s.cfg.CallTimeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, s.cfg.CallTimeout)
		defer cancel()
	}

	start := time.Now()
	result, err := provider.Execute(callCtx, req.Action, req.Params, token)
	elapsed := time.Since(start)
	latencyMs := int(elapsed.Milliseconds())

	mcpLatencySeconds.WithLabelValues(req.Provider, req.Action).Observe(elapsed.Seconds())

	if err != nil {
		s.registry.RecordFailure(req.Provider)
		mcpCallsTotal.WithLabelValues(req.Provider, req.Action, "error").Inc()
		s.logCall(ctx, conn.ID, req, "error", latencyMs, err.Error())
		return nil, fmt.Errorf("provider %s action %s failed: %w", req.Provider, req.Action, err)
	}

	s.registry.RecordSuccess(req.Provider)
	mcpCallsTotal.WithLabelValues(req.Provider, req.Action, "ok").Inc()

	if result != nil && result.Data != nil {
		s.sanitizeMap(result.Data)
	}

	_ = s.repo.TouchLastUsed(ctx, conn.ID)
	s.logCall(ctx, conn.ID, req, "ok", latencyMs, "")

	return result, nil
}

func (s *Service) ListProviders() []ProviderInfo {
	names := s.registry.List()
	out := make([]ProviderInfo, 0, len(names))
	for _, name := range names {
		if !s.isProviderAllowed(name) {
			continue
		}
		p, ok := s.registry.Get(name)
		if !ok {
			continue
		}
		out = append(out, ProviderInfo{
			Name:        name,
			Category:    p.Category(),
			Description: p.Description(),
			Actions:     p.Actions(),
		})
	}
	return out
}

type ProviderInfo struct {
	Name        string      `json:"name"`
	Category    string      `json:"category"`
	Description string      `json:"description,omitempty"`
	Actions     []ActionDef `json:"actions"`
}

func (s *Service) ListConnections(ctx context.Context, userID uuid.UUID, orgID *uuid.UUID) ([]store.MCPConnection, error) {
	return s.repo.ListByUser(ctx, userID, orgID)
}

func (s *Service) GetConnection(ctx context.Context, id uuid.UUID) (*store.MCPConnection, error) {
	return s.repo.GetConnection(ctx, id)
}

func (s *Service) CreateConnection(ctx context.Context, conn *store.MCPConnection, accessToken, refreshToken string) error {
	vaultKey := fmt.Sprintf("mcp:%s:%s:access", conn.Provider, conn.ID)
	if err := s.vault.Set(vaultKey, accessToken); err != nil {
		return fmt.Errorf("vault write failed: %w", err)
	}
	conn.VaultKey = vaultKey

	if refreshToken != "" {
		refreshKey := fmt.Sprintf("mcp:%s:%s:refresh", conn.Provider, conn.ID)
		if err := s.vault.Set(refreshKey, refreshToken); err != nil {
			return fmt.Errorf("vault write failed: %w", err)
		}
		conn.RefreshVaultKey = refreshKey
	}

	conn.Status = "active"
	if err := s.repo.CreateConnection(ctx, conn); err != nil {
		_ = s.vault.Delete(vaultKey)
		if conn.RefreshVaultKey != "" {
			_ = s.vault.Delete(conn.RefreshVaultKey)
		}
		return err
	}

	mcpActiveConnections.WithLabelValues(conn.Provider).Inc()
	return nil
}

func (s *Service) DeleteConnection(ctx context.Context, id uuid.UUID) error {
	conn, err := s.repo.GetConnection(ctx, id)
	if err != nil {
		return err
	}

	if conn.VaultKey != "" {
		_ = s.vault.Delete(conn.VaultKey)
	}
	if conn.RefreshVaultKey != "" {
		_ = s.vault.Delete(conn.RefreshVaultKey)
	}

	if s.rdb != nil {
		s.rdb.Del(ctx, tokenCachePrefix+id.String())
	}

	mcpActiveConnections.WithLabelValues(conn.Provider).Dec()
	return s.repo.Delete(ctx, id)
}

func (s *Service) TestConnection(ctx context.Context, id uuid.UUID) (*Result, error) {
	conn, err := s.repo.GetConnection(ctx, id)
	if err != nil {
		return nil, err
	}

	provider, ok := s.registry.Get(conn.Provider)
	if !ok {
		return nil, fmt.Errorf("unknown provider %q", conn.Provider)
	}

	token, err := s.resolveToken(ctx, conn)
	if err != nil {
		return nil, err
	}

	actions := provider.Actions()
	if len(actions) == 0 {
		return &Result{Success: true, Data: map[string]interface{}{"message": "no actions to test"}}, nil
	}

	result, err := provider.Execute(ctx, actions[0].Name, nil, token)
	if err != nil {
		_ = s.repo.UpdateStatus(ctx, id, "error")
		return nil, err
	}

	return result, nil
}

func (s *Service) StartRefreshLoop(ctx context.Context) {
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	slog.Info("mcp: token refresh goroutine started", "interval", refreshInterval)
	for {
		select {
		case <-ctx.Done():
			slog.Info("mcp: token refresh goroutine stopped")
			return
		case <-ticker.C:
			s.refreshExpiring(ctx)
		}
	}
}

func (s *Service) refreshExpiring(ctx context.Context) {
	connections, err := s.repo.ListExpiring(ctx, refreshThreshold)
	if err != nil {
		slog.Warn("mcp: failed to list expiring connections", "error", err)
		return
	}

	for _, conn := range connections {
		if conn.RefreshVaultKey == "" {
			continue
		}

		provider, ok := s.registry.Get(conn.Provider)
		if !ok {
			continue
		}

		refreshToken, err := s.vault.Get(conn.RefreshVaultKey)
		if err != nil || refreshToken == "" {
			slog.Warn("mcp: missing refresh token", "connection_id", conn.ID, "provider", conn.Provider)
			continue
		}

		newAccess, newRefresh, expiresIn, err := provider.RefreshToken(ctx, refreshToken)
		if err != nil {
			slog.Warn("mcp: token refresh failed", "connection_id", conn.ID, "provider", conn.Provider, "error", err)
			_ = s.repo.UpdateStatus(ctx, conn.ID, "expired")
			continue
		}

		newVaultKey := fmt.Sprintf("mcp:%s:%s:access", conn.Provider, conn.ID)
		if err := s.vault.Set(newVaultKey, newAccess); err != nil {
			slog.Error("mcp: vault write failed during refresh", "error", err)
			continue
		}

		newRefreshKey := conn.RefreshVaultKey
		if newRefresh != "" {
			if err := s.vault.Set(newRefreshKey, newRefresh); err != nil {
				slog.Error("mcp: vault write failed during refresh (refresh token)", "error", err)
			}
		}

		expiresAt := time.Now().Add(expiresIn)
		if err := s.repo.UpdateTokenRefs(ctx, conn.ID, newVaultKey, newRefreshKey, &expiresAt); err != nil {
			slog.Error("mcp: failed to update token refs", "error", err)
			continue
		}

		if s.rdb != nil {
			s.rdb.Del(ctx, tokenCachePrefix+conn.ID.String())
		}

		slog.Info("mcp: refreshed token", "connection_id", conn.ID, "provider", conn.Provider, "expires_in", expiresIn)
	}
}

func (s *Service) resolveToken(ctx context.Context, conn *store.MCPConnection) (string, error) {
	if s.rdb != nil {
		cached, err := s.rdb.Get(ctx, tokenCachePrefix+conn.ID.String()).Result()
		if err == nil && cached != "" {
			return cached, nil
		}
	}

	token, err := s.vault.Get(conn.VaultKey)
	if err != nil {
		return "", fmt.Errorf("vault read failed for key %s: %w", conn.VaultKey, err)
	}
	if token == "" {
		return "", fmt.Errorf("empty token in vault for connection %s", conn.ID)
	}

	if s.rdb != nil {
		s.rdb.Set(ctx, tokenCachePrefix+conn.ID.String(), token, tokenCacheTTL)
	}

	return token, nil
}

func (s *Service) isProviderAllowed(name string) bool {
	if len(s.cfg.AllowedProviders) == 0 {
		return true
	}
	for _, p := range s.cfg.AllowedProviders {
		if strings.EqualFold(p, name) {
			return true
		}
	}
	return false
}

func (s *Service) findAction(p Provider, name string) *ActionDef {
	for _, a := range p.Actions() {
		if strings.EqualFold(a.Name, name) {
			return &a
		}
	}
	return nil
}

func (s *Service) validateParams(action *ActionDef, params map[string]interface{}) error {
	for _, rp := range action.RequiredParams {
		val, ok := params[rp]
		if !ok {
			return fmt.Errorf("missing required parameter %q for action %q", rp, action.Name)
		}
		if str, isStr := val.(string); isStr && str == "" {
			return fmt.Errorf("required parameter %q cannot be empty for action %q", rp, action.Name)
		}
	}
	return nil
}

func (s *Service) sanitizeMap(data map[string]interface{}) {
	for key, val := range data {
		switch v := val.(type) {
		case string:
			data[key] = sanitizeString(v)
		case map[string]interface{}:
			s.sanitizeMap(v)
		}
	}
}

func sanitizeString(s string) string {
	result := s
	for _, pat := range sensitivePatterns {
		result = pat.ReplaceAllString(result, "[REDACTED]")
	}
	return result
}

func (s *Service) logCall(ctx context.Context, connectionID uuid.UUID, req ExecuteRequest, status string, latencyMs int, errMsg string) {
	inputBytes, _ := json.Marshal(req.Params)
	inputHash := hashSHA256(inputBytes)
	outputHash := ""
	if errMsg != "" {
		outputHash = hashSHA256([]byte(errMsg))
	}

	entry := &store.MCPCallLogEntry{
		ConnectionID: connectionID,
		AgentID:      req.AgentID,
		TaskID:       req.TaskID,
		Action:       req.Action,
		InputHash:    inputHash,
		OutputHash:   outputHash,
		LatencyMs:    latencyMs,
		Status:       status,
		ErrorMessage: errMsg,
	}
	if err := s.repo.InsertCallLog(ctx, entry); err != nil {
		slog.Warn("mcp: failed to insert call log", "error", err)
	}
}

func hashSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}
