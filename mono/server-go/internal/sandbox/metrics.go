package sandbox

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const metricsNS = "rtvortex"
const metricsSub = "sandbox"

// ── Build Metrics ───────────────────────────────────────────────────────────

var (
	// BuildTotal counts builds by final status.
	BuildTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_total",
		Help:      "Total sandbox builds by status (success, failed, blocked, skipped).",
	}, []string{"status"})

	// BuildDuration observes build execution duration.
	BuildDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_duration_seconds",
		Help:      "Build execution duration from start to exit.",
		Buckets:   []float64{5, 15, 30, 60, 120, 300, 600},
	})

	// BuildRetries counts build retry attempts.
	BuildRetries = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_retries_total",
		Help:      "Total build retry attempts.",
	})

	// BuildSecretInjects counts secret injections into containers.
	BuildSecretInjects = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_secret_injects_total",
		Help:      "Total secrets injected into sandbox containers (count, not values).",
	})

	// BuildContainersActive tracks currently running build containers.
	BuildContainersActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_containers_active",
		Help:      "Number of currently active sandbox build containers.",
	})

	// BuildCacheHits counts layer cache hits.
	BuildCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_cache_hits_total",
		Help:      "Total build cache hits (reused dependency layer).",
	})

	// BuildSkipped counts builds skipped because diffs only touch non-code files.
	BuildSkipped = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_skipped_total",
		Help:      "Total builds skipped (non-code-only diffs).",
	})

	// ProbeTotal counts pre-build environment probe invocations.
	ProbeTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "probe_total",
		Help:      "Total pre-build environment probes executed.",
	})

	// ProbeMissingSecrets counts env vars detected that have no matching secret.
	ProbeMissingSecrets = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "probe_missing_secrets_total",
		Help:      "Total missing secrets detected across all probes.",
	})

	// HITLConfirmations counts build plan confirmation requests sent to humans.
	HITLConfirmations = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "hitl_confirmations_total",
		Help:      "Build plan HITL confirmations by outcome (approved, rejected, timeout).",
	}, []string{"outcome"})

	// SecretResolutions counts secret resolution attempts by result.
	SecretResolutions = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "secret_resolutions_total",
		Help:      "Secret resolution attempts by result (resolved, failed).",
	}, []string{"result"})

	// ArtifactsCollected counts build artifacts collected after builds.
	ArtifactsCollected = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "artifacts_collected_total",
		Help:      "Total build artifacts collected from sandbox containers.",
	})

	// ArtifactBytes tracks total bytes of collected artifacts.
	ArtifactBytes = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "artifact_bytes_total",
		Help:      "Total bytes of collected build artifacts.",
	})

	// WorkspaceInjections counts workspace changeset injections into containers.
	WorkspaceInjections = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "workspace_injections_total",
		Help:      "Total workspace changeset injections into sandbox containers.",
	})
)
