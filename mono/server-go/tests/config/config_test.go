package config_test

import (
	"testing"

	"github.com/AuralithAI/rtvortex-server/internal/config"
)

// ── DSN ─────────────────────────────────────────────────────────────────────

func TestDatabaseConfig_DSN(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host:     "localhost",
		Port:     5432,
		Name:     "rtvortex",
		User:     "admin",
		Password: "secret",
		SSLMode:  "disable",
	}

	expected := "postgres://admin:secret@localhost:5432/rtvortex?sslmode=disable"
	if dsn := cfg.DSN(); dsn != expected {
		t.Errorf("expected DSN %q, got %q", expected, dsn)
	}
}

func TestDatabaseConfig_DSN_RequireSSL(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host:     "db.example.com",
		Port:     5433,
		Name:     "prod_db",
		User:     "app",
		Password: "p@ss",
		SSLMode:  "require",
	}

	expected := "postgres://app:p@ss@db.example.com:5433/prod_db?sslmode=require"
	if dsn := cfg.DSN(); dsn != expected {
		t.Errorf("expected DSN %q, got %q", expected, dsn)
	}
}

func TestDatabaseConfig_DSN_SpecialChars(t *testing.T) {
	cfg := config.DatabaseConfig{
		Host:     "127.0.0.1",
		Port:     5432,
		Name:     "mydb",
		User:     "user",
		Password: "p@ss:w0rd",
		SSLMode:  "disable",
	}

	dsn := cfg.DSN()
	if dsn == "" {
		t.Error("expected non-empty DSN")
	}
	// Just verify it includes the components
	if dsn != "postgres://user:p@ss:w0rd@127.0.0.1:5432/mydb?sslmode=disable" {
		t.Errorf("unexpected DSN: %s", dsn)
	}
}

// ── Config struct defaults ──────────────────────────────────────────────────

func TestConfig_ZeroValue(t *testing.T) {
	var cfg config.Config
	if cfg.Server.Port != 0 {
		t.Error("expected zero port for default config")
	}
	if cfg.Database.Host != "" {
		t.Error("expected empty host for default config")
	}
}

func TestServerConfig_Fields(t *testing.T) {
	sc := config.ServerConfig{
		Port:           8080,
		AllowedOrigins: []string{"http://localhost:3000", "https://app.example.com"},
		ContextPath:    "/api",
	}

	if sc.Port != 8080 {
		t.Errorf("expected port 8080, got %d", sc.Port)
	}
	if len(sc.AllowedOrigins) != 2 {
		t.Errorf("expected 2 origins, got %d", len(sc.AllowedOrigins))
	}
	if sc.ContextPath != "/api" {
		t.Errorf("expected /api, got %s", sc.ContextPath)
	}
}

func TestTLSConfig_Disabled(t *testing.T) {
	tlsCfg := config.TLSConfig{Enabled: false}
	if tlsCfg.Enabled {
		t.Error("expected TLS disabled")
	}
}

func TestTLSConfig_Enabled(t *testing.T) {
	tlsCfg := config.TLSConfig{
		Enabled:  true,
		CertFile: "/etc/ssl/cert.pem",
		KeyFile:  "/etc/ssl/key.pem",
	}
	if !tlsCfg.Enabled {
		t.Error("expected TLS enabled")
	}
	if tlsCfg.CertFile != "/etc/ssl/cert.pem" {
		t.Error("wrong cert file")
	}
}

func TestRedisConfig_Fields(t *testing.T) {
	rc := config.RedisConfig{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		PoolSize: 10,
	}

	if rc.Addr != "localhost:6379" {
		t.Error("wrong addr")
	}
	if rc.PoolSize != 10 {
		t.Errorf("expected pool size 10, got %d", rc.PoolSize)
	}
}

func TestEngineConfig_Fields(t *testing.T) {
	ec := config.EngineConfig{
		Host:        "localhost",
		Port:        50051,
		TLS:         false,
		MaxChannels: 4,
		MaxRetries:  3,
	}

	if ec.Host != "localhost" {
		t.Error("wrong host")
	}
	if ec.Port != 50051 {
		t.Errorf("expected port 50051, got %d", ec.Port)
	}
	if ec.MaxChannels != 4 {
		t.Errorf("expected 4 channels, got %d", ec.MaxChannels)
	}
}

func TestLLMConfig_Fields(t *testing.T) {
	lc := config.LLMConfig{
		Primary:     "openai",
		Fallback:    "anthropic",
		MaxTokens:   4096,
		Temperature: 0.2,
	}

	if lc.Primary != "openai" {
		t.Error("wrong primary")
	}
	if lc.Temperature != 0.2 {
		t.Errorf("expected 0.2, got %f", lc.Temperature)
	}
}

func TestReviewConfig_Fields(t *testing.T) {
	rc := config.ReviewConfig{
		MaxDiffSize:      512 * 1024,
		MaxFilesPerPR:    50,
		MaxComments:      100,
		EnableHeuristics: true,
	}

	if rc.MaxFilesPerPR != 50 {
		t.Errorf("expected 50, got %d", rc.MaxFilesPerPR)
	}
	if !rc.EnableHeuristics {
		t.Error("expected heuristics enabled")
	}
}

// ── Priority Matrix Config Tests ────────────────────────────────────────────

func TestLLMPriorityEntry_Fields(t *testing.T) {
	entry := config.LLMPriorityEntry{
		Provider:    "anthropic",
		Model:       "claude-sonnet-4-20250514",
		ActionTypes: []string{"reasoning", "code_gen"},
	}

	if entry.Provider != "anthropic" {
		t.Errorf("expected anthropic, got %s", entry.Provider)
	}
	if entry.Model != "claude-sonnet-4-20250514" {
		t.Errorf("expected claude model, got %s", entry.Model)
	}
	if len(entry.ActionTypes) != 2 {
		t.Errorf("expected 2 action types, got %d", len(entry.ActionTypes))
	}
}

func TestLLMConfig_PriorityMatrix(t *testing.T) {
	lc := config.LLMConfig{
		Primary:  "openai",
		Fallback: "anthropic",
		PriorityMatrix: map[string][]config.LLMPriorityEntry{
			"orchestrator": {
				{Provider: "grok", Model: "grok-3"},
				{Provider: "anthropic", Model: "claude-sonnet-4-20250514"},
				{Provider: "openai", Model: "gpt-4o"},
			},
			"junior_dev": {
				{Provider: "grok", Model: "grok-3-mini"},
				{Provider: "openai", Model: "gpt-4o-mini"},
			},
		},
	}

	if len(lc.PriorityMatrix) != 2 {
		t.Fatalf("expected 2 roles in matrix, got %d", len(lc.PriorityMatrix))
	}

	orch := lc.PriorityMatrix["orchestrator"]
	if len(orch) != 3 {
		t.Fatalf("expected 3 providers for orchestrator, got %d", len(orch))
	}
	if orch[0].Provider != "grok" {
		t.Errorf("expected grok first for orchestrator, got %s", orch[0].Provider)
	}
	// GPT should be last
	if orch[len(orch)-1].Provider != "openai" {
		t.Errorf("expected openai last for orchestrator, got %s", orch[len(orch)-1].Provider)
	}
}

func TestDefaultProviderCapabilities(t *testing.T) {
	caps := config.DefaultProviderCapabilities()

	// Should have at least grok, anthropic, gemini, openai, ollama
	expected := []string{"grok", "anthropic", "gemini", "openai", "ollama"}
	for _, name := range expected {
		cap, ok := caps[name]
		if !ok {
			t.Errorf("missing capabilities for %s", name)
			continue
		}
		if len(cap.Strengths) == 0 {
			t.Errorf("expected non-empty strengths for %s", name)
		}
		if cap.LatencyTier == "" {
			t.Errorf("expected non-empty latency tier for %s", name)
		}
		if cap.MaxContextTokens == 0 {
			t.Errorf("expected non-zero max context tokens for %s", name)
		}
	}
}

func TestLLMProviderCapabilities_GeminiHasLargestContext(t *testing.T) {
	caps := config.DefaultProviderCapabilities()
	gemini := caps["gemini"]
	openai := caps["openai"]
	if gemini.MaxContextTokens <= openai.MaxContextTokens {
		t.Errorf("expected gemini (%d) > openai (%d) context tokens",
			gemini.MaxContextTokens, openai.MaxContextTokens)
	}
}
