package bot

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/heartbeat"
	"github.com/nczz/kiro-discord-bot/stt"
)

type Bot struct {
	discord        *discordgo.Session
	manager        *channel.Manager
	guildID        string
	dataDir        string
	hb             *heartbeat.Heartbeat
	hbCancel       context.CancelFunc
	cronStore      *heartbeat.CronStore
	cronTimezone   string
	version        string
	downloadClient *http.Client
	seen           *seenMessages
	sttClient      *stt.Client
	sttMaxDuration int
	cronPromptCache sync.Map // key → *ParsedCronJob for button callbacks
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
	CronTimezone       string
	DownloadTimeoutSec int
	STTEnabled         bool
	STTProvider        string
	STTAPIKey          string
	STTModel           string
	STTLanguage        string
	STTMaxDurationSec  int
}

func NewFromConfig(cfg BotConfig) (*Bot, error) {
	ds, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, err
	}
	ds.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsGuildMessageReactions | discordgo.IntentsMessageContent | discordgo.IntentsGuilds

	store, err := channel.NewSessionStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	cfg.ManagerConfig.Store = store
	manager := channel.NewManager(cfg.ManagerConfig)

	b := &Bot{discord: ds, manager: manager, guildID: cfg.GuildID, dataDir: cfg.DataDir, cronTimezone: cfg.CronTimezone, version: cfg.BotVersion,
		downloadClient: &http.Client{Timeout: time.Duration(cfg.DownloadTimeoutSec) * time.Second},
		seen:           newSeenMessages(),
		sttMaxDuration: cfg.STTMaxDurationSec,
	}
	if cfg.STTEnabled && cfg.STTAPIKey != "" {
		b.sttClient = stt.New(cfg.STTProvider, cfg.STTAPIKey, cfg.STTModel, cfg.STTLanguage)
		log.Printf("[stt] enabled provider=%s model=%s", cfg.STTProvider, b.sttClient.Model())
	}

	cronStore, err := heartbeat.NewCronStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	b.cronStore = cronStore

	hb := heartbeat.New(cfg.HeartbeatSec)
	n := botNotifier{bot: b}
	hb.Register(heartbeat.NewHealthTask(&healthAdapter{n}))
	hb.Register(heartbeat.NewCleanupTask(cfg.DataDir, cfg.AttRetainDays))
	hb.Register(heartbeat.NewCronTask(cronStore, &cronAdapter{n}, cfg.DataDir, cfg.CronTimezone, cfg.GuildID))
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
}
