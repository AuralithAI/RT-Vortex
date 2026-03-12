// Package main is the entry point for the RTVortex API server.
//
// Architecture:
//
//	Clients (Web UI, CLI, Webhooks, SDKs)
//	        │
//	        ▼  REST / WebSocket
//	┌───────────────────────┐
//	│   RTVortex API Server │  ← this binary
//	│   (Go, chi router)    │
//	└───────┬───────────────┘
//	        │  gRPC
//	┌───────▼────────────────┐
//	│  RTVortex C++ Engine   │
//	│  (indexing, retrieval) │
//	└────────────────────────┘
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/AuralithAI/rtvortex-server/internal/audit"
	"github.com/AuralithAI/rtvortex-server/internal/auth"
	authproviders "github.com/AuralithAI/rtvortex-server/internal/auth/providers"
	"github.com/AuralithAI/rtvortex-server/internal/background"
	"github.com/AuralithAI/rtvortex-server/internal/chat"
	"github.com/AuralithAI/rtvortex-server/internal/config"
	rtcrypto "github.com/AuralithAI/rtvortex-server/internal/crypto"
	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/indexing"
	"github.com/AuralithAI/rtvortex-server/internal/llm"
	"github.com/AuralithAI/rtvortex-server/internal/prsync"
	"github.com/AuralithAI/rtvortex-server/internal/review"
	"github.com/AuralithAI/rtvortex-server/internal/rtenv"
	"github.com/AuralithAI/rtvortex-server/internal/rtlog"
	"github.com/AuralithAI/rtvortex-server/internal/server"
	"github.com/AuralithAI/rtvortex-server/internal/session"
	"github.com/AuralithAI/rtvortex-server/internal/store"
	"github.com/AuralithAI/rtvortex-server/internal/swarm"
	swarmauth "github.com/AuralithAI/rtvortex-server/internal/swarm/auth"
	"github.com/AuralithAI/rtvortex-server/internal/vault"
	"github.com/AuralithAI/rtvortex-server/internal/vcs"

	// Import platform packages to trigger init() factory registration.
	_ "github.com/AuralithAI/rtvortex-server/internal/vcs/azuredevops"
	_ "github.com/AuralithAI/rtvortex-server/internal/vcs/bitbucket"
	_ "github.com/AuralithAI/rtvortex-server/internal/vcs/github"
	_ "github.com/AuralithAI/rtvortex-server/internal/vcs/gitlab"
	"github.com/AuralithAI/rtvortex-server/internal/ws"
)

// Build-time variables set via -ldflags
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	// ── CLI flags ───────────────────────────────────────────────────────
	serverPropsPath := flag.String("config", "", "Path to rtserverprops.xml (auto-discovered if omitted)")
	showVersion := flag.Bool("version", false, "Print version and exit")
	showHelp := flag.Bool("help", false, "Print usage and exit")
	flag.Parse()

	if *showHelp {
		fmt.Println("RTVortex Go API Server")
		fmt.Printf("  Version: %s  Commit: %s  Built: %s\n\n", version, commit, buildDate)
		flag.PrintDefaults()
		os.Exit(0)
	}
	if *showVersion {
		fmt.Printf("RTVortexGo %s (commit %s, built %s)\n", version, commit, buildDate)
		os.Exit(0)
	}

	// ── Resolve RTVORTEX_HOME environment ───────────────────────────────
	env, err := rtenv.Resolve()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to resolve RTVORTEX_HOME: %v\n", err)
		os.Exit(1)
	}

	// ── Setup file-based logging (dual stdout + log file) ───────────────
	logCleanup, err := rtlog.Setup(env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup file logging: %v\n", err)
		os.Exit(1)
	}
	defer logCleanup()

	log.Printf("[INFO] RTVortexGo %s (commit %s, built %s)", version, commit, buildDate)
	log.Printf("[INFO] RTVORTEX_HOME = %s", env.Home)
	log.Printf("[INFO] Hostname      = %s", env.Hostname)
	log.Printf("[INFO] Config Dir    = %s", env.ConfigDir)
	log.Printf("[INFO] Temp Dir      = %s", env.TempDir)
	log.Printf("[INFO] Data Dir      = %s", env.DataDir)
	log.Printf("[INFO] Models Dir    = %s", env.ModelsDir)

	// ── Load configuration from XML ─────────────────────────────────────
	cfg, err := config.Load(config.LoadOptions{
		ServerPropsPath: *serverPropsPath,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	// ── Setup structured logging ────────────────────────────────────────
	logger := setupLogger(cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(logger)

	// Export RTVORTEX_HOME so child processes (C++ engine, scripts) inherit it.
	_ = os.Setenv("RTVORTEX_HOME", env.Home)

	// ── Print startup banner ────────────────────────────────────────────
	printBanner(env, cfg)

	slog.Info("RTVortexGo API Server starting",
		"version", version,
		"commit", commit,
		"build_date", buildDate,
		"pid", os.Getpid(),
		"rtvortex_home", env.Home,
		"hostname", env.Hostname,
	)

	// ── Root context with cancellation ──────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ── Initialize PostgreSQL connection pool ───────────────────────────
	db, err := store.NewPostgresPool(ctx, cfg.Database)
	if err != nil {
		slog.Error("failed to connect to PostgreSQL", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	slog.Info("PostgreSQL connected",
		"host", cfg.Database.Host,
		"database", cfg.Database.Name,
		"pool_size", cfg.Database.MaxConns,
	)

	// Run database schema initialization (auto-applies initData.sql if needed)
	sqlDir := filepath.Join(env.DataDir, "sql")
	if err := db.RunMigrations(sqlDir); err != nil {
		slog.Warn("database schema not auto-initialized (run SQL scripts manually)",
			"sql_dir", sqlDir,
			"error", err,
		)
	}

	// ── Initialize Redis ────────────────────────────────────────────────
	redisClient, err := session.NewRedisClient(cfg.Redis)
	if err != nil {
		slog.Error("failed to connect to Redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()
	slog.Info("Redis connected", "addr", cfg.Redis.Addr)

	// ── Initialize gRPC engine client ───────────────────────────────────
	enginePool, err := engine.NewPool(ctx, cfg.Engine)
	if err != nil {
		slog.Error("failed to connect to RTVortex engine", "error", err)
		os.Exit(1)
	}
	defer enginePool.Close()
	slog.Info("Engine gRPC pool connected",
		"target", fmt.Sprintf("%s:%d", cfg.Engine.Host, cfg.Engine.Port),
		"channels", cfg.Engine.MaxChannels,
		"tls", cfg.Engine.TLS,
		"client_cert", cfg.Engine.CertFile,
		"ca", cfg.Engine.CAFile,
	)

	// ── Build dependencies (manual DI — no magic) ───────────────────────
	// Repositories
	userRepo := store.NewUserRepository(db.Pool)
	repoRepo := store.NewRepositoryRepo(db.Pool)
	repoMemberRepo := store.NewRepoMemberRepo(db.Pool)
	reviewRepo := store.NewReviewRepository(db.Pool)
	orgRepo := store.NewOrgRepository(db.Pool)
	webhookRepo := store.NewWebhookRepository(db.Pool)
	prRepo := store.NewPullRequestRepo(db.Pool)

	// Engine gRPC client
	engineClient := engine.NewClient(enginePool)

	// JWT Manager
	jwtSecret := cfg.Auth.JWTSecret
	if jwtSecret == "" {
		jwtSecret, _ = auth.GenerateRandomSecret(32)
		slog.Warn("no JWT secret configured — using random secret (sessions will not survive restarts)")
	}
	jwtMgr := auth.NewJWTManager(auth.JWTConfig{
		Secret:          jwtSecret,
		Issuer:          "rtvortex",
		AccessDuration:  15 * time.Minute,
		RefreshDuration: 7 * 24 * time.Hour,
	})

	// Session manager
	sessionMgr := session.NewManager(redisClient.Client(), 24*time.Hour)

	// OAuth2 provider registry
	oauthReg := auth.NewProviderRegistry()
	scheme := "http"
	if cfg.Server.TLS.Enabled {
		scheme = "https"
	}
	// Use configured host for OAuth callback URLs; fall back to "localhost".
	serverHost := cfg.Server.Host
	if serverHost == "" || serverHost == "0.0.0.0" || serverHost == "::" {
		serverHost = "localhost"
	}
	serverBase := fmt.Sprintf("%s://%s:%d", scheme, serverHost, cfg.Server.Port)
	for name, p := range cfg.Auth.Providers {
		callbackPath := p.CallbackPath
		if callbackPath == "" {
			callbackPath = fmt.Sprintf("/api/v1/auth/callback/%s", name)
		}
		oauthCfg := auth.OAuthProviderConfig{
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			RedirectURL:  serverBase + callbackPath,
			Scopes:       p.Scopes,
		}
		switch name {
		case "google":
			oauthReg.Register(authproviders.NewGoogleProvider(oauthCfg))
		case "github":
			oauthReg.Register(authproviders.NewGitHubProvider(oauthCfg))
		case "gitlab":
			oauthReg.Register(authproviders.NewGitLabProvider(oauthCfg))
		case "microsoft":
			oauthReg.Register(authproviders.NewMicrosoftProvider(oauthCfg))
		case "bitbucket":
			oauthReg.Register(authproviders.NewBitbucketProvider(oauthCfg))
		case "linkedin":
			oauthReg.Register(authproviders.NewLinkedInProvider(oauthCfg))
		case "apple":
			oauthReg.Register(authproviders.NewAppleProvider(oauthCfg))
		case "x":
			oauthReg.Register(authproviders.NewXProvider(oauthCfg))
		}
		slog.Info("OAuth provider registered", "provider", name)
	}

	// Token encryptor (AES-256-GCM for OAuth tokens at rest)
	tokenEnc, err := rtcrypto.NewTokenEncryptor(cfg.Auth.EncryptionKey)
	if err != nil {
		slog.Warn("Token encryptor init failed, tokens will be stored unencrypted", "error", err)
		tokenEnc, _ = rtcrypto.NewTokenEncryptor("") // fall back to no-op
	}
	if tokenEnc.IsEnabled() {
		slog.Info("Token encryption enabled (AES-256-GCM)")
	} else {
		slog.Warn("Token encryption DISABLED — set encryption-key in security config for production")
	}

	// LLM provider registry — all providers pre-registered with default URLs.
	// API keys come from environment variables or the dashboard settings UI — never from XML.
	llmRegistry := llm.NewRegistry()

	// API keys are sourced exclusively from env vars (or set at runtime via dashboard).
	envKey := func(envVar string) string {
		return os.Getenv(envVar)
	}
	cfgModel := func(name, fallback string) string {
		if p, ok := cfg.LLM.Providers[name]; ok && p.Model != "" {
			return p.Model
		}
		return fallback
	}
	cfgURL := func(name, fallback string) string {
		if p, ok := cfg.LLM.Providers[name]; ok && p.BaseURL != "" {
			return p.BaseURL
		}
		return fallback
	}

	// OpenAI
	openaiKey := envKey("LLM_OPENAI_API_KEY")
	llmRegistry.RegisterWithMeta(
		llm.NewOpenAIProvider(llm.OpenAIConfig{
			APIKey: openaiKey, BaseURL: cfgURL("openai", "https://api.openai.com/v1"),
			DefaultModel: cfgModel("openai", "gpt-4o"),
		}),
		llm.ProviderMeta{
			DisplayName: "OpenAI", BaseURL: cfgURL("openai", "https://api.openai.com/v1"),
			DefaultModel: cfgModel("openai", "gpt-4o"),
			Configured:   openaiKey != "", RequiresKey: true, APIKey: openaiKey,
		},
	)

	// Anthropic
	anthropicKey := envKey("LLM_ANTHROPIC_API_KEY")
	llmRegistry.RegisterWithMeta(
		llm.NewAnthropicProvider(llm.AnthropicConfig{
			APIKey: anthropicKey, BaseURL: cfgURL("anthropic", "https://api.anthropic.com/v1"),
			DefaultModel: cfgModel("anthropic", "claude-sonnet-4-20250514"),
		}),
		llm.ProviderMeta{
			DisplayName: "Anthropic", BaseURL: cfgURL("anthropic", "https://api.anthropic.com/v1"),
			DefaultModel: cfgModel("anthropic", "claude-sonnet-4-20250514"),
			Configured:   anthropicKey != "", RequiresKey: true, APIKey: anthropicKey,
		},
	)

	// Gemini
	geminiKey := envKey("LLM_GEMINI_API_KEY")
	llmRegistry.RegisterWithMeta(
		llm.NewGeminiProvider(llm.GeminiConfig{
			APIKey: geminiKey, BaseURL: cfgURL("gemini", "https://generativelanguage.googleapis.com/v1beta"),
			DefaultModel: cfgModel("gemini", "gemini-2.5-flash"),
		}),
		llm.ProviderMeta{
			DisplayName: "Google Gemini", BaseURL: cfgURL("gemini", "https://generativelanguage.googleapis.com/v1beta"),
			DefaultModel: cfgModel("gemini", "gemini-2.5-flash"),
			Configured:   geminiKey != "", RequiresKey: true, APIKey: geminiKey,
		},
	)

	// Grok (xAI)
	grokKey := envKey("LLM_GROK_API_KEY")
	llmRegistry.RegisterWithMeta(
		llm.NewGrokProvider(llm.GrokConfig{
			APIKey: grokKey, BaseURL: cfgURL("grok", "https://api.x.ai/v1"),
			DefaultModel: cfgModel("grok", "grok-3-mini"),
		}),
		llm.ProviderMeta{
			DisplayName: "Grok (xAI)", BaseURL: cfgURL("grok", "https://api.x.ai/v1"),
			DefaultModel: cfgModel("grok", "grok-3-mini"),
			Configured:   grokKey != "", RequiresKey: true, APIKey: grokKey,
		},
	)

	// Ollama (local — no API key required)
	llmRegistry.RegisterWithMeta(
		llm.NewOllamaProvider(llm.OllamaConfig{
			BaseURL:      cfgURL("ollama", "http://localhost:11434"),
			DefaultModel: cfgModel("ollama", "llama3.1:8b"),
		}),
		llm.ProviderMeta{
			DisplayName: "Ollama (Local)", BaseURL: cfgURL("ollama", "http://localhost:11434"),
			DefaultModel: cfgModel("ollama", "llama3.1:8b"),
			Configured:   true, RequiresKey: false,
		},
	)

	// File vault — persists API keys entered via the dashboard across restarts.
	// Uses the same AES-256-GCM key as token encryption.
	vaultPath := filepath.Join(env.ConfigDir, ".vault.enc")
	var vaultOpts []vault.FileVaultOption
	if cfg.Auth.EncryptionKey != "" {
		vaultOpts = append(vaultOpts, vault.WithEncryptionKey(cfg.Auth.EncryptionKey))
	}
	fileVault, err := vault.NewFileVault(vaultPath, vaultOpts...)
	if err != nil {
		slog.Warn("File vault init failed — API keys will not persist", "error", err)
	} else {
		llmRegistry.SetVault(fileVault)
		loaded := llmRegistry.LoadFromVault()
		if loaded > 0 {
			slog.Info("Loaded API keys from vault", "count", loaded)
		}
	}

	// Set primary from config (default: openai).
	if cfg.LLM.Primary != "" {
		llmRegistry.SetPrimary(cfg.LLM.Primary)
	}

	slog.Info("LLM providers registered",
		"count", len(llmRegistry.ListProviders()),
		"primary", cfg.LLM.Primary,
	)

	// VCS resolver — resolves credentials dynamically from vault/DB per repo
	vcsResolver := vcs.NewResolver(db.Pool, vault.NewVCSVaultAdapter(fileVault))
	slog.Info("VCS resolver initialised (credentials resolved per-repo from vault)")

	// Review pipeline
	reviewPipeline := review.NewPipeline(reviewRepo, repoRepo, llmRegistry, vcsResolver, engineClient, review.PipelineConfig{
		MaxFilesPerReview: 50,
		MaxDiffSizeBytes:  512 * 1024,
		ConcurrentFiles:   5,
	})

	// Indexing service
	indexingService := indexing.NewService(engineClient, repoRepo)

	// WebSocket hub for real-time review progress
	wsHub := ws.NewHub()
	defer wsHub.Stop()
	slog.Info("WebSocket hub started")

	// Wire indexing progress callback — emits events to WebSocket subscribers
	indexingService.SetProgressFunc(func(jobID string, status indexing.JobStatus) {
		wsHub.BroadcastIndex(status.RepoID, ws.IndexProgressEvent{
			JobID:          jobID,
			State:          string(status.State),
			Progress:       status.Progress,
			Phase:          status.Phase,
			Message:        status.Message,
			FilesProcessed: status.FilesProcessed,
			FilesTotal:     status.FilesTotal,
			CurrentFile:    status.CurrentFile,
			ETASeconds:     status.ETASeconds,
			Error:          status.Error,
		})
	})

	// Wire progress callback — pipeline emits events to WebSocket subscribers
	reviewPipeline.SetProgressFunc(func(reviewID uuid.UUID, step string, stepIndex, totalSteps int, status, message string, meta map[string]interface{}) {
		wsHub.Broadcast(reviewID, ws.ProgressEvent{
			Step:      step,
			StepIndex: stepIndex,
			TotalStep: totalSteps,
			Status:    status,
			Message:   message,
			Metadata:  meta,
		})
	})

	// Rate limiter (Redis-backed sliding window)
	rateLimiter := session.NewRateLimiter(redisClient.Client())
	rateLimiter.Configure("api", session.RateLimitConfig{
		MaxRequests: 100,
		Window:      1 * time.Minute,
	})
	rateLimiter.Configure("auth", session.RateLimitConfig{
		MaxRequests: 20,
		Window:      1 * time.Minute,
	})
	rateLimiter.Configure("webhook", session.RateLimitConfig{
		MaxRequests: 60,
		Window:      1 * time.Minute,
	})
	slog.Info("Rate limiter configured",
		"api", "100/min",
		"auth", "20/min",
		"webhook", "60/min",
	)

	// Audit logger (security event tracking)
	auditRepo := store.NewAuditRepository(db.Pool)
	auditLogger := audit.NewLogger(auditRepo)
	slog.Info("Audit logger initialized")

	// Background scheduler
	bgScheduler := background.NewScheduler(ctx, engineClient, llmRegistry, indexingService)
	bgScheduler.Start()
	defer bgScheduler.Stop()

	// PR sync worker — discovers and tracks open PRs from connected VCS platforms
	prSyncWorker := prsync.NewWorker(ctx, prRepo, repoRepo, vcsResolver, engineClient, wsHub, prsync.DefaultConfig())
	prSyncWorker.Start()
	defer prSyncWorker.Stop()

	// Chat repo + RAG chat service
	chatRepo := store.NewChatRepository(db.Pool)
	chatService := chat.NewService(chatRepo, engineClient, llmRegistry, chat.DefaultConfig())

	// VCS platform config repo — per-user non-secret VCS settings (URLs, usernames)
	vcsPlatformRepo := store.NewVCSPlatformRepo(db.Pool)

	// ── Initialize Swarm Agent infrastructure ───────────────────────────
	// The swarm service secret is derived from the existing JWT secret,
	// so there is no extra env var to manage. It authenticates the initial
	// agent registration call on the /internal/ routes (before the agent
	// has its own JWT). The SHA-256 prefix ensures it differs from the JWT
	// signing key while remaining deterministic across restarts.
	swarmServiceSecret := deriveSwarmSecret(jwtSecret)

	swarmAuthSvc := swarmauth.NewService([]byte(jwtSecret), swarmServiceSecret, redisClient.Client())
	swarmTeamMgr := swarm.NewTeamManager(db.Pool)
	swarmTaskMgr := swarm.NewTaskManager(db.Pool, redisClient.Client(), swarmTeamMgr)
	swarmLLMProxy := swarm.NewLLMProxy(llmRegistry)
	swarmELO := swarm.NewELOService(db.Pool)
	swarmWSHub := swarm.NewWSHub(wsHub)
	swarmPRCreator := swarm.NewPRCreator(db.Pool, vcsResolver, swarmTaskMgr, swarmWSHub)

	swarmHandler := &swarm.Handler{
		AuthSvc:   swarmAuthSvc,
		TaskMgr:   swarmTaskMgr,
		TeamMgr:   swarmTeamMgr,
		LLMProxy:  swarmLLMProxy,
		ELO:       swarmELO,
		WS:        swarmWSHub,
		PRCreator: swarmPRCreator,
	}

	// Start the swarm task assignment loop.
	swarmTaskMgr.Start(ctx)
	defer swarmTaskMgr.Stop()

	slog.Info("Swarm agent infrastructure initialized")

	// Opens a gRPC streaming connection to the C++ engine to receive real-time metrics.
	metricsCollector := engine.NewMetricsCollector(engineClient, 1000)
	metricsCollector.Start(ctx)
	if wsHub != nil {
		metricsCollector.OnSnapshot(func(snap *engine.EngineMetricsSnapshot) {
			if !wsHub.HasMetricsSubscribers() {
				return
			}
			data, err := engine.MarshalWSEvent(snap)
			if err != nil {
				slog.Error("failed to marshal engine metrics WS event", "error", err)
				return
			}
			wsHub.BroadcastMetrics(data)
		})
	}

	deps := &server.Dependencies{
		Config:     cfg,
		DB:         db,
		Redis:      redisClient,
		EnginePool: enginePool,
		Version:    version,

		EngineClient:    engineClient,
		JWTMgr:          jwtMgr,
		SessionMgr:      sessionMgr,
		OAuthReg:        oauthReg,
		TokenEncryptor:  tokenEnc,
		LLMRegistry:     llmRegistry,
		VCSResolver:     vcsResolver,
		ReviewPipeline:  reviewPipeline,
		IndexingService: indexingService,
		RateLimiter:     rateLimiter,
		AuditLogger:     auditLogger,
		WSHub:           wsHub,

		UserRepo:         userRepo,
		RepoRepo:         repoRepo,
		RepoMemberRepo:   repoMemberRepo,
		ReviewRepo:       reviewRepo,
		OrgRepo:          orgRepo,
		WebhookRepo:      webhookRepo,
		PRRepo:           prRepo,
		PRSyncWorker:     prSyncWorker,
		ChatRepo:         chatRepo,
		ChatService:      chatService,
		Vault:            fileVault,
		VCSPlatformRepo:  vcsPlatformRepo,
		MetricsCollector: metricsCollector,

		SwarmAuthSvc:  swarmAuthSvc,
		SwarmTaskMgr:  swarmTaskMgr,
		SwarmTeamMgr:  swarmTeamMgr,
		SwarmLLMProxy: swarmLLMProxy,
		SwarmELO:      swarmELO,
		SwarmHandler:  swarmHandler,
	}

	// ── Create HTTP server ──────────────────────────────────────────────
	srv := server.New(deps)
	listenAddr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	httpServer := &http.Server{
		Addr:         listenAddr,
		Handler:      srv.Router(),
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// ── Start server in background ──────────────────────────────────────
	errCh := make(chan error, 1)
	go func() {
		scheme := "http"
		if cfg.Server.TLS.Enabled {
			scheme = "https"
		}
		slog.Info("RTVortex API server ready",
			"url", fmt.Sprintf("%s://0.0.0.0:%d", scheme, cfg.Server.Port),
			"tls", cfg.Server.TLS.Enabled,
		)
		if cfg.Server.TLS.Enabled {
			errCh <- httpServer.ListenAndServeTLS(
				cfg.Server.TLS.CertFile,
				cfg.Server.TLS.KeyFile,
			)
		} else {
			errCh <- httpServer.ListenAndServe()
		}
	}()

	// ── Graceful shutdown on SIGINT / SIGTERM ────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-quit:
		slog.Info("shutdown signal received", "signal", sig)
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer shutdownCancel()

	slog.Info("shutting down gracefully", "timeout", cfg.Server.ShutdownTimeout)
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("forced shutdown", "error", err)
	}

	metricsCollector.Stop()
	cancel() // Cancel root context to stop background workers
	slog.Info("RTVortexGo API Server stopped")
}

// setupLogger creates a structured slog.Logger.
func setupLogger(level, format string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: level == "debug",
	}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

// deriveSwarmSecret produces a deterministic service secret from the existing
// JWT signing key. This avoids a separate SWARM_SERVICE_SECRET env var while
// keeping the registration endpoint authenticated.
func deriveSwarmSecret(jwtSecret string) string {
	h := sha256.Sum256([]byte("rtvortex-swarm:" + jwtSecret))
	return hex.EncodeToString(h[:])
}
