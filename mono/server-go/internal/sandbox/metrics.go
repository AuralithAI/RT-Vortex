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

	// BuildComplexityDistribution tracks build complexity label distribution.
	BuildComplexityDistribution = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_complexity_total",
		Help:      "Build complexity scoring distribution by label.",
	}, []string{"label"})

	// BuildComplexityScore observes the raw complexity score (0.0–1.0).
	BuildComplexityScore = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_complexity_score",
		Help:      "Raw build complexity score distribution.",
		Buckets:   []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
	})

	// BuildFailureProbability observes estimated failure probability.
	BuildFailureProbability = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_failure_probability",
		Help:      "Estimated build failure probability.",
		Buckets:   []float64{0.05, 0.1, 0.15, 0.2, 0.3, 0.4, 0.5},
	})

	// BuildFastPathTotal counts builds that took the fast path (dep-install skipped).
	BuildFastPathTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_fast_path_total",
		Help:      "Total builds that skipped dependency installation (fast path).",
	})

	// BuildFingerprintHits counts fingerprint cache hits.
	BuildFingerprintHits = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_fingerprint_hits_total",
		Help:      "Total builds where the build-config fingerprint matched the previous successful build.",
	})

	// BuildConfigLimitsApplied counts builds where config-driven limits were applied.
	BuildConfigLimitsApplied = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "build_config_limits_applied_total",
		Help:      "Total builds where server config limits overrode request values.",
	})

	// AuditEventsTotal counts audit trail events by action type.
	AuditEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "audit_events_total",
		Help:      "Total security audit events by action.",
	}, []string{"action"})

	// LogRedactions counts log redaction operations.
	LogRedactions = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "log_redactions_total",
		Help:      "Total log redaction operations applied to build output.",
	})

	// SecretLeakDetections counts times a secret value was found verbatim in logs.
	SecretLeakDetections = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "secret_leak_detections_total",
		Help:      "Total times a secret value was detected verbatim in build output.",
	})

	// OwnershipCheckFailures counts failed build ownership validations.
	OwnershipCheckFailures = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: metricsNS,
		Subsystem: metricsSub,
		Name:      "ownership_check_failures_total",
		Help:      "Total failed build ownership validation attempts.",
	})
)
