package a2a

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

const (
	StreamTasks    = "A2A_TASKS"
	StreamControls = "A2A_CONTROLS"
	StreamEvents   = "A2A_EVENTS"

	SubjectTasks    = "a2a.v1.task.>"
	SubjectControls = "a2a.v1.control.>"
	SubjectEvents   = "a2a.v1.event.>"

	TaskMaxMsgSize    int32 = 1048576
	ControlMaxMsgSize int32 = 262144
	EventMaxMsgSize   int32 = 1048576
)

const (
	ConsumerTasksPrefix    = "a2a_tasks_"
	ConsumerControlsPrefix = "a2a_controls_"
	ConsumerEventsPrefix   = "a2a_events_"
)

func EnsureStreams(ctx context.Context, node *Node) error {
	if node == nil || !node.IsEnabled() {
		return nil
	}
	js := node.JetStream()
	if js == nil {
		return fmt.Errorf("A2A JetStream is not initialized")
	}
	for _, cfg := range streamConfigs() {
		if containsPoolSubject(cfg.Subjects) {
			return fmt.Errorf("pool subjects are not allowed in A2A v1 topology")
		}
		if _, err := js.CreateOrUpdateStream(ctx, cfg); err != nil {
			return fmt.Errorf("ensure stream %s: %w", cfg.Name, err)
		}
	}
	return nil
}

func EnsureConsumers(ctx context.Context, node *Node) error {
	if node == nil || !node.IsEnabled() {
		return nil
	}
	agentID := node.AgentID()
	if err := ValidateAgentID(agentID); err != nil {
		return fmt.Errorf("agent id: %w", err)
	}
	js := node.JetStream()
	if js == nil {
		return fmt.Errorf("A2A JetStream is not initialized")
	}
	for _, cfg := range consumerConfigs(agentID) {
		if strings.Contains(cfg.Config.FilterSubject, "pool") {
			return fmt.Errorf("pool subjects are not allowed in A2A v1 topology")
		}
		stream, err := js.Stream(ctx, cfg.Stream)
		if err != nil {
			return fmt.Errorf("open stream %s for consumer %s: %w", cfg.Stream, cfg.Config.Durable, err)
		}
		if _, err := stream.CreateOrUpdateConsumer(ctx, cfg.Config); err != nil {
			return fmt.Errorf("ensure consumer %s: %w", cfg.Config.Durable, err)
		}
	}
	return nil
}

type streamConsumerConfig struct {
	Stream string
	Config jetstream.ConsumerConfig
}

func streamConfigs() []jetstream.StreamConfig {
	return []jetstream.StreamConfig{
		{
			Name:       StreamTasks,
			Subjects:   []string{SubjectTasks},
			Retention:  jetstream.LimitsPolicy,
			Storage:    jetstream.FileStorage,
			Discard:    jetstream.DiscardNew,
			MaxMsgSize: TaskMaxMsgSize,
			Duplicates: 24 * time.Hour,
		},
		{
			Name:       StreamControls,
			Subjects:   []string{SubjectControls},
			Retention:  jetstream.LimitsPolicy,
			Storage:    jetstream.FileStorage,
			Discard:    jetstream.DiscardNew,
			MaxMsgSize: ControlMaxMsgSize,
			Duplicates: 24 * time.Hour,
		},
		{
			Name:       StreamEvents,
			Subjects:   []string{SubjectEvents},
			Retention:  jetstream.LimitsPolicy,
			Storage:    jetstream.FileStorage,
			Discard:    jetstream.DiscardNew,
			MaxMsgSize: EventMaxMsgSize,
			Duplicates: 72 * time.Hour,
		},
	}
}

func consumerConfigs(agentID AgentID) []streamConsumerConfig {
	agent := string(agentID)
	return []streamConsumerConfig{
		{
			Stream: StreamTasks,
			Config: jetstream.ConsumerConfig{
				Name:          ConsumerTasksPrefix + agent,
				Durable:       ConsumerTasksPrefix + agent,
				FilterSubject: "a2a.v1.task.*." + agent + ".>",
				DeliverPolicy: jetstream.DeliverAllPolicy,
				AckPolicy:     jetstream.AckExplicitPolicy,
				AckWait:       2 * time.Minute,
				MaxDeliver:    5,
				MaxAckPending: 10,
			},
		},
		{
			Stream: StreamControls,
			Config: jetstream.ConsumerConfig{
				Name:          ConsumerControlsPrefix + agent,
				Durable:       ConsumerControlsPrefix + agent,
				FilterSubject: "a2a.v1.control.*." + agent + ".>",
				DeliverPolicy: jetstream.DeliverAllPolicy,
				AckPolicy:     jetstream.AckExplicitPolicy,
				AckWait:       30 * time.Second,
				MaxDeliver:    10,
			},
		},
		{
			Stream: StreamEvents,
			Config: jetstream.ConsumerConfig{
				Name:          ConsumerEventsPrefix + agent,
				Durable:       ConsumerEventsPrefix + agent,
				FilterSubject: "a2a.v1.event.*." + agent + ".>",
				DeliverPolicy: jetstream.DeliverAllPolicy,
				AckPolicy:     jetstream.AckExplicitPolicy,
				AckWait:       30 * time.Second,
				MaxDeliver:    10,
			},
		},
	}
}

func containsPoolSubject(subjects []string) bool {
	for _, subject := range subjects {
		if strings.Contains(subject, "a2a.v1."+"pool") {
			return true
		}
	}
	return false
}
