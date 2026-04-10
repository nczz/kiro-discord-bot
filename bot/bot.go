package bot

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/channel"
	"github.com/nczz/kiro-discord-bot/heartbeat"
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
	DiscordToken       string
	KiroCLIPath        string
	DefaultCWD         string
	DataDir            string
	QueueBufferSize    int
	AskTimeoutSec      int
	StreamUpdateSec    int
	ThreadAutoArchive  int
	GuildID            string
	KiroModel          string
	HeartbeatSec       int
	AttRetainDays      int
	CronTimezone       string
	BotVersion         string
	DownloadTimeoutSec int
	ThreadAgentMax     int
	ThreadAgentIdleSec int
	MaxScannerBuffer   int
	AgentProfile       string
	TrustAllTools      bool
	TrustTools         string
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

	manager := channel.NewManager(channel.ManagerConfig{
		Store:              store,
		KiroCLI:            cfg.KiroCLIPath,
		DefaultCWD:         cfg.DefaultCWD,
		QueueBufSize:       cfg.QueueBufferSize,
		AskTimeoutSec:      cfg.AskTimeoutSec,
		StreamUpdateSec:    cfg.StreamUpdateSec,
		ThreadArchive:      cfg.ThreadAutoArchive,
		DefaultModel:       cfg.KiroModel,
		DataDir:            cfg.DataDir,
		BotVersion:         cfg.BotVersion,
		GuildID:            cfg.GuildID,
		ThreadAgentMax:     cfg.ThreadAgentMax,
		ThreadAgentIdleSec: cfg.ThreadAgentIdleSec,
		MaxScannerBuffer:   cfg.MaxScannerBuffer,
		AgentProfile:       cfg.AgentProfile,
		TrustAllTools:      cfg.TrustAllTools,
		TrustTools:         cfg.TrustTools,
	})

	b := &Bot{discord: ds, manager: manager, guildID: cfg.GuildID, dataDir: cfg.DataDir, cronTimezone: cfg.CronTimezone, version: cfg.BotVersion,
		downloadClient: &http.Client{Timeout: time.Duration(cfg.DownloadTimeoutSec) * time.Second},
	}

	cronStore, err := heartbeat.NewCronStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	b.cronStore = cronStore

	hb := heartbeat.New(cfg.HeartbeatSec)
	hb.Register(heartbeat.NewHealthTask(&healthAdapter{bot: b}))
	hb.Register(heartbeat.NewCleanupTask(cfg.DataDir, cfg.AttRetainDays))
	hb.Register(heartbeat.NewCronTask(cronStore, &cronAdapter{bot: b}, cfg.DataDir, cfg.CronTimezone, cfg.GuildID))
	hb.Register(heartbeat.NewThreadCleanupTask(&threadCleanupAdapter{bot: b}, cfg.ThreadAgentIdleSec, cfg.ThreadAgentMax))
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
	b.manager.StopAll()
	b.discord.Close()
}
