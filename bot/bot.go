package bot

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nats-io/nats.go"
	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/audit"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	"github.com/nczz/kiro-discord-bot/internal/paths"
	"github.com/nczz/kiro-discord-bot/internal/skills"
	"github.com/nczz/kiro-discord-bot/stt"
	"github.com/nczz/kiro-discord-bot/webshare"
)

type Bot struct {
	discord                  *discordgo.Session
	manager                  *channel.Manager
	guildID                  string
	dataDir                  string
	hb                       *heartbeat.Heartbeat
	hbCancel                 context.CancelFunc
	safeEgress               *safeEgressTask
	cronStore                *heartbeat.CronStore
	cronTask                 *heartbeat.CronTask
	auditRecorder            *audit.Recorder
	skillsStore              *skills.Store
	a2aNode                  *a2a.Node
	a2aConfig                a2a.Config
	a2aPeerStore             *a2a.SQLitePeerStore
	a2aPeerFallbackSub       *nats.Subscription
	a2aInstanceID            string
	cronTimezone             string
	version                  string
	startedAt                time.Time
	downloadClient           *http.Client
	attachmentMaxBytes       int64
	seen                     *seenMessages
	sttClient                *stt.Client
	sttMaxDuration           int
	peerMu                   sync.RWMutex
	peers                    []BotPeer
	manualPeers              []BotPeer
	peerPermMu               sync.Mutex
	peerPermCache            map[string]peerPermissionCacheEntry
	cronPromptCache          cronPromptStore // parsed cron jobs awaiting button confirmation
	a2aConfirmations         *a2aPolicyConfirmationStore
	setupPromptMu            sync.Mutex
	setupPromptCooldown      *setupPromptCooldown
	webshareConfig           WebShareConfig
	webshareStore            *webshare.Store
	webshareMu               sync.Mutex
	webshareHosts            map[string]*webshareHostLoop
	webshareUploadMu         sync.Mutex
	webshareUploads          map[string]*webshareUploadSession
	webshareWebhookMu        sync.Mutex
	webshareWebhookByChannel map[string]webshareWebhookCredential
	webshareWebhookIDs       map[string]bool
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
	CronTimeoutMin     int
	DownloadTimeoutSec int
	STTEnabled         bool
	STTProvider        string
	STTAPIKey          string
	STTModel           string
	STTLanguage        string
	STTMaxDurationSec  int
	BotPeers           string
	Audit              audit.Config
	A2ANode            *a2a.Node
	WebShare           WebShareConfig
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

	var webshareStore *webshare.Store
	if cfg.WebShare.Enabled {
		webshareStore, err = webshare.OpenStore(context.Background(), cfg.DataDir)
		if err != nil {
			if auditRecorder != nil {
				auditRecorder.Close()
			}
			return nil, fmt.Errorf("open webshare store: %w", err)
		}
	}

	cfg.ManagerConfig.DiscordSession = ds
	cfg.ManagerConfig.Store = store
	manager := channel.NewManager(cfg.ManagerConfig)
	if err := manager.UsageInitError(); err != nil {
		manager.StopAll()
		if auditRecorder != nil {
			auditRecorder.Close()
		}
		if webshareStore != nil {
			_ = webshareStore.Close()
		}
		return nil, fmt.Errorf("open usage database: %w", err)
	}
	manager.RegisterBuiltinMCP("bot-tools", []string{"mcp-bot"}, map[string]string{
		"DATA_DIR":             cfg.DataDir,
		"CRON_TIMEZONE":        cfg.CronTimezone,
		"USAGE_TIMEZONE":       cfg.UsageTimezone,
		"KIRO_BOT_A2A_ENABLED": strconv.FormatBool(cfg.A2A.Enabled()),
	})

	skillsStore, err := skills.Open(cfg.DataDir)
	if err != nil {
		manager.StopAll()
		if auditRecorder != nil {
			auditRecorder.Close()
		}
		if webshareStore != nil {
			_ = webshareStore.Close()
		}
		return nil, fmt.Errorf("open skills store: %w", err)
	}

	manualPeers := parseBotPeers(cfg.BotPeers)
	b := &Bot{discord: ds, manager: manager, guildID: cfg.GuildID, dataDir: cfg.DataDir, cronTimezone: cfg.CronTimezone, version: cfg.BotVersion,
		startedAt:                time.Now(),
		downloadClient:           &http.Client{Timeout: time.Duration(cfg.DownloadTimeoutSec) * time.Second},
		attachmentMaxBytes:       cfg.AttachmentMaxBytes,
		seen:                     newSeenMessages(),
		sttMaxDuration:           cfg.STTMaxDurationSec,
		peers:                    activeBotPeers(manualPeers),
		manualPeers:              manualPeers,
		peerPermCache:            make(map[string]peerPermissionCacheEntry),
		auditRecorder:            auditRecorder,
		skillsStore:              skillsStore,
		a2aNode:                  cfg.A2ANode,
		a2aConfig:                cfg.A2A,
		a2aInstanceID:            fmt.Sprintf("%s-%d", cfg.A2A.AgentID, time.Now().UnixNano()),
		setupPromptCooldown:      newSetupPromptCooldown(nil),
		a2aConfirmations:         newA2APolicyConfirmationStore(nil),
		webshareConfig:           cfg.WebShare,
		webshareStore:            webshareStore,
		webshareUploads:          make(map[string]*webshareUploadSession),
		webshareWebhookByChannel: make(map[string]webshareWebhookCredential),
		webshareWebhookIDs:       make(map[string]bool),
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
	b.safeEgress = safeEgress
	manager.SetSafeEgressDrain(safeEgress.DrainChannel)
	hb.Register(safeEgress)
	cronTask := heartbeat.NewCronTask(cronStore, &cronAdapter{n}, cfg.DataDir, cfg.CronTimezone, cfg.GuildID, cfg.CronTimeoutMin)
	cronTask.RecalcAll()
	hb.Register(cronTask)
	b.cronTask = cronTask
	hb.Register(heartbeat.NewThreadCleanupTask(&threadCleanupAdapter{n}, cfg.ThreadAgentIdleSec, cfg.ThreadAgentMax))
	hb.Register(heartbeat.NewChannelCleanupTask(&channelCleanupAdapter{n}, cfg.ChannelAgentIdleSec))
	if cfg.A2A.Enabled() {
		if store, err := a2a.OpenPeerStore(cfg.DataDir); err != nil {
			log.Printf("[a2a] peer discovery store disabled: %v", err)
		} else {
			b.a2aPeerStore = store
			hb.Register(&a2aDiscoveryTask{bot: b})
		}
	}
	b.hb = hb
	ds.AddHandler(b.handleMessage)
	ds.AddHandler(b.handleInteraction)
	ds.AddHandler(b.handleThreadCreate)
	ds.AddHandler(b.handleThreadUpdate)
	ds.AddHandler(b.handleThreadDelete)
	ds.AddHandler(b.handleMessageUpdate)
	ds.AddHandler(b.handleMessageDelete)
	return b, nil
}

func (b *Bot) Start() error {
	b.discord.AddHandler(func(ds *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Bot running as %s#%s", r.User.Username, r.User.Discriminator)
		b.manager.SetBotID(r.User.ID)
		for _, guildID := range b.peerDiscoveryGuildIDs(r) {
			b.syncGuildChannelMetadata(ds, guildID)
		}
		b.discoverBotPeers(ds, r)
		_ = ds.UpdateGameStatus(0, "ACP agent "+b.version)
		b.registerSlashCommands()
	})
	if err := b.discord.Open(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.hbCancel = cancel
	b.startA2APeerDiscovery(ctx)
	b.startWebShareReconnectLoop(ctx)
	go b.hb.Start(ctx)
	return nil
}

func (b *Bot) Stop() {
	if b.hbCancel != nil {
		b.hbCancel()
	}
	b.stopAllWebShareHosts()
	b.seen.Stop()
	if b.a2aPeerFallbackSub != nil {
		_ = b.a2aPeerFallbackSub.Unsubscribe()
	}
	b.manager.StopAll()
	if b.a2aNode != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := b.a2aNode.Drain(ctx); err != nil {
			log.Printf("[a2a] drain failed: %v", err)
		}
		cancel()
	}
	b.discord.Close()
	if b.auditRecorder != nil {
		b.auditRecorder.Close()
	}
	if b.a2aPeerStore != nil {
		_ = b.a2aPeerStore.Close()
	}
	if b.skillsStore != nil {
		_ = b.skillsStore.Close()
	}
	if b.webshareStore != nil {
		_ = b.webshareStore.Close()
	}
}
