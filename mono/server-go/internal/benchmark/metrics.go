package benchmark

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ── Prometheus Metrics ──────────────────────────────────────────────────────

const ns = "rtvortex"

var (
	// BenchRunsTotal counts benchmark runs by mode and status.
	BenchRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns,
		Subsystem: "benchmark",
		Name:      "runs_total",
		Help:      "Total benchmark runs by mode and status.",
	}, []string{"mode", "status"})

	// BenchTasksTotal counts individual task executions.
	BenchTasksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns,
		Subsystem: "benchmark",
		Name:      "tasks_total",
		Help:      "Total benchmark task executions by mode and result.",
	}, []string{"mode", "result"})

	// BenchTaskDuration tracks task execution latency.
	BenchTaskDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: ns,
		Subsystem: "benchmark",
		Name:      "task_duration_seconds",
		Help:      "Benchmark task execution duration in seconds.",
		Buckets:   []float64{1, 5, 10, 30, 60, 120, 300, 600},
	}, []string{"mode", "category"})

	// BenchScoreHistogram tracks composite quality scores.
	BenchScoreHistogram = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: ns,
		Subsystem: "benchmark",
		Name:      "quality_score",
		Help:      "Benchmark task quality scores (0-1).",
		Buckets:   []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
	}, []string{"mode"})

	// BenchELORating tracks current ELO ratings.
	BenchELORating = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: ns,
		Subsystem: "benchmark",
		Name:      "elo_rating",
		Help:      "Current ELO rating by mode.",
	}, []string{"mode"})

	// BenchLLMCallsTotal tracks total LLM calls across benchmarks.
	BenchLLMCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: ns,
		Subsystem: "benchmark",
		Name:      "llm_calls_total",
		Help:      "Total LLM calls during benchmark runs by mode.",
	}, []string{"mode"})
)
