package bot

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	"github.com/nczz/kiro-discord-bot/internal/paths"
	"github.com/nczz/kiro-discord-bot/stt"
)

type Bot struct {
	discord             *discordgo.Session
	manager             *channel.Manager
	guildID             string
	dataDir             string
	hb                  *heartbeat.Heartbeat
	hbCancel            context.CancelFunc
	cronStore           *heartbeat.CronStore
	cronTask            *heartbeat.CronTask
	auditRecorder       *audit.Recorder
	cronTimezone        string
	version             string
	startedAt           time.Time
	downloadClient      *http.Client
	attachmentMaxBytes  int64
	seen                *seenMessages
	sttClient           *stt.Client
	sttMaxDuration      int
	peerMu              sync.RWMutex
	peers               []BotPeer
	manualPeers         []BotPeer
	peerPermMu          sync.Mutex
	peerPermCache       map[string]peerPermissionCacheEntry
	cronPromptCache     cronPromptStore // parsed cron jobs awaiting button confirmation
	setupPromptMu       sync.Mutex
	setupPromptCooldown *setupPromptCooldown
}

func New(cfg interface{ GetBotConfig() BotConfig }) (*Bot, error) {
	return NewFromConfig(cfg.GetBotConfig())
}

// isMyGuild returns true if the given guildID belongs to this bot instance.
// Returns true if bot has no guild restriction (guildID is empty).
func (b *Bot) isMyGuild(guildID string) bool {
	return b.guildID == "" || guildID == b.guildID
}

type BotConfig struct {
	channel.ManagerConfig

	DiscordToken       string
	HeartbeatSec       int
	AttRetainDays      int
	AttachmentMaxBytes int64
	CronTimezone       string
	DownloadTimeoutSec int
	STTEnabled         bool
	STTProvider        string
	STTAPIKey          string
	STTModel           string
	STTLanguage        string
	STTMaxDurationSec  int
	BotPeers           string
	Audit              audit.Config
}

func NewFromConfig(cfg BotConfig) (*Bot, error) {
	dataDir, err := paths.DataDir(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve data dir: %w", err)
	}
	cfg.DataDir = dataDir
	cfg.ManagerConfig.DataDir = dataDir

	ds, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, err
	}
	ds.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsGuildMessageReactions | discordgo.IntentsMessageContent | discordgo.IntentsGuilds

	store, err := channel.NewSessionStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	var auditRecorder *audit.Recorder
	if cfg.Audit.Enabled {
		cfg.Audit.DataDir = cfg.DataDir
		auditStore, err := audit.Open(cfg.Audit)
		if err != nil {
			return nil, fmt.Errorf("open audit recorder: %w", err)
		}
		auditRecorder = audit.NewRecorder(auditStore, cfg.Audit.QueueSize, func(channelID string) string {
			return resolveThreadParent(ds, channelID)
		}, cfg.Audit.RecordTyping)
		auditRecorder.Register(ds)
		cfg.ManagerConfig.Audit = auditRecorder
		log.Printf("[audit] sqlite recorder enabled")
	}

	cfg.ManagerConfig.Store = store
	manager := channel.NewManager(cfg.ManagerConfig)
	manager.RegisterBuiltinMCP("bot-tools", []string{"mcp-bot"}, map[string]string{"DATA_DIR": cfg.DataDir})

	manualPeers := parseBotPeers(cfg.BotPeers)
	b := &Bot{discord: ds, manager: manager, guildID: cfg.GuildID, dataDir: cfg.DataDir, cronTimezone: cfg.CronTimezone, version: cfg.BotVersion,
		startedAt:           time.Now(),
		downloadClient:      &http.Client{Timeout: time.Duration(cfg.DownloadTimeoutSec) * time.Second},
		attachmentMaxBytes:  cfg.AttachmentMaxBytes,
		seen:                newSeenMessages(),
		sttMaxDuration:      cfg.STTMaxDurationSec,
		peers:               activeBotPeers(manualPeers),
		manualPeers:         manualPeers,
		peerPermCache:       make(map[string]peerPermissionCacheEntry),
		auditRecorder:       auditRecorder,
		setupPromptCooldown: newSetupPromptCooldown(nil),
	}
	if cfg.STTEnabled && cfg.STTAPIKey != "" {
		b.sttClient = stt.New(cfg.STTProvider, cfg.STTAPIKey, cfg.STTModel, cfg.STTLanguage)
		log.Printf("[stt] enabled provider=%s model=%s", cfg.STTProvider, b.sttClient.Model())
	}

	cronStore, err := heartbeat.NewCronStore(cfg.DataDir)
	if err != nil {
		if b.auditRecorder != nil {
			b.auditRecorder.Close()
		}
		return nil, err
	}
	b.cronStore = cronStore

	hb := heartbeat.New(cfg.HeartbeatSec)
	n := botNotifier{bot: b}
	hb.Register(heartbeat.NewHealthTask(&healthAdapter{n}))
	hb.Register(heartbeat.NewCleanupTask(cfg.DataDir, cfg.AttRetainDays))
	safeEgress := newSafeEgressTask(b)
	manager.SetSafeEgressDrain(safeEgress.DrainChannel)
	hb.Register(safeEgress)
	cronTask := heartbeat.NewCronTask(cronStore, &cronAdapter{n}, cfg.DataDir, cfg.CronTimezone, cfg.GuildID)
	cronTask.RecalcAll()
	hb.Register(cronTask)
	b.cronTask = cronTask
	hb.Register(heartbeat.NewThreadCleanupTask(&threadCleanupAdapter{n}, cfg.ThreadAgentIdleSec, cfg.ThreadAgentMax))
	hb.Register(heartbeat.NewChannelCleanupTask(&channelCleanupAdapter{n}, cfg.ChannelAgentIdleSec))
	b.hb = hb
	ds.AddHandler(b.handleMessage)
	ds.AddHandler(b.handleInteraction)
	ds.AddHandler(b.handleThreadUpdate)
	return b, nil
}

func (b *Bot) Start() error {
	b.discord.AddHandler(func(ds *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Bot running as %s#%s", r.User.Username, r.User.Discriminator)
		b.manager.SetBotID(r.User.ID)
		b.discoverBotPeers(ds, r)
		_ = ds.UpdateGameStatus(0, "kiro-cli agent "+b.version)
		b.registerSlashCommands()
	})
	if err := b.discord.Open(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.hbCancel = cancel
	go b.hb.Start(ctx)
	return nil
}

func (b *Bot) Stop() {
	if b.hbCancel != nil {
		b.hbCancel()
	}
	b.seen.Stop()
	b.manager.StopAll()
	b.discord.Close()
	if b.auditRecorder != nil {
		b.auditRecorder.Close()
	}
}
