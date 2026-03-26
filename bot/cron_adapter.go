package bot

import (
	"context"
	"time"

	"github.com/nczz/kiro-discord-bot/heartbeat"
)

// cronAdapter bridges Bot to heartbeat.CronDeps.
type cronAdapter struct {
	bot *Bot
}

var _ heartbeat.CronDeps = (*cronAdapter)(nil)

func (a *cronAdapter) StartTempAgent(name, cwd, model string) error {
	_, err := a.bot.manager.StartTempAgent(name, cwd, model)
	return err
}

func (a *cronAdapter) StopTempAgent(name string) {
	a.bot.manager.StopTempAgent(name)
}

func (a *cronAdapter) AskAgent(ctx context.Context, name, prompt string) (string, error) {
	// Wait for agent to be idle first
	if err := a.bot.manager.WaitAgentIdle(name, 30*time.Second); err != nil {
		return "", err
	}
	return a.bot.manager.AskAgent(ctx, name, prompt)
}

func (a *cronAdapter) Notify(channelID, msg string) {
	_, _ = a.bot.discord.ChannelMessageSend(channelID, msg)
}
