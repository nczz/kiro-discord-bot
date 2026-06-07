package audit

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

type parentResolver func(channelID string) string

type queuedEvent struct {
	eventType string
	payload   any
	botEvent  *BotEvent
}

type Recorder struct {
	store         *Store
	queue         chan queuedEvent
	done          chan struct{}
	closeOnce     sync.Once
	wg            sync.WaitGroup
	parentChannel parentResolver
	recordTyping  bool
}

func NewRecorder(store *Store, queueSize int, parentChannel parentResolver, recordTyping bool) *Recorder {
	if queueSize <= 0 {
		queueSize = 1000
	}
	r := &Recorder{
		store:         store,
		queue:         make(chan queuedEvent, queueSize),
		done:          make(chan struct{}),
		parentChannel: parentChannel,
		recordTyping:  recordTyping,
	}
	r.wg.Add(1)
	go r.run()
	return r
}

func (r *Recorder) Close() {
	if r == nil {
		return
	}
	r.closeOnce.Do(func() {
		close(r.done)
		r.wg.Wait()
		if r.store != nil {
			_ = r.store.Close()
		}
	})
}

func (r *Recorder) Register(ds *discordgo.Session) {
	if r == nil || ds == nil {
		return
	}
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.MessageCreate) { r.Enqueue("message_create", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.MessageUpdate) { r.Enqueue("message_update", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.MessageDelete) { r.Enqueue("message_delete", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.MessageDeleteBulk) { r.Enqueue("message_delete_bulk", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.MessageReactionAdd) { r.Enqueue("reaction_add", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.MessageReactionRemove) { r.Enqueue("reaction_remove", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.MessageReactionRemoveAll) { r.Enqueue("reaction_remove_all", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.ChannelPinsUpdate) { r.Enqueue("channel_pins_update", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.ChannelCreate) { r.Enqueue("channel_create", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.ChannelUpdate) { r.Enqueue("channel_update", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.ChannelDelete) { r.Enqueue("channel_delete", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.ThreadCreate) { r.Enqueue("thread_create", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.ThreadUpdate) { r.Enqueue("thread_update", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.ThreadDelete) { r.Enqueue("thread_delete", e) })
	ds.AddHandler(func(_ *discordgo.Session, e *discordgo.InteractionCreate) { r.Enqueue("interaction_create", e) })
	if r.recordTyping {
		ds.AddHandler(func(_ *discordgo.Session, e *discordgo.TypingStart) { r.Enqueue("typing_start", e) })
	}
}

func (r *Recorder) Enqueue(eventType string, payload any) {
	if r == nil || r.store == nil || payload == nil {
		return
	}
	select {
	case <-r.done:
		return
	case r.queue <- queuedEvent{eventType: eventType, payload: payload}:
	default:
		log.Printf("[audit] dropped event queue_full type=%s", eventType)
	}
}

func (r *Recorder) RecordBotEvent(evt BotEvent) {
	if r == nil || r.store == nil || evt.Type == "" {
		return
	}
	select {
	case <-r.done:
		return
	case r.queue <- queuedEvent{botEvent: &evt}:
	default:
		log.Printf("[audit] dropped bot event queue_full type=%s", evt.Type)
	}
}

func (r *Recorder) RecentTimeline(ctx context.Context, targetID string, limit int) ([]TimelineEvent, error) {
	if r == nil || r.store == nil {
		return nil, nil
	}
	return r.store.RecentTimeline(ctx, targetID, limit)
}

func (r *Recorder) run() {
	defer r.wg.Done()
	for {
		select {
		case <-r.done:
			r.drain(2 * time.Second)
			return
		case evt := <-r.queue:
			r.record(evt)
		}
	}
}

func (r *Recorder) drain(limit time.Duration) {
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	for {
		select {
		case evt := <-r.queue:
			r.record(evt)
		case <-deadline.C:
			return
		default:
			return
		}
	}
}

func (r *Recorder) record(q queuedEvent) {
	if q.botEvent != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := r.store.RecordBotEvent(ctx, *q.botEvent); err != nil {
			log.Printf("[audit] record bot event failed type=%s err=%v", q.botEvent.Type, err)
		}
		return
	}
	resolver := func(channelID string) string {
		if r.parentChannel == nil || channelID == "" {
			return ""
		}
		return r.parentChannel(channelID)
	}
	evt := EventFromPayload(q.eventType, q.payload, resolver)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.store.Record(ctx, evt, q.payload); err != nil {
		log.Printf("[audit] record failed type=%s err=%v", q.eventType, err)
	}
}
