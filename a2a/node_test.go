package a2a

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNodeDisabled(t *testing.T) {
	node, err := Connect(context.Background(), NodeConfig{Config: Config{}})
	if err != nil {
		t.Fatalf("Connect disabled: %v", err)
	}
	if node.IsEnabled() {
		t.Fatal("disabled node reported enabled")
	}
	if node.NATSConn() != nil || node.JetStream() != nil {
		t.Fatalf("disabled node opened NATS resources: conn=%v js=%v", node.NATSConn(), node.JetStream())
	}
	if err := EnsureStreams(context.Background(), node); err != nil {
		t.Fatalf("EnsureStreams disabled: %v", err)
	}
	if err := EnsureConsumers(context.Background(), node); err != nil {
		t.Fatalf("EnsureConsumers disabled: %v", err)
	}
}

func TestConnectDrain(t *testing.T) {
	srv := runEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	node, err := Connect(ctx, NodeConfig{Config: Config{NATSURL: srv.ClientURL(), AgentID: "adam-n200"}})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !node.IsEnabled() || node.NATSConn() == nil || !node.NATSConn().IsConnected() || node.JetStream() == nil {
		t.Fatalf("node not connected: enabled=%v conn=%v js=%v", node.IsEnabled(), node.NATSConn(), node.JetStream())
	}
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer drainCancel()
	if err := node.Drain(drainCtx); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if !node.NATSConn().IsClosed() {
		t.Fatal("connection should be closed after drain")
	}
}

func TestEnsureStreams(t *testing.T) {
	node := connectedTestNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := EnsureStreams(ctx, node); err != nil {
		t.Fatalf("EnsureStreams: %v", err)
	}
	want := map[string]struct {
		subject string
		maxSize int32
	}{
		StreamTasks:    {SubjectTasks, TaskMaxMsgSize},
		StreamControls: {SubjectControls, ControlMaxMsgSize},
		StreamEvents:   {SubjectEvents, EventMaxMsgSize},
	}
	for stream, spec := range want {
		got, err := node.JetStream().Stream(ctx, stream)
		if err != nil {
			t.Fatalf("stream %s missing: %v", stream, err)
		}
		info, err := got.Info(ctx)
		if err != nil {
			t.Fatalf("stream %s info: %v", stream, err)
		}
		if len(info.Config.Subjects) != 1 || info.Config.Subjects[0] != spec.subject {
			t.Fatalf("stream %s subjects = %v, want [%s]", stream, info.Config.Subjects, spec.subject)
		}
		if info.Config.MaxMsgSize != spec.maxSize {
			t.Fatalf("stream %s max size = %d, want %d", stream, info.Config.MaxMsgSize, spec.maxSize)
		}
	}
}

func TestNoPoolSubject(t *testing.T) {
	node := connectedTestNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := EnsureStreams(ctx, node); err != nil {
		t.Fatalf("EnsureStreams: %v", err)
	}
	if err := EnsureConsumers(ctx, node); err != nil {
		t.Fatalf("EnsureConsumers: %v", err)
	}
	for _, streamName := range []string{StreamTasks, StreamControls, StreamEvents} {
		stream, err := node.JetStream().Stream(ctx, streamName)
		if err != nil {
			t.Fatalf("stream %s: %v", streamName, err)
		}
		info, err := stream.Info(ctx)
		if err != nil {
			t.Fatalf("stream %s info: %v", streamName, err)
		}
		for _, subject := range info.Config.Subjects {
			if strings.Contains(subject, "a2a.v1."+"pool") {
				t.Fatalf("stream %s has forbidden pool subject %q", streamName, subject)
			}
		}
	}
	for _, cfg := range consumerConfigs("adam-n200") {
		stream, err := node.JetStream().Stream(ctx, cfg.Stream)
		if err != nil {
			t.Fatalf("stream %s: %v", cfg.Stream, err)
		}
		consumer, err := stream.Consumer(ctx, cfg.Config.Durable)
		if err != nil {
			t.Fatalf("consumer %s: %v", cfg.Config.Durable, err)
		}
		info, err := consumer.Info(ctx)
		if err != nil {
			t.Fatalf("consumer %s info: %v", cfg.Config.Durable, err)
		}
		if strings.Contains(info.Config.FilterSubject, "pool") {
			t.Fatalf("consumer %s has forbidden pool filter %q", cfg.Config.Durable, info.Config.FilterSubject)
		}
	}
}

func TestDuplicateNatsMsgID(t *testing.T) {
	node := connectedTestNode(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := EnsureStreams(ctx, node); err != nil {
		t.Fatalf("EnsureStreams: %v", err)
	}
	subject := TaskSubject("eve-local", "adam-n200", "msg_123")
	msgID := TaskNatsMsgID("eve-local", "adam-n200", "msg_123")
	first, err := node.Publish(ctx, subject, []byte(`{"ok":true}`), msgID)
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	second, err := node.Publish(ctx, subject, []byte(`{"ok":true}`), msgID)
	if err != nil {
		t.Fatalf("duplicate publish: %v", err)
	}
	if first.Duplicate {
		t.Fatal("first publish unexpectedly marked duplicate")
	}
	if !second.Duplicate {
		t.Fatal("second publish should be marked duplicate by Nats-Msg-Id")
	}
	if first.Sequence != second.Sequence {
		t.Fatalf("duplicate publish sequence = %d, want original %d", second.Sequence, first.Sequence)
	}
}

func connectedTestNode(t *testing.T) *Node {
	t.Helper()
	srv := runEmbeddedNATS(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	node, err := Connect(ctx, NodeConfig{Config: Config{NATSURL: srv.ClientURL(), AgentID: "adam-n200"}})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { node.Close() })
	return node
}
