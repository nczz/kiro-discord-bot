package bot

import (
	"sync"

	"github.com/bwmarrin/discordgo"
)

type deferredSlashResponder struct {
	mu          sync.Mutex
	ds          *discordgo.Session
	interaction *discordgo.Interaction
	followup    discordgo.MessageFlags
	edited      bool
}

func newDeferredSlashResponder(ds *discordgo.Session, interaction *discordgo.Interaction, followupFlags discordgo.MessageFlags) *deferredSlashResponder {
	return &deferredSlashResponder{
		ds:          ds,
		interaction: interaction,
		followup:    followupFlags,
	}
}

func (r *deferredSlashResponder) Send(content string) (*discordgo.Message, error) {
	r.mu.Lock()
	useOriginal := !r.edited
	if useOriginal {
		r.edited = true
	}
	r.mu.Unlock()

	allowedMentions := &discordgo.MessageAllowedMentions{}
	if useOriginal {
		return r.ds.InteractionResponseEdit(r.interaction, &discordgo.WebhookEdit{
			Content:         &content,
			AllowedMentions: allowedMentions,
		})
	}
	return r.ds.FollowupMessageCreate(r.interaction, true, &discordgo.WebhookParams{
		Content:         content,
		AllowedMentions: allowedMentions,
		Flags:           r.followup,
	})
}
