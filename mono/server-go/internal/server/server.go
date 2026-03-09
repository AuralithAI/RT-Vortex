// Package server wires together the HTTP router, middleware, and API handlers.
package server

import (
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
	"github.com/AuralithAI/rtvortex-server/internal/chat"
	"github.com/AuralithAI/rtvortex-server/internal/config"
	rtcrypto "github.com/AuralithAI/rtvortex-server/internal/crypto"
	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/indexing"
	"github.com/AuralithAI/rtvortex-server/internal/llm"
	rtmetrics "github.com/AuralithAI/rtvortex-server/internal/metrics"
	"github.com/AuralithAI/rtvortex-server/internal/prsync"
	"github.com/AuralithAI/rtvortex-server/internal/quota"
	"github.com/AuralithAI/rtvortex-server/internal/review"
	"github.com/AuralithAI/rtvortex-server/internal/session"
	"github.com/AuralithAI/rtvortex-server/internal/store"
	"github.com/AuralithAI/rtvortex-server/internal/tracing"
	"github.com/AuralithAI/rtvortex-server/internal/vault"
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
	VCSRegistry     *vcs.PlatformRegistry
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

	// File Vault — shared vault for per-user secret storage
	Vault *vault.FileVault

	// VCS Platform Config — per-user non-secret VCS settings (URLs, usernames)
	VCSPlatformRepo *store.VCSPlatformRepo

	// Engine Metrics Collector — Phase 0 observability
	MetricsCollector *engine.MetricsCollector
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
		VCSRegistry:     s.deps.VCSRegistry,
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
		Vault:           s.deps.Vault,
		VCSPlatformRepo: s.deps.VCSPlatformRepo,
	}
	if s.deps.MetricsCollector != nil {
		h.MetricsCollector = s.deps.MetricsCollector
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
					r.Get("/members", h.ListRepoMembers)
					r.Post("/members", h.AddRepoMember)
					r.Delete("/members/{userID}", h.RemoveRepoMember)

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
				r.Post("/providers/test", h.TestLLMProvider)
				r.Put("/primary", h.SetPrimaryLLMProvider)
				r.Post("/stream", h.StreamLLMCompletion) // SSE streaming
			})

			// Embeddings Configuration
			r.Route("/embeddings", func(r chi.Router) {
				r.Get("/config", h.GetEmbeddingsConfig)
				r.Put("/config", h.UpdateEmbeddingsConfig)
			})

			// VCS Platform Settings (per-user credentials stored in vault)
			r.Route("/vcs", func(r chi.Router) {
				r.Get("/platforms", h.ListVCSPlatforms)
				r.Put("/platforms/{platform}", h.ConfigureVCSPlatform)
				r.Delete("/platforms/{platform}", h.DeleteVCSPlatform)
				r.Post("/platforms/{platform}/test", h.TestVCSPlatform)
			})

			// Engine Metrics — Phase 0 observability
			r.Route("/engine", func(r chi.Router) {
				r.Get("/metrics", h.GetEngineMetrics)
				r.Get("/health", h.GetEngineHealth)

				// WebSocket: real-time engine metrics streaming
				if s.deps.WSHub != nil {
					metricsWSHandler := ws.NewMetricsHandler(s.deps.WSHub)
					r.Get("/metrics/ws", metricsWSHandler.ServeHTTP)
				}
			})

			// Admin
			r.Route("/admin", func(r chi.Router) {
				r.Use(auth.RequireRole("admin"))
				r.Get("/stats", h.GetSystemStats)
				r.Get("/health/detailed", h.GetDetailedHealth)
			})
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

	s.router = r
}
