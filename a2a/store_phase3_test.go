package a2a

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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
	artifactEvent := EventRow{TaskID: "task_one", Revision: 1, EventType: "artifact", State: TaskStateWorking, PayloadJSON: `{"artifact":true}`}
	if err := store.AppendEvent(ctx, artifactEvent); err != nil {
		t.Fatalf("same revision different event type AppendEvent: %v", err)
	}
	if err := store.AppendEvent(ctx, artifactEvent); err != nil {
		t.Fatalf("idempotent same revision artifact AppendEvent: %v", err)
	}
	if err := store.AppendEvent(ctx, EventRow{TaskID: "task_one", Revision: 1, EventType: "status", State: TaskStateFailed, PayloadJSON: `{}`}); err == nil {
		t.Fatal("changed replay accepted")
	}
	replayed, err := store.ReplayEvents(ctx, "task_one", 0)
	if err != nil {
		t.Fatalf("ReplayEvents: %v", err)
	}
	if len(replayed) != 2 || replayed[0].Revision != 1 || replayed[1].Revision != 1 {
		t.Fatalf("unexpected replay: %+v", replayed)
	}
}

func TestTaskRequestFromRowDoesNotInferSharedDiscordContext(t *testing.T) {
	row := TaskRow{
		MessageID:             "msg-1",
		ClientTaskRef:         "client-1",
		ContextID:             "ctx-1",
		FromAgent:             "eve-local",
		ToAgent:               "adam-n200-case",
		ChannelID:             "channel-1",
		GuildID:               "guild-1",
		ChannelRef:            "case",
		SkillID:               "review",
		ResultVisibility:      "transparent",
		DiscordTranscriptMode: "co_present",
		DiscordContextJSON:    `{"guildId":"guild-1","channelId":"channel-1","threadId":"thread-1"}`,
	}
	req := taskRequestFromRow(row)
	if req.Delivery.ShareDiscordContext || req.Delivery.CoPresentFrom != "" {
		t.Fatalf("delivery sharing = share:%v from:%q, want persisted context without inferred sharing", req.Delivery.ShareDiscordContext, req.Delivery.CoPresentFrom)
	}
	if req.Delivery.DiscordContext == nil || req.Delivery.DiscordReplyChannelID != "channel-1" || req.Delivery.DiscordReplyThreadID != "thread-1" {
		t.Fatalf("discord delivery = %+v context=%+v, want channel/thread from persisted context", req.Delivery, req.Delivery.DiscordContext)
	}
}

func TestTaskRequestFromEnvelopeForExistingRowKeepsEnvelopePayload(t *testing.T) {
	env := newEnvelope("eve-local", "adam-n200-case", EnvelopeTypeTask, "msg-1", "", 0, []byte(`{"a2a":{"message":{"parts":[{"kind":"text","text":"from envelope"}]}},"channelRef":"wire-channel","skillId":"wire/skill","userVisibleSummary":"visible summary","clientTaskRef":"wire-client","contextId":"wire-context","delivery":{"timeoutSec":9,"maxDelegationDepth":2,"discordContext":{"guildId":"guild-wire","channelId":"channel-wire","threadId":"thread-wire"},"resultVisibility":"transparent","discordTranscriptMode":"co_present"},"auditMetadata":{"origin":"wire"}}`))
	row := TaskRow{
		MessageID:             "msg-1",
		ClientTaskRef:         "stored-client",
		ContextID:             "stored-context",
		FromAgent:             "eve-local",
		ToAgent:               "adam-n200-case",
		ChannelID:             "channel-stored",
		GuildID:               "guild-stored",
		ChannelRef:            "stored-channel",
		SkillID:               "stored/skill",
		ResultVisibility:      "proxy",
		DiscordTranscriptMode: "delegator",
	}
	req, err := taskRequestFromEnvelopeForExistingRow(env, Subject{Kind: SubjectKindTask, From: "eve-local", To: "adam-n200-case", MessageID: "msg-1"}, row)
	if err != nil {
		t.Fatalf("taskRequestFromEnvelopeForExistingRow: %v", err)
	}
	if !strings.Contains(string(req.Payload), "from envelope") || req.UserVisibleSummary != "visible summary" || req.AuditMetadata["origin"] != "wire" {
		t.Fatalf("rehydrated envelope-only fields = payload:%s summary:%q audit:%v", req.Payload, req.UserVisibleSummary, req.AuditMetadata)
	}
	if req.ChannelID != "channel-stored" || req.GuildID != "guild-stored" || req.ChannelRef != "stored-channel" || req.SkillID != "stored/skill" || req.ResultVisibility != "proxy" || req.DiscordTranscriptMode != "delegator" {
		t.Fatalf("rehydrated policy fields = %+v, want persisted row policy", req)
	}
	if req.Delivery.TimeoutSec != 9 || req.Delivery.MaxDelegationDepth != 2 || req.Delivery.DiscordReplyChannelID != "channel-wire" || req.Delivery.DiscordReplyThreadID != "thread-wire" {
		t.Fatalf("rehydrated delivery = %+v, want envelope execution delivery", req.Delivery)
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
func TestFetchObjectRefValidatesMetadataBeforeBackendRead(t *testing.T) {
	backend := newMemoryObjectBackend()
	store, err := OpenObjectStore(t.TempDir(), WithObjectBackend(backend))
	if err != nil {
		t.Fatalf("OpenObjectStore: %v", err)
	}
	defer store.Close()
	_, _, err = store.FetchObjectRef(context.Background(), ObjectRef{ArtifactID: "artifact-1", TaskID: "task_one", Bucket: "evil-bucket", Key: "tasks/task_one/artifact-1/file.txt", Digest: "sha256:" + strings.Repeat("a", 64), Size: 1, MediaType: "text/plain"})
	if err == nil || !strings.Contains(err.Error(), "bucket") {
		t.Fatalf("FetchObjectRef invalid metadata = %v, want bucket rejection", err)
	}
	if backend.gets != 0 {
		t.Fatalf("FetchObjectRef read backend %d times before metadata validation", backend.gets)
	}
}

func TestObjectStoreMigratesLegacyObjectRefsOnce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "a2a", "objects.sqlite")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE a2a_object_refs (
		artifact_id TEXT PRIMARY KEY,
		task_id TEXT NOT NULL,
		bucket TEXT NOT NULL,
		key TEXT NOT NULL,
		digest TEXT NOT NULL,
		size INTEGER NOT NULL,
		media_type TEXT NOT NULL,
		expires_at TEXT,
		created_at TEXT NOT NULL
	)`); err != nil {
		_ = db.Close()
		t.Fatalf("create legacy refs: %v", err)
	}
	content := []byte("legacy artifact")
	key := objectKey("task_legacy", "artifact-1", "legacy.txt")
	if _, err := db.ExecContext(ctx, `INSERT INTO a2a_object_refs(artifact_id, task_id, bucket, key, digest, size, media_type, expires_at, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "artifact-1", "task_legacy", DefaultObjectBucket, key, "sha256:"+sha256ForTest(content), len(content), "text/plain", nil, time.Now().UTC().Format(sqliteTimeFormat)); err != nil {
		_ = db.Close()
		t.Fatalf("insert legacy ref: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}
	backend := newMemoryObjectBackend()
	backend.objects[DefaultObjectBucket+"/"+key] = content
	store, err := OpenObjectStore(dir, WithObjectBackend(backend))
	if err != nil {
		t.Fatalf("OpenObjectStore migrated legacy refs: %v", err)
	}
	got, ref, err := store.FetchObject(ctx, "task_legacy", "artifact-1")
	if err != nil {
		t.Fatalf("FetchObject migrated ref: %v", err)
	}
	if string(got) != string(content) || ref.TaskID != "task_legacy" {
		t.Fatalf("migrated ref fetch got=%q ref=%+v", got, ref)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close migrated store: %v", err)
	}
	reopened, err := OpenObjectStore(dir, WithObjectBackend(backend))
	if err != nil {
		t.Fatalf("OpenObjectStore after migration: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened store: %v", err)
	}
	verifyDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open verify: %v", err)
	}
	defer verifyDB.Close()
	composite, err := objectRefsHaveCompositePrimaryKey(ctx, verifyDB)
	if err != nil {
		t.Fatalf("objectRefsHaveCompositePrimaryKey: %v", err)
	}
	if !composite {
		t.Fatal("legacy refs were not migrated to composite primary key")
	}
	var migratedTables int
	if err := verifyDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'a2a_object_refs_migrated'`).Scan(&migratedTables); err != nil {
		t.Fatalf("count migrated table: %v", err)
	}
	if migratedTables != 0 {
		t.Fatalf("temporary migration table remains: %d", migratedTables)
	}
}

func TestArtifactFetchFailureIsRetryableEventDelivery(t *testing.T) {
	err := fmt.Errorf("%s: object store temporarily unavailable", ErrorArtifactFetchFailed)
	if permanentEventDeliveryError(err) {
		t.Fatal("artifact fetch failure was classified as permanent")
	}
}

func TestExistingInboundTaskSubjectMustMatchStoredRow(t *testing.T) {
	row := TaskRow{FromAgent: "alice-runtime", ToAgent: "bob-runtime", ExecutorAgent: "bob-runtime"}
	if err := validateExistingInboundTaskSubject(row, Subject{From: "alice-runtime", To: "bob-runtime"}); err != nil {
		t.Fatalf("matching subject rejected: %v", err)
	}
	if err := validateExistingInboundTaskSubject(row, Subject{From: "mallory-runtime", To: "bob-runtime"}); err == nil {
		t.Fatal("subject with mismatched sender accepted")
	}
	if err := validateExistingInboundTaskSubject(row, Subject{From: "alice-runtime", To: "eve-runtime"}); err == nil {
		t.Fatal("subject with mismatched target accepted")
	}
	row.ExecutorAgent = "other-runtime"
	if err := validateExistingInboundTaskSubject(row, Subject{From: "alice-runtime", To: "bob-runtime"}); err == nil {
		t.Fatal("subject with mismatched executor accepted")
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
	bad.ChannelRef = "case/alpha"
	if err := store.Save(ctx, bad, "manager"); err == nil {
		t.Fatal("invalid channel_ref accepted")
	}
	bad = policy
	bad.ChannelID = "other"
	bad.Enabled = false
	bad.Discoverable = false
	bad.ChannelRef = "case/alpha"
	if err := store.Save(ctx, bad, "manager"); err == nil {
		t.Fatal("disabled invalid channel_ref accepted")
	}
	bad = policy
	bad.ChannelID = "other"
	bad.ChannelRef = "other"
	bad.DelegateTargets = []DelegateTargetPolicy{{RuntimeAgentID: "eve-local-backend", ChannelRef: "case/alpha", SkillID: "backend/review"}}
	if err := store.Save(ctx, bad, "manager"); err == nil {
		t.Fatal("invalid delegate target channel_ref accepted")
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
func TestPolicyStoreOpenPreservesLegacyFrontendAllowlists(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := openA2ASQLite(dir, "policy.sqlite", policyStoreMigrations())
	if err != nil {
		t.Fatalf("open raw policy store: %v", err)
	}
	rawStore := &SQLitePolicyStore{db: db, agentID: "adam-n200"}
	if err := rawStore.Save(ctx, ChannelA2APolicy{
		GuildID:                 "guild",
		ChannelID:               "channel",
		Enabled:                 true,
		RuntimeAgentID:          "adam-n200-channel",
		BotAgentID:              "adam-n200",
		ChannelRef:              "backend",
		AcceptFrom:              []string{"legacy-bot"},
		AcceptFromRuntimes:      []string{"runtime-bot"},
		AcceptSkills:            []string{"task"},
		DelegateTo:              []string{"legacy-bot"},
		DelegateSkills:          []string{"review"},
		DelegateTargets:         []DelegateTargetPolicy{{RuntimeAgentID: "runtime-bot", ChannelRef: "backend", SkillID: "general/task"}},
		CoPresentFrom:           []string{"legacy-bot"},
		AutoDelegateEnabled:     true,
		RemoteToolPolicy:        RemoteToolPolicy{AllowMemoryWrite: true},
		ResultVisibility:        "proxy",
		DiscordTranscriptMode:   "delegator",
		ShareDiscordContext:     false,
		Discoverable:            true,
		DelegateMedia:           DelegateMediaPolicy{},
		CoPresentFromRuntimes:   []string{"runtime-bot"},
		CoPresentTargetChannels: []string{"other"},
	}, "manager"); err != nil {
		t.Fatalf("save raw policy: %v", err)
	}
	if err := rawStore.Close(); err != nil {
		t.Fatalf("close raw policy store: %v", err)
	}

	store, err := OpenPolicyStore(dir, "adam-n200")
	if err != nil {
		t.Fatalf("OpenPolicyStore: %v", err)
	}
	defer store.Close()
	got, err := store.Get(ctx, "guild", "channel")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AcceptFrom[0] != "legacy-bot" || got.DelegateTo[0] != "legacy-bot" || got.DelegateSkills[0] != "review" || got.CoPresentFrom[0] != "legacy-bot" || !got.AutoDelegateEnabled || !got.RemoteToolPolicy.AllowMemoryWrite {
		t.Fatalf("legacy frontend allowlists or operator policy were not preserved: %+v", got)
	}
	if len(got.AcceptFromRuntimes) != 1 || got.AcceptFromRuntimes[0] != "runtime-bot" || len(got.DelegateTargets) != 1 || got.DelegateTargets[0].RuntimeAgentID != "runtime-bot" || got.DelegateTargets[0].SkillID != "general/task" || len(got.CoPresentFromRuntimes) != 1 || got.CoPresentFromRuntimes[0] != "runtime-bot" || len(got.CoPresentTargetChannels) != 1 {
		t.Fatalf("runtime allowlist fields were not preserved: %+v", got)
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
	got, gotRef, err := store.FetchObject(ctx, "task_object", "artifact-image")
	if err != nil {
		t.Fatalf("FetchObject: %v", err)
	}
	if string(got) != string(content) || gotRef.Digest != "sha256:"+sha256ForTest(content) {
		t.Fatalf("bad fetch got=%q ref=%+v", got, gotRef)
	}
	secondContent := []byte("second-image")
	secondRef, err := store.PutObject(ctx, "task_second", "artifact-image", "other.png", "image/png", secondContent, 1)
	if err != nil {
		t.Fatalf("PutObject second task: %v", err)
	}
	gotAgain, gotAgainRef, err := store.FetchObject(ctx, "task_object", "artifact-image")
	if err != nil {
		t.Fatalf("FetchObject first task after colliding artifact id: %v", err)
	}
	if string(gotAgain) != string(content) || gotAgainRef.TaskID != "task_object" || secondRef.TaskID != "task_second" {
		t.Fatalf("object refs were not scoped by task: first=%q ref=%+v second=%+v", gotAgain, gotAgainRef, secondRef)
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
	gets    int
}

func newMemoryObjectBackend() *memoryObjectBackend {
	return &memoryObjectBackend{objects: map[string][]byte{}}
}

func (b *memoryObjectBackend) PutObject(_ context.Context, bucket string, key string, content []byte, _ string) error {
	b.objects[bucket+"/"+key] = append([]byte(nil), content...)
	return nil
}

func (b *memoryObjectBackend) GetObject(_ context.Context, bucket string, key string) ([]byte, error) {
	b.gets++
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

func TestValidateInboundRuntimeTrustedSenderDefaultsAllSkills(t *testing.T) {
	policy := ChannelA2APolicy{
		Enabled:            true,
		AcceptFromRuntimes: []string{"peer-runtime"},
	}
	for _, skillID := range []string{"task", "general/task", "ch-2cbaf623/task", "danger/admin"} {
		if err := policy.ValidateInboundRuntime("peer-runtime", skillID); err != nil {
			t.Fatalf("ValidateInboundRuntime(%q): %v", skillID, err)
		}
	}
}

func sha256ForTest(b []byte) string { sum := sha256.Sum256(b); return fmt.Sprintf("%x", sum[:]) }
