package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/bot"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/internal/botmcp"
	"github.com/nczz/kiro-discord-bot/internal/kirosettings"
	"github.com/nczz/kiro-discord-bot/locale"
	"github.com/nczz/kiro-discord-bot/mcpproxy"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "mcp-proxy" {
		cfg, err := mcpproxy.LoadConfigFromEnv()
		if err != nil {
			log.Fatal(err)
		}
		if err := mcpproxy.Run(context.Background(), cfg, os.Stdin, os.Stdout, os.Stderr); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "mcp-bot" {
		if err := botmcp.Run(); err != nil {
			log.Fatal(err)
		}
		return
	}

	cfg := loadConfig()
	locale.Load(cfg.BotLocale)

	// Engine-aware preflight: check each enabled engine's binary/auth/ACP
	// handshake. A pure-omp runtime never references kiro-cli.
	runPreflight(cfg)

	log.Printf("kiro-discord-bot %s starting", Version)

	b, err := bot.NewFromConfig(bot.BotConfig{
		ManagerConfig: channel.ManagerConfig{
			KiroCLIPath:          cfg.KiroCLIPath,
			OMPPath:              cfg.OMPPath,
			AgentEngine:          cfg.AgentEngine,
			AgentEnginesEnabled:  cfg.AgentEnginesEnabled,
			DefaultCWD:           cfg.DefaultCWD,
			AllowedCwdRoots:      cfg.AllowedCwdRoots,
			DataDir:              cfg.DataDir,
			QueueBufferSize:      cfg.QueueBufferSize,
			AskTimeoutSec:        cfg.AskTimeoutSec,
			StreamUpdateSec:      cfg.StreamUpdateSec,
			ThreadAutoArchive:    cfg.ThreadAutoArchive,
			GuildID:              cfg.DiscordGuildID,
			KiroModel:            cfg.KiroModel,
			BotVersion:           Version,
			ThreadAgentMax:       cfg.ThreadAgentMax,
			ThreadAgentIdleSec:   cfg.ThreadAgentIdleSec,
			ChannelAgentIdleSec:  cfg.ChannelAgentIdleSec,
			MaxScannerBuffer:     cfg.MaxScannerBuffer,
			AgentProfile:         cfg.AgentProfile,
			TrustAllTools:        cfg.TrustAllTools,
			TrustTools:           cfg.TrustTools,
			UsageTimezone:        cfg.UsageTimezone,
			UsageRetentionMonths: cfg.UsageRetentionMonths,
		},
		DiscordToken:       cfg.DiscordToken,
		HeartbeatSec:       cfg.HeartbeatSec,
		AttRetainDays:      cfg.AttRetainDays,
		AttachmentMaxBytes: cfg.AttachmentMaxBytes,
		CronTimezone:       cfg.CronTimezone,
		DownloadTimeoutSec: cfg.DownloadTimeoutSec,
		BotPeers:           cfg.BotPeers,
		Audit: audit.Config{
			Enabled:       cfg.AuditEnabled,
			DBPath:        cfg.AuditDBPath,
			RetentionDays: cfg.AuditRetentionDays,
			QueueSize:     cfg.AuditQueueSize,
			RecordContent: cfg.AuditRecordContent,
			RecordTyping:  cfg.AuditRecordTyping,
		},
		STTEnabled:        cfg.STTEnabled,
		STTProvider:       cfg.STTProvider,
		STTAPIKey:         cfg.STTAPIKey,
		STTModel:          cfg.STTModel,
		STTLanguage:       cfg.STTLanguage,
		STTMaxDurationSec: cfg.STTMaxDurationSec,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := b.Start(); err != nil {
		log.Fatal(err)
	}
	log.Println("Bot running. Ctrl+C to stop.")

	sc := make(chan os.Signal, 1)
	signal.Notify(sc, syscall.SIGINT, syscall.SIGTERM)
	<-sc

	b.Stop()
}

func preflightAgentOptions(cfg *Config, dialect acp.Dialect) acp.AgentOptions {
	opts := acp.AgentOptions{
		MaxBuffer: cfg.MaxScannerBuffer,
		Dialect:   dialect,
	}
	if dialect != acp.DialectKiro {
		// Non-kiro engines (omp) take no --agent profile and no KIRO_* runtime env.
		return opts
	}
	opts.Agent = cfg.AgentProfile
	if strings.TrimSpace(cfg.DataDir) == "" {
		return opts
	}
	runtimeHome := filepath.Join(cfg.DataDir, "kiro-agent-runtime")
	if err := os.MkdirAll(runtimeHome, 0755); err != nil {
		log.Printf("[preflight] create runtime KIRO_HOME: %v", err)
	}
	opts.Env = append(opts.Env, "KIRO_HOME="+runtimeHome)
	if mcpConfig, err := kirosettings.EnsureRuntimeSettings(runtimeHome); err != nil {
		log.Printf("[preflight] prepare runtime Kiro settings: %v", err)
	} else {
		opts.Env = append(opts.Env, "KIRO_MCP_CONFIG="+mcpConfig)
	}
	return opts
}

type engineSpec struct {
	name    string
	binary  string
	dialect acp.Dialect
}

// enabledEngineSpecs returns the engines to preflight: the default engine plus
// any in AGENT_ENGINES_ENABLED. Order: kiro then omp.
func enabledEngineSpecs(cfg *Config) []engineSpec {
	names := map[string]bool{}
	if d := strings.ToLower(strings.TrimSpace(cfg.AgentEngine)); d != "" {
		names[d] = true
	} else {
		names["kiro"] = true
	}
	for _, p := range strings.Split(cfg.AgentEnginesEnabled, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			names[p] = true
		}
	}
	var out []engineSpec
	if names["kiro"] {
		out = append(out, engineSpec{"kiro", cfg.KiroCLIPath, acp.DialectKiro})
	}
	if names["omp"] {
		out = append(out, engineSpec{"omp", cfg.OMPPath, acp.DialectOmp})
	}
	return out
}

// runPreflight checks each enabled engine. PREFLIGHT_MODE=strict|fatal fails on
// any enabled engine's failure; warn (default) logs and continues; skip bypasses.
func runPreflight(cfg *Config) {
	mode := strings.ToLower(strings.TrimSpace(cfg.PreflightMode))
	if os.Getenv("SKIP_PREFLIGHT") != "" {
		mode = "skip"
	}
	switch mode {
	case "skip", "off", "false":
		log.Printf("[preflight] skipped")
		return
	case "", "warn", "strict", "fatal":
	default:
		log.Printf("[preflight] unknown PREFLIGHT_MODE=%q, using warn", cfg.PreflightMode)
		mode = "warn"
	}
	fatal := mode == "strict" || mode == "fatal"
	for _, eng := range enabledEngineSpecs(cfg) {
		err := acp.PreflightCheckWithOptions(eng.binary, preflightAgentOptions(cfg, eng.dialect))
		if err == nil {
			continue
		}
		if fatal {
			log.Fatalf("[preflight] FATAL (%s): %v — set PREFLIGHT_MODE=warn or skip only if you accept delayed per-request failures", eng.name, err)
		}
		log.Printf("[preflight] WARNING (%s): %v — agent may be unavailable, errors will surface per-request", eng.name, err)
	}
}
