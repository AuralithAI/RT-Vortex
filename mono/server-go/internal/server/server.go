// Package server wires together the HTTP router, middleware, and API handlers.
package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	apidocs "github.com/AuralithAI/rtvortex-server/api"
	"github.com/AuralithAI/rtvortex-server/internal/api"
	"github.com/AuralithAI/rtvortex-server/internal/audit"
	"github.com/AuralithAI/rtvortex-server/internal/auth"
	"github.com/AuralithAI/rtvortex-server/internal/benchmark"
	"github.com/AuralithAI/rtvortex-server/internal/chat"
	"github.com/AuralithAI/rtvortex-server/internal/config"
	"github.com/AuralithAI/rtvortex-server/internal/crossrepo"
	rtcrypto "github.com/AuralithAI/rtvortex-server/internal/crypto"
	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/indexing"
	"github.com/AuralithAI/rtvortex-server/internal/llm"
	"github.com/AuralithAI/rtvortex-server/internal/mcp"
	rtmetrics "github.com/AuralithAI/rtvortex-server/internal/metrics"
	"github.com/AuralithAI/rtvortex-server/internal/prsync"
	"github.com/AuralithAI/rtvortex-server/internal/quota"
	"github.com/AuralithAI/rtvortex-server/internal/review"
	"github.com/AuralithAI/rtvortex-server/internal/sandbox"
	"github.com/AuralithAI/rtvortex-server/internal/session"
	"github.com/AuralithAI/rtvortex-server/internal/store"
	"github.com/AuralithAI/rtvortex-server/internal/swarm"
	swarmauth "github.com/AuralithAI/rtvortex-server/internal/swarm/auth"
	"github.com/AuralithAI/rtvortex-server/internal/tracing"
	"github.com/AuralithAI/rtvortex-server/internal/vault/keychain"
	"github.com/AuralithAI/rtvortex-server/internal/vcs"
	"github.com/AuralithAI/rtvortex-server/internal/webhookq"
	"github.com/AuralithAI/rtvortex-server/internal/ws"
)

// Dependencies holds all injected dependencies for the server.
type Dependencies struct {
	Config     *config.Config
	DB         *store.DB
	Redis      *session.RedisClient
	EnginePool *engine.Pool
	Version    string

	// Service-layer dependencies
	EngineClient    *engine.Client
	JWTMgr          *auth.JWTManager
	SessionMgr      *session.Manager
	OAuthReg        *auth.ProviderRegistry
	TokenEncryptor  *rtcrypto.TokenEncryptor
	LLMRegistry     *llm.Registry
	VCSResolver     *vcs.Resolver
	ReviewPipeline  *review.Pipeline
	IndexingService *indexing.Service
	RateLimiter     *session.RateLimiter
	AuditLogger     *audit.Logger
	WSHub           *ws.Hub
	Tracer          *tracing.Tracer
	QuotaEnforcer   *quota.Enforcer
	DeliveryRepo    *webhookq.Repository

	// Repositories
	UserRepo       *store.UserRepository
	RepoRepo       *store.RepositoryRepo
	RepoMemberRepo *store.RepoMemberRepo
	ReviewRepo     *store.ReviewRepository
	OrgRepo        *store.OrgRepository
	WebhookRepo    *store.WebhookRepository
	PRRepo         *store.PullRequestRepo

	// PR Sync
	PRSyncWorker *prsync.Worker

	// Chat
	ChatRepo    *store.ChatRepository
	ChatService *chat.Service

	// Multimodal assets
	AssetRepo *store.AssetRepository

	// Keychain — production-grade encrypted secret storage
	KeychainService *keychain.Service

	// VCS Platform Config — per-user non-secret VCS settings (URLs, usernames)
	VCSPlatformRepo *store.VCSPlatformRepo

	// Engine Metrics Collector — real-time engine observability
	MetricsCollector *engine.MetricsCollector

	// Swarm — agent swarm infrastructure
	SwarmAuthSvc  *swarmauth.Service
	SwarmTaskMgr  *swarm.TaskManager
	SwarmTeamMgr  *swarm.TeamManager
	SwarmLLMProxy *swarm.LLMProxy
	SwarmELO      *swarm.ELOService
	SwarmHandler  *swarm.Handler

	// Benchmark — A/B testing and benchmark harness
	BenchmarkRunner *benchmark.Runner

	// MCP — external service integrations (Slack, MS365, Gmail, Discord)
	MCPService *mcp.Service
	MCPRepo    *store.MCPRepository

	// Cross-Repo Observatory — centralized authorization and link management
	CrossRepoAuthorizer   *crossrepo.Authorizer
	CrossRepoHandler      *crossrepo.Handler
	CrossRepoGraphHandler *crossrepo.GraphHandler
	RepoLinkRepo          *store.RepoLinkRepo

	// App Config — system-wide non-secret settings (feature flags, toggles)
	AppConfigRepo *store.AppConfigRepo

	// ServerBase — canonical server URL for constructing OAuth callback URLs.
	ServerBase string
}

// Server holds the HTTP server components.
type Server struct {
	deps   *Dependencies
	router chi.Router
}

// New creates a new Server with all routes and middleware configured.
func New(deps *Dependencies) *Server {
	s := &Server{deps: deps}
	s.setupRouter()
	return s
}

// Router returns the chi.Router for use with http.Server.
func (s *Server) Router() http.Handler {
	return s.router
}

func (s *Server) setupRouter() {
	r := chi.NewRouter()

	// ── Global middleware ────────────────────────────────────────────────
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(rtmetrics.Middleware)

	// Distributed tracing
	if s.deps.Tracer != nil {
		r.Use(tracing.HTTPMiddleware(s.deps.Tracer))
	}

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   s.deps.Config.Server.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		ExposedHeaders:   []string{"X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// ── Build the Handler with all dependencies ─────────────────────────
	h := &api.Handler{
		UserRepo:        s.deps.UserRepo,
		RepoRepo:        s.deps.RepoRepo,
		RepoMemberRepo:  s.deps.RepoMemberRepo,
		ReviewRepo:      s.deps.ReviewRepo,
		OrgRepo:         s.deps.OrgRepo,
		WebhookRepo:     s.deps.WebhookRepo,
		SessionMgr:      s.deps.SessionMgr,
		JWTMgr:          s.deps.JWTMgr,
		OAuthReg:        s.deps.OAuthReg,
		TokenEnc:        s.deps.TokenEncryptor,
		LLMRegistry:     s.deps.LLMRegistry,
		VCSResolver:     s.deps.VCSResolver,
		EngineClient:    s.deps.EngineClient,
		ReviewPipeline:  s.deps.ReviewPipeline,
		IndexingService: s.deps.IndexingService,
		AuditLogger:     s.deps.AuditLogger,
		QuotaEnforcer:   s.deps.QuotaEnforcer,
		DeliveryRepo:    s.deps.DeliveryRepo,
		PRRepo:          s.deps.PRRepo,
		PRSyncWorker:    s.deps.PRSyncWorker,
		ChatRepo:        s.deps.ChatRepo,
		ChatService:     s.deps.ChatService,
		AssetRepo:       s.deps.AssetRepo,
		KeychainService: s.deps.KeychainService,
		VCSPlatformRepo: s.deps.VCSPlatformRepo,
		AppConfigRepo:   s.deps.AppConfigRepo,
	}
	if s.deps.MetricsCollector != nil {
		h.MetricsCollector = s.deps.MetricsCollector
	}
	if s.deps.Redis != nil {
		h.EmbedCache = engine.NewEmbedCacheService(s.deps.Redis.Client())
	}

	// ── Health & readiness (no auth required) ───────────────────────────
	healthHandler := api.NewHealthHandler(s.deps.DB, s.deps.Redis, s.deps.EnginePool, s.deps.Version)
	r.Get("/health", healthHandler.Health)
	r.Get("/ready", healthHandler.Ready)
	r.Get("/version", healthHandler.Version)
	r.Handle("/metrics", promhttp.Handler())
	r.Get("/api/v1/docs/openapi.yaml", apidocs.Handler)

	// ── API v1 routes ───────────────────────────────────────────────────
	r.Route("/api/v1", func(r chi.Router) {
		// ── Auth routes (public — stricter rate limit) ──────────────
		r.Route("/auth", func(r chi.Router) {
			r.Use(session.RateLimitMiddleware(s.deps.RateLimiter, "auth"))
			r.Get("/providers", h.ListProviders)
			r.Get("/login/{provider}", h.OAuthLogin)
			r.Get("/callback/{provider}", h.OAuthCallback)
			r.Post("/refresh", h.RefreshToken)
			r.Post("/logout", h.Logout)
		})

		// ── MCP OAuth callback (public — Google/MS/etc. redirect here) ──
		if s.deps.MCPService != nil {
			mh := &mcpHandler{
				svc:        s.deps.MCPService,
				repo:       s.deps.MCPRepo,
				sessionMgr: s.deps.SessionMgr,
				mcpCfg:     s.deps.Config.MCP,
				serverBase: s.deps.ServerBase,
			}
			r.Get("/integrations/oauth/{provider}/callback", mh.OAuthCallback)
		}

		// ── Protected routes (require JWT) ──────────────────────────
		r.Group(func(r chi.Router) {
			if s.deps.JWTMgr != nil {
				r.Use(auth.Middleware(s.deps.JWTMgr))
			}
			r.Use(session.RateLimitMiddleware(s.deps.RateLimiter, "api"))

			// User
			r.Get("/user/me", h.GetCurrentUser)
			r.Put("/user/me", h.UpdateCurrentUser)

			// Organizations
			r.Route("/orgs", func(r chi.Router) {
				r.Get("/", h.ListOrgs)
				r.Post("/", h.CreateOrg)
				r.Route("/{orgID}", func(r chi.Router) {
					r.Get("/", h.GetOrg)
					r.Put("/", h.UpdateOrg)
					r.Get("/members", h.ListOrgMembers)
					r.Post("/members", h.InviteOrgMember)
					r.Delete("/members/{userID}", h.RemoveOrgMember)

					// Cross-repo links (org-level view)
					if s.deps.CrossRepoHandler != nil {
						r.Route("/links", func(r chi.Router) {
							s.deps.CrossRepoHandler.RegisterOrgRoutes(r)
						})
					}

					// Cross-repo graph (org-level operations)
					if s.deps.CrossRepoGraphHandler != nil {
						r.Route("/cross-repo", func(r chi.Router) {
							s.deps.CrossRepoGraphHandler.RegisterOrgRoutes(r)
						})
					}
				})
			})

			// Repositories
			r.Route("/repos", func(r chi.Router) {
				r.Get("/", h.ListRepos)
				r.Post("/", h.RegisterRepo)
				r.Route("/{repoID}", func(r chi.Router) {
					r.Get("/", h.GetRepo)
					r.Put("/", h.UpdateRepo)
					r.Delete("/", h.DeleteRepo)
					r.Post("/index", h.TriggerIndex)
					r.Get("/index/status", h.GetIndexStatus)
					r.Get("/embed-stats", h.GetEmbedStats)
					r.Get("/file-map", h.GetRepoFileMap)
					r.Get("/branches", h.ListBranches)
					r.Get("/members", h.ListRepoMembers)
					r.Post("/members", h.AddRepoMember)
					r.Delete("/members/{userID}", h.RemoveRepoMember)

					// Repo-scoped build secrets (stored in user keychain with repo_id)
					r.Route("/build-secrets", func(r chi.Router) {
						r.Put("/", h.PutBuildSecret)
						r.Get("/", h.ListBuildSecrets)
						r.Delete("/{name}", h.DeleteBuildSecret)
					})

					// Pull Request discovery & management
					r.Route("/pull-requests", func(r chi.Router) {
						r.Get("/", h.ListPullRequests)
						r.Post("/sync", h.SyncPullRequests)
						r.Get("/stats", h.GetPullRequestStats)
						r.Get("/by-number/{prNumber}", h.GetPullRequestByNumber)
						r.Route("/{prID}", func(r chi.Router) {
							r.Get("/", h.GetPullRequest)
							r.Post("/review", h.ReviewPullRequest)
						})

						// WebSocket: real-time PR embedding progress streaming
						if s.deps.WSHub != nil {
							prEmbedWSHandler := ws.NewPREmbedHandler(s.deps.WSHub)
							r.Get("/embed/ws", prEmbedWSHandler.ServeHTTP)
						}
					})

					// WebSocket: real-time indexing progress streaming
					if s.deps.WSHub != nil {
						indexWSHandler := ws.NewIndexHandler(s.deps.WSHub)
						r.Get("/index/ws", indexWSHandler.ServeHTTP)
					}

					// Chat sessions & messages
					r.Route("/chat/sessions", func(r chi.Router) {
						r.Post("/", h.CreateChatSession)
						r.Get("/", h.ListChatSessions)
						r.Route("/{sessionID}", func(r chi.Router) {
							r.Get("/", h.GetChatSession)
							r.Put("/", h.UpdateChatSession)
							r.Delete("/", h.DeleteChatSession)
							r.Get("/messages", h.ListChatMessages)
							r.Post("/messages", h.SendChatMessage)
						})
					})

					// Multimodal asset management
					r.Route("/assets", func(r chi.Router) {
						r.Post("/upload", h.UploadAsset)
						r.Post("/ingest-url", h.IngestURL)
						r.Get("/", h.ListAssets)
						r.Get("/{assetID}/content", h.ServeAssetContent)
						r.Delete("/{assetID}", h.DeleteAsset)
					})

					// Cross-repo links (repo-level management)
					if s.deps.CrossRepoHandler != nil {
						r.Route("/links", func(r chi.Router) {
							s.deps.CrossRepoHandler.RegisterRoutes(r)
						})
					}

					// Cross-repo graph + federated search (repo-level)
					if s.deps.CrossRepoGraphHandler != nil {
						r.Route("/cross-repo", func(r chi.Router) {
							s.deps.CrossRepoGraphHandler.RegisterRepoRoutes(r)
						})
					}
				})
			})

			// Reviews
			r.Route("/reviews", func(r chi.Router) {
				r.Get("/", h.ListReviews)
				r.Post("/", h.TriggerReview)
				r.Get("/{reviewID}", h.GetReview)
				r.Get("/{reviewID}/comments", h.GetReviewComments)

				// WebSocket: real-time review progress streaming
				if s.deps.WSHub != nil {
					wsHandler := ws.NewHandler(s.deps.WSHub)
					r.Get("/{reviewID}/ws", wsHandler.ServeHTTP)
				}
			})

			// LLM Management
			r.Route("/llm", func(r chi.Router) {
				r.Get("/providers", h.ListLLMProviders)
				r.Put("/providers/{provider}", h.ConfigureLLMProvider)
				r.Post("/providers/{provider}/balance", h.CheckLLMBalance)
				r.Get("/providers/{provider}/status", h.GetLLMProviderStatus)
				r.Post("/providers/test", h.TestLLMProvider)
				r.Put("/primary", h.SetPrimaryLLMProvider)
				r.Get("/routes", h.GetLLMRoutes)
				r.Put("/routes", h.SetLLMRoutes)
				r.Post("/stream", h.StreamLLMCompletion) // SSE streaming
			})

			// Embeddings Configuration
			r.Route("/embeddings", func(r chi.Router) {
				r.Get("/config", h.GetEmbeddingsConfig)
				r.Put("/config", h.UpdateEmbeddingsConfig)
				r.Post("/test", h.TestEmbeddingProvider)
				r.Post("/credits", h.CheckEmbeddingCredits)
				r.Get("/multimodal", h.GetMultimodalConfig)
				r.Put("/multimodal", h.UpdateMultimodalConfig)
			})

			// VCS Platform Settings (per-user credentials stored in vault)
			r.Route("/vcs", func(r chi.Router) {
				r.Get("/platforms", h.ListVCSPlatforms)
				r.Put("/platforms/{platform}", h.ConfigureVCSPlatform)
				r.Delete("/platforms/{platform}", h.DeleteVCSPlatform)
				r.Post("/platforms/{platform}/test", h.TestVCSPlatform)
				r.Post("/platforms/{platform}/check-clone", h.CheckClonePermission)
				r.Get("/token-capabilities", h.ListVCSTokenCapabilities)
			})

			// Keychain — encrypted per-user secret vault
			r.Route("/keychain", func(r chi.Router) {
				// Standard CRUD operations use the default "api" rate limit.
				r.Get("/status", h.GetKeychainStatus)
				r.Get("/secrets", h.ListKeychainSecrets)
				r.Put("/secrets", h.PutKeychainSecret)
				r.Get("/secret", h.GetKeychainSecret)
				r.Delete("/secret", h.DeleteKeychainSecret)
				r.Post("/sync", h.SyncKeychainSecrets)
				r.Get("/audit", h.ListKeychainAuditLog)

				// Sensitive operations get a stricter rate limit to prevent
				// brute-force attacks on recovery phrases and abuse of init/rotate.
				r.Group(func(r chi.Router) {
					r.Use(session.RateLimitMiddleware(s.deps.RateLimiter, "keychain_sensitive"))
					r.Post("/init", h.InitKeychain)
					r.Post("/rotate", h.RotateKeychainKeys)
					r.Post("/recover", h.RecoverKeychain)
					r.Post("/refresh-recovery", h.RefreshRecovery)
				})
			})

			// MCP Integrations (connected apps: Slack, MS365, Gmail, Discord)
			if s.deps.MCPService != nil {
				mh := &mcpHandler{
					svc:        s.deps.MCPService,
					repo:       s.deps.MCPRepo,
					sessionMgr: s.deps.SessionMgr,
					mcpCfg:     s.deps.Config.MCP,
					serverBase: s.deps.ServerBase,
				}
				r.Route("/integrations", func(r chi.Router) {
					r.Get("/providers", mh.ListProviders)
					r.Get("/connections", mh.ListConnections)
					r.Post("/connections", mh.CreateConnection)
					r.Delete("/connections/{connectionID}", mh.DeleteConnection)
					r.Post("/connections/{connectionID}/test", mh.TestConnection)
					r.Get("/connections/{connectionID}/logs", mh.GetCallLog)
					r.Get("/oauth/status", mh.OAuthStatus)
					r.Get("/oauth/{provider}/authorize", mh.InitiateOAuth)

					// Custom MCP templates
					r.Get("/custom-templates", mh.ListCustomTemplates)
					r.Post("/custom-templates", mh.CreateCustomTemplate)
					r.Delete("/custom-templates/{templateID}", mh.DeleteCustomTemplate)
					r.Post("/custom-templates/validate", mh.ValidateCustomTemplate)
					r.Post("/custom-templates/simulate", mh.SimulateCustomConnection)
				})
			} // Engine Metrics
			r.Route("/engine", func(r chi.Router) {
				r.Get("/metrics", h.GetEngineMetrics)
				r.Get("/health", h.GetEngineHealth)

				// WebSocket: real-time engine metrics streaming
				if s.deps.WSHub != nil {
					metricsWSHandler := ws.NewMetricsHandler(s.deps.WSHub)
					r.Get("/metrics/ws", metricsWSHandler.ServeHTTP)
				}

				// Internal: Redis-backed embedding cache for C++ engine
				r.Get("/embed-cache/{repoID}/{chunkHash}", h.GetEmbedCache)
				r.Put("/embed-cache/{repoID}/{chunkHash}", h.PutEmbedCache)
			})

			// Admin
			r.Route("/admin", func(r chi.Router) {
				r.Use(auth.RequireRole("admin"))
				r.Get("/stats", h.GetSystemStats)
				r.Get("/health/detailed", h.GetDetailedHealth)
			})

			// Benchmark — A/B testing & agent benchmarks
			if s.deps.BenchmarkRunner != nil {
				bh := benchmark.NewHandler(s.deps.BenchmarkRunner, slog.Default())
				r.Route("/benchmark", func(r chi.Router) {
					bh.RegisterRoutes(r)
				})
			}
		})

		// ── Webhooks (authenticated via platform-specific signatures) ─
		r.Route("/webhooks", func(r chi.Router) {
			r.Use(session.RateLimitMiddleware(s.deps.RateLimiter, "webhook"))
			r.Post("/github", h.HandleGitHubWebhook)
			r.Post("/gitlab", h.HandleGitLabWebhook)
			r.Post("/bitbucket", h.HandleBitbucketWebhook)
			r.Post("/azure-devops", h.HandleAzureDevOpsWebhook)
		})
	})

	// ── Swarm internal routes (agent JWT or service secret) ──────────
	if s.deps.SwarmHandler != nil && s.deps.SwarmAuthSvc != nil {
		sh := s.deps.SwarmHandler

		r.Route("/internal/swarm", func(r chi.Router) {
			// Registration — requires service secret, not agent JWT.
			r.Route("/auth", func(r chi.Router) {
				r.With(swarmauth.RequireServiceSecret(s.deps.SwarmAuthSvc)).
					Post("/register", sh.RegisterAgent)
			})

			// All other internal routes require agent JWT.
			r.Group(func(r chi.Router) {
				r.Use(swarmauth.RequireAgentToken(s.deps.SwarmAuthSvc))

				r.Delete("/auth/revoke", sh.RevokeAgent)
				r.Get("/tasks/next", sh.GetNextTask)
				r.Get("/tasks/{id}", sh.GetTaskInternal)
				r.Get("/tasks/{id}/status", sh.GetTaskStatus)
				r.Post("/tasks/{id}/plan", sh.SubmitPlan)
				r.Post("/tasks/{id}/diffs", sh.SubmitDiff)
				r.Get("/tasks/{id}/diffs", sh.ListDiffs)
				r.Post("/tasks/{id}/diffs/{diffId}/comments", sh.AddDiffComment)
				r.Post("/tasks/{id}/complete", sh.CompleteTask)
				r.Post("/tasks/{id}/fail", sh.FailTask)
				r.Post("/tasks/{id}/declare-size", sh.DeclareTeamSize)
				r.Post("/tasks/{id}/contribution", sh.RecordContribution)
				r.Post("/tasks/{id}/agent-message", sh.AgentMessage)
				r.Post("/tasks/{id}/discussion", sh.DiscussionEvent)
				r.Post("/tasks/{id}/consensus", sh.ConsensusEvent)
				r.Post("/heartbeat/{id}", sh.Heartbeat)

				// Memory hierarchy.
				r.Post("/memory/mtm", sh.HandleMTMStore)
				r.Get("/memory/mtm", sh.HandleMTMRecall)

				// Cross-task consensus insights.
				r.Post("/memory/insights", sh.HandleInsightStore)
				r.Get("/memory/insights", sh.HandleInsightRecall)
				r.Get("/memory/provider-stats", sh.HandleProviderStats)

				// Role-based ELO.
				r.Post("/role-elo/outcome", sh.HandleRoleELOOutcome)
				r.Get("/role-elo/{role}", sh.HandleGetRoleELO)

				// Human-in-the-loop.
				r.Post("/hitl/ask", sh.HandleHITLAsk)

				// CI proxy.
				r.Post("/ci/run", sh.HandleCIRun)

				// CI signal ingestion (webhook + agent report).
				r.Post("/ci-signal/webhook", sh.HandleCISignalWebhook)
				r.Post("/ci-signal/report", sh.HandleCISignalReport)

				// Team formation (dynamic complexity → team sizing).
				r.Post("/team-recommend", sh.HandleTeamRecommend)

				// Adaptive probe tuning (config fetch + outcome recording).
				r.Get("/probe-config", sh.HandleGetProbeConfig)
				r.Post("/probe-history", sh.HandleRecordProbeHistory)

				// Self-healing pipeline (provider outcome reporting + status check).
				r.Post("/self-heal/provider-outcome", sh.HandleProviderOutcome)
				r.Get("/self-heal/provider-status", sh.HandleProviderStatus)

				// Web fetch proxy (URL fetching for agents).
				r.Post("/web/fetch", sh.HandleWebFetch)

				// Inter-agent communication bus.
				r.Post("/agent-bus/publish", sh.HandleAgentBusPublish)
				r.Get("/agent-bus/read", sh.HandleAgentBusRead)

				// Asset ingestion (document/PDF/URL → engine embedding).
				r.Post("/ingest-asset", sh.HandleIngestAsset)

				// MCP integration call (agent → external services).
				r.Post("/mcp/call", sh.HandleMCPCall)
				r.Post("/mcp/batch", sh.HandleMCPBatchCall)
				r.Get("/mcp/providers", sh.HandleMCPListProviders)
				r.Get("/mcp/describe", sh.HandleMCPDescribeAction)
				r.Get("/mcp/tools", sh.HandleMCPListTools)
				r.Get("/mcp/connections", sh.HandleMCPCheckConnections)

				// VCS proxy for agent workspace reads.
				r.Post("/vcs/read-file", sh.VCSReadFile)
				r.Post("/vcs/list-dir", sh.VCSListDir)

				// Sandbox builder (ephemeral container builds).
				if s.deps.Config != nil && s.deps.Config.Sandbox.Enabled {
					var buildStore *sandbox.BuildStore
					if s.deps.DB != nil {
						buildStore = sandbox.NewBuildStore(s.deps.DB.Pool)
					}
					sandboxHandler := sandbox.NewHandler(
						sandbox.NewDockerRuntime(nil),
						s.deps.KeychainService,
						buildStore,
						nil,
					)
					sandboxHandler.Limits = &sandbox.SandboxLimits{
						MaxTimeoutSec:  s.deps.Config.Sandbox.MaxTimeoutSec,
						MaxMemoryMB:    s.deps.Config.Sandbox.MaxMemoryMB,
						MaxCPU:         s.deps.Config.Sandbox.MaxCPU,
						MaxRetries:     s.deps.Config.Sandbox.MaxRetries,
						DefaultSandbox: s.deps.Config.Sandbox.DefaultSandbox,
					}
					if s.deps.DB != nil {
						sandboxHandler.Audit = sandbox.NewAuditLogger(nil, s.deps.DB.Pool)
					} else {
						sandboxHandler.Audit = sandbox.NewAuditLogger(nil, nil)
					}
					r.Post("/sandbox/plan", sandboxHandler.HandleGeneratePlan)
					r.Post("/sandbox/probe", sandboxHandler.HandleProbeEnv)
					r.Post("/sandbox/execute", sandboxHandler.HandleExecute)
					r.Post("/sandbox/resolve-execute", sandboxHandler.HandleResolveAndExecute)
					r.Post("/sandbox/retry", sandboxHandler.HandleRetry)
					r.Get("/sandbox/status/{id}", sandboxHandler.HandleStatus)
					r.Get("/sandbox/logs/{id}", sandboxHandler.HandleLogs)
					r.Get("/sandbox/secrets", sandboxHandler.HandleListBuildSecrets)
					r.Get("/sandbox/artifacts/{id}", sandboxHandler.HandleListArtifacts)
					r.Get("/sandbox/complexity/{repo_id}", sandboxHandler.HandleBuildComplexity)
					r.Get("/sandbox/health", sandboxHandler.HandleHealth)
				}

				// LLM proxy.
				if s.deps.SwarmLLMProxy != nil {
					r.Post("/llm/complete", s.deps.SwarmLLMProxy.HandleComplete)
					r.Post("/llm/probe", s.deps.SwarmLLMProxy.HandleProbe)
				}
			})
		})

		// ── Swarm user-facing routes (under /api/v1, user JWT) ──────
		r.Route("/api/v1/swarm", func(r chi.Router) {
			if s.deps.JWTMgr != nil {
				r.Use(auth.Middleware(s.deps.JWTMgr))
			}
			r.Use(session.RateLimitMiddleware(s.deps.RateLimiter, "api"))

			r.Post("/tasks", sh.CreateTaskUser)
			r.Get("/tasks", sh.ListTasksUser)
			r.Get("/tasks/history", sh.TaskHistory)
			r.Get("/tasks/{id}", sh.GetTaskUser)
			r.Post("/tasks/{id}/plan-action", sh.PlanAction)
			r.Get("/tasks/{id}/diffs", sh.GetDiffsUser)
			r.Get("/tasks/{id}/diffs/{diffId}/content", sh.GetDiffContent)
			r.Post("/tasks/{id}/diffs/{diffId}/comments", sh.UserDiffComment)
			r.Post("/tasks/{id}/diff-action", sh.DiffAction)
			r.Post("/tasks/{id}/rate", sh.RateTaskUser)
			r.Post("/tasks/{id}/retry", sh.RetryTask)
			r.Post("/tasks/{id}/cancel", sh.CancelTask)
			r.Delete("/tasks/{id}", sh.DeleteTaskUser)
			r.Get("/tasks/{id}/agents", sh.GetTaskAgents)
			r.Get("/agents", sh.ListAgentsUser)
			r.Get("/teams", sh.ListTeamsUser)
			r.Get("/overview", sh.SwarmOverview)

			// CI signal status (user JWT).
			r.Get("/tasks/{id}/ci-signal", sh.HandleGetCISignal)
			r.Get("/ci-signals", sh.HandleListCISignals)

			// Team formation (user JWT).
			r.Get("/tasks/{id}/team-formation", sh.HandleGetTeamFormation)

			// Adaptive probe tuning (user JWT).
			r.Get("/probe-configs", sh.HandleListProbeConfigs)
			r.Get("/probe-configs/{role}", sh.HandleGetProbeConfigByRole)
			r.Put("/probe-configs/{role}", sh.HandleUpdateProbeConfig)
			r.Get("/probe-stats/{role}", sh.HandleGetProbeStats)
			r.Get("/probe-history", sh.HandleListProbeHistory)

			// Self-healing pipeline (user JWT).
			r.Get("/self-heal/summary", sh.HandleSelfHealSummary)
			r.Get("/self-heal/events", sh.HandleSelfHealEvents)
			r.Post("/self-heal/events/{id}/resolve", sh.HandleResolveEvent)
			r.Post("/self-heal/circuits/{provider}/reset", sh.HandleResetCircuit)
			r.Get("/self-heal/circuits", sh.HandleListCircuits)

			// Observability dashboard (user JWT).
			r.Get("/observability/dashboard", sh.HandleObservabilityDashboard)
			r.Get("/observability/time-series", sh.HandleObservabilityTimeSeries)
			r.Get("/observability/providers", sh.HandleObservabilityProviders)
			r.Get("/observability/providers/{provider}", sh.HandleObservabilityProviderDetail)
			r.Get("/observability/cost", sh.HandleObservabilityCost)
			r.Get("/observability/health", sh.HandleObservabilityHealth)
			r.Put("/observability/budget", sh.HandleObservabilitySetBudget)

			// Human-in-the-loop response (user JWT).
			r.Post("/hitl/respond", sh.HandleHITLRespond)

			// Cross-task consensus insights (user JWT).
			r.Get("/insights", sh.HandleInsightRecallPublic)

			// Role-based ELO leaderboard + history (user JWT).
			r.Get("/role-elo", sh.HandleRoleELOLeaderboard)
			r.Get("/role-elo/{role}/history", sh.HandleRoleELOHistory)

			// WebSocket: real-time swarm task events
			if s.deps.WSHub != nil {
				swarmWS := ws.NewSwarmHandler(s.deps.WSHub)
				r.Get("/tasks/{id}/ws", swarmWS.ServeHTTP)
				r.Get("/ws", swarmWS.ServeHTTP) // global swarm events
			}
		})
	}

	s.router = r
}
