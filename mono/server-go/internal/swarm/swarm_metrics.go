// Package swarm — Prometheus metrics for the agent swarm subsystem.
//
// All swarm-specific counters, histograms, and gauges are declared here.
// Other packages import metrics from this file via the recording helpers.
package swarm

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const swarmNS = "rtvortex"
const swarmSub = "swarm"

// ── Task Metrics ────────────────────────────────────────────────────────────

var (
	// SwarmTasksTotal counts tasks by final status.
	SwarmTasksTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "tasks_total",
		Help:      "Total swarm tasks by status outcome (completed, failed, timed_out, cancelled).",
	}, []string{"status"})

	// SwarmTasksActive tracks currently active tasks (not in a terminal state).
	SwarmTasksActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "tasks_active",
		Help:      "Number of swarm tasks currently in-progress.",
	})

	// SwarmTaskDuration observes end-to-end task duration from creation to completion.
	SwarmTaskDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "task_duration_seconds",
		Help:      "End-to-end task duration from creation to terminal state.",
		Buckets:   []float64{30, 60, 120, 300, 600, 900, 1200, 1800, 3600},
	})

	// SwarmTaskRetriesTotal counts task retry attempts.
	SwarmTaskRetriesTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "task_retries_total",
		Help:      "Total number of task retry attempts.",
	})
)

// ── Team Metrics ────────────────────────────────────────────────────────────

var (
	// SwarmTeamsActive tracks teams that are not offline.
	SwarmTeamsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "teams_active",
		Help:      "Number of active swarm teams (idle or busy).",
	})

	// SwarmTeamsBusy tracks teams currently working on a task.
	SwarmTeamsBusy = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "teams_busy",
		Help:      "Number of swarm teams currently assigned to a task.",
	})
)

// ── Agent Metrics ───────────────────────────────────────────────────────────

var (
	// SwarmAgentsOnline tracks agents with a non-offline status.
	SwarmAgentsOnline = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "agents_online",
		Help:      "Number of online swarm agents.",
	})

	// SwarmAgentHeartbeatMisses counts heartbeat timeouts.
	SwarmAgentHeartbeatMisses = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "agent_heartbeat_misses_total",
		Help:      "Total agent heartbeat misses (agent marked offline).",
	})
)

// ── LLM Proxy Metrics ──────────────────────────────────────────────────────

var (
	// SwarmLLMCallsTotal counts LLM proxy calls from agents.
	SwarmLLMCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "llm_calls_total",
		Help:      "Total LLM proxy calls from swarm agents by status.",
	}, []string{"status"})

	// SwarmLLMCallDuration observes LLM proxy call latency.
	SwarmLLMCallDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "llm_call_duration_seconds",
		Help:      "LLM proxy call duration from swarm agents.",
		Buckets:   []float64{0.5, 1, 2, 5, 10, 30, 60, 120},
	})

	// SwarmLLMTokensTotal tracks token consumption through the swarm LLM proxy.
	SwarmLLMTokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "llm_tokens_total",
		Help:      "Total tokens used by swarm agents through the LLM proxy.",
	}, []string{"type"}) // prompt, completion

	// SwarmRAGCallsTotal counts RAG calls (engine searches) from agents.
	SwarmRAGCallsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "rag_calls_total",
		Help:      "Total RAG (engine search) calls from swarm agents.",
	})

	// SwarmLLMPercentage tracks the ratio of LLM calls to total calls (LLM + RAG).
	// This is updated periodically by the metrics refresh goroutine.
	SwarmLLMPercentage = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "llm_percentage",
		Help:      "Percentage of queries routed to LLM vs RAG-only (0-100).",
	})
)

// ── Diff / PR Metrics ───────────────────────────────────────────────────────

var (
	// SwarmDiffsTotal counts diffs produced by agents.
	SwarmDiffsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "diffs_total",
		Help:      "Total diffs produced by swarm agents by outcome (approved, rejected, pending).",
	}, []string{"status"})

	// SwarmPRsCreated counts PRs created by the swarm.
	SwarmPRsCreated = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "prs_created_total",
		Help:      "Total pull requests created by the swarm.",
	})

	// SwarmPRsFailed counts failed PR creation attempts.
	SwarmPRsFailed = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "prs_failed_total",
		Help:      "Total failed pull request creation attempts.",
	})
)

// ── Agent Utilisation ───────────────────────────────────────────────────────

var (
	// SwarmAgentUtilisation tracks the fraction of agents that are busy (0.0 - 1.0).
	SwarmAgentUtilisation = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "agent_utilisation",
		Help:      "Fraction of online agents that are currently busy (0.0 - 1.0).",
	})

	// SwarmQueueDepth tracks how many tasks are waiting in the pending queue.
	SwarmQueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "queue_depth",
		Help:      "Number of tasks waiting in the pending queue.",
	})
)

// ── Memory & HITL Metrics ───────────────────────────────────────────────────

var (
	// SwarmMTMStoreOps counts MTM store operations.
	SwarmMTMStoreOps = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "mtm_store_ops_total",
		Help:      "Total MTM (medium-term memory) store operations.",
	})

	// SwarmMTMRecallOps counts MTM recall operations.
	SwarmMTMRecallOps = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "mtm_recall_ops_total",
		Help:      "Total MTM (medium-term memory) recall operations.",
	})

	// SwarmHITLQuestionsTotal counts HITL questions asked by agents.
	SwarmHITLQuestionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "hitl_questions_total",
		Help:      "Total human-in-the-loop questions asked by agents.",
	})

	// SwarmHITLResponseTime observes how long humans take to respond.
	SwarmHITLResponseTime = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "hitl_response_seconds",
		Help:      "Time for humans to respond to HITL questions.",
		Buckets:   []float64{5, 15, 30, 60, 120, 300, 600},
	})

	// SwarmJanitorCycleDuration observes janitor cycle duration.
	SwarmJanitorCycleDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "janitor_cycle_seconds",
		Help:      "Duration of janitor cleanup cycles.",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 5},
	})

	// SwarmAgentTierDistribution tracks how many agents are in each tier.
	SwarmAgentTierDistribution = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "agent_tier_distribution",
		Help:      "Number of agents in each ELO-based tier (standard, expert, restricted).",
	}, []string{"tier"})

	// SwarmMemoryReflections counts memory reflection operations per agent role.
	SwarmMemoryReflections = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "memory_reflections_total",
		Help:      "Total memory reflection operations by agent role.",
	}, []string{"role"})
)

// ── Performance & Observability Metrics ─────────────────────────────────────

var (
	// SwarmEmbedLatency observes end-to-end embedding latency (including cache).
	SwarmEmbedLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "embed_latency_seconds",
		Help:      "End-to-end embedding call latency (including L1/L2 cache lookup).",
		Buckets:   []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	})

	// SwarmEmbedCacheHitRate tracks the cache hit ratio for embeddings (0.0-1.0).
	SwarmEmbedCacheHitRate = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "embed_cache_hit_rate",
		Help:      "Current embedding cache hit rate (0.0-1.0).",
	})

	// SwarmAgentIntercommLatency observes inter-agent message delivery latency.
	SwarmAgentIntercommLatency = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "agent_intercomm_latency_seconds",
		Help:      "Latency of inter-agent message delivery via Redis Streams.",
		Buckets:   []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
	})

	// SwarmWebFetchDuration observes URL fetch latency for agent web tool.
	SwarmWebFetchDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "web_fetch_duration_seconds",
		Help:      "HTTP fetch duration for agent web_search_and_fetch tool.",
		Buckets:   []float64{0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
	})

	// SwarmLLMAvoidanceRate tracks percentage of queries resolved by RAG without LLM.
	SwarmLLMAvoidanceRate = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "llm_avoidance_rate",
		Help:      "Fraction of agent queries resolved by RAG/cache without LLM call (0.0-1.0).",
	})

	// SwarmTaskQueueWait observes time tasks spend in pending queue before assignment.
	SwarmTaskQueueWait = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "task_queue_wait_seconds",
		Help:      "Time tasks spend in pending queue before team assignment.",
		Buckets:   []float64{0.5, 1, 5, 10, 30, 60, 120, 300},
	})

	// SwarmCleanupItemsTotal counts items cleaned by the janitor per type.
	SwarmCleanupItemsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "cleanup_items_total",
		Help:      "Total items cleaned by janitor by type (idle_teams, stale_heartbeats, orphan_stm, recycled_agents).",
	}, []string{"type"})

	// SwarmIngestAssetChunks counts asset chunks ingested (documents, PDFs, URLs).
	SwarmIngestAssetChunks = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "ingest_asset_chunks_total",
		Help:      "Total asset chunks ingested by type (document, pdf, url).",
	}, []string{"asset_type"})
)
