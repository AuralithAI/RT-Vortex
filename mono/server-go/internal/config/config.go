// Package config loads application configuration from XML files.
//
// It reads the main configuration file:
//   - rtserverprops.xml  -- server, database, redis, engine, LLM, review, security, storage
//
// VCS platform credentials (OAuth tokens, webhook secrets) are resolved at
// runtime from the per-user vault and database — see the vcs.Resolver type.
//
// Variables of the form ${ENV_VAR:default} are resolved at load time:
//  1. Environment variable ENV_VAR
//  2. The default value after the colon
//  3. Empty string if neither is available
package config

import (
	"encoding/xml"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---- Resolved Go structs (used by the rest of the server) ----
// These are the "output" types the rest of the codebase depends on.
// The XML raw types below are internal intermediaries.

// Config is the root configuration for the RTVortex API server.
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Engine   EngineConfig
	Auth     AuthConfig
	LLM      LLMConfig
	Review   ReviewConfig
	Storage  StorageConfig
	Log      LogConfig
	MCP      MCPConfig
	Sandbox  SandboxConfig
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Host            string // Bind address (default "0.0.0.0"); also used for OAuth callback URLs
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	AllowedOrigins  []string
	TLS             TLSConfig
	ContextPath     string
}

// TLSConfig holds TLS/HTTPS settings.
type TLSConfig struct {
	Enabled  bool
	CertFile string
	KeyFile  string
}

// DatabaseConfig holds PostgreSQL connection settings.
type DatabaseConfig struct {
	Host            string
	Port            int
	Name            string
	User            string
	Password        string
	SSLMode         string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	ConnTimeout     time.Duration
	MigrationsPath  string
}

// DSN returns the PostgreSQL connection string.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User, d.Password, d.Host, d.Port, d.Name, d.SSLMode,
	)
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Addr         string
	Password     string
	DB           int
	MaxRetries   int
	PoolSize     int
	MinIdleConns int
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// EngineConfig holds gRPC connection settings to the RTVortex C++ engine.
type EngineConfig struct {
	Host           string
	Port           int
	TLS            bool
	CertFile       string
	KeyFile        string
	CAFile         string
	MaxChannels    int
	IdleTimeout    time.Duration
	RequestTimeout time.Duration
	MaxRetries     int
	RetryBackoff   time.Duration
}

// AuthConfig holds authentication/authorization settings.
type AuthConfig struct {
	JWTSecret         string
	JWTExpiration     time.Duration
	RefreshExpiration time.Duration
	EncryptionKey     string
	Providers         map[string]OAuthProvider
}

// OAuthProvider holds configuration for a single OAuth2 provider.
type OAuthProvider struct {
	ClientID     string
	ClientSecret string
	Scopes       []string
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	CallbackPath string
}

// LLMConfig holds LLM provider settings.
type LLMConfig struct {
	Primary        string
	Fallback       string
	MaxTokens      int
	Temperature    float64
	Timeout        time.Duration
	Providers      map[string]LLMProviderConfig
	Routes         map[string]LLMRouteConfig     // agent role → preferred provider/model (legacy single-route)
	PriorityMatrix map[string][]LLMPriorityEntry // role → ordered list of providers (multi-LLM)
}

// LLMRouteConfig maps an agent role to a preferred provider and optional model.
// Retained for backward compatibility — single-provider routing.
type LLMRouteConfig struct {
	Provider string
	Model    string
}

// LLMPriorityEntry is a single entry in the multi-LLM priority matrix.
// Each agent role maps to an ordered slice of these, tried in sequence.
// GPT/OpenAI is always placed last by convention (enforced at load time).
type LLMPriorityEntry struct {
	Provider    string   `json:"provider"`               // provider name, e.g. "grok", "anthropic"
	Model       string   `json:"model,omitempty"`        // optional model override
	ActionTypes []string `json:"action_types,omitempty"` // if set, only used for these actions (e.g. "reasoning", "code_gen")
}

// LLMProviderCapabilities describes a provider's strengths for intelligent routing.
type LLMProviderCapabilities struct {
	Strengths         []string `json:"strengths"`          // e.g. ["reasoning", "code_gen", "security"]
	LatencyTier       string   `json:"latency_tier"`       // "fast", "medium", "slow"
	MaxContextTokens  int      `json:"max_context_tokens"` // e.g. 128000, 200000
	SupportsStreaming bool     `json:"supports_streaming"`
	CostTier          string   `json:"cost_tier"` // "low", "medium", "high" (informational, not enforced)
}

// DefaultProviderCapabilities returns built-in capability profiles for known providers.
// These are used when no explicit capabilities are configured.
func DefaultProviderCapabilities() map[string]LLMProviderCapabilities {
	return map[string]LLMProviderCapabilities{
		"grok": {
			Strengths:         []string{"reasoning", "analysis", "architecture", "code_gen"},
			LatencyTier:       "fast",
			MaxContextTokens:  131072,
			SupportsStreaming: true,
			CostTier:          "medium",
		},
		"anthropic": {
			Strengths:         []string{"reasoning", "code_gen", "security", "architecture", "refactoring"},
			LatencyTier:       "medium",
			MaxContextTokens:  200000,
			SupportsStreaming: true,
			CostTier:          "high",
		},
		"gemini": {
			Strengths:         []string{"reasoning", "analysis", "multimodal", "large_context"},
			LatencyTier:       "medium",
			MaxContextTokens:  1000000,
			SupportsStreaming: true,
			CostTier:          "medium",
		},
		"openai": {
			Strengths:         []string{"code_gen", "general", "function_calling"},
			LatencyTier:       "fast",
			MaxContextTokens:  128000,
			SupportsStreaming: true,
			CostTier:          "high",
		},
		"ollama": {
			Strengths:         []string{"local", "privacy", "low_latency"},
			LatencyTier:       "fast",
			MaxContextTokens:  32768,
			SupportsStreaming: true,
			CostTier:          "low",
		},
	}
}

// LLMProviderConfig holds settings for a single LLM provider.
// API keys are NOT stored here — they come from env vars or the dashboard UI
// and are managed by the LLM registry at runtime.
type LLMProviderConfig struct {
	BaseURL string
	Model   string
	Models  []string
}

// ReviewConfig holds review pipeline settings.
type ReviewConfig struct {
	MaxDiffSize      int
	MaxFilesPerPR    int
	MaxComments      int
	EnableHeuristics bool
}

// StorageConfig holds settings for index data storage.
type StorageConfig struct {
	Type     string // local, s3, gcs, azure, oci, minio
	BasePath string
}

// LogConfig holds logging settings.
type LogConfig struct {
	Level  string // debug, info, warn, error
	Format string // text, json
}

// MCPConfig holds MCP (Model Context Protocol) integration settings.
type MCPConfig struct {
	Enabled          bool
	MaxCallsPerTask  int
	AllowedProviders []string
	CallTimeout      time.Duration
	SlackBaseURL     string
	MS365GraphURL    string
	MS365TokenURL    string
	GmailBaseURL     string
	GmailTokenURL    string
	DiscordBaseURL   string
	GitLabBaseURL    string

	// Per-provider OAuth credentials for MCP integrations (separate from auth SSO providers).
	// These enable the one-click OAuth connect flow in the MCP Integrations UI.
	OAuthProviders map[string]MCPOAuthProviderConfig
}

// MCPOAuthProviderConfig holds OAuth2 credentials for a single MCP provider.
type MCPOAuthProviderConfig struct {
	ClientID     string
	ClientSecret string
	Scopes       []string
	AuthURL      string
	TokenURL     string
}

// SandboxConfig holds settings for the ephemeral container build system.
type SandboxConfig struct {
	Enabled         bool          // Master switch — must be true to run sandbox builds.
	AlwaysValidate  bool          // When true, builder runs on every task (not just build-file changes).
	DefaultSandbox  bool          // When true, workspace is mounted read-only (sandbox mode).
	MaxTimeoutSec   int           // Maximum build timeout in seconds (default 600 = 10 min).
	MaxMemoryMB     int           // Maximum container memory in MB (default 2048 = 2 GB).
	MaxCPU          int           // Maximum container CPU cores (default 2).
	MaxRetries      int           // Maximum build retry attempts (default 2).
}

// ---- XML intermediate types ----
// These mirror the XML structure and are unmarshalled first, then converted.

type xmlServerProps struct {
	XMLName       xml.Name         `xml:"serverproperties"`
	Server        xmlServer        `xml:"server"`
	Database      xmlDatabase      `xml:"database"`
	Redis         xmlRedis         `xml:"redis"`
	GRPCServer    xmlGRPCServer    `xml:"grpc-server"`
	Engine        xmlEngine        `xml:"engine"`
	LLM           xmlLLM           `xml:"llm"`
	Review        xmlReview        `xml:"review"`
	RateLimit     xmlRateLimit     `xml:"rate-limit"`
	Security      xmlSecurity      `xml:"security"`
	AuthProviders xmlAuthProviders `xml:"auth-providers"`
	Storage       xmlStorage       `xml:"storage"`
	Repos         xmlRepositories  `xml:"repositories"`
	Logging       xmlLogging       `xml:"logging"`
	MCP           xmlMCP           `xml:"mcp"`
	Sandbox       xmlSandbox       `xml:"sandbox"`
}

// xmlSandbox maps the <sandbox> element in rtserverprops.xml.
//
//	<sandbox enabled="true" always-validate="true" default-sandbox-mode="true"
//	         max-timeout-seconds="600" max-memory-mb="2048" max-cpu="2"
//	         max-retries="2" />
type xmlSandbox struct {
	Enabled           string `xml:"enabled,attr"`
	AlwaysValidate    string `xml:"always-validate,attr"`
	DefaultSandboxMode string `xml:"default-sandbox-mode,attr"`
	MaxTimeoutSec     string `xml:"max-timeout-seconds,attr"`
	MaxMemoryMB       string `xml:"max-memory-mb,attr"`
	MaxCPU            string `xml:"max-cpu,attr"`
	MaxRetries        string `xml:"max-retries,attr"`
}

type xmlServer struct {
	Host        string       `xml:"host,attr"`
	Port        string       `xml:"port,attr"`
	Shutdown    string       `xml:"shutdown,attr"`
	ContextPath string       `xml:"context-path,attr"`
	TLS         xmlServerTLS `xml:"tls"`
}

// xmlServerTLS holds TLS config for the HTTP server.
type xmlServerTLS struct {
	Enabled  string `xml:"enabled,attr"`
	CertFile string `xml:"cert-file,attr"`
	KeyFile  string `xml:"key-file,attr"`
}

type xmlDatabase struct {
	URL      string    `xml:"url,attr"`
	Username string    `xml:"username,attr"`
	Password string    `xml:"password,attr"`
	Driver   string    `xml:"driver,attr"`
	Pool     xmlDBPool `xml:"pool"`
	Flyway   xmlFlyway `xml:"flyway"`
}

type xmlDBPool struct {
	Name                string `xml:"name,attr"`
	MaxSize             string `xml:"max-size,attr"`
	MinIdle             string `xml:"min-idle,attr"`
	ConnectionTimeoutMs string `xml:"connection-timeout-ms,attr"`
	IdleTimeoutMs       string `xml:"idle-timeout-ms,attr"`
	MaxLifetimeMs       string `xml:"max-lifetime-ms,attr"`
	LeakDetectionMs     string `xml:"leak-detection-threshold-ms,attr"`
}

type xmlFlyway struct {
	Enabled           string `xml:"enabled,attr"`
	Locations         string `xml:"locations,attr"`
	BaselineOnMigrate string `xml:"baseline-on-migrate,attr"`
}

type xmlRedis struct {
	Host      string       `xml:"host,attr"`
	Port      string       `xml:"port,attr"`
	Password  string       `xml:"password,attr"`
	Database  string       `xml:"database,attr"`
	TimeoutMs string       `xml:"timeout-ms,attr"`
	Pool      xmlRedisPool `xml:"pool"`
	Cluster   xmlCluster   `xml:"cluster"`
}

type xmlRedisPool struct {
	MaxActive string `xml:"max-active,attr"`
	MaxIdle   string `xml:"max-idle,attr"`
	MinIdle   string `xml:"min-idle,attr"`
	MaxWaitMs string `xml:"max-wait-ms,attr"`
}

type xmlCluster struct {
	Enabled string `xml:"enabled,attr"`
	Nodes   string `xml:"nodes,attr"`
}

type xmlGRPCServer struct {
	Port     string          `xml:"port,attr"`
	Security xmlGRPCSecurity `xml:"security"`
}

type xmlGRPCSecurity struct {
	Enabled    string `xml:"enabled,attr"`
	CertChain  string `xml:"cert-chain,attr"`
	PrivateKey string `xml:"private-key,attr"`
	TrustCerts string `xml:"trust-certs,attr"`
	ClientAuth string `xml:"client-auth,attr"`
}

type xmlEngine struct {
	Host            string        `xml:"host,attr"`
	Port            string        `xml:"port,attr"`
	TimeoutMs       string        `xml:"timeout-ms,attr"`
	NegotiationType string        `xml:"negotiation-type,attr"`
	TLSCfg          xmlEngineTLS  `xml:"tls"`
	Retry           xmlRetry      `xml:"retry"`
	Pool            xmlEnginePool `xml:"pool"`
}

type xmlEngineTLS struct {
	CertChain  string `xml:"cert-chain,attr"`
	PrivateKey string `xml:"private-key,attr"`
	TrustCerts string `xml:"trust-certs,attr"`
}

type xmlRetry struct {
	MaxAttempts      string `xml:"max-attempts,attr"`
	InitialBackoffMs string `xml:"initial-backoff-ms,attr"`
	MaxBackoffMs     string `xml:"max-backoff-ms,attr"`
}

type xmlEnginePool struct {
	MaxChannels        string `xml:"max-channels,attr"`
	IdleTimeoutSeconds string `xml:"idle-timeout-seconds,attr"`
}

type xmlLLM struct {
	Primary           string         `xml:"primary,attr"`
	Fallback          string         `xml:"fallback,attr"`
	AutoDiscoverLocal string         `xml:"auto-discover-local,attr"`
	MaxTokens         string         `xml:"max-tokens,attr"`
	Temperature       string         `xml:"temperature,attr"`
	TimeoutMs         string         `xml:"timeout-ms,attr"`
	OpenAI            xmlLLMProvider `xml:"openai"`
	Anthropic         xmlLLMProvider `xml:"anthropic"`
	Gemini            xmlLLMProvider `xml:"gemini"`
	Grok              xmlLLMProvider `xml:"grok"`
	AzureOpenAI       xmlLLMProvider `xml:"azure-openai"`
	Ollama            xmlLLMProvider `xml:"ollama"`
	Custom            xmlLLMProvider `xml:"custom"`
	Routing           xmlLLMRouting  `xml:"routing"`
}

type xmlLLMRouting struct {
	Routes         []xmlLLMRoute     `xml:"route"`
	PriorityMatrix []xmlPriorityRole `xml:"priority-matrix>role-priority"`
}

type xmlLLMRoute struct {
	Role     string `xml:"role,attr"`
	Provider string `xml:"provider,attr"`
	Model    string `xml:"model,attr"`
}

// xmlPriorityRole holds a single role's ordered list of providers.
type xmlPriorityRole struct {
	Role    string             `xml:"name,attr"`
	Entries []xmlPriorityEntry `xml:"provider"`
}

// xmlPriorityEntry is one provider in the priority order for a role.
type xmlPriorityEntry struct {
	Name        string `xml:"name,attr"`
	Model       string `xml:"model,attr"`
	ActionTypes string `xml:"action-types,attr"`
}

type xmlLLMProvider struct {
	BaseURL        string `xml:"base-url,attr"`
	Model          string `xml:"model,attr"`
	Models         string `xml:"models,attr"`
	Endpoint       string `xml:"endpoint,attr"`
	Deployment     string `xml:"deployment,attr"`
	DiscoveryHost  string `xml:"discovery-host,attr"`
	DiscoveryPorts string `xml:"discovery-ports,attr"`
}

type xmlReview struct {
	MaxDiffSize            string `xml:"max-diff-size,attr"`
	MaxFilesPerPR          string `xml:"max-files-per-pr,attr"`
	MaxComments            string `xml:"max-comments,attr"`
	EnableHeuristics       string `xml:"enable-heuristics,attr"`
	EnableContextRetrieval string `xml:"enable-context-retrieval,attr"`
}

type xmlRateLimit struct {
	Enabled              string `xml:"enabled,attr"`
	ReviewsPerHour       string `xml:"reviews-per-hour,attr"`
	IndexRequestsPerHour string `xml:"index-requests-per-hour,attr"`
}

type xmlSecurity struct {
	JWTSecret       string `xml:"jwt-secret,attr"`
	JWTExpirationMs string `xml:"jwt-expiration-ms,attr"`
	AllowedOrigins  string `xml:"allowed-origins,attr"`
	EncryptionKey   string `xml:"encryption-key,attr"`
}

type xmlAuthProviders struct {
	Providers []xmlAuthProvider `xml:"provider"`
}

type xmlAuthProvider struct {
	Name         string `xml:"name,attr"`
	ClientID     string `xml:"client-id,attr"`
	ClientSecret string `xml:"client-secret,attr"`
	Scopes       string `xml:"scopes,attr"`
}

type xmlStorage struct {
	Type       string          `xml:"type,attr"`
	TimeoutMs  string          `xml:"timeout-ms,attr"`
	MaxRetries string          `xml:"max-retries,attr"`
	UseSSL     string          `xml:"use-ssl,attr"`
	VerifySSL  string          `xml:"verify-ssl,attr"`
	CABundle   string          `xml:"ca-bundle-path,attr"`
	Local      xmlStorageLocal `xml:"local"`
	S3         xmlStorageS3    `xml:"s3"`
}

type xmlStorageLocal struct {
	BasePath string `xml:"base-path,attr"`
}

type xmlStorageS3 struct {
	Bucket       string `xml:"bucket,attr"`
	Region       string `xml:"region,attr"`
	Endpoint     string `xml:"endpoint,attr"`
	AccessKey    string `xml:"access-key,attr"`
	SecretKey    string `xml:"secret-key,attr"`
	SessionToken string `xml:"session-token,attr"`
	UseIRSA      string `xml:"use-irsa,attr"`
	RoleARN      string `xml:"role-arn,attr"`
}

type xmlRepositories struct {
	BasePath string `xml:"base-path,attr"`
}

type xmlLogging struct {
	RootLevel string `xml:"root-level,attr"`
	AppLevel  string `xml:"app-level,attr"`
}

type xmlMCP struct {
	Enabled          string `xml:"enabled,attr"`
	MaxCallsPerTask  string `xml:"max-calls-per-task,attr"`
	AllowedProviders string `xml:"allowed-providers,attr"`
	CallTimeoutMs    string `xml:"call-timeout-ms,attr"`
	SlackBaseURL     string `xml:"slack-base-url,attr"`
	MS365GraphURL    string `xml:"ms365-graph-url,attr"`
	MS365TokenURL    string `xml:"ms365-token-url,attr"`
	GmailBaseURL     string `xml:"gmail-base-url,attr"`
	GmailTokenURL    string `xml:"gmail-token-url,attr"`
	DiscordBaseURL   string `xml:"discord-base-url,attr"`
	GitLabBaseURL    string `xml:"gitlab-base-url,attr"`

	// ── Google Workspace (single OAuth client, per-product scopes) ──
	// All Google products (Gmail, Calendar, Drive, etc.) share the same
	// Google Cloud OAuth 2.0 credentials. Only the scopes differ.
	GoogleClientID     string `xml:"google-client-id,attr"`
	GoogleClientSecret string `xml:"google-client-secret,attr"`
	// Per-product scope overrides (comma-separated). Each product sends
	// only its own scopes during the authorize redirect.
	GmailOAuthScopes          string `xml:"gmail-oauth-scopes,attr"`
	GoogleCalendarOAuthScopes string `xml:"google-calendar-oauth-scopes,attr"`
	GoogleDriveOAuthScopes    string `xml:"google-drive-oauth-scopes,attr"`

	// ── Microsoft 365 ──
	MS365ClientID     string `xml:"ms365-client-id,attr"`
	MS365ClientSecret string `xml:"ms365-client-secret,attr"`
	MS365OAuthScopes  string `xml:"ms365-oauth-scopes,attr"`

	// ── Slack ──
	SlackClientID     string `xml:"slack-client-id,attr"`
	SlackClientSecret string `xml:"slack-client-secret,attr"`
	SlackOAuthScopes  string `xml:"slack-oauth-scopes,attr"`

	// ── Discord ──
	DiscordClientID     string `xml:"discord-client-id,attr"`
	DiscordClientSecret string `xml:"discord-client-secret,attr"`
	DiscordOAuthScopes  string `xml:"discord-oauth-scopes,attr"`

	// ── GitHub ──
	GitHubClientID     string `xml:"github-client-id,attr"`
	GitHubClientSecret string `xml:"github-client-secret,attr"`
	GitHubOAuthScopes  string `xml:"github-oauth-scopes,attr"`

	// ── Atlassian (Jira + Confluence share the same Atlassian OAuth client) ──
	AtlassianClientID     string `xml:"atlassian-client-id,attr"`
	AtlassianClientSecret string `xml:"atlassian-client-secret,attr"`
	JiraOAuthScopes       string `xml:"jira-oauth-scopes,attr"`
	ConfluenceOAuthScopes string `xml:"confluence-oauth-scopes,attr"`

	// ── Notion ──
	NotionClientID     string `xml:"notion-client-id,attr"`
	NotionClientSecret string `xml:"notion-client-secret,attr"`
	NotionOAuthScopes  string `xml:"notion-oauth-scopes,attr"`

	// ── GitLab ──
	GitLabClientID     string `xml:"gitlab-client-id,attr"`
	GitLabClientSecret string `xml:"gitlab-client-secret,attr"`
	GitLabOAuthScopes  string `xml:"gitlab-oauth-scopes,attr"`

	// ── Linear ──
	LinearClientID     string `xml:"linear-client-id,attr"`
	LinearClientSecret string `xml:"linear-client-secret,attr"`
	LinearOAuthScopes  string `xml:"linear-oauth-scopes,attr"`

	// ── Asana ──
	AsanaClientID     string `xml:"asana-client-id,attr"`
	AsanaClientSecret string `xml:"asana-client-secret,attr"`
	AsanaOAuthScopes  string `xml:"asana-oauth-scopes,attr"`

	// ── Trello (Atlassian — uses REST key+token, wrapped in OAuth-like flow) ──
	TrelloClientID     string `xml:"trello-client-id,attr"`
	TrelloClientSecret string `xml:"trello-client-secret,attr"`
	TrelloOAuthScopes  string `xml:"trello-oauth-scopes,attr"`

	// ── Figma ──
	FigmaClientID     string `xml:"figma-client-id,attr"`
	FigmaClientSecret string `xml:"figma-client-secret,attr"`
	FigmaOAuthScopes  string `xml:"figma-oauth-scopes,attr"`

	// ── Zendesk ──
	ZendeskClientID     string `xml:"zendesk-client-id,attr"`
	ZendeskClientSecret string `xml:"zendesk-client-secret,attr"`
	ZendeskOAuthScopes  string `xml:"zendesk-oauth-scopes,attr"`

	// ── PagerDuty ──
	PagerDutyClientID     string `xml:"pagerduty-client-id,attr"`
	PagerDutyClientSecret string `xml:"pagerduty-client-secret,attr"`
	PagerDutyOAuthScopes  string `xml:"pagerduty-oauth-scopes,attr"`

	// ── Datadog ──
	DatadogClientID     string `xml:"datadog-client-id,attr"`
	DatadogClientSecret string `xml:"datadog-client-secret,attr"`
	DatadogOAuthScopes  string `xml:"datadog-oauth-scopes,attr"`

	// ── Stripe ──
	StripeClientID     string `xml:"stripe-client-id,attr"`
	StripeClientSecret string `xml:"stripe-client-secret,attr"`
	StripeOAuthScopes  string `xml:"stripe-oauth-scopes,attr"`

	// ── HubSpot ──
	HubSpotClientID     string `xml:"hubspot-client-id,attr"`
	HubSpotClientSecret string `xml:"hubspot-client-secret,attr"`
	HubSpotOAuthScopes  string `xml:"hubspot-oauth-scopes,attr"`

	// ── Salesforce ──
	SalesforceClientID     string `xml:"salesforce-client-id,attr"`
	SalesforceClientSecret string `xml:"salesforce-client-secret,attr"`
	SalesforceOAuthScopes  string `xml:"salesforce-oauth-scopes,attr"`

	// ── Twilio ──
	TwilioClientID     string `xml:"twilio-client-id,attr"`
	TwilioClientSecret string `xml:"twilio-client-secret,attr"`
	TwilioOAuthScopes  string `xml:"twilio-oauth-scopes,attr"`
}

// ---- VCS platforms XML types ----

// ---- Environment variable expansion ----

// envVarRegex matches the innermost ${…} (no nested braces).
var envVarRegex = regexp.MustCompile(`\$\{([^{}]+)\}`)

// builtinAliases maps legacy property-style names to real env-var names.
var builtinAliases = map[string]string{
	"rtvortex.home": "RTVORTEX_HOME",
}

// expandEnvVars resolves ${ENV_VAR:default} in a string.
//
// Supports nested references such as:
//
//	${SERVER_TLS_CERT:${rtvortex.home}/config/certificates/server.crt}
//
// Resolution is iterative (inside-out): the innermost ${…} is expanded
// first, then the result is re-scanned until no ${…} references remain
// (up to 10 iterations to avoid infinite loops).
//
//	${VAR}       -> os.Getenv("VAR"), or ""
//	${VAR:val}   -> os.Getenv("VAR"), or "val"
//	${rtvortex.home} -> os.Getenv("RTVORTEX_HOME"), or ""
func expandEnvVars(s string) string {
	for i := 0; i < 10; i++ {
		if !strings.Contains(s, "${") {
			break
		}
		next := envVarRegex.ReplaceAllStringFunc(s, func(match string) string {
			inner := match[2 : len(match)-1] // strip ${ and }
			parts := strings.SplitN(inner, ":", 2)
			envKey := parts[0]
			defaultVal := ""
			if len(parts) == 2 {
				defaultVal = parts[1]
			}
			// Check built-in aliases (e.g. rtvortex.home → RTVORTEX_HOME).
			if alias, ok := builtinAliases[envKey]; ok {
				envKey = alias
			}
			if v, ok := os.LookupEnv(envKey); ok {
				return v
			}
			return defaultVal
		})
		if next == s {
			break // no more substitutions possible
		}
		s = next
	}
	return s
}

// expand is a shorthand -- expand + return.
func expand(s string) string {
	return expandEnvVars(s)
}

// ---- Helper parsers ----

func parseInt(s string, fallback int) int {
	s = expand(s)
	if s == "" {
		return fallback
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return v
}

func parseInt32(s string, fallback int32) int32 {
	return int32(parseInt(s, int(fallback)))
}

func parseFloat(s string, fallback float64) float64 {
	s = expand(s)
	if s == "" {
		return fallback
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fallback
	}
	return v
}

func parseBool(s string, fallback bool) bool {
	s = expand(s)
	if s == "" {
		return fallback
	}
	v, err := strconv.ParseBool(s)
	if err != nil {
		return fallback
	}
	return v
}

func parseMs(s string, fallback time.Duration) time.Duration {
	ms := parseInt(s, -1)
	if ms < 0 {
		return fallback
	}
	return time.Duration(ms) * time.Millisecond
}

func parseSec(s string, fallback time.Duration) time.Duration {
	sec := parseInt(s, -1)
	if sec < 0 {
		return fallback
	}
	return time.Duration(sec) * time.Second
}

func parseString(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// splitCSV splits a comma-separated string into trimmed, non-empty tokens.
func splitCSV(s string) []string {
	parts := strings.Split(s, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// enforceGPTLast reorders a priority list so that any OpenAI/GPT entry is last.
// This is a system-wide invariant: GPT always gets the final word.
func enforceGPTLast(entries []LLMPriorityEntry) []LLMPriorityEntry {
	var gptEntries []LLMPriorityEntry
	var rest []LLMPriorityEntry
	for _, e := range entries {
		if isOpenAIProvider(e.Provider) {
			gptEntries = append(gptEntries, e)
		} else {
			rest = append(rest, e)
		}
	}
	return append(rest, gptEntries...)
}

// isOpenAIProvider returns true for providers that are OpenAI/GPT variants.
func isOpenAIProvider(name string) bool {
	lower := strings.ToLower(name)
	return lower == "openai" || lower == "azure-openai" || strings.HasPrefix(lower, "gpt")
}

// ---- Config file search ----

// configSearchPaths returns directories to search for config files.
func configSearchPaths() []string {
	paths := []string{}

	// 1. RTVORTEX_HOME/config
	if home := os.Getenv("RTVORTEX_HOME"); home != "" {
		paths = append(paths, home+"/config")
	}

	// 2. Current directory config/
	paths = append(paths, "./config")

	// 3. Mono repo config/ (relative to binary)
	paths = append(paths, "../config")

	// 4. /etc/rtvortex
	paths = append(paths, "/etc/rtvortex")

	return paths
}

// findConfigFile locates a config file by name across search paths.
func findConfigFile(name string, explicitPath string) (string, error) {
	// If an explicit path was given, use it directly.
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err != nil {
			return "", fmt.Errorf("config file not found: %s", explicitPath)
		}
		return explicitPath, nil
	}

	for _, dir := range configSearchPaths() {
		path := dir + "/" + name
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("config file %q not found in search paths: %v", name, configSearchPaths())
}

// ---- Load ----

// LoadOptions controls how configuration is loaded.
type LoadOptions struct {
	// ServerPropsPath overrides the search path for rtserverprops.xml.
	ServerPropsPath string
}

// Load reads configuration from rtserverprops.xml.
// VCS credentials are resolved at runtime from the user vault/DB.
func Load(opts ...LoadOptions) (*Config, error) {
	var o LoadOptions
	if len(opts) > 0 {
		o = opts[0]
	}

	// -- Locate config files --
	serverPropsFile, err := findConfigFile("rtserverprops.xml", o.ServerPropsPath)
	if err != nil {
		return nil, fmt.Errorf("server props: %w", err)
	}

	// -- Parse rtserverprops.xml --
	cfg, err := loadServerProps(serverPropsFile)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", serverPropsFile, err)
	}

	return cfg, nil
}

// ---- rtserverprops.xml loader ----

func loadServerProps(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw xmlServerProps
	if err := xml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("XML parse error: %w", err)
	}

	cfg := &Config{}

	// -- Server --
	serverTLSEnabled := strings.EqualFold(expand(raw.Server.TLS.Enabled), "true")
	serverCert := expand(raw.Server.TLS.CertFile)
	serverKey := expand(raw.Server.TLS.KeyFile)
	// Auto-enable TLS if cert and key are provided but enabled is not explicitly set
	if serverCert != "" && serverKey != "" && raw.Server.TLS.Enabled == "" {
		serverTLSEnabled = true
	}
	cfg.Server = ServerConfig{
		Host:            expand(raw.Server.Host),
		Port:            parseInt(raw.Server.Port, 8080),
		ReadTimeout:     30 * time.Second,
		WriteTimeout:    60 * time.Second,
		IdleTimeout:     120 * time.Second,
		ShutdownTimeout: 30 * time.Second,
		ContextPath:     expand(raw.Server.ContextPath),
		TLS: TLSConfig{
			Enabled:  serverTLSEnabled,
			CertFile: serverCert,
			KeyFile:  serverKey,
		},
	}

	// -- Database --
	// Parse JDBC URL: jdbc:postgresql://host:port/dbname -> host, port, dbname
	dbHost, dbPort, dbName := parseJDBCURL(expand(raw.Database.URL))
	cfg.Database = DatabaseConfig{
		Host:            dbHost,
		Port:            dbPort,
		Name:            dbName,
		User:            expand(raw.Database.Username),
		Password:        expand(raw.Database.Password),
		SSLMode:         "prefer",
		MaxConns:        parseInt32(raw.Database.Pool.MaxSize, 20),
		MinConns:        parseInt32(raw.Database.Pool.MinIdle, 5),
		MaxConnLifetime: parseMs(raw.Database.Pool.MaxLifetimeMs, 30*time.Minute),
		MaxConnIdleTime: parseMs(raw.Database.Pool.IdleTimeoutMs, 10*time.Minute),
		ConnTimeout:     parseMs(raw.Database.Pool.ConnectionTimeoutMs, 30*time.Second),
		MigrationsPath:  "db/migrations",
	}

	// -- Redis --
	redisHost := expand(raw.Redis.Host)
	if redisHost == "" {
		redisHost = "localhost"
	}
	redisPort := parseInt(raw.Redis.Port, 6379)
	cfg.Redis = RedisConfig{
		Addr:         fmt.Sprintf("%s:%d", redisHost, redisPort),
		Password:     expand(raw.Redis.Password),
		DB:           parseInt(raw.Redis.Database, 0),
		MaxRetries:   3,
		PoolSize:     parseInt(raw.Redis.Pool.MaxActive, 16),
		MinIdleConns: parseInt(raw.Redis.Pool.MinIdle, 2),
		DialTimeout:  parseMs(raw.Redis.TimeoutMs, 5*time.Second),
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}

	// -- Engine (C++ gRPC) --
	negotiation := expand(raw.Engine.NegotiationType)
	engineTLS := strings.EqualFold(negotiation, "TLS") || strings.EqualFold(negotiation, "mTLS")
	cfg.Engine = EngineConfig{
		Host:           expand(raw.Engine.Host),
		Port:           parseInt(raw.Engine.Port, 50051),
		TLS:            engineTLS,
		CertFile:       expand(raw.Engine.TLSCfg.CertChain),
		KeyFile:        expand(raw.Engine.TLSCfg.PrivateKey),
		CAFile:         expand(raw.Engine.TLSCfg.TrustCerts),
		MaxChannels:    parseInt(raw.Engine.Pool.MaxChannels, 10),
		IdleTimeout:    parseSec(raw.Engine.Pool.IdleTimeoutSeconds, 5*time.Minute),
		RequestTimeout: parseMs(raw.Engine.TimeoutMs, 30*time.Second),
		MaxRetries:     parseInt(raw.Engine.Retry.MaxAttempts, 3),
		RetryBackoff:   parseMs(raw.Engine.Retry.InitialBackoffMs, 100*time.Millisecond),
	}

	// -- Auth / Security --
	cfg.Auth = AuthConfig{
		JWTSecret:         expand(raw.Security.JWTSecret),
		JWTExpiration:     parseMs(raw.Security.JWTExpirationMs, time.Hour),
		RefreshExpiration: 7 * 24 * time.Hour,
		EncryptionKey:     expand(raw.Security.EncryptionKey),
		Providers:         make(map[string]OAuthProvider),
	}
	if origins := expand(raw.Security.AllowedOrigins); origins != "" {
		cfg.Server.AllowedOrigins = strings.Split(origins, ",")
	} else {
		cfg.Server.AllowedOrigins = []string{"http://localhost:3000"}
	}

	// -- Standalone auth providers (Google, Microsoft, etc.) from <auth-providers> --
	for _, ap := range raw.AuthProviders.Providers {
		name := expand(ap.Name)
		clientID := expand(ap.ClientID)
		if name == "" || clientID == "" {
			continue
		}
		scopeStr := expand(ap.Scopes)
		var scopes []string
		if scopeStr != "" {
			scopes = strings.Split(scopeStr, ",")
		}
		cfg.Auth.Providers[name] = OAuthProvider{
			ClientID:     clientID,
			ClientSecret: expand(ap.ClientSecret),
			Scopes:       scopes,
		}
	}

	// -- LLM --
	cfg.LLM = LLMConfig{
		Primary:     expand(raw.LLM.Primary),
		Fallback:    expand(raw.LLM.Fallback),
		MaxTokens:   parseInt(raw.LLM.MaxTokens, 4096),
		Temperature: parseFloat(raw.LLM.Temperature, 0.1),
		Timeout:     parseMs(raw.LLM.TimeoutMs, 120*time.Second),
		Providers:   make(map[string]LLMProviderConfig),
	}

	// Collect providers declared in XML — structural config only (URLs, models).
	// API keys are NEVER read from XML. They come from env vars or the dashboard UI.
	addProvider := func(name, baseURL, model, models string) {
		p := LLMProviderConfig{
			BaseURL: baseURL,
			Model:   model,
		}
		if models != "" {
			p.Models = strings.Split(models, ",")
		}
		cfg.LLM.Providers[name] = p
	}

	addProvider("openai",
		expand(raw.LLM.OpenAI.BaseURL),
		expand(raw.LLM.OpenAI.Model),
		"",
	)
	addProvider("anthropic",
		expand(raw.LLM.Anthropic.BaseURL),
		"",
		expand(raw.LLM.Anthropic.Models),
	)
	addProvider("gemini",
		expand(raw.LLM.Gemini.BaseURL),
		expand(raw.LLM.Gemini.Model),
		"",
	)
	addProvider("grok",
		expand(raw.LLM.Grok.BaseURL),
		expand(raw.LLM.Grok.Model),
		"",
	)
	addProvider("ollama",
		expand(raw.LLM.Ollama.BaseURL),
		"",
		"",
	)

	// Azure OpenAI (only if an endpoint is configured)
	if endpoint := expand(raw.LLM.AzureOpenAI.Endpoint); endpoint != "" {
		cfg.LLM.Providers["azure-openai"] = LLMProviderConfig{
			BaseURL: endpoint,
			Model:   expand(raw.LLM.AzureOpenAI.Deployment),
		}
	}

	// Custom (OpenAI-compatible) — only if base URL set
	if baseURL := expand(raw.LLM.Custom.BaseURL); baseURL != "" {
		cfg.LLM.Providers["custom"] = LLMProviderConfig{
			BaseURL: baseURL,
		}
	}

	// Role-based model routing — maps agent roles to preferred providers/models.
	// Example XML:
	//   <routing>
	//     <route role="orchestrator" provider="anthropic" model="claude-sonnet-4-20250514"/>
	//     <route role="senior_dev" provider="anthropic"/>
	//     <route role="junior_dev" provider="openai" model="gpt-4o-mini"/>
	//   </routing>
	cfg.LLM.Routes = make(map[string]LLMRouteConfig)
	for _, route := range raw.LLM.Routing.Routes {
		role := expand(route.Role)
		provider := expand(route.Provider)
		if role != "" && provider != "" {
			cfg.LLM.Routes[role] = LLMRouteConfig{
				Provider: provider,
				Model:    expand(route.Model),
			}
		}
	}

	// Multi-LLM Priority Matrix — maps each agent role to an ordered list of
	// providers. Agents probe all providers in the list, then GPT finalizes.
	// If no priority-matrix is configured, we auto-generate one from the legacy
	// single-route table so the system degrades gracefully.
	cfg.LLM.PriorityMatrix = make(map[string][]LLMPriorityEntry)
	for _, rp := range raw.LLM.Routing.PriorityMatrix {
		role := expand(rp.Role)
		if role == "" {
			continue
		}
		var entries []LLMPriorityEntry
		for _, e := range rp.Entries {
			name := expand(e.Name)
			if name == "" {
				continue
			}
			entry := LLMPriorityEntry{
				Provider: name,
				Model:    expand(e.Model),
			}
			if at := expand(e.ActionTypes); at != "" {
				entry.ActionTypes = splitCSV(at)
			}
			entries = append(entries, entry)
		}
		if len(entries) > 0 {
			cfg.LLM.PriorityMatrix[role] = enforceGPTLast(entries)
		}
	}

	// If no explicit priority matrix but legacy routes exist, build a minimal
	// matrix: each role gets its legacy provider first, then openai last.
	if len(cfg.LLM.PriorityMatrix) == 0 && len(cfg.LLM.Routes) > 0 {
		for role, route := range cfg.LLM.Routes {
			entries := []LLMPriorityEntry{
				{Provider: route.Provider, Model: route.Model},
			}
			// Add all other configured providers that aren't already the route provider
			for provName := range cfg.LLM.Providers {
				if provName != route.Provider {
					entries = append(entries, LLMPriorityEntry{Provider: provName})
				}
			}
			cfg.LLM.PriorityMatrix[role] = enforceGPTLast(entries)
		}
	}

	// -- Review --
	cfg.Review = ReviewConfig{
		MaxDiffSize:      parseInt(raw.Review.MaxDiffSize, 500000),
		MaxFilesPerPR:    parseInt(raw.Review.MaxFilesPerPR, 100),
		MaxComments:      parseInt(raw.Review.MaxComments, 50),
		EnableHeuristics: parseBool(raw.Review.EnableHeuristics, true),
	}

	// -- Storage --
	storageType := expand(raw.Storage.Type)
	if storageType == "" {
		storageType = "local"
	}
	basePath := expand(raw.Storage.Local.BasePath)
	if basePath == "" {
		basePath = "./data"
	}
	cfg.Storage = StorageConfig{
		Type:     storageType,
		BasePath: basePath,
	}

	// -- Logging --
	appLevel := strings.ToLower(expand(raw.Logging.AppLevel))
	if appLevel == "" {
		appLevel = "info"
	}
	cfg.Log = LogConfig{
		Level:  appLevel,
		Format: "text",
	}

	// -- MCP Integrations --
	cfg.MCP = MCPConfig{
		Enabled:         parseBool(raw.MCP.Enabled, true),
		MaxCallsPerTask: parseInt(raw.MCP.MaxCallsPerTask, 50),
		CallTimeout:     parseMs(raw.MCP.CallTimeoutMs, 30*time.Second),
		SlackBaseURL:    parseString(expand(raw.MCP.SlackBaseURL), "https://slack.com/api"),
		MS365GraphURL:   parseString(expand(raw.MCP.MS365GraphURL), "https://graph.microsoft.com/v1.0"),
		MS365TokenURL:   parseString(expand(raw.MCP.MS365TokenURL), "https://login.microsoftonline.com/common/oauth2/v2.0/token"),
		GmailBaseURL:    parseString(expand(raw.MCP.GmailBaseURL), "https://gmail.googleapis.com/gmail/v1"),
		GmailTokenURL:   parseString(expand(raw.MCP.GmailTokenURL), "https://oauth2.googleapis.com/token"),
		DiscordBaseURL:  parseString(expand(raw.MCP.DiscordBaseURL), "https://discord.com/api/v10"),
		GitLabBaseURL:   parseString(expand(raw.MCP.GitLabBaseURL), "https://gitlab.com/api/v4"),
		OAuthProviders:  make(map[string]MCPOAuthProviderConfig),
	}
	if providers := expand(raw.MCP.AllowedProviders); providers != "" {
		for _, p := range strings.Split(providers, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				cfg.MCP.AllowedProviders = append(cfg.MCP.AllowedProviders, p)
			}
		}
	}
	// Parse per-provider MCP OAuth credentials.
	type mcpOAuthRaw struct {
		name         string
		clientID     string
		clientSecret string
		scopes       string
		authURL      string
		tokenURL     string
	}
	mcpOAuthProviders := []mcpOAuthRaw{
		// ── Google Workspace (single client-id/secret, per-product scopes) ──
		{
			name:         "gmail",
			clientID:     expand(raw.MCP.GoogleClientID),
			clientSecret: expand(raw.MCP.GoogleClientSecret),
			scopes:       expand(raw.MCP.GmailOAuthScopes),
			authURL:      "https://accounts.google.com/o/oauth2/v2/auth",
			tokenURL:     "https://oauth2.googleapis.com/token",
		},
		{
			name:         "google_calendar",
			clientID:     expand(raw.MCP.GoogleClientID),
			clientSecret: expand(raw.MCP.GoogleClientSecret),
			scopes:       expand(raw.MCP.GoogleCalendarOAuthScopes),
			authURL:      "https://accounts.google.com/o/oauth2/v2/auth",
			tokenURL:     "https://oauth2.googleapis.com/token",
		},
		{
			name:         "google_drive",
			clientID:     expand(raw.MCP.GoogleClientID),
			clientSecret: expand(raw.MCP.GoogleClientSecret),
			scopes:       expand(raw.MCP.GoogleDriveOAuthScopes),
			authURL:      "https://accounts.google.com/o/oauth2/v2/auth",
			tokenURL:     "https://oauth2.googleapis.com/token",
		},
		// ── Microsoft 365 ──
		{
			name:         "ms365",
			clientID:     expand(raw.MCP.MS365ClientID),
			clientSecret: expand(raw.MCP.MS365ClientSecret),
			scopes:       expand(raw.MCP.MS365OAuthScopes),
			authURL:      "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
			tokenURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		},
		// ── Slack ──
		{
			name:         "slack",
			clientID:     expand(raw.MCP.SlackClientID),
			clientSecret: expand(raw.MCP.SlackClientSecret),
			scopes:       expand(raw.MCP.SlackOAuthScopes),
			authURL:      "https://slack.com/oauth/v2/authorize",
			tokenURL:     "https://slack.com/api/oauth.v2.access",
		},
		// ── Discord ──
		{
			name:         "discord",
			clientID:     expand(raw.MCP.DiscordClientID),
			clientSecret: expand(raw.MCP.DiscordClientSecret),
			scopes:       expand(raw.MCP.DiscordOAuthScopes),
			authURL:      "https://discord.com/api/oauth2/authorize",
			tokenURL:     "https://discord.com/api/oauth2/token",
		},
		// ── GitHub ──
		{
			name:         "github",
			clientID:     expand(raw.MCP.GitHubClientID),
			clientSecret: expand(raw.MCP.GitHubClientSecret),
			scopes:       expand(raw.MCP.GitHubOAuthScopes),
			authURL:      "https://github.com/login/oauth/authorize",
			tokenURL:     "https://github.com/login/oauth/access_token",
		},
		// ── Atlassian (single client-id/secret, per-product scopes) ──
		{
			name:         "jira",
			clientID:     expand(raw.MCP.AtlassianClientID),
			clientSecret: expand(raw.MCP.AtlassianClientSecret),
			scopes:       expand(raw.MCP.JiraOAuthScopes),
			authURL:      "https://auth.atlassian.com/authorize",
			tokenURL:     "https://auth.atlassian.com/oauth/token",
		},
		{
			name:         "confluence",
			clientID:     expand(raw.MCP.AtlassianClientID),
			clientSecret: expand(raw.MCP.AtlassianClientSecret),
			scopes:       expand(raw.MCP.ConfluenceOAuthScopes),
			authURL:      "https://auth.atlassian.com/authorize",
			tokenURL:     "https://auth.atlassian.com/oauth/token",
		},
		// ── Notion ──
		{
			name:         "notion",
			clientID:     expand(raw.MCP.NotionClientID),
			clientSecret: expand(raw.MCP.NotionClientSecret),
			scopes:       expand(raw.MCP.NotionOAuthScopes),
			authURL:      "https://api.notion.com/v1/oauth/authorize",
			tokenURL:     "https://api.notion.com/v1/oauth/token",
		},
		// ── GitLab ──
		{
			name:         "gitlab",
			clientID:     expand(raw.MCP.GitLabClientID),
			clientSecret: expand(raw.MCP.GitLabClientSecret),
			scopes:       expand(raw.MCP.GitLabOAuthScopes),
			authURL:      "https://gitlab.com/oauth/authorize",
			tokenURL:     "https://gitlab.com/oauth/token",
		},
		// ── Linear ──
		{
			name:         "linear",
			clientID:     expand(raw.MCP.LinearClientID),
			clientSecret: expand(raw.MCP.LinearClientSecret),
			scopes:       expand(raw.MCP.LinearOAuthScopes),
			authURL:      "https://linear.app/oauth/authorize",
			tokenURL:     "https://api.linear.app/oauth/token",
		},
		// ── Asana ──
		{
			name:         "asana",
			clientID:     expand(raw.MCP.AsanaClientID),
			clientSecret: expand(raw.MCP.AsanaClientSecret),
			scopes:       expand(raw.MCP.AsanaOAuthScopes),
			authURL:      "https://app.asana.com/-/oauth_authorize",
			tokenURL:     "https://app.asana.com/-/oauth_token",
		},
		// ── Trello ──
		{
			name:         "trello",
			clientID:     expand(raw.MCP.TrelloClientID),
			clientSecret: expand(raw.MCP.TrelloClientSecret),
			scopes:       expand(raw.MCP.TrelloOAuthScopes),
			authURL:      "https://trello.com/1/authorize",
			tokenURL:     "https://trello.com/1/OAuthGetAccessToken",
		},
		// ── Figma ──
		{
			name:         "figma",
			clientID:     expand(raw.MCP.FigmaClientID),
			clientSecret: expand(raw.MCP.FigmaClientSecret),
			scopes:       expand(raw.MCP.FigmaOAuthScopes),
			authURL:      "https://www.figma.com/oauth",
			tokenURL:     "https://api.figma.com/v1/oauth/token",
		},
		// ── Zendesk ──
		{
			name:         "zendesk",
			clientID:     expand(raw.MCP.ZendeskClientID),
			clientSecret: expand(raw.MCP.ZendeskClientSecret),
			scopes:       expand(raw.MCP.ZendeskOAuthScopes),
			authURL:      "https://d3v.zendesk.com/oauth/authorizations/new",
			tokenURL:     "https://d3v.zendesk.com/oauth/tokens",
		},
		// ── PagerDuty ──
		{
			name:         "pagerduty",
			clientID:     expand(raw.MCP.PagerDutyClientID),
			clientSecret: expand(raw.MCP.PagerDutyClientSecret),
			scopes:       expand(raw.MCP.PagerDutyOAuthScopes),
			authURL:      "https://app.pagerduty.com/oauth/authorize",
			tokenURL:     "https://app.pagerduty.com/oauth/token",
		},
		// ── Datadog ──
		{
			name:         "datadog",
			clientID:     expand(raw.MCP.DatadogClientID),
			clientSecret: expand(raw.MCP.DatadogClientSecret),
			scopes:       expand(raw.MCP.DatadogOAuthScopes),
			authURL:      "https://app.datadoghq.com/oauth2/v1/authorize",
			tokenURL:     "https://app.datadoghq.com/oauth2/v1/token",
		},
		// ── Stripe ──
		{
			name:         "stripe",
			clientID:     expand(raw.MCP.StripeClientID),
			clientSecret: expand(raw.MCP.StripeClientSecret),
			scopes:       expand(raw.MCP.StripeOAuthScopes),
			authURL:      "https://connect.stripe.com/oauth/authorize",
			tokenURL:     "https://connect.stripe.com/oauth/token",
		},
		// ── HubSpot ──
		{
			name:         "hubspot",
			clientID:     expand(raw.MCP.HubSpotClientID),
			clientSecret: expand(raw.MCP.HubSpotClientSecret),
			scopes:       expand(raw.MCP.HubSpotOAuthScopes),
			authURL:      "https://app.hubspot.com/oauth/authorize",
			tokenURL:     "https://api.hubapi.com/oauth/v1/token",
		},
		// ── Salesforce ──
		{
			name:         "salesforce",
			clientID:     expand(raw.MCP.SalesforceClientID),
			clientSecret: expand(raw.MCP.SalesforceClientSecret),
			scopes:       expand(raw.MCP.SalesforceOAuthScopes),
			authURL:      "https://login.salesforce.com/services/oauth2/authorize",
			tokenURL:     "https://login.salesforce.com/services/oauth2/token",
		},
		// ── Twilio ──
		{
			name:         "twilio",
			clientID:     expand(raw.MCP.TwilioClientID),
			clientSecret: expand(raw.MCP.TwilioClientSecret),
			scopes:       expand(raw.MCP.TwilioOAuthScopes),
			authURL:      "https://www.twilio.com/authorize",
			tokenURL:     "https://api.twilio.com/oauth/token",
		},
	}
	for _, op := range mcpOAuthProviders {
		if op.clientID != "" && op.clientSecret != "" {
			var scopes []string
			if op.scopes != "" {
				for _, s := range strings.Split(op.scopes, ",") {
					s = strings.TrimSpace(s)
					if s != "" {
						scopes = append(scopes, s)
					}
				}
			}
			cfg.MCP.OAuthProviders[op.name] = MCPOAuthProviderConfig{
				ClientID:     op.clientID,
				ClientSecret: op.clientSecret,
				Scopes:       scopes,
				AuthURL:      op.authURL,
				TokenURL:     op.tokenURL,
			}
		}
	}

	// -- Sandbox (ephemeral container build system) --
	cfg.Sandbox = SandboxConfig{
		Enabled:        parseBool(raw.Sandbox.Enabled, false),
		AlwaysValidate: parseBool(raw.Sandbox.AlwaysValidate, false),
		DefaultSandbox: parseBool(raw.Sandbox.DefaultSandboxMode, true),
		MaxTimeoutSec:  parseInt(raw.Sandbox.MaxTimeoutSec, 600),
		MaxMemoryMB:    parseInt(raw.Sandbox.MaxMemoryMB, 2048),
		MaxCPU:         parseInt(raw.Sandbox.MaxCPU, 2),
		MaxRetries:     parseInt(raw.Sandbox.MaxRetries, 2),
	}

	return cfg, nil
}

// parseJDBCURL extracts host, port, dbname from a JDBC URL.
// Input:  jdbc:postgresql://localhost:5432/rtvortex
// Output: localhost, 5432, rtvortex
func parseJDBCURL(url string) (string, int, string) {
	host := "localhost"
	port := 5432
	name := "rtvortex"

	// Strip jdbc: prefix if present
	url = strings.TrimPrefix(url, "jdbc:")

	// Also handle plain postgres:// URIs
	url = strings.TrimPrefix(url, "postgresql://")
	url = strings.TrimPrefix(url, "postgres://")

	// Now we have "host:port/dbname" or "host/dbname" or "host:port"
	// Strip any query params
	if idx := strings.Index(url, "?"); idx >= 0 {
		url = url[:idx]
	}

	// Split by /
	parts := strings.SplitN(url, "/", 2)
	hostPort := parts[0]
	if len(parts) == 2 && parts[1] != "" {
		name = parts[1]
	}

	// Split host:port
	hpParts := strings.SplitN(hostPort, ":", 2)
	if hpParts[0] != "" {
		host = hpParts[0]
	}
	if len(hpParts) == 2 {
		if p, err := strconv.Atoi(hpParts[1]); err == nil {
			port = p
		}
	}

	return host, port, name
}
