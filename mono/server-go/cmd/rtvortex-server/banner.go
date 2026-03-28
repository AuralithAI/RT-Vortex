package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/AuralithAI/rtvortex-server/internal/config"
	"github.com/AuralithAI/rtvortex-server/internal/rtenv"
)

// ANSI color codes — disabled automatically when not writing to a terminal.
var (
	colorReset  = "\033[0m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorWhite  = "\033[37m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

func init() {
	// Disable colors when stdout is not a terminal (piped / redirected).
	if !isTerminal() {
		colorReset = ""
		colorCyan = ""
		colorGreen = ""
		colorYellow = ""
		colorWhite = ""
		colorBold = ""
		colorDim = ""
	}
}

// isTerminal returns true if stdout is connected to a TTY.
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// printBanner prints the RTVortex Go server startup banner.
// It matches the visual style of the C++ engine banner.
func printBanner(env *rtenv.Env, cfg *config.Config) {
	// The banner uses a simple approach: print each line with exact padding.
	// Unicode box-drawing chars (║ ╔ ╗ ╚ ╝ ═) are 1 column wide in terminals.
	// The block chars (█ ╗ ╔ ╚ ╝) are also 1 column each.
	// We use a fixed inner width of 65 chars between the ║ delimiters.
	bc := colorBold + colorCyan  // border color
	gc := colorGreen             // green for logo
	wc := colorBold + colorWhite // white for title
	dc := colorDim               // dim for subtitle
	r := colorReset

	banner := fmt.Sprintf(`
%s╔═══════════════════════════════════════════════════════════════════╗%s
%s║%s                                                                   %s║%s
%s║%s   %s██████╗ ████████╗%s                  _                            %s║%s
%s║%s   %s██╔══██╗╚══██╔══╝%s__   _____  _ __| |_ _____  __                 %s║%s
%s║%s   %s██████╔╝   ██║%s   \ \ / / _ \| '__| __/ _ \ \/ /                 %s║%s
%s║%s   %s██╔══██╗   ██║%s    \ V / (_) | |  | ||  __/>  <                  %s║%s
%s║%s   %s██║  ██║   ██║%s     \_/ \___/|_|   \__\___/_/\_\                 %s║%s
%s║%s   %s╚═╝  ╚═╝   ╚═╝%s                                                  %s║%s
%s║%s                                                                   %s║%s
%s║%s         %sGo API Server%s  ·  REST  ·  WebSocket  ·  gRPC             %s║%s
%s║%s         %sWebhooks · OAuth · Review Pipeline · LLM%s                  %s║%s
%s║%s                                                                   %s║%s
%s╚═══════════════════════════════════════════════════════════════════╝%s
`,
		bc, r,
		bc, r, bc, r,
		bc, r, gc, r, bc, r,
		bc, r, gc, r, bc, r,
		bc, r, gc, r, bc, r,
		bc, r, gc, r, bc, r,
		bc, r, gc, r, bc, r,
		bc, r, gc, r, bc, r,
		bc, r, bc, r,
		bc, r, wc, r, bc, r,
		bc, r, dc, r, bc, r,
		bc, r, bc, r,
		bc, r,
	)

	fmt.Print(banner)

	// ── Version & Build ─────────────────────────────────────────────
	section("Version Info")
	kv("Version", version)
	kv("Commit", commit)
	kv("Built", buildDate)
	kv("Go", runtime.Version())
	kv("OS/Arch", fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))
	kv("PID", fmt.Sprintf("%d", os.Getpid()))
	kv("Hostname", env.Hostname)
	kv("Started", time.Now().Format("2006-01-02 15:04:05 MST"))

	// ── Environment ─────────────────────────────────────────────────
	section("Environment")
	kv("RTVORTEX_HOME", env.Home)
	kv("Config Dir", env.ConfigDir)
	kv("Data Dir", env.DataDir)
	kv("Temp Dir", env.TempDir)
	kv("Models Dir", env.ModelsDir)

	// ── Server Config ───────────────────────────────────────────────
	section("HTTP Server")
	listenHost := cfg.Server.Host
	if listenHost == "" {
		listenHost = "0.0.0.0"
	}
	kv("Listen", fmt.Sprintf("%s:%d", listenHost, cfg.Server.Port))
	if cfg.Server.TLS.Enabled {
		kv("TLS", fmt.Sprintf("%s✔ enabled%s", colorGreen, colorReset))
		kv("  Cert", cfg.Server.TLS.CertFile)
		kv("  Key", cfg.Server.TLS.KeyFile)
	} else {
		kv("TLS", fmt.Sprintf("%sdisabled%s", colorYellow, colorReset))
	}
	kv("Read Timeout", cfg.Server.ReadTimeout.String())
	kv("Write Timeout", cfg.Server.WriteTimeout.String())
	kv("Shutdown Grace", cfg.Server.ShutdownTimeout.String())

	// ── Engine (C++ gRPC) ───────────────────────────────────────────
	section("Engine (C++ gRPC)")
	kv("Target", fmt.Sprintf("%s:%d", cfg.Engine.Host, cfg.Engine.Port))
	kv("Max Channels", fmt.Sprintf("%d", cfg.Engine.MaxChannels))
	if cfg.Engine.TLS {
		kv("TLS", fmt.Sprintf("%s✔ enabled%s", colorGreen, colorReset))
		if cfg.Engine.CertFile != "" {
			kv("  Client Cert", cfg.Engine.CertFile)
			kv("  Client Key", cfg.Engine.KeyFile)
		}
		if cfg.Engine.CAFile != "" {
			kv("  CA Cert", cfg.Engine.CAFile)
		}
	} else {
		kv("TLS", fmt.Sprintf("%sdisabled (insecure)%s", colorYellow, colorReset))
	}
	kv("Request Timeout", cfg.Engine.RequestTimeout.String())
	kv("Max Retries", fmt.Sprintf("%d", cfg.Engine.MaxRetries))

	// ── Database ────────────────────────────────────────────────────
	section("PostgreSQL")
	kv("Host", fmt.Sprintf("%s:%d", cfg.Database.Host, cfg.Database.Port))
	kv("Database", cfg.Database.Name)
	kv("Max Conns", fmt.Sprintf("%d", cfg.Database.MaxConns))

	// ── Redis ───────────────────────────────────────────────────────
	section("Redis")
	kv("Address", cfg.Redis.Addr)
	kv("DB", fmt.Sprintf("%d", cfg.Redis.DB))
	kv("Pool Size", fmt.Sprintf("%d", cfg.Redis.PoolSize))

	// ── Security ────────────────────────────────────────────────────
	section("Security")
	if cfg.Auth.JWTSecret != "" {
		kv("JWT", fmt.Sprintf("%s✔ configured%s", colorGreen, colorReset))
	} else {
		kv("JWT", fmt.Sprintf("%s⚠ random secret (restart clears sessions)%s", colorYellow, colorReset))
	}
	if cfg.Auth.EncryptionKey != "" {
		kv("Token Encryption", fmt.Sprintf("%s✔ AES-256-GCM%s", colorGreen, colorReset))
	} else {
		kv("Token Encryption", fmt.Sprintf("%sdisabled%s", colorYellow, colorReset))
	}
	if len(cfg.Server.AllowedOrigins) > 0 {
		kv("CORS Origins", strings.Join(cfg.Server.AllowedOrigins, ", "))
	}

	// ── LLM Providers ──────────────────────────────────────────────
	section("LLM Providers")
	if len(cfg.LLM.Providers) == 0 {
		kv("Status", fmt.Sprintf("%snone configured%s", colorYellow, colorReset))
	}
	for name, p := range cfg.LLM.Providers {
		status := fmt.Sprintf("%sno key%s", colorYellow, colorReset)
		// API keys come from env vars, not XML config.
		envVarMap := map[string]string{
			"openai": "LLM_OPENAI_API_KEY", "anthropic": "LLM_ANTHROPIC_API_KEY",
			"gemini": "LLM_GEMINI_API_KEY", "grok": "LLM_GROK_API_KEY",
			"azure-openai": "LLM_AZURE_OPENAI_API_KEY", "custom": "LLM_CUSTOM_API_KEY",
		}
		if name == "ollama" {
			status = fmt.Sprintf("%sready%s", colorGreen, colorReset)
		} else if envVar, ok := envVarMap[name]; ok && os.Getenv(envVar) != "" {
			status = fmt.Sprintf("%sready%s", colorGreen, colorReset)
		}
		kv(name, fmt.Sprintf("model=%s  %s", p.Model, status))
	}
	if cfg.LLM.Primary != "" {
		kv("Primary", cfg.LLM.Primary)
	}
	if cfg.LLM.Fallback != "" {
		kv("Fallback", cfg.LLM.Fallback)
	}

	// ── VCS Platforms ───────────────────────────────────────────────
	section("VCS Platforms")
	kv("Resolution", "dynamic (per-repo from vault/DB)")

	divider()
	fmt.Println()
}

// section prints a section header.
func section(title string) {
	fmt.Printf("\n  %s%s── %s ──────────────────────────────────────────%s\n",
		colorBold, colorCyan, title, colorReset)
}

// kv prints a key-value pair with aligned formatting.
func kv(key, value string) {
	fmt.Printf("  %s%-18s%s %s\n", colorDim, key, colorReset, value)
}

// divider prints a closing divider.
func divider() {
	fmt.Printf("\n  %s%s════════════════════════════════════════════════════════════%s\n",
		colorBold, colorCyan, colorReset)
}
