// Package background provides periodic background tasks for the server.
//
// Tasks include index job cleanup, LLM health monitoring, and engine health checks.
package background

import (
	"context"
	"log/slog"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/indexing"
	"github.com/AuralithAI/rtvortex-server/internal/llm"
)

// Scheduler runs periodic background tasks.
type Scheduler struct {
	ctx             context.Context
	cancel          context.CancelFunc
	engineClient    *engine.Client
	llmRegistry     *llm.Registry
	indexingService *indexing.Service
}

// NewScheduler creates a background task scheduler.
func NewScheduler(
	ctx context.Context,
	engineClient *engine.Client,
	llmRegistry *llm.Registry,
	indexingService *indexing.Service,
) *Scheduler {
	taskCtx, cancel := context.WithCancel(ctx)
	return &Scheduler{
		ctx:             taskCtx,
		cancel:          cancel,
		engineClient:    engineClient,
		llmRegistry:     llmRegistry,
		indexingService: indexingService,
	}
}

// Start launches all background tasks.
func (s *Scheduler) Start() {
	slog.Info("background scheduler starting")

	go s.runPeriodic("index-job-cleanup", 5*time.Minute, s.cleanupIndexJobs)
	go s.runPeriodic("llm-health-check", 2*time.Minute, s.checkLLMHealth)
	go s.runPeriodic("engine-health-check", 30*time.Second, s.checkEngineHealth)

	slog.Info("background scheduler started", "tasks", 3)
}

// Stop cancels all background tasks.
func (s *Scheduler) Stop() {
	s.cancel()
	slog.Info("background scheduler stopped")
}

// runPeriodic runs a task function at regular intervals.
func (s *Scheduler) runPeriodic(name string, interval time.Duration, fn func()) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	slog.Debug("background task registered", "task", name, "interval", interval)

	for {
		select {
		case <-s.ctx.Done():
			slog.Debug("background task stopping", "task", name)
			return
		case <-ticker.C:
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("background task panicked", "task", name, "panic", r)
					}
				}()
				fn()
			}()
		}
	}
}

// ── Tasks ───────────────────────────────────────────────────────────────────

func (s *Scheduler) cleanupIndexJobs() {
	removed := s.indexingService.CleanupOldJobs(1 * time.Hour)
	if removed > 0 {
		slog.Info("cleaned up old index jobs", "removed", removed)
	}
}

func (s *Scheduler) checkLLMHealth() {
	for _, name := range s.llmRegistry.ListProviders() {
		provider, ok := s.llmRegistry.Get(name)
		if !ok {
			continue
		}
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		healthy := provider.Healthy(ctx)
		cancel()
		if !healthy {
			slog.Warn("LLM provider unhealthy", "provider", name)
		}
	}
}

func (s *Scheduler) checkEngineHealth() {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	healthy := s.engineClient.IsHealthy(ctx)
	if !healthy {
		slog.Warn("RTVortex engine unhealthy")
	}
}
