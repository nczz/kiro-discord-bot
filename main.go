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

	// Preflight mode preserves the historical default of warning-only startup
	// while allowing production deployments to fail fast when desired.
	preflightMode := strings.ToLower(strings.TrimSpace(cfg.PreflightMode))
	if os.Getenv("SKIP_PREFLIGHT") != "" {
		preflightMode = "skip"
	}
	switch preflightMode {
	case "", "warn":
		if err := acp.PreflightCheckWithOptions(cfg.KiroCLIPath, preflightAgentOptions(cfg)); err != nil {
			log.Printf("[preflight] WARNING: %v — agent may be unavailable, errors will surface per-request", err)
		}
	case "strict", "fatal":
		if err := acp.PreflightCheckWithOptions(cfg.KiroCLIPath, preflightAgentOptions(cfg)); err != nil {
			log.Fatalf("[preflight] FATAL: %v — set PREFLIGHT_MODE=warn or skip only if you accept delayed per-request failures", err)
		}
	case "skip", "off", "false":
		log.Printf("[preflight] skipped")
	default:
		log.Printf("[preflight] unknown PREFLIGHT_MODE=%q, using warn", cfg.PreflightMode)
		if err := acp.PreflightCheckWithOptions(cfg.KiroCLIPath, preflightAgentOptions(cfg)); err != nil {
			log.Printf("[preflight] WARNING: %v — agent may be unavailable, errors will surface per-request", err)
		}
	}

	log.Printf("kiro-discord-bot %s starting", Version)

	b, err := bot.NewFromConfig(bot.BotConfig{
		ManagerConfig: channel.ManagerConfig{
			KiroCLIPath:          cfg.KiroCLIPath,
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

func preflightAgentOptions(cfg *Config) acp.AgentOptions {
	opts := acp.AgentOptions{
		MaxBuffer: cfg.MaxScannerBuffer,
		Agent:     cfg.AgentProfile,
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		return opts
	}
	runtimeHome := filepath.Join(cfg.DataDir, "kiro-runtime")
	if err := os.MkdirAll(runtimeHome, 0755); err != nil {
		log.Printf("[preflight] create runtime KIRO_HOME: %v", err)
	}
	opts.Env = append(opts.Env, "KIRO_HOME="+runtimeHome)
	return opts
}
