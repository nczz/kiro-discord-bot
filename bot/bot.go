package bot

import (
	"log"

	"github.com/bwmarrin/discordgo"
	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/channel"
)

type Bot struct {
	discord *discordgo.Session
	manager *channel.Manager
	guildID string
	dataDir string
}

func New(cfg interface{ GetBotConfig() BotConfig }) (*Bot, error) {
	return NewFromConfig(cfg.GetBotConfig())
}

type BotConfig struct {
	DiscordToken    string
	AcpBridgeURL    string
	KiroCLIPath     string
	DefaultCWD      string
	DataDir         string
	QueueBufferSize int
	AskTimeoutSec   int
	StreamUpdateSec int
	GuildID         string
	KiroModel       string
}

func NewFromConfig(cfg BotConfig) (*Bot, error) {
	ds, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, err
	}
	ds.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsGuildMessageReactions | discordgo.IntentsMessageContent

	acpClient := acp.NewClient(cfg.AcpBridgeURL)

	store, err := channel.NewSessionStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	manager := channel.NewManager(
		store, acpClient,
		cfg.KiroCLIPath, cfg.DefaultCWD,
		cfg.QueueBufferSize, cfg.AskTimeoutSec, cfg.StreamUpdateSec,
		cfg.KiroModel,
	)

	b := &Bot{discord: ds, manager: manager, guildID: cfg.GuildID, dataDir: cfg.DataDir}
	ds.AddHandler(b.handleMessage)
	ds.AddHandler(b.handleInteraction)
	ds.AddHandler(func(s *discordgo.Session, e *discordgo.InteractionCreate) {
		log.Printf("[debug] InteractionCreate type=%d", e.Type)
	})
	ds.AddHandler(func(s *discordgo.Session, e *discordgo.MessageCreate) {
		log.Printf("[debug] MessageCreate from=%s content=%q", e.Author.Username, e.Content)
	})
	return b, nil
}

func (b *Bot) Start() error {
	b.discord.AddHandler(func(ds *discordgo.Session, r *discordgo.Ready) {
		log.Printf("Bot running as %s#%s", r.User.Username, r.User.Discriminator)
		_ = ds.UpdateGameStatus(0, "kiro-cli agent")
		b.registerSlashCommands()
	})
	return b.discord.Open()
}

func (b *Bot) Stop() {
	b.discord.Close()
}
