package channel

import (
	"context"
	"fmt"
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
	var drained []string
	m := &Manager{dataDir: dir, audit: auditSink, safeEgressDrain: func(target string) int {
		drained = append(drained, target)
		return 0
	}}
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
	if len(drained) != 1 || drained[0] != "thread-1" {
		t.Fatalf("safe egress drain = %v, want immediate drain for queued A2A result", drained)
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
	store, err := a2a.OpenPolicyStore(dir, "local-bot")
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
		RuntimeAgentID:        "local-bot-m5-main",
		BotAgentID:            "local-bot",
		ChannelRef:            "m5-main",
		AcceptSkills:          []string{"task"},
		ExposeSkills:          []a2a.SkillPolicy{{ID: "task", InputModes: []string{"text/plain"}, OutputModes: []string{"text/plain"}}},
		ResultVisibility:      "proxy",
		DiscordTranscriptMode: "delegator",
	}, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	m := &Manager{dataDir: dir, a2aConfig: a2a.Config{AgentID: "local-bot", RuntimeIDMode: a2a.RuntimeIDModeRuntime}, a2aPolicies: store}
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
	if active.ChannelRef == "m5-main" || active.RuntimeAgentID == "local-bot-m5-main" || !strings.HasPrefix(active.ChannelRef, "ch-") || !strings.HasPrefix(active.RuntimeAgentID, "local-bot-ch-") {
		t.Fatalf("active policy was not normalized from metadata: %+v", active)
	}
	foundInactive := false
	for _, policy := range policies {
		if policy.ChannelID == "channel-2" {
			foundInactive = true
			if policy.Enabled || len(policy.ExposeSkills) != 0 || !strings.HasPrefix(policy.ChannelRef, "ch-") || !strings.HasPrefix(policy.RuntimeAgentID, "local-bot-ch-") {
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

func TestA2ACoPresentTransparentArtifactsDoNotDuplicateExecutorTranscript(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{dataDir: dir}
	row := phase8TaskRow()
	row.ResultVisibility = "transparent"
	row.DiscordTranscriptMode = "co_present"
	artifact := a2a.TaskExecutionArtifact{ID: "artifact-1", Name: "report.png", MediaType: "image/png"}

	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindArtifact, a2a.TaskEventPayload{Revision: 4, Artifact: &artifact}); err != nil {
		t.Fatalf("deliverA2AEvent artifact: %v", err)
	}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindResult, a2a.TaskEventPayload{Revision: 5, Result: &a2a.TaskExecutionResult{State: a2a.TaskStateCompleted, Content: "done", Artifacts: []a2a.TaskExecutionArtifact{artifact}}}); err != nil {
		t.Fatalf("deliverA2AEvent result artifacts: %v", err)
	}

	actions, _ := botegress.ReadPending(dir)
	if len(actions) != 0 {
		t.Fatalf("co-present transparent artifacts duplicated executor transcript: %+v", actions)
	}
}

func TestA2AProxyDelegatorArtifactsDoNotBypassResultProxy(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{dataDir: dir}
	row := phase8TaskRow()
	row.ResultVisibility = "proxy"
	row.DiscordTranscriptMode = "delegator"
	artifact := a2a.TaskExecutionArtifact{ID: "artifact-1", Name: "report.png", MediaType: "image/png"}

	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindArtifact, a2a.TaskEventPayload{Revision: 4, Artifact: &artifact}); err != nil {
		t.Fatalf("deliverA2AEvent artifact: %v", err)
	}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindResult, a2a.TaskEventPayload{Revision: 5, Result: &a2a.TaskExecutionResult{State: a2a.TaskStateCompleted, Content: "done", Artifacts: []a2a.TaskExecutionArtifact{artifact}}}); err != nil {
		t.Fatalf("deliverA2AEvent result artifacts: %v", err)
	}

	actions, _ := botegress.ReadPending(dir)
	if len(actions) != 0 {
		t.Fatalf("proxy/delegator artifacts bypassed result proxy: %+v", actions)
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
	row := phase8TaskRow()
	policyStore := phase8ArtifactPolicyStore(t, dir, row)
	defer policyStore.Close()
	content := []byte("artifact-bytes")
	ref, err := store.PutObject(context.Background(), row.TaskID, "artifact-1", "report.png", "image/png", content, 1)
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	m := &Manager{dataDir: dir, a2aObjects: store, a2aPolicies: policyStore, audit: &recordingAuditSink{}}
	payload := a2a.TaskEventPayload{Revision: 4, Artifact: &a2a.TaskExecutionArtifact{ID: ref.ArtifactID, Name: "report.png", MediaType: ref.MediaType, Digest: ref.Digest, SizeBytes: ref.Size}}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindArtifact, payload); err != nil {
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

func TestA2AArtifactFetchesRemoteObjectRefWithoutLocalRow(t *testing.T) {
	dir := t.TempDir()
	backend := newChannelMemoryObjectBackend()
	store, err := a2a.OpenObjectStore(dir, a2a.WithObjectBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	row := phase8TaskRow()
	policyStore := phase8ArtifactPolicyStore(t, dir, row)
	defer policyStore.Close()
	content := []byte("remote-artifact")
	ref := a2a.NewObjectRef(row.TaskID, "artifact-remote", "report.txt", "text/plain", content, 1)
	backend.objects[ref.Bucket+"/"+ref.Key] = append([]byte(nil), content...)
	m := &Manager{dataDir: dir, a2aObjects: store, a2aPolicies: policyStore, audit: &recordingAuditSink{}}
	payload := a2a.TaskEventPayload{Revision: 4, Artifact: &a2a.TaskExecutionArtifact{ID: ref.ArtifactID, Name: "report.txt", MediaType: ref.MediaType, Digest: ref.Digest, SizeBytes: ref.Size, Bucket: ref.Bucket, Key: ref.Key}}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindArtifact, payload); err != nil {
		t.Fatalf("deliverA2AEvent: %v", err)
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 1 || actions[0].Action != botegress.ActionSendFile {
		t.Fatalf("remote object ref was not queued as one file action: %+v", actions)
	}
}

func TestA2AArtifactRejectsMismatchedRefMediaTypeAndSize(t *testing.T) {
	dir := t.TempDir()
	backend := newChannelMemoryObjectBackend()
	store, err := a2a.OpenObjectStore(dir, a2a.WithObjectBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	row := phase8TaskRow()
	policyStore := phase8ArtifactPolicyStore(t, dir, row)
	defer policyStore.Close()
	content := []byte("artifact-bytes")
	ref, err := store.PutObject(context.Background(), row.TaskID, "artifact-1", "report.png", "image/png", content, 1)
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	m := &Manager{dataDir: dir, a2aObjects: store, a2aPolicies: policyStore}
	payload := a2a.TaskEventPayload{Revision: 4, Artifact: &a2a.TaskExecutionArtifact{ID: ref.ArtifactID, Name: "report.txt", MediaType: "text/plain", Digest: ref.Digest, SizeBytes: 1}}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindArtifact, payload); err == nil || !strings.Contains(err.Error(), string(a2a.ErrorPayloadTooLarge)) {
		t.Fatalf("deliver mismatched artifact = %v, want size policy error before queueing", err)
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("mismatched artifact queued: %+v", actions)
	}
	payload.Artifact.SizeBytes = ref.Size
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindArtifact, payload); err == nil || !strings.Contains(err.Error(), string(a2a.ErrorUnsupportedMediaType)) {
		t.Fatalf("deliver mismatched artifact media = %v, want media policy error", err)
	}
}

func TestA2AArtifactDeliveryDedupesArtifactAndResultEvents(t *testing.T) {
	dir := t.TempDir()
	backend := newChannelMemoryObjectBackend()
	store, err := a2a.OpenObjectStore(dir, a2a.WithObjectBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	row := phase8TaskRow()
	policyStore := phase8ArtifactPolicyStore(t, dir, row)
	defer policyStore.Close()
	content := []byte("artifact-bytes")
	ref, err := store.PutObject(context.Background(), row.TaskID, "artifact-1", "report.png", "image/png", content, 1)
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	artifact := a2a.TaskExecutionArtifact{ID: ref.ArtifactID, Name: "report.png", MediaType: ref.MediaType, Digest: ref.Digest, SizeBytes: ref.Size}
	m := &Manager{dataDir: dir, a2aObjects: store, a2aPolicies: policyStore, audit: &recordingAuditSink{}}
	artifactPayload := a2a.TaskEventPayload{Revision: 4, Artifact: &artifact}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindArtifact, artifactPayload); err != nil {
		t.Fatalf("deliver artifact event: %v", err)
	}
	resultPayload := a2a.TaskEventPayload{Revision: 5, Result: &a2a.TaskExecutionResult{TaskID: row.TaskID, State: a2a.TaskStateCompleted, Revision: 5, Artifacts: []a2a.TaskExecutionArtifact{artifact}}}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindResult, resultPayload); err != nil {
		t.Fatalf("deliver result event: %v", err)
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 1 || actions[0].ID != a2aArtifactDeliveryActionID(row, a2aDeliveryTarget(row), artifact) {
		t.Fatalf("artifact/result events should share one pending action: %+v", actions)
	}
}

func TestA2AArtifactPolicyUnavailableDoesNotQueueFile(t *testing.T) {
	dir := t.TempDir()
	backend := newChannelMemoryObjectBackend()
	store, err := a2a.OpenObjectStore(dir, a2a.WithObjectBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	row := phase8TaskRow()
	content := []byte("artifact-bytes")
	ref, err := store.PutObject(context.Background(), row.TaskID, "artifact-1", "report.png", "image/png", content, 1)
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	m := &Manager{dataDir: dir, a2aObjects: store}
	payload := a2a.TaskEventPayload{Revision: 4, Artifact: &a2a.TaskExecutionArtifact{ID: ref.ArtifactID, Name: "report.png", MediaType: ref.MediaType, Digest: ref.Digest, SizeBytes: ref.Size}}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindArtifact, payload); err == nil || !strings.Contains(err.Error(), string(a2a.ErrorPolicyDenied)) {
		t.Fatalf("expected fail-closed artifact policy denial, got %v", err)
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("artifact queued despite unavailable policy: %+v", actions)
	}
}

func TestA2AResultArtifactBatchValidatesBeforeQueueing(t *testing.T) {
	dir := t.TempDir()
	backend := newChannelMemoryObjectBackend()
	store, err := a2a.OpenObjectStore(dir, a2a.WithObjectBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	row := phase8TaskRow()
	policyStore := phase8ArtifactPolicyStore(t, dir, row)
	defer policyStore.Close()
	content := []byte("artifact-bytes")
	ref, err := store.PutObject(context.Background(), row.TaskID, "artifact-1", "report.png", "image/png", content, 1)
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	good := a2a.TaskExecutionArtifact{ID: ref.ArtifactID, Name: "report.png", MediaType: ref.MediaType, Digest: ref.Digest, SizeBytes: ref.Size}
	m := &Manager{dataDir: dir, a2aObjects: store, a2aPolicies: policyStore}
	payload := a2a.TaskEventPayload{Revision: 4, Result: &a2a.TaskExecutionResult{State: a2a.TaskStateCompleted, Content: "done", Artifacts: []a2a.TaskExecutionArtifact{good, {ID: "missing", Name: "missing.txt", MediaType: "text/plain"}}}}
	if err := m.deliverA2AEvent(context.Background(), row, a2a.EventKindResult, payload); err == nil || !strings.Contains(err.Error(), string(a2a.ErrorArtifactFetchFailed)) {
		t.Fatalf("expected artifact fetch failure, got %v", err)
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("artifact queued before validating the full batch: %+v", actions)
	}
}

func TestA2AResultArtifactFailureDoesNotQueueText(t *testing.T) {
	dir := t.TempDir()
	backend := newChannelMemoryObjectBackend()
	store, err := a2a.OpenObjectStore(dir, a2a.WithObjectBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	m := &Manager{dataDir: dir, a2aObjects: store}
	payload := a2a.TaskEventPayload{Revision: 4, Result: &a2a.TaskExecutionResult{State: a2a.TaskStateCompleted, Content: "done", Artifacts: []a2a.TaskExecutionArtifact{{ID: "missing", Name: "missing.txt", MediaType: "text/plain"}}}}
	if err := m.deliverA2AEvent(context.Background(), phase8TaskRow(), a2a.EventKindResult, payload); err == nil || !strings.Contains(err.Error(), string(a2a.ErrorArtifactFetchFailed)) {
		t.Fatalf("expected artifact fetch failure, got %v", err)
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("text was queued despite artifact delivery failure: %+v", actions)
	}
}

func TestA2AForeignArtifactRefDoesNotQueueFile(t *testing.T) {
	dir := t.TempDir()
	backend := newChannelMemoryObjectBackend()
	store, err := a2a.OpenObjectStore(dir, a2a.WithObjectBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ref, err := store.PutObject(context.Background(), "task_other", "artifact-other", "other.txt", "text/plain", []byte("secret"), 1)
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	m := &Manager{dataDir: dir, a2aObjects: store}
	payload := a2a.TaskEventPayload{Revision: 4, Artifact: &a2a.TaskExecutionArtifact{ID: ref.ArtifactID, Name: "other.txt", MediaType: ref.MediaType, Digest: ref.Digest, SizeBytes: ref.Size}}
	if err := m.deliverA2AEvent(context.Background(), phase8TaskRow(), a2a.EventKindArtifact, payload); err == nil {
		t.Fatal("expected foreign artifact ref to fail")
	}
	actions, err := botegress.ReadPending(dir)
	if err != nil {
		t.Fatalf("ReadPending: %v", err)
	}
	if len(actions) != 0 {
		t.Fatalf("mismatched artifact was queued: %+v", actions)
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

func phase8ArtifactPolicyStore(t *testing.T, dir string, row a2a.TaskRow) *a2a.SQLitePolicyStore {
	t.Helper()
	policyStore, err := a2a.OpenPolicyStore(dir, "adam-n200")
	if err != nil {
		t.Fatal(err)
	}
	policy := a2a.ChannelA2APolicy{
		GuildID:               row.GuildID,
		ChannelID:             row.ChannelID,
		Enabled:               true,
		RuntimeAgentID:        "adam-n200-" + row.ChannelRef,
		BotAgentID:            "adam-n200",
		ChannelRef:            row.ChannelRef,
		AcceptFrom:            []string{"eve-local"},
		AcceptSkills:          []string{"review"},
		DelegateMedia:         a2a.DelegateMediaPolicy{AllowedMIMETypes: []string{"image/png", "text/plain"}, MaxBytes: 0, AllowObjectRefs: true},
		ResultVisibility:      "transparent",
		DiscordTranscriptMode: "delegator",
	}
	if err := policyStore.Save(context.Background(), policy, "manager"); err != nil {
		t.Fatalf("Save policy: %v", err)
	}
	return policyStore
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
	content, ok := b.objects[bucket+"/"+key]
	if !ok {
		return nil, fmt.Errorf("object not found")
	}
	return append([]byte(nil), content...), nil
}
func (b *channelMemoryObjectBackend) DeleteObject(_ context.Context, bucket string, key string) error {
	delete(b.objects, bucket+"/"+key)
	return nil
}
