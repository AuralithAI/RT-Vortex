// Package config loads application configuration from XML files.
//
// It reads two XML configuration files:
//   - rtserverprops.xml  -- server, database, redis, engine, LLM, review, security, storage
//   - vcsplatforms.xml   -- VCS platform OAuth, webhook, and token settings
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
	Webhooks WebhooksConfig
	Review   ReviewConfig
	Storage  StorageConfig
	Log      LogConfig

	// VCS platforms (from vcsplatforms.xml)
	VCS VCSConfig
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
	Primary     string
	Fallback    string
	MaxTokens   int
	Temperature float64
	Timeout     time.Duration
	Providers   map[string]LLMProviderConfig
}

// LLMProviderConfig holds settings for a single LLM provider.
// API keys are NOT stored here — they come from env vars or the dashboard UI
// and are managed by the LLM registry at runtime.
type LLMProviderConfig struct {
	BaseURL string
	Model   string
	Models  []string
}

// WebhooksConfig holds webhook secrets per platform.
type WebhooksConfig struct {
	GitHub      WebhookPlatformConfig
	GitLab      WebhookPlatformConfig
	Bitbucket   WebhookPlatformConfig
	AzureDevOps WebhookPlatformConfig
}

// WebhookPlatformConfig holds per-platform webhook settings.
type WebhookPlatformConfig struct {
	Secret string
	Path   string
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

// VCSConfig holds all VCS platform settings (from vcsplatforms.xml).
type VCSConfig struct {
	GitHub      *VCSPlatformConfig
	GitLab      *VCSPlatformConfig
	Bitbucket   *VCSPlatformConfig
	AzureDevOps *VCSPlatformConfig
}

// VCSPlatformConfig holds the full config for one VCS platform.
type VCSPlatformConfig struct {
	Enabled      bool
	BaseURL      string
	APIURL       string
	Organization string // Azure DevOps only

	// OAuth
	OAuth VCSOAuthConfig

	// GitHub App (optional)
	App VCSAppConfig

	// Webhook
	Webhook VCSWebhookConfig

	// Token / PAT fallback
	Token string

	// Bitbucket credentials
	Credentials VCSCredentials

	// Azure AD
	AzureAD VCSAzureADConfig

	// Azure DevOps events
	Events []VCSEventConfig
}

// VCSOAuthConfig holds OAuth settings for a VCS platform.
type VCSOAuthConfig struct {
	ClientID     string
	ClientSecret string
	CallbackPath string
	Scopes       string
}

// VCSAppConfig holds GitHub App settings.
type VCSAppConfig struct {
	Enabled        bool
	AppID          string
	PrivateKeyPath string
}

// VCSWebhookConfig holds webhook settings for a VCS platform.
type VCSWebhookConfig struct {
	Enabled bool
	Secret  string
	Path    string
}

// VCSCredentials holds Bitbucket App Password credentials.
type VCSCredentials struct {
	Username    string
	AppPassword string
	Token       string
}

// VCSAzureADConfig holds Azure AD/Entra ID settings.
type VCSAzureADConfig struct {
	Enabled      bool
	TenantID     string
	ClientID     string
	ClientSecret string
}

// VCSEventConfig holds an event subscription config.
type VCSEventConfig struct {
	Name    string
	Enabled bool
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

// ---- VCS platforms XML types ----

type xmlPlatforms struct {
	XMLName     xml.Name       `xml:"platforms"`
	GitHub      xmlGitHub      `xml:"github"`
	GitLab      xmlGitLab      `xml:"gitlab"`
	Bitbucket   xmlBitbucket   `xml:"bitbucket"`
	AzureDevOps xmlAzureDevOps `xml:"azure-devops"`
}

type xmlGitHub struct {
	Enabled string        `xml:"enabled,attr"`
	BaseURL string        `xml:"base-url,attr"`
	APIURL  string        `xml:"api-url,attr"`
	OAuth   xmlVCSOAuth   `xml:"oauth"`
	App     xmlGitHubApp  `xml:"app"`
	Webhook xmlVCSWebhook `xml:"webhook"`
	Token   xmlVCSToken   `xml:"token"`
}

type xmlGitHubApp struct {
	Enabled        string `xml:"enabled,attr"`
	AppID          string `xml:"app-id,attr"`
	PrivateKeyPath string `xml:"private-key-path,attr"`
}

type xmlGitLab struct {
	Enabled string        `xml:"enabled,attr"`
	BaseURL string        `xml:"base-url,attr"`
	OAuth   xmlVCSOAuthGL `xml:"oauth"`
	Token   xmlVCSToken   `xml:"token"`
	Webhook xmlVCSWebhook `xml:"webhook"`
}

type xmlVCSOAuth struct {
	ClientID     string `xml:"client-id,attr"`
	ClientSecret string `xml:"client-secret,attr"`
	CallbackPath string `xml:"callback-path,attr"`
	Scopes       string `xml:"scopes,attr"`
}

type xmlVCSOAuthGL struct {
	ApplicationID     string `xml:"application-id,attr"`
	ApplicationSecret string `xml:"application-secret,attr"`
	CallbackPath      string `xml:"callback-path,attr"`
	Scopes            string `xml:"scopes,attr"`
}

type xmlVCSWebhook struct {
	Enabled string `xml:"enabled,attr"`
	Secret  string `xml:"secret,attr"`
	Path    string `xml:"path,attr"`
}

type xmlVCSToken struct {
	Value string `xml:"value,attr"`
}

type xmlBitbucket struct {
	Enabled     string           `xml:"enabled,attr"`
	BaseURL     string           `xml:"base-url,attr"`
	APIURL      string           `xml:"api-url,attr"`
	OAuth       xmlVCSOAuth      `xml:"oauth"`
	Credentials xmlBBCredentials `xml:"credentials"`
	Webhook     xmlVCSWebhook    `xml:"webhook"`
}

type xmlBBCredentials struct {
	Username    string `xml:"username,attr"`
	AppPassword string `xml:"app-password,attr"`
	Token       string `xml:"token,attr"`
}

type xmlAzureDevOps struct {
	Enabled      string         `xml:"enabled,attr"`
	Organization string         `xml:"organization,attr"`
	BaseURL      string         `xml:"base-url,attr"`
	PAT          xmlVCSToken    `xml:"pat"`
	AzureAD      xmlAzureADCfg  `xml:"azure-ad"`
	Webhook      xmlVCSWebhook  `xml:"webhook"`
	Events       xmlAzureEvents `xml:"events"`
}

type xmlAzureADCfg struct {
	Enabled      string `xml:"enabled,attr"`
	TenantID     string `xml:"tenant-id,attr"`
	ClientID     string `xml:"client-id,attr"`
	ClientSecret string `xml:"client-secret,attr"`
}

type xmlAzureEvents struct {
	Events []xmlAzureEvent `xml:"event"`
}

type xmlAzureEvent struct {
	Name    string `xml:"name,attr"`
	Enabled string `xml:"enabled,attr"`
}

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

	// VCSPlatformsPath overrides the search path for vcsplatforms.xml.
	VCSPlatformsPath string
}

// Load reads configuration from rtserverprops.xml and vcsplatforms.xml.
// It resolves ${ENV_VAR:default} placeholders in all attribute values.
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

	vcsFile, err := findConfigFile("vcsplatforms.xml", o.VCSPlatformsPath)
	if err != nil {
		return nil, fmt.Errorf("vcs platforms: %w", err)
	}

	// -- Parse rtserverprops.xml --
	cfg, err := loadServerProps(serverPropsFile)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", serverPropsFile, err)
	}

	// -- Parse vcsplatforms.xml --
	vcs, err := loadVCSPlatforms(vcsFile)
	if err != nil {
		return nil, fmt.Errorf("loading %s: %w", vcsFile, err)
	}
	cfg.VCS = *vcs

	// -- Merge VCS webhook secrets into WebhooksConfig for backward compat --
	if cfg.VCS.GitHub != nil {
		cfg.Webhooks.GitHub = WebhookPlatformConfig{
			Secret: cfg.VCS.GitHub.Webhook.Secret,
			Path:   cfg.VCS.GitHub.Webhook.Path,
		}
	}
	if cfg.VCS.GitLab != nil {
		cfg.Webhooks.GitLab = WebhookPlatformConfig{
			Secret: cfg.VCS.GitLab.Webhook.Secret,
			Path:   cfg.VCS.GitLab.Webhook.Path,
		}
	}
	if cfg.VCS.Bitbucket != nil {
		cfg.Webhooks.Bitbucket = WebhookPlatformConfig{
			Secret: cfg.VCS.Bitbucket.Webhook.Secret,
			Path:   cfg.VCS.Bitbucket.Webhook.Path,
		}
	}
	if cfg.VCS.AzureDevOps != nil {
		cfg.Webhooks.AzureDevOps = WebhookPlatformConfig{
			Secret: cfg.VCS.AzureDevOps.Webhook.Secret,
			Path:   cfg.VCS.AzureDevOps.Webhook.Path,
		}
	}

	// -- Merge VCS OAuth into Auth.Providers for backward compat --
	if cfg.Auth.Providers == nil {
		cfg.Auth.Providers = make(map[string]OAuthProvider)
	}
	if cfg.VCS.GitHub != nil && cfg.VCS.GitHub.OAuth.ClientID != "" {
		cfg.Auth.Providers["github"] = OAuthProvider{
			ClientID:     cfg.VCS.GitHub.OAuth.ClientID,
			ClientSecret: cfg.VCS.GitHub.OAuth.ClientSecret,
			Scopes:       strings.Split(cfg.VCS.GitHub.OAuth.Scopes, ","),
			CallbackPath: cfg.VCS.GitHub.OAuth.CallbackPath,
		}
	}
	if cfg.VCS.GitLab != nil && cfg.VCS.GitLab.OAuth.ClientID != "" {
		cfg.Auth.Providers["gitlab"] = OAuthProvider{
			ClientID:     cfg.VCS.GitLab.OAuth.ClientID,
			ClientSecret: cfg.VCS.GitLab.OAuth.ClientSecret,
			Scopes:       strings.Split(cfg.VCS.GitLab.OAuth.Scopes, ","),
			CallbackPath: cfg.VCS.GitLab.OAuth.CallbackPath,
		}
	}
	if cfg.VCS.Bitbucket != nil && cfg.VCS.Bitbucket.OAuth.ClientID != "" {
		cfg.Auth.Providers["bitbucket"] = OAuthProvider{
			ClientID:     cfg.VCS.Bitbucket.OAuth.ClientID,
			ClientSecret: cfg.VCS.Bitbucket.OAuth.ClientSecret,
			CallbackPath: cfg.VCS.Bitbucket.OAuth.CallbackPath,
		}
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

// ---- vcsplatforms.xml loader ----

func loadVCSPlatforms(path string) (*VCSConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw xmlPlatforms
	if err := xml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("XML parse error: %w", err)
	}

	vcs := &VCSConfig{}

	// -- GitHub --
	vcs.GitHub = &VCSPlatformConfig{
		Enabled: parseBool(raw.GitHub.Enabled, true),
		BaseURL: expand(raw.GitHub.BaseURL),
		APIURL:  expand(raw.GitHub.APIURL),
		OAuth: VCSOAuthConfig{
			ClientID:     expand(raw.GitHub.OAuth.ClientID),
			ClientSecret: expand(raw.GitHub.OAuth.ClientSecret),
			CallbackPath: expand(raw.GitHub.OAuth.CallbackPath),
			Scopes:       expand(raw.GitHub.OAuth.Scopes),
		},
		App: VCSAppConfig{
			Enabled:        parseBool(raw.GitHub.App.Enabled, false),
			AppID:          expand(raw.GitHub.App.AppID),
			PrivateKeyPath: expand(raw.GitHub.App.PrivateKeyPath),
		},
		Webhook: VCSWebhookConfig{
			Enabled: parseBool(raw.GitHub.Webhook.Enabled, true),
			Secret:  expand(raw.GitHub.Webhook.Secret),
			Path:    expand(raw.GitHub.Webhook.Path),
		},
		Token: expand(raw.GitHub.Token.Value),
	}

	// -- GitLab --
	vcs.GitLab = &VCSPlatformConfig{
		Enabled: parseBool(raw.GitLab.Enabled, false),
		BaseURL: expand(raw.GitLab.BaseURL),
		OAuth: VCSOAuthConfig{
			ClientID:     expand(raw.GitLab.OAuth.ApplicationID),
			ClientSecret: expand(raw.GitLab.OAuth.ApplicationSecret),
			CallbackPath: expand(raw.GitLab.OAuth.CallbackPath),
			Scopes:       expand(raw.GitLab.OAuth.Scopes),
		},
		Webhook: VCSWebhookConfig{
			Enabled: parseBool(raw.GitLab.Webhook.Enabled, true),
			Secret:  expand(raw.GitLab.Webhook.Secret),
			Path:    expand(raw.GitLab.Webhook.Path),
		},
		Token: expand(raw.GitLab.Token.Value),
	}

	// -- Bitbucket --
	vcs.Bitbucket = &VCSPlatformConfig{
		Enabled: parseBool(raw.Bitbucket.Enabled, false),
		BaseURL: expand(raw.Bitbucket.BaseURL),
		APIURL:  expand(raw.Bitbucket.APIURL),
		OAuth: VCSOAuthConfig{
			ClientID:     expand(raw.Bitbucket.OAuth.ClientID),
			ClientSecret: expand(raw.Bitbucket.OAuth.ClientSecret),
			CallbackPath: expand(raw.Bitbucket.OAuth.CallbackPath),
		},
		Credentials: VCSCredentials{
			Username:    expand(raw.Bitbucket.Credentials.Username),
			AppPassword: expand(raw.Bitbucket.Credentials.AppPassword),
			Token:       expand(raw.Bitbucket.Credentials.Token),
		},
		Webhook: VCSWebhookConfig{
			Enabled: parseBool(raw.Bitbucket.Webhook.Enabled, true),
			Secret:  expand(raw.Bitbucket.Webhook.Secret),
			Path:    expand(raw.Bitbucket.Webhook.Path),
		},
	}

	// -- Azure DevOps --
	events := make([]VCSEventConfig, 0, len(raw.AzureDevOps.Events.Events))
	for _, e := range raw.AzureDevOps.Events.Events {
		events = append(events, VCSEventConfig{
			Name:    expand(e.Name),
			Enabled: parseBool(e.Enabled, true),
		})
	}
	vcs.AzureDevOps = &VCSPlatformConfig{
		Enabled:      parseBool(raw.AzureDevOps.Enabled, false),
		BaseURL:      expand(raw.AzureDevOps.BaseURL),
		Organization: expand(raw.AzureDevOps.Organization),
		Token:        expand(raw.AzureDevOps.PAT.Value),
		AzureAD: VCSAzureADConfig{
			Enabled:      parseBool(raw.AzureDevOps.AzureAD.Enabled, false),
			TenantID:     expand(raw.AzureDevOps.AzureAD.TenantID),
			ClientID:     expand(raw.AzureDevOps.AzureAD.ClientID),
			ClientSecret: expand(raw.AzureDevOps.AzureAD.ClientSecret),
		},
		Webhook: VCSWebhookConfig{
			Enabled: parseBool(raw.AzureDevOps.Webhook.Enabled, true),
			Secret:  expand(raw.AzureDevOps.Webhook.Secret),
			Path:    expand(raw.AzureDevOps.Webhook.Path),
		},
		Events: events,
	}

	return vcs, nil
}
