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
)

// ── Multi-LLM Probe Metrics ────────────────────────────────────────────────

var (
	// SwarmProbeCallsTotal counts multi-LLM probe requests by outcome.
	SwarmProbeCallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "probe_calls_total",
		Help:      "Total multi-LLM probe calls by status (ok, error, all_failed).",
	}, []string{"status"})

	// SwarmProbeWallTime observes the total wall-clock time for a probe (all providers).
	SwarmProbeWallTime = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "probe_wall_time_seconds",
		Help:      "Wall-clock time for a multi-LLM probe (all providers in parallel).",
		Buckets:   []float64{1, 2, 5, 10, 20, 30, 60, 120},
	})

	// SwarmProbeProviderLatency observes per-provider latency within a probe.
	SwarmProbeProviderLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "probe_provider_latency_seconds",
		Help:      "Per-provider latency within a multi-LLM probe.",
		Buckets:   []float64{0.5, 1, 2, 5, 10, 20, 30, 60},
	}, []string{"provider", "status"})

	// SwarmProbeProviderCount observes how many providers were probed per request.
	SwarmProbeProviderCount = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "probe_provider_count",
		Help:      "Number of providers probed per multi-LLM request.",
		Buckets:   []float64{1, 2, 3, 4, 5, 6, 7, 8},
	})

	// ── Discussion Protocol metrics ─────────────────────────

	// SwarmDiscussionEventsTotal counts discussion thread lifecycle events.
	SwarmDiscussionEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "discussion_events_total",
		Help:      "Total multi-LLM discussion thread events by type.",
	}, []string{"event"})

	// ── Consensus Engine metrics ─────────────────────────────

	// SwarmConsensusRunsTotal counts consensus engine runs by strategy.
	SwarmConsensusRunsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "consensus_runs_total",
		Help:      "Total consensus engine runs by strategy (pick_best, majority_vote, gpt_as_judge, multi_judge_panel).",
	}, []string{"strategy"})

	// SwarmConsensusWinnerTotal counts which provider won consensus by strategy.
	SwarmConsensusWinnerTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "consensus_winner_total",
		Help:      "Which provider won consensus, by strategy.",
	}, []string{"strategy", "provider"})

	// SwarmConsensusConfidence observes consensus confidence scores.
	SwarmConsensusConfidence = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "consensus_confidence",
		Help:      "Confidence score distribution per consensus strategy.",
		Buckets:   []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
	}, []string{"strategy"})

	// SwarmConsensusLatency observes the time taken for consensus runs.
	SwarmConsensusLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "consensus_latency_seconds",
		Help:      "Time taken for consensus engine runs by strategy.",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30},
	}, []string{"strategy"})

	// SwarmConsensusJudgeCount observes how many judges participated in multi-judge panel runs.
	SwarmConsensusJudgeCount = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "consensus_judge_count",
		Help:      "Number of judges in multi-judge panel consensus runs.",
		Buckets:   []float64{1, 2, 3, 4, 5, 6, 7, 8},
	}, []string{"strategy"})

	// SwarmConsensusJudgeAgreement observes inter-judge agreement in multi-judge panel runs.
	SwarmConsensusJudgeAgreement = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "consensus_judge_agreement",
		Help:      "Inter-judge agreement (0.0-1.0) in multi-judge panel consensus runs.",
		Buckets:   []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
	}, []string{"strategy"})

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

// ── Role-Based ELO Metrics ────────────────────────────────────────

var (
	// SwarmRoleELOGauge tracks current ELO score per (role, repo_id).
	SwarmRoleELOGauge = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "role_elo_score",
		Help:      "Current ELO score per (role, repo_id) pair.",
	}, []string{"role", "repo_id"})

	// SwarmRoleTierChanges counts tier promotions/demotions.
	SwarmRoleTierChanges = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "role_tier_changes_total",
		Help:      "Total role tier changes (promotion/demotion) by role and direction.",
	}, []string{"role", "old_tier", "new_tier"})

	// SwarmRoleELODecayTotal counts decay events per role.
	SwarmRoleELODecayTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "role_elo_decay_total",
		Help:      "Total ELO decay events per role (from inactivity).",
	}, []string{"role"})

	// SwarmRoleOutcomeTotal counts task outcomes recorded for role ELO.
	SwarmRoleOutcomeTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "role_outcome_total",
		Help:      "Total task outcomes recorded for role ELO by event type.",
	}, []string{"role", "event_type"})

	// SwarmRoleTrainingProbes counts extra training probes for restricted-tier roles.
	SwarmRoleTrainingProbes = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "role_training_probes_total",
		Help:      "Total extra training probes issued for restricted-tier roles.",
	}, []string{"role"})

	// SwarmRoleTierDistribution tracks current count of role ELOs per tier.
	SwarmRoleTierDistribution = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "role_tier_distribution",
		Help:      "Number of role ELO records in each tier (standard, expert, restricted).",
	}, []string{"tier"})
)

// ── CI Signal Ingestion Metrics ───────────────────────────────────

var (
	// SwarmCISignalPollCycles counts completed CI signal poll cycles.
	SwarmCISignalPollCycles = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "ci_signal_poll_cycles_total",
		Help:      "Total CI signal poll cycles completed.",
	})

	// SwarmCISignalSeeded counts CI signal records seeded (new tasks with PRs).
	SwarmCISignalSeeded = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "ci_signal_seeded_total",
		Help:      "Total CI signal records seeded from completed tasks.",
	})

	// SwarmCISignalPolled counts individual CI signal polls against VCS platforms.
	SwarmCISignalPolled = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "ci_signal_polled_total",
		Help:      "Total individual CI signal polls against VCS platforms.",
	})

	// SwarmCISignalIngested counts CI signals that were ingested into role ELO.
	SwarmCISignalIngested = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "ci_signal_elo_ingested_total",
		Help:      "Total CI signals ingested into role ELO system.",
	})

	// SwarmCISignalFinalized counts CI signals that were finalized (no more polling).
	SwarmCISignalFinalized = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "ci_signal_finalized_total",
		Help:      "Total CI signal records finalized.",
	})

	// SwarmCISignalCycleDuration observes CI signal poll cycle duration.
	SwarmCISignalCycleDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "ci_signal_cycle_seconds",
		Help:      "Duration of CI signal poll cycles.",
		Buckets:   prometheus.DefBuckets,
	})

	// SwarmCISignalELOUpdates counts ELO updates from CI signals per role and CI state.
	SwarmCISignalELOUpdates = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "ci_signal_elo_updates_total",
		Help:      "ELO updates triggered by CI signal ingestion, by role and CI state.",
	}, []string{"role", "ci_state"})
)

// ── Team Formation Metrics ────────────────────────────────────────

var (
	// SwarmTeamFormationsTotal counts team formation recommendations by complexity and strategy.
	SwarmTeamFormationsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "team_formations_total",
		Help:      "Total team formation recommendations by complexity label and strategy.",
	}, []string{"complexity", "strategy"})
)

// ── Adaptive Probe Tuning Metrics ─────────────────────────────────

var (
	// SwarmProbeTuningOutcomes counts probe outcomes by role, complexity, and status.
	SwarmProbeTuningOutcomes = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "probe_tuning_outcomes_total",
		Help:      "Total adaptive probe outcomes by role, complexity label, and status (ok/error).",
	}, []string{"role", "complexity", "status"})

	// SwarmProbeTuningLatency observes probe latency per role.
	SwarmProbeTuningLatency = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "probe_tuning_latency_seconds",
		Help:      "Adaptive probe latency per role.",
		Buckets:   []float64{0.5, 1, 2, 5, 10, 20, 30, 60, 120},
	}, []string{"role"})

	// SwarmProbeTuningTokens counts tokens consumed per role through adaptive probes.
	SwarmProbeTuningTokens = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "probe_tuning_tokens_total",
		Help:      "Total tokens consumed by adaptive probes per role.",
	}, []string{"role"})

	// SwarmProbeTuningCostUSD tracks estimated USD cost per role.
	SwarmProbeTuningCostUSD = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "probe_tuning_cost_usd_total",
		Help:      "Estimated USD cost of adaptive probes per role.",
	}, []string{"role"})

	// SwarmProbeTuningAdjustments counts tuning engine adjustments by role and strategy.
	SwarmProbeTuningAdjustments = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "probe_tuning_adjustments_total",
		Help:      "Total adaptive probe tuning config adjustments by role and strategy.",
	}, []string{"role", "strategy"})
)

// ── Self-Healing Pipeline Metrics ─────────────────────────────────

var (
	// SwarmSelfHealEventsTotal counts self-heal events by type.
	SwarmSelfHealEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "self_heal_events_total",
		Help:      "Total self-healing events by type (circuit_opened, task_retry, stuck_task_detected, etc.).",
	}, []string{"event_type"})

	// SwarmSelfHealCircuitTransitions counts circuit-breaker state transitions.
	SwarmSelfHealCircuitTransitions = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "self_heal_circuit_transitions_total",
		Help:      "Total circuit-breaker state transitions by provider and new state.",
	}, []string{"provider", "new_state"})

	// SwarmSelfHealProviderFailures counts provider failure reports.
	SwarmSelfHealProviderFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "self_heal_provider_failures_total",
		Help:      "Total provider failure reports by provider name.",
	}, []string{"provider"})

	// SwarmSelfHealCycleDuration observes the self-heal background cycle duration.
	SwarmSelfHealCycleDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "self_heal_cycle_seconds",
		Help:      "Duration of self-heal background loop cycles.",
		Buckets:   []float64{0.01, 0.05, 0.1, 0.5, 1, 5},
	})

	// SwarmSelfHealOpenCircuits tracks the current number of open circuits.
	SwarmSelfHealOpenCircuits = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: swarmNS,
		Subsystem: swarmSub,
		Name:      "self_heal_open_circuits",
		Help:      "Current number of open (unavailable) provider circuit breakers.",
	})
)
