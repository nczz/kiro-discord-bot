package bot

import (
	"github.com/bwmarrin/discordgo"
	"github.com/jianghongjun/kiro-discord-bot/acp"
	"github.com/jianghongjun/kiro-discord-bot/channel"
)

type Bot struct {
	discord *discordgo.Session
	manager *channel.Manager
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
}

func NewFromConfig(cfg BotConfig) (*Bot, error) {
	ds, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, err
	}
	ds.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages

	acpClient := acp.NewClient(cfg.AcpBridgeURL)

	store, err := channel.NewSessionStore(cfg.DataDir)
	if err != nil {
		return nil, err
	}

	manager := channel.NewManager(
		store, acpClient,
		cfg.KiroCLIPath, cfg.DefaultCWD,
		cfg.QueueBufferSize, cfg.AskTimeoutSec, cfg.StreamUpdateSec,
	)

	b := &Bot{discord: ds, manager: manager}
	ds.AddHandler(b.handleMessage)
	return b, nil
}

func (b *Bot) Start() error {
	return b.discord.Open()
}

func (b *Bot) Stop() {
	b.discord.Close()
}
