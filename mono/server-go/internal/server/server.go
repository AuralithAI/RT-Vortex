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
	"github.com/AuralithAI/rtvortex-server/internal/config"
	rtcrypto "github.com/AuralithAI/rtvortex-server/internal/crypto"
	"github.com/AuralithAI/rtvortex-server/internal/engine"
	"github.com/AuralithAI/rtvortex-server/internal/indexing"
	"github.com/AuralithAI/rtvortex-server/internal/llm"
	rtmetrics "github.com/AuralithAI/rtvortex-server/internal/metrics"
	"github.com/AuralithAI/rtvortex-server/internal/quota"
	"github.com/AuralithAI/rtvortex-server/internal/review"
	"github.com/AuralithAI/rtvortex-server/internal/session"
	"github.com/AuralithAI/rtvortex-server/internal/store"
	"github.com/AuralithAI/rtvortex-server/internal/tracing"
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
	UserRepo    *store.UserRepository
	RepoRepo    *store.RepositoryRepo
	ReviewRepo  *store.ReviewRepository
	OrgRepo     *store.OrgRepository
	WebhookRepo *store.WebhookRepository
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
				r.Post("/providers/test", h.TestLLMProvider)
				r.Post("/stream", h.StreamLLMCompletion) // SSE streaming
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
