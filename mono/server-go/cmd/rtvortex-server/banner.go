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
	banner := fmt.Sprintf(`
%s%s╔═════════════════════════════════════════════════════════════════════╗%s
%s%s║%s                                                               %s%s║%s
%s%s║%s   %s██████╗ ████████╗                  _               %s     %s%s║%s
%s%s║%s   %s██╔══██╗╚══██╔══╝__   _____  _ __| |_ _____  __%s         %s%s║%s
%s%s║%s   %s██████╔╝   ██║   \ \ / / _ \| '__| __/ _ \ \/ /%s         %s%s║%s
%s%s║%s   %s██╔══██╗   ██║    \ V / (_) | |  | ||  __/>  <%s          %s%s║%s
%s%s║%s   %s██║  ██║   ██║     \_/ \___/|_|   \__\___/_/\_\%s         %s%s║%s
%s%s║%s   %s╚═╝  ╚═╝   ╚═╝%s                                          %s%s║%s
%s%s║%s                                                               %s%s║%s
%s%s║%s         %s%sGo API Server%s  ·  REST  ·  WebSocket  ·  gRPC   %s%s║%s
%s%s║%s         %sWebhooks · OAuth · Review Pipeline · LLM%s          %s%s║%s
%s%s║%s                                                               %s%s║%s
%s%s╚═════════════════════════════════════════════════════════════════════╝%s
`,
		colorBold, colorCyan, colorReset,
		colorBold, colorCyan, colorReset, colorBold, colorCyan, colorReset,
		colorBold, colorCyan, colorReset, colorGreen, colorReset, colorBold, colorCyan, colorReset,
		colorBold, colorCyan, colorReset, colorGreen, colorReset, colorBold, colorCyan, colorReset,
		colorBold, colorCyan, colorReset, colorGreen, colorReset, colorBold, colorCyan, colorReset,
		colorBold, colorCyan, colorReset, colorGreen, colorReset, colorBold, colorCyan, colorReset,
		colorBold, colorCyan, colorReset, colorGreen, colorReset, colorBold, colorCyan, colorReset,
		colorBold, colorCyan, colorReset, colorGreen, colorReset, colorBold, colorCyan, colorReset,
		colorBold, colorCyan, colorReset, colorBold, colorCyan, colorReset,
		colorBold, colorCyan, colorReset, colorBold, colorWhite, colorReset, colorBold, colorCyan, colorReset,
		colorBold, colorCyan, colorReset, colorDim, colorReset, colorBold, colorCyan, colorReset,
		colorBold, colorCyan, colorReset, colorBold, colorCyan, colorReset,
		colorBold, colorCyan, colorReset,
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
	kv("Listen Port", fmt.Sprintf(":%d", cfg.Server.Port))
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
		kv(name, fmt.Sprintf("model=%s", p.Model))
	}
	if cfg.LLM.Primary != "" {
		kv("Primary", cfg.LLM.Primary)
	}
	if cfg.LLM.Fallback != "" {
		kv("Fallback", cfg.LLM.Fallback)
	}

	// ── VCS Platforms ───────────────────────────────────────────────
	section("VCS Platforms")
	printVCSStatus("GitHub", cfg.VCS.GitHub)
	printVCSStatus("GitLab", cfg.VCS.GitLab)
	printVCSStatus("Bitbucket", cfg.VCS.Bitbucket)
	printVCSStatus("Azure DevOps", cfg.VCS.AzureDevOps)

	divider()
	fmt.Println()
}

// printVCSStatus prints a single VCS platform line.
func printVCSStatus(name string, p *config.VCSPlatformConfig) {
	if p == nil || !p.Enabled {
		return
	}
	kv(name, fmt.Sprintf("%s✔ enabled%s", colorGreen, colorReset))
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
