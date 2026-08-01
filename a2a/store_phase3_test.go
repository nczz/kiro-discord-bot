package a2a

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTaskStoreCreateInboundOutboundAndReplay(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenTaskStore: %v", err)
	}
	defer store.Close()

	out, err := store.CreateOutbound(ctx, phase3TaskRow("out_msg"))
	if err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}
	if out.Direction != "outbound" || out.Role != "delegator" || out.State != TaskStateSubmitted || out.ResultVisibility != "proxy" {
		t.Fatalf("unexpected outbound row: %+v", out)
	}
	dup, err := store.CreateOutbound(ctx, phase3TaskRow("out_msg"))
	if err != nil {
		t.Fatalf("duplicate CreateOutbound: %v", err)
	}
	if dup.LocalID != out.LocalID {
		t.Fatalf("duplicate message created new row %q != %q", dup.LocalID, out.LocalID)
	}

	in, err := store.AdmitInbound(ctx, phase3TaskRow("in_msg"))
	if err != nil {
		t.Fatalf("AdmitInbound: %v", err)
	}
	if in.Direction != "inbound" || in.Role != "executor" {
		t.Fatalf("unexpected inbound row: %+v", in)
	}

	event := EventRow{TaskID: "task_one", Revision: 1, EventType: "status", State: TaskStateWorking, PayloadJSON: `{"ok":true}`}
	if err := store.AppendEvent(ctx, event); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if err := store.AppendEvent(ctx, event); err != nil {
		t.Fatalf("idempotent AppendEvent: %v", err)
	}
	if err := store.AppendEvent(ctx, EventRow{TaskID: "task_one", Revision: 1, EventType: "status", State: TaskStateFailed, PayloadJSON: `{}`}); err == nil {
		t.Fatal("changed replay accepted")
	}
	replayed, err := store.ReplayEvents(ctx, "task_one", 0)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}
	if len(replayed) != 1 || replayed[0].Revision != 1 {
		t.Fatalf("unexpected replay: %+v", replayed)
	}
}

func TestTaskStorePersistsOriginRuntimeRef(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenTaskStore: %v", err)
	}
	defer store.Close()

	row := phase3TaskRow("origin_ref_msg")
	row.OriginRuntimeRef = OriginRuntimeRef{
		RuntimeAgentID:   "local-bot-ch-2cbaf623",
		BotAgentID:       "local-bot",
		ChannelRef:       "ch-2cbaf623",
		DisplayName:      "隨口問",
		DiscordGuildID:   "guild",
		DiscordChannelID: "channel",
		DiscordThreadID:  "thread",
		MessageID:        "discord-message",
	}
	created, err := store.CreateOutbound(ctx, row)
	if err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}
	got, err := store.GetByLocalID(ctx, created.LocalID)
	if err != nil {
		t.Fatalf("GetByLocalID: %v", err)
	}
	if got.OriginRuntimeRef.RuntimeAgentID != "local-bot-ch-2cbaf623" ||
		got.OriginRuntimeRef.BotAgentID != "local-bot" ||
		got.OriginRuntimeRef.ChannelRef != "ch-2cbaf623" ||
		got.OriginRuntimeRef.DisplayName != "隨口問" ||
		got.OriginRuntimeRef.DiscordThreadID != "thread" {
		t.Fatalf("origin runtime ref = %+v", got.OriginRuntimeRef)
	}
}

func TestAcceptedBootstrapBindsOnlyTargetExecutor(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	row := phase3TaskRow("accept_msg")
	row.ToAgent = "eve-local"
	if _, err := store.CreateOutbound(ctx, row); err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}
	if _, err := store.BindAccepted(ctx, "accept_msg", "remote_task", "mallory"); err == nil {
		t.Fatal("forged accepted bootstrap bound wrong executor")
	}
	bound, err := store.BindAccepted(ctx, "accept_msg", "remote_task", "eve-local")
	if err != nil {
		t.Fatalf("BindAccepted: %v", err)
	}
	if bound.TaskID != "remote_task" || bound.ExecutorAgent != "eve-local" || bound.State != TaskStateWorking || bound.Revision != 1 {
		t.Fatalf("unexpected bound row: %+v", bound)
	}
	again, err := store.BindAccepted(ctx, "accept_msg", "remote_task", "eve-local")
	if err != nil {
		t.Fatalf("idempotent BindAccepted: %v", err)
	}
	if again.TaskID != bound.TaskID {
		t.Fatalf("idempotent bind changed task: %+v", again)
	}
}

func TestRejectedBeforeAcceptedCorrelatesMessageID(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	row := phase3TaskRow("reject_msg")
	row.ClientTaskRef = "client-ref"
	if _, err := store.CreateOutbound(ctx, row); err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}
	rejected, err := store.RejectBeforeAccepted(ctx, "reject_msg", "client-ref", "eve-local", TaskError{Code: ErrorPolicyDenied, Message: "denied"})
	if err != nil {
		t.Fatalf("RejectBeforeAccepted: %v", err)
	}
	if rejected.TaskID != "msg_reject_msg" || rejected.State != TaskStateRejected || !rejected.Terminal {
		t.Fatalf("unexpected rejection row: %+v", rejected)
	}
	if _, err := store.RejectBeforeAccepted(ctx, "reject_msg", "wrong", "eve-local", TaskError{}); err == nil {
		t.Fatal("client_task_ref mismatch accepted")
	}
}

func TestTerminalImmutableExceptIdempotentReplay(t *testing.T) {
	ctx := context.Background()
	store, err := OpenTaskStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	row := phase3TaskRow("terminal_msg")
	row.TaskID = "terminal_task"
	created, err := store.CreateOutbound(ctx, row)
	if err != nil {
		t.Fatalf("CreateOutbound: %v", err)
	}
	terminal, err := store.markTerminal(ctx, created.LocalID, TaskStateCompleted, TaskError{})
	if err != nil {
		t.Fatalf("markTerminal: %v", err)
	}
	if !terminal.Terminal || terminal.Revision != 1 {
		t.Fatalf("unexpected terminal: %+v", terminal)
	}
	if _, err := store.markTerminal(ctx, created.LocalID, TaskStateCompleted, TaskError{}); err != nil {
		t.Fatalf("idempotent terminal replay: %v", err)
	}
	if _, err := store.markTerminal(ctx, created.LocalID, TaskStateFailed, TaskError{Code: ErrorInternal}); err == nil {
		t.Fatal("terminal mutation accepted")
	}
}

func TestPolicyStoreValidationAndPersistence(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPolicyStore(t.TempDir(), "adam-n200")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	policy := ChannelA2APolicy{
		GuildID:                 "guild",
		ChannelID:               "channel",
		Enabled:                 true,
		Discoverable:            true,
		RuntimeAgentID:          "adam-n200-backend",
		BotAgentID:              "adam-n200",
		ChannelRef:              "backend",
		AcceptFrom:              []string{"eve-local", "*"},
		AcceptFromRuntimes:      []string{"eve-local-backend", "*"},
		AcceptSkills:            []string{"review"},
		ExposeSkills:            []SkillPolicy{{ID: "review", InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}}},
		DelegateTo:              []string{"eve-local"},
		DelegateSkills:          []string{"backend/review"},
		DelegateTargets:         []DelegateTargetPolicy{{RuntimeAgentID: "eve-local-backend", SkillID: "backend/review"}},
		DelegateMedia:           DelegateMediaPolicy{AllowedMIMETypes: []string{"image/png"}, MaxBytes: 0, AllowObjectRefs: true},
		MaxConcurrent:           0,
		ResultVisibility:        "proxy",
		DiscordTranscriptMode:   "co_present",
		ShareDiscordContext:     true,
		CoPresentTargetChannels: []string{"channel-2"},
		RemoteToolPolicy:        RemoteToolPolicy{},
	}
	if err := store.Save(ctx, policy, "manager"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := store.Get(ctx, "guild", "channel")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Enabled || !got.Discoverable || got.RuntimeAgentID != "adam-n200-backend" || got.MaxConcurrent != 0 || got.RemoteToolPolicy.AllowMemoryWrite || len(got.DelegateTargets) != 1 || got.DelegateTargets[0].RuntimeAgentID != "eve-local-backend" || len(got.AcceptFromRuntimes) != 2 || len(got.CoPresentTargetChannels) != 1 || got.CoPresentTargetChannels[0] != "channel-2" {
		t.Fatalf("unexpected policy: %+v", got)
	}
	listCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	discoverable, err := store.ListDiscoverable(listCtx)
	if err != nil {
		t.Fatalf("ListDiscoverable: %v", err)
	}
	if len(discoverable) != 1 || discoverable[0].RuntimeAgentID != "adam-n200-backend" {
		t.Fatalf("unexpected discoverable policies: %+v", discoverable)
	}
	bad := policy
	bad.ChannelID = "other"
	bad.ShareDiscordContext = true
	bad.DiscordTranscriptMode = "mirror"
	if err := store.Save(ctx, bad, "manager"); err == nil {
		t.Fatal("share_discord_context without co_present accepted")
	}
	bad = policy
	bad.ChannelID = "other"
	bad.MaxConcurrent = 65
	if err := store.Save(ctx, bad, "manager"); err == nil {
		t.Fatal("max_concurrent over 64 accepted")
	}
	bad = policy
	bad.ChannelID = "other"
	bad.AcceptFrom = []string{"adam-1234567890"}
	if err := store.Save(ctx, bad, "manager"); err == nil {
		t.Fatal("unstable agent id accepted")
	}
	bad = policy
	bad.ChannelID = "other"
	bad.CoPresentTargetChannels = []string{""}
	if err := store.Save(ctx, bad, "manager"); err == nil {
		t.Fatal("empty co-present target channel accepted")
	}
	bad = policy
	bad.ChannelID = "other"
	bad.ChannelRef = "backend"
	if err := store.Save(ctx, bad, "manager"); err == nil {
		t.Fatal("duplicate channel_ref accepted")
	}

	delegator := policy
	delegator.ChannelID = "delegator"
	delegator.ChannelRef = "delegator"
	delegator.RuntimeAgentID = "adam-n200-delegator"
	delegator.AcceptFrom = []string{"adam-n200-backend"}
	delegator.AcceptFromRuntimes = []string{"adam-n200-backend"}
	delegator.DelegateTo = []string{"adam-n200-backend"}
	delegator.DelegateTargets = []DelegateTargetPolicy{{RuntimeAgentID: "adam-n200-backend", SkillID: "backend/review"}}
	delegator.CoPresentFrom = []string{"adam-n200-backend"}
	delegator.CoPresentFromRuntimes = []string{"adam-n200-backend"}
	if err := store.Save(ctx, delegator, "manager"); err != nil {
		t.Fatalf("Save delegator: %v", err)
	}
	renamed := policy
	renamed.ChannelRef = "backend-renamed"
	renamed.RuntimeAgentID = "adam-n200-backend-renamed"
	if err := store.Save(ctx, renamed, "manager"); err != nil {
		t.Fatalf("Save renamed: %v", err)
	}
	gotDelegator, err := store.Get(ctx, "guild", "delegator")
	if err != nil {
		t.Fatalf("Get delegator: %v", err)
	}
	if gotDelegator.AcceptFrom[0] != "adam-n200-backend-renamed" || gotDelegator.AcceptFromRuntimes[0] != "adam-n200-backend-renamed" || gotDelegator.DelegateTo[0] != "adam-n200-backend-renamed" || gotDelegator.DelegateTargets[0].RuntimeAgentID != "adam-n200-backend-renamed" || gotDelegator.CoPresentFrom[0] != "adam-n200-backend-renamed" || gotDelegator.CoPresentFromRuntimes[0] != "adam-n200-backend-renamed" {
		t.Fatalf("runtime references were not rewritten: %+v", gotDelegator)
	}
}

func TestPeerStoreSanitizesCardAndMarksStale(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPeerStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	card := AgentCard{Name: "eve-local", Description: "uses /Users/eve/project and DISCORD_TOKEN", Version: "1.0.0", SupportedInterfaces: []A2AInterface{{URL: "http://127.0.0.1:4222", ProtocolBinding: "urn:kiro-discord-bot:a2a:nats:v1", ProtocolVersion: "1.0"}}, Skills: []AgentSkill{{ID: "backend/review", Name: "Review", Description: "reads /data/private", Examples: []string{"secret example"}}}}
	row, err := store.UpsertCard(ctx, "eve-local", card, true, time.Now().Add(-time.Minute))
	if err != nil {
		t.Fatalf("UpsertCard: %v", err)
	}
	if strings.Contains(row.CardJSON, "/Users") || strings.Contains(row.CardJSON, "DISCORD_TOKEN") || strings.Contains(row.CardJSON, "secret example") || strings.Contains(row.CardJSON, "127.0.0.1") {
		t.Fatalf("card leaked sensitive data: %s", row.CardJSON)
	}
	display, err := store.TrustDisplay(ctx, time.Hour)
	if err != nil {
		t.Fatalf("TrustDisplay: %v", err)
	}
	if len(display) != 1 || !display[0].Stale || !display[0].Trusted || display[0].SkillIDs[0] != "backend/review" {
		t.Fatalf("unexpected display: %+v", display)
	}
}

func TestObjectRefValidatesDigestSizeAndRetention(t *testing.T) {
	ctx := context.Background()
	store, err := OpenObjectStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	content := []byte("artifact")
	ref := ObjectRef{ArtifactID: "artifact-1", TaskID: "task_object", Bucket: "a2a-artifacts", Key: "tasks/task_object/report.txt", Digest: "sha256:" + sha256ForTest(content), Size: int64(len(content)), MediaType: "text/plain", ExpiresAt: time.Now().Add(-time.Minute)}
	if _, err := store.PutRef(ctx, ref, content); err != nil {
		t.Fatalf("PutRef: %v", err)
	}
	bad := ref
	bad.ArtifactID = "artifact-2"
	bad.Digest = "sha256:" + strings.Repeat("0", 64)
	if _, err := store.PutRef(ctx, bad, content); err == nil {
		t.Fatal("digest mismatch accepted")
	}
	bad = ref
	bad.ArtifactID = "artifact-3"
	bad.Size++
	if _, err := store.PutRef(ctx, bad, content); err == nil {
		t.Fatal("size mismatch accepted")
	}
	pruned, err := store.PruneExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("PruneExpired: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned %d, want 1", pruned)
	}
}

func TestObjectStoreStoresFetchesAndPrunesBackendBytes(t *testing.T) {
	ctx := context.Background()
	backend := newMemoryObjectBackend()
	store, err := OpenObjectStore(t.TempDir(), WithObjectBackend(backend))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	content := []byte("image-bytes")
	ref, err := store.PutObject(ctx, "task_object", "artifact-image", "../report image.png", "image/png", content, 1)
	if err != nil {
		t.Fatalf("PutObject: %v", err)
	}
	if strings.Contains(ref.Key, "..") || !strings.HasPrefix(ref.Key, "tasks/task_object/artifact-image/") {
		t.Fatalf("unsafe key generated: %q", ref.Key)
	}
	got, gotRef, err := store.FetchObject(ctx, "artifact-image")
	if err != nil {
		t.Fatalf("FetchObject: %v", err)
	}
	if string(got) != string(content) || gotRef.Digest != "sha256:"+sha256ForTest(content) {
		t.Fatalf("bad fetch got=%q ref=%+v", got, gotRef)
	}
	expired := gotRef
	expired.ArtifactID = "artifact-expired"
	expired.Key = "tasks/task_object/artifact-expired/report.png"
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	if _, err := store.PutRef(ctx, expired, content); err != nil {
		t.Fatalf("PutRef expired: %v", err)
	}
	backend.objects[expired.Bucket+"/"+expired.Key] = content
	pruned, err := store.PruneExpired(ctx, time.Now())
	if err != nil {
		t.Fatalf("PruneExpired: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("pruned %d, want 1", pruned)
	}
	if _, ok := backend.objects[expired.Bucket+"/"+expired.Key]; ok {
		t.Fatal("expired backend object was not deleted")
	}
}

type memoryObjectBackend struct {
	objects map[string][]byte
}

func newMemoryObjectBackend() *memoryObjectBackend {
	return &memoryObjectBackend{objects: map[string][]byte{}}
}

func (b *memoryObjectBackend) PutObject(_ context.Context, bucket string, key string, content []byte, _ string) error {
	b.objects[bucket+"/"+key] = append([]byte(nil), content...)
	return nil
}

func (b *memoryObjectBackend) GetObject(_ context.Context, bucket string, key string) ([]byte, error) {
	content, ok := b.objects[bucket+"/"+key]
	if !ok {
		return nil, fmt.Errorf("missing object")
	}
	return append([]byte(nil), content...), nil
}

func (b *memoryObjectBackend) DeleteObject(_ context.Context, bucket string, key string) error {
	delete(b.objects, bucket+"/"+key)
	return nil
}

func phase3TaskRow(message string) TaskRow {
	return TaskRow{MessageID: MessageID(message), FromAgent: "adam-n200", ToAgent: "eve-local", ChannelID: "channel", GuildID: "guild", ChannelRef: "backend", SkillID: "review"}
}

func TestValidateInboundRuntimeTrustedSenderDefaultsTaskSkill(t *testing.T) {
	policy := ChannelA2APolicy{
		Enabled:            true,
		AcceptFromRuntimes: []string{"peer-runtime"},
	}
	for _, skillID := range []string{"task", "general/task", "ch-2cbaf623/task"} {
		if err := policy.ValidateInboundRuntime("peer-runtime", skillID); err != nil {
			t.Fatalf("ValidateInboundRuntime(%q): %v", skillID, err)
		}
	}
	if err := policy.ValidateInboundRuntime("peer-runtime", "danger/admin"); err == nil {
		t.Fatalf("ValidateInboundRuntime accepted non-task skill with empty accept_skills")
	}
}

func sha256ForTest(b []byte) string { sum := sha256.Sum256(b); return fmt.Sprintf("%x", sum[:]) }
