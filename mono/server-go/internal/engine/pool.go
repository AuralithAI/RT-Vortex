// Package engine provides a gRPC connection pool to the RTVortex C++ engine.
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/AuralithAI/rtvortex-server/internal/config"
	"github.com/AuralithAI/rtvortex-server/internal/metrics"
)

// Pool manages a pool of gRPC connections to the RTVortex C++ engine.
// It provides round-robin channel selection and automatic reconnection.
type Pool struct {
	cfg    config.EngineConfig
	conns  []*grpc.ClientConn
	mu     sync.RWMutex
	next   atomic.Uint64
	ctx    context.Context
	cancel context.CancelFunc
}

// NewPool creates a new gRPC connection pool to the RTVortex engine.
func NewPool(ctx context.Context, cfg config.EngineConfig) (*Pool, error) {
	poolCtx, cancel := context.WithCancel(ctx)
	p := &Pool{
		cfg:    cfg,
		conns:  make([]*grpc.ClientConn, 0, cfg.MaxChannels),
		ctx:    poolCtx,
		cancel: cancel,
	}

	target := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	// Build dial options
	opts := p.buildDialOptions()

	// Create connection pool
	for i := 0; i < cfg.MaxChannels; i++ {
		conn, err := grpc.NewClient(target, opts...)
		if err != nil {
			// Close already-created connections
			p.Close()
			return nil, fmt.Errorf("creating gRPC channel %d: %w", i, err)
		}
		p.conns = append(p.conns, conn)
	}

	// Start health check loop
	go p.healthCheckLoop()

	slog.Info("engine gRPC pool initialized",
		"target", target,
		"channels", cfg.MaxChannels,
		"tls", cfg.TLS,
	)

	return p, nil
}

// GetConn returns a gRPC connection from the pool using round-robin.
func (p *Pool) GetConn() *grpc.ClientConn {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if len(p.conns) == 0 {
		return nil
	}

	idx := p.next.Add(1) % uint64(len(p.conns))
	return p.conns[idx]
}

// Close closes all connections in the pool.
func (p *Pool) Close() {
	p.cancel()
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, conn := range p.conns {
		if conn != nil {
			conn.Close()
		}
	}
	p.conns = nil
	slog.Info("engine gRPC pool closed")
}

// Healthy returns true if at least one connection is in a ready state.
func (p *Pool) Healthy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, conn := range p.conns {
		if conn == nil {
			continue
		}
		state := conn.GetState()
		if state == connectivity.Ready || state == connectivity.Idle {
			return true
		}
	}
	return false
}

func (p *Pool) buildDialOptions() []grpc.DialOption {
	opts := []grpc.DialOption{
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(64*1024*1024), // 64 MB
			grpc.MaxCallSendMsgSize(64*1024*1024),
		),
	}

	if p.cfg.TLS {
		creds, err := credentials.NewClientTLSFromFile(p.cfg.CAFile, "")
		if err != nil {
			slog.Warn("failed to load TLS creds, falling back to insecure", "error", err)
			opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
		} else {
			opts = append(opts, grpc.WithTransportCredentials(creds))
		}
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	return opts
}

func (p *Pool) healthCheckLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	consecutiveFailures := 0

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			healthy := p.checkAndReconnect()
			if healthy {
				metrics.EnginePoolHealthy.Set(1)
				if consecutiveFailures > 0 {
					slog.Info("engine gRPC pool recovered",
						"after_failures", consecutiveFailures,
					)
				}
				consecutiveFailures = 0
			} else {
				metrics.EnginePoolHealthy.Set(0)
				consecutiveFailures++
				slog.Warn("engine gRPC pool unhealthy",
					"consecutive_failures", consecutiveFailures,
				)
			}
		}
	}
}

// checkAndReconnect inspects each connection in the pool and attempts to
// rebuild any that are in a terminal failure state. It returns true if at
// least one connection is healthy after the check.
func (p *Pool) checkAndReconnect() bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	target := fmt.Sprintf("%s:%d", p.cfg.Host, p.cfg.Port)
	anyHealthy := false

	for i, conn := range p.conns {
		if conn == nil {
			// Connection slot is empty — try to create a new one.
			if replacement := p.tryCreateConn(target, i); replacement != nil {
				p.conns[i] = replacement
				anyHealthy = true
			}
			continue
		}

		state := conn.GetState()
		switch state {
		case connectivity.Ready, connectivity.Idle:
			anyHealthy = true

		case connectivity.TransientFailure:
			// Ask gRPC to retry immediately instead of waiting for backoff.
			conn.ResetConnectBackoff()
			slog.Debug("engine gRPC channel reset backoff",
				"channel", i,
				"state", state.String(),
			)

		case connectivity.Shutdown:
			// Connection is permanently closed — rebuild it.
			slog.Warn("engine gRPC channel is shut down, reconnecting",
				"channel", i,
			)
			conn.Close()
			if replacement := p.tryCreateConn(target, i); replacement != nil {
				p.conns[i] = replacement
				anyHealthy = true
			} else {
				p.conns[i] = nil
			}

		case connectivity.Connecting:
			// Still trying — give it time.
			slog.Debug("engine gRPC channel connecting", "channel", i)
		}
	}

	return anyHealthy
}

// tryCreateConn creates a new gRPC connection to the target.
// Returns nil on failure (logged but not fatal).
func (p *Pool) tryCreateConn(target string, channelIndex int) *grpc.ClientConn {
	metrics.EngineReconnectsTotal.Inc()

	opts := p.buildDialOptions()
	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		slog.Error("engine gRPC reconnect failed",
			"channel", channelIndex,
			"target", target,
			"error", err,
		)
		return nil
	}

	slog.Info("engine gRPC channel reconnected",
		"channel", channelIndex,
		"target", target,
	)
	return conn
}
