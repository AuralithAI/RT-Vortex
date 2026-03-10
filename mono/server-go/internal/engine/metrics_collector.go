// Package engine provides the gRPC client for communicating with the RTVortex C++ engine.
//
// metrics_collector.go implements a background goroutine that opens a
// StreamEngineMetrics server-streaming RPC to the C++ engine and stores
// the latest snapshot atomically.  The API layer can read the latest
// snapshot via LatestSnapshot() and the WebSocket hub can broadcast it.

package engine

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/AuralithAI/rtvortex-server/internal/engine/pb"
)

// ── Public types ────────────────────────────────────────────────────────────

// HistogramSnapshot mirrors the proto HistogramProto.
type HistogramSnapshot struct {
	Count  uint64  `json:"count"`
	Sum    float64 `json:"sum"`
	MinVal float64 `json:"min_val"`
	MaxVal float64 `json:"max_val"`
	Avg    float64 `json:"avg"`
	P50    float64 `json:"p50"`
	P90    float64 `json:"p90"`
	P95    float64 `json:"p95"`
	P99    float64 `json:"p99"`
}

// MetricValue represents a single metric in the snapshot.
type MetricValue struct {
	Type      string             `json:"type"` // "counter", "gauge", "histogram"
	Scalar    float64            `json:"scalar,omitempty"`
	Histogram *HistogramSnapshot `json:"histogram,omitempty"`
}

// EngineMetricsSnapshot is the Go representation of a metrics push from the C++ engine.
type EngineMetricsSnapshot struct {
	TimestampMs         uint64                 `json:"timestamp_ms"`
	Metrics             map[string]MetricValue `json:"metrics"`
	UptimeS             uint64                 `json:"uptime_s"`
	IndexSizesBytes     map[string]uint64      `json:"index_sizes_bytes,omitempty"`
	KnowledgeGraphNodes uint64                 `json:"knowledge_graph_nodes,omitempty"`
	KnowledgeGraphEdges uint64                 `json:"knowledge_graph_edges,omitempty"`
}

// MetricsWSEvent is the JSON envelope sent over the WebSocket.
type MetricsWSEvent struct {
	Type string                 `json:"type"` // always "engine_metrics"
	Data *EngineMetricsSnapshot `json:"data"`
}

// ── Collector ───────────────────────────────────────────────────────────────

// MetricsCollector opens a long-lived gRPC stream to the C++ engine and
// stores the latest EngineMetricsSnapshot atomically.  It automatically
// reconnects with exponential back-off.
type MetricsCollector struct {
	client     *Client
	intervalMs uint32

	latest atomic.Pointer[EngineMetricsSnapshot]

	// Broadcast callback — set by the WS layer via OnSnapshot.
	mu       sync.RWMutex
	onUpdate func(snap *EngineMetricsSnapshot)

	cancel context.CancelFunc
}

// NewMetricsCollector creates a new collector.
// Call Start() to begin streaming.
func NewMetricsCollector(client *Client, intervalMs uint32) *MetricsCollector {
	if intervalMs == 0 {
		intervalMs = 1000
	}
	return &MetricsCollector{
		client:     client,
		intervalMs: intervalMs,
	}
}

// OnSnapshot registers a callback that fires on each new snapshot.
// Typically used to broadcast to WebSocket subscribers.
func (mc *MetricsCollector) OnSnapshot(fn func(snap *EngineMetricsSnapshot)) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.onUpdate = fn
}

// LatestSnapshot returns the most recently received snapshot, or nil.
func (mc *MetricsCollector) LatestSnapshot() *EngineMetricsSnapshot {
	return mc.latest.Load()
}

// Start begins the background streaming loop.
func (mc *MetricsCollector) Start(ctx context.Context) {
	ctx, mc.cancel = context.WithCancel(ctx)
	go mc.loop(ctx)
}

// Stop terminates the streaming loop.
func (mc *MetricsCollector) Stop() {
	if mc.cancel != nil {
		mc.cancel()
	}
}

func (mc *MetricsCollector) loop(ctx context.Context) {
	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			slog.Info("metrics collector stopped")
			return
		default:
		}

		err := mc.stream(ctx)
		if ctx.Err() != nil {
			return // context cancelled
		}

		slog.Warn("engine metrics stream disconnected, reconnecting",
			"error", err,
			"backoff", backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Exponential backoff
		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (mc *MetricsCollector) stream(ctx context.Context) error {
	stub := mc.client.stub()
	stream, err := stub.StreamEngineMetrics(ctx, &pb.EngineMetricsRequest{
		IntervalMs: mc.intervalMs,
	})
	if err != nil {
		return err
	}

	slog.Info("engine metrics stream connected", "interval_ms", mc.intervalMs)

	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		snap := convertMetricsSnapshot(msg)
		mc.latest.Store(snap)

		mc.mu.RLock()
		fn := mc.onUpdate
		mc.mu.RUnlock()

		if fn != nil {
			fn(snap)
		}
	}
}

// ── Conversion ──────────────────────────────────────────────────────────────

func convertMetricsSnapshot(msg *pb.EngineMetricsSnapshot) *EngineMetricsSnapshot {
	snap := &EngineMetricsSnapshot{
		TimestampMs:         msg.TimestampMs,
		UptimeS:             msg.UptimeS,
		Metrics:             make(map[string]MetricValue, len(msg.Metrics)),
		KnowledgeGraphNodes: msg.KnowledgeGraphNodes,
		KnowledgeGraphEdges: msg.KnowledgeGraphEdges,
	}

	// Per-repo index sizes
	if len(msg.IndexSizesBytes) > 0 {
		snap.IndexSizesBytes = make(map[string]uint64, len(msg.IndexSizesBytes))
		for k, v := range msg.IndexSizesBytes {
			snap.IndexSizesBytes[k] = v
		}
	}

	for name, mv := range msg.Metrics {
		val := MetricValue{}
		switch mv.Type {
		case pb.MetricValueProto_COUNTER:
			val.Type = "counter"
			val.Scalar = mv.Scalar
		case pb.MetricValueProto_GAUGE:
			val.Type = "gauge"
			val.Scalar = mv.Scalar
		case pb.MetricValueProto_HISTOGRAM:
			val.Type = "histogram"
			if mv.Histogram != nil {
				val.Histogram = &HistogramSnapshot{
					Count:  mv.Histogram.Count,
					Sum:    mv.Histogram.Sum,
					MinVal: mv.Histogram.MinVal,
					MaxVal: mv.Histogram.MaxVal,
					Avg:    mv.Histogram.Avg,
					P50:    mv.Histogram.P50,
					P90:    mv.Histogram.P90,
					P95:    mv.Histogram.P95,
					P99:    mv.Histogram.P99,
				}
			}
		}
		snap.Metrics[name] = val
	}

	return snap
}

// MarshalWSEvent serializes a MetricsWSEvent for the WS hub.
func MarshalWSEvent(snap *EngineMetricsSnapshot) ([]byte, error) {
	evt := MetricsWSEvent{
		Type: "engine_metrics",
		Data: snap,
	}
	return json.Marshal(evt)
}
