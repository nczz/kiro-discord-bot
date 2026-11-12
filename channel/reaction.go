package channel

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/bwmarrin/discordgo"
)

const (
	reactionQueued uint16 = 1 << iota
	reactionWorking
	reactionThinking
	reactionSparkles
	reactionRepeat
	reactionTools
	reactionGear
	reactionWrench
	reactionPlan
	reactionWarning
	reactionFailure
	reactionSuccess
)

var trackedJobReactions = [...]struct {
	emoji string
	bit   uint16
}{
	{"⏳", reactionQueued},
	{"🔄", reactionWorking},
	{"💭", reactionThinking},
	{"✨", reactionSparkles},
	{"🔁", reactionRepeat},
	{"🛠️", reactionTools},
	{"⚙️", reactionGear},
	{"🔧", reactionWrench},
	{"📋", reactionPlan},
	{"⚠️", reactionWarning},
	{"❌", reactionFailure},
	{"✅", reactionSuccess},
}

// jobReactionState serializes every REST transition for one Discord source
// message. The bit mask retains reactions whose removal failed so a later
// transition, especially the terminal one, can reconcile them.
type jobReactionState struct {
	ds        *discordgo.Session
	channelID string
	messageID string
	enabled   bool

	mu        sync.Mutex
	present   uint16
	finalized bool
}

func newJobReactionState(ds *discordgo.Session, channelID, messageID string, queuedReactionAdded, enabled bool) *jobReactionState {
	state := &jobReactionState{
		ds:        ds,
		channelID: channelID,
		messageID: messageID,
		enabled:   enabled && ds != nil && channelID != "" && messageID != "",
	}
	if state.enabled && queuedReactionAdded {
		state.present = trackedJobReactionBit("⏳")
	}
	return state
}

func trackedJobReactionBit(emoji string) uint16 {
	switch emoji {
	case "⏳":
		return reactionQueued
	case "🔄":
		return reactionWorking
	case "💭":
		return reactionThinking
	case "✨":
		return reactionSparkles
	case "🔁":
		return reactionRepeat
	case "🛠️":
		return reactionTools
	case "⚙️":
		return reactionGear
	case "🔧":
		return reactionWrench
	case "📋":
		return reactionPlan
	case "⚠️":
		return reactionWarning
	case "❌":
		return reactionFailure
	case "✅":
		return reactionSuccess
	default:
		return 0
	}
}

func (s *jobReactionState) Set(emoji string) {
	if err := s.transition(emoji, false); err != nil {
		s.logError("transition", emoji, err)
	}
}

func (s *jobReactionState) Final(emoji string) {
	if err := s.transition(emoji, true); err != nil {
		s.logError("finalize", emoji, err)
	}
}

func (s *jobReactionState) AddHint(emoji string) {
	if s == nil || !s.enabled || emoji == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.finalized {
		return
	}
	if err := s.ds.MessageReactionAdd(s.channelID, s.messageID, emoji); err != nil {
		if retryErr := s.ds.MessageReactionAdd(s.channelID, s.messageID, emoji); retryErr != nil {
			s.logError("add hint", emoji, errors.Join(err, retryErr))
		}
	}
}

func (s *jobReactionState) transition(emoji string, terminal bool) error {
	if s == nil || !s.enabled || emoji == "" {
		return nil
	}
	next := trackedJobReactionBit(emoji)
	if next == 0 {
		return fmt.Errorf("unsupported job reaction %q", emoji)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finalized {
		return nil
	}
	if terminal {
		s.finalized = true
	}

	err := s.reconcileLocked(emoji, next)
	if !terminal || err == nil {
		return err
	}
	if retryErr := s.reconcileLocked(emoji, next); retryErr != nil {
		return errors.Join(err, retryErr)
	}
	return nil
}

func (s *jobReactionState) reconcileLocked(nextEmoji string, next uint16) error {
	var result error
	for _, reaction := range trackedJobReactions {
		if reaction.bit == next || s.present&reaction.bit == 0 {
			continue
		}
		if err := s.ds.MessageReactionRemove(s.channelID, s.messageID, reaction.emoji, "@me"); err != nil {
			result = errors.Join(result, fmt.Errorf("remove %s: %w", reaction.emoji, err))
			continue
		}
		s.present &^= reaction.bit
	}
	if s.present&next == 0 {
		if err := s.ds.MessageReactionAdd(s.channelID, s.messageID, nextEmoji); err != nil {
			result = errors.Join(result, fmt.Errorf("add %s: %w", nextEmoji, err))
		} else {
			s.present |= next
		}
	}
	return result
}

func (s *jobReactionState) logError(action, emoji string, err error) {
	if s == nil || err == nil {
		return
	}
	log.Printf("[reaction] %s failed channel=%s msg=%s emoji=%s: %v", action, s.channelID, s.messageID, emoji, err)
}

func addQueuedJobReaction(ds *discordgo.Session, job *Job) {
	if ds == nil || job == nil || job.MessageID == "" || job.QueuedReactionAdded || job.RemoteA2A {
		return
	}
	if err := ds.MessageReactionAdd(job.ChannelID, job.MessageID, "⏳"); err != nil {
		log.Printf("[reaction] add queued failed channel=%s msg=%s: %v", job.ChannelID, job.MessageID, err)
		return
	}
	job.QueuedReactionAdded = true
}

func rollbackQueuedJobReaction(ds *discordgo.Session, job *Job) {
	if ds == nil || job == nil || job.RemoteA2A || !job.QueuedReactionAdded {
		return
	}
	err := ds.MessageReactionRemove(job.ChannelID, job.MessageID, "⏳", "@me")
	if err != nil {
		if retryErr := ds.MessageReactionRemove(job.ChannelID, job.MessageID, "⏳", "@me"); retryErr != nil {
			log.Printf("[reaction] rollback queued failed channel=%s msg=%s: %v", job.ChannelID, job.MessageID, errors.Join(err, retryErr))
			return
		}
	}
	job.QueuedReactionAdded = false
}
