package bot

import (
	"context"

	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/heartbeat"
)

type cronAdapter struct {
	botNotifier
}

var _ heartbeat.CronDeps = (*cronAdapter)(nil)

func (a *cronAdapter) StartTempAgent(name, cwd, model string) (*acp.Agent, error) {
	return a.bot.manager.StartTempAgent(name, cwd, model)
}

func (a *cronAdapter) StopTempAgent(agent *acp.Agent) {
	a.bot.manager.StopTempAgent(agent)
}

func (a *cronAdapter) AskAgentStream(ctx context.Context, agent *acp.Agent, prompt string) (string, string, error) {
	return a.bot.manager.AskAgentStream(ctx, agent, prompt)
}
