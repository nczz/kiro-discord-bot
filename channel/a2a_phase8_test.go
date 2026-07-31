package channel

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nczz/kiro-discord-bot/a2a"
	"github.com/nczz/kiro-discord-bot/internal/botegress"
	"github.com/nczz/kiro-discord-bot/internal/channelmeta"
)

func TestA2ATransparentResultQueuesSafeEgressAndAudit(t *testing.T) {
	dir := t.TempDir()
	auditSink := &recordingAuditSink{}
	m := &Manager{dataDir: dir, audit: auditSink, safeEgressDrain: func(target string) int { return 0 }}
	payload := a2a.TaskEventPayload{Revision: 3, Result: &a2a.TaskExecutionResult{State: a2a.TaskStateCompleted, Content: "done"}}
	row := phase8TaskRow()
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindResult, payload); err != nil {
		t.Fatalf("deliverA2AEvent: %v", err)
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 1 || actions[0].Action != botegress.ActionSendMessage || actions[0].ChannelID != "thread-1" || !strings.Contains(actions[0].Content, "done") {
		t.Fatalf("unexpected pending actions: %+v", actions)
	}
	auditSink.mu.Lock()
	defer auditSink.mu.Unlock()
	if len(auditSink.events) != 2 || auditSink.events[0].Type != a2a.AuditTranscriptPosted || auditSink.events[1].Type != a2a.AuditResultDelivered || auditSink.events[0].Metadata["task_id"] != "task_phase8" {
		t.Fatalf("unexpected audit events: %+v", auditSink.events)
	}
}

func TestA2AProxyDelegatorResultDoesNotDuplicateExecutorTranscript(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{dataDir: dir}
	row := phase8TaskRow()
	row.ResultVisibility = "proxy"
	row.DiscordTranscriptMode = "delegator"
	payload := a2a.TaskEventPayload{Revision: 3, Result: &a2a.TaskExecutionResult{State: a2a.TaskStateCompleted, Content: "proxy done"}}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindResult, payload); err != nil {
		t.Fatalf("deliverA2AEvent: %v", err)
	}
	actions, _ := botegress.ReadPending(dir)
	if len(actions) != 0 {
		t.Fatalf("proxy delegator result duplicated executor transcript: %+v", actions)
	}
}

func TestA2AKnownRuntimePoliciesNormalizeMetadataAndIncludeInactiveChannels(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := a2a.OpenPolicyStore(dir, "m5bot-local")
	if err != nil {
		t.Fatalf("OpenPolicyStore: %v", err)
	}
	defer store.Close()
	if err := channelmeta.Upsert(dir, channelmeta.Entry{ID: "channel-1", GuildID: "guild-1", Name: "隨口問", Type: "channel"}); err != nil {
		t.Fatalf("metadata channel-1: %v", err)
	}
	if err := channelmeta.Upsert(dir, channelmeta.Entry{ID: "channel-2", GuildID: "guild-1", Name: "大廳", Type: "channel"}); err != nil {
		t.Fatalf("metadata channel-2: %v", err)
	}
	if err := store.Save(ctx, a2a.ChannelA2APolicy{
		GuildID:               "guild-1",
		ChannelID:             "channel-1",
		Enabled:               true,
		Discoverable:          true,
		RuntimeAgentID:        "m5bot-local-m5-main",
		BotAgentID:            "m5bot-local",
		ChannelRef:            "m5-main",
		AcceptSkills:          []string{"task"},
		ExposeSkills:          []a2a.SkillPolicy{{ID: "task", InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}}},
		ResultVisibility:      "proxy",
		DiscordTranscriptMode: "delegator",
	}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	m := &Manager{dataDir: dir, a2aConfig: a2a.Config{AgentID: "m5bot-local", RuntimeIDMode: a2a.RuntimeIDModeRuntime}, a2aPolicies: store}
	policies, err := m.A2AKnownRuntimePolicies(ctx)
	if err != nil {
		t.Fatalf("A2AKnownRuntimePolicies: %v", err)
	}
	if len(policies) != 2 {
		t.Fatalf("policies = %+v, want active plus inactive metadata channel", policies)
	}
	active, err := store.Get(ctx, "guild-1", "channel-1")
	if err != nil {
		t.Fatalf("Get active: %v", err)
	}
	if active.ChannelRef == "m5-main" || active.RuntimeAgentID == "m5bot-local-m5-main" || !strings.HasPrefix(active.ChannelRef, "ch-") || !strings.HasPrefix(active.RuntimeAgentID, "m5bot-local-ch-") {
		t.Fatalf("active policy was not normalized from metadata: %+v", active)
	}
	foundInactive := false
	for _, policy := range policies {
		if policy.ChannelID == "channel-2" {
			foundInactive = true
			if policy.Enabled || len(policy.ExposeSkills) != 0 || !strings.HasPrefix(policy.ChannelRef, "ch-") || !strings.HasPrefix(policy.RuntimeAgentID, "m5bot-local-ch-") {
				t.Fatalf("inactive metadata policy = %+v", policy)
			}
		}
	}
	if !foundInactive {
		t.Fatalf("inactive metadata channel missing: %+v", policies)
	}
}

func TestA2ACoPresentTransparentResultDoesNotDuplicateExecutorTranscript(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{dataDir: dir}
	row := phase8TaskRow()
	row.ResultVisibility = "transparent"
	row.DiscordTranscriptMode = "co_present"
	payload := a2a.TaskEventPayload{Revision: 3, Result: &a2a.TaskExecutionResult{State: a2a.TaskStateCompleted, Content: "co-present done"}}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindResult, payload); err != nil {
		t.Fatalf("deliverA2AEvent: %v", err)
	}
	actions, _ := botegress.ReadPending(dir)
	if len(actions) != 0 {
		t.Fatalf("co-present transparent result duplicated executor transcript: %+v", actions)
	}
}

func TestA2AMirrorTranscriptQueuesStatusLabel(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{dataDir: dir}
	row := phase8TaskRow()
	row.ResultVisibility = "proxy"
	row.DiscordTranscriptMode = "mirror"
	payload := a2a.TaskEventPayload{Revision: 2, State: a2a.TaskStateWorking, Content: "working"}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindStatus, payload); err != nil {
		t.Fatalf("deliverA2AEvent: %v", err)
	}
	actions, _ := botegress.ReadPending(dir)
	if len(actions) != 1 || !strings.Contains(actions[0].Content, "A2A status") {
		t.Fatalf("mirror status was not queued: %+v", actions)
	}
}

func TestA2AArtifactFetchesObjectAndQueuesFile(t *testing.T) {
	dir := t.TempDir()
	backend := newChannelMemoryObjectBackend()
	store, err := a2a.OpenObjectStore(dir, a2a.WithObjectBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	content := []byte("artifact-bytes")
	ref, err := store.PutObject(context.Background(), "task_phase8", "artifact-1", "report.png", "image/png", content, 1)
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	m := &Manager{dataDir: dir, a2aObjects: store, audit: &recordingAuditSink{}}
	payload := a2a.TaskEventPayload{Revision: 4, Artifact: &a2a.TaskExecutionArtifact{ID: ref.ArtifactID, Name: "report.png", MediaType: ref.MediaType, Digest: ref.Digest, SizeBytes: ref.Size}}
	if err := m.deliverA2AEvent(context.Background(), phase8TaskRow(), a2a.EventKindArtifact, payload); err != nil {
		t.Fatalf("deliverA2AEvent: %v", err)
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 1 || actions[0].Action != botegress.ActionSendFile || !actions[0].RemoveFileAfterSend || actions[0].FilePath == "" {
		t.Fatalf("unexpected pending actions: %+v", actions)
	}
}

func TestA2AMediaPolicyBlocksDisallowedArtifact(t *testing.T) {
	dir := t.TempDir()
	backend := newChannelMemoryObjectBackend()
	store, err := a2a.OpenObjectStore(dir, a2a.WithObjectBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	policyStore, err := a2a.OpenPolicyStore(dir, "adam-n200")
	if err != nil {
		t.Fatal(err)
	}
	defer policyStore.Close()
	row := phase8TaskRow()
	policy := a2a.ChannelA2APolicy{GuildID: row.GuildID, ChannelID: row.ChannelID, Enabled: true, RuntimeAgentID: "adam-n200-" + row.ChannelRef, BotAgentID: "adam-n200", ChannelRef: row.ChannelRef, AcceptFrom: []string{"eve-local"}, AcceptSkills: []string{"review"}, DelegateMedia: a2a.DelegateMediaPolicy{AllowedMIMETypes: []string{"image/png"}, MaxBytes: 4, AllowObjectRefs: true}, ResultVisibility: "transparent", DiscordTranscriptMode: "delegator"}
	if err := policyStore.Save(context.Background(), policy, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	content := []byte("too-large")
	ref, err := store.PutObject(context.Background(), row.TaskID, "artifact-large", "report.png", "image/png", content, 1)
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	m := &Manager{dataDir: dir, a2aObjects: store, a2aPolicies: policyStore}
	payload := a2a.TaskEventPayload{Revision: 4, Artifact: &a2a.TaskExecutionArtifact{ID: ref.ArtifactID, Name: "report.png", MediaType: ref.MediaType, Digest: ref.Digest, SizeBytes: ref.Size}}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindArtifact, payload); err == nil || !strings.Contains(err.Error(), string(a2a.ErrorPayloadTooLarge)) {
		t.Fatalf("expected media policy rejection, got %v", err)
	}
}

func phase8TaskRow() a2a.TaskRow {
	return a2a.TaskRow{TaskID: "task_phase8", ClientTaskRef: "client-1", MessageID: "msg_phase8", ContextID: "ctx", FromAgent: "eve-local", ToAgent: "adam-n200", ExecutorAgent: "adam-n200", ChannelID: "channel-1", GuildID: "guild-1", ChannelRef: "case", SkillID: "review", State: a2a.TaskStateCompleted, Revision: 3, ResultVisibility: "transparent", DiscordTranscriptMode: "delegator", DiscordContextJSON: `{"guildId":"guild-1","channelId":"channel-1","threadId":"thread-1"}`, CreatedAt: time.Now()}
}

type channelMemoryObjectBackend struct{ objects map[string][]byte }

func newChannelMemoryObjectBackend() *channelMemoryObjectBackend {
	return &channelMemoryObjectBackend{objects: map[string][]byte{}}
}
func (b *channelMemoryObjectBackend) PutObject(_ context.Context, bucket string, key string, content []byte, _ string) error {
	b.objects[bucket+"/"+key] = append([]byte(nil), content...)
	return nil
}
func (b *channelMemoryObjectBackend) GetObject(_ context.Context, bucket string, key string) ([]byte, error) {
	return append([]byte(nil), b.objects[bucket+"/"+key]...), nil
}
func (b *channelMemoryObjectBackend) DeleteObject(_ context.Context, bucket string, key string) error {
	delete(b.objects, bucket+"/"+key)
	return nil
}
