package webshare

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func fixedBytes(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = seed + byte(i)
	}
	return b
}

func TestLinkParseFormatCapabilities(t *testing.T) {
	roomKey := fixedBytes(RoomKeySize, 1)
	writeToken := fixedBytes(WriteTokenSize, 101)

	viewLink, err := FormatViewLink("https://relay.example/", "wr_room", roomKey)
	if err != nil {
		t.Fatal(err)
	}
	if viewLink != "https://relay.example#/join/wr_room."+strings.TrimPrefix(viewLink, "https://relay.example#/join/wr_room.") {
		t.Fatalf("unexpected link shape: %s", viewLink)
	}
	view, err := ParseJoinLink(viewLink)
	if err != nil {
		t.Fatal(err)
	}
	if view.RoomID != "wr_room" || !bytes.Equal(view.RoomKey, roomKey) || view.CanWrite || len(view.WriteToken) != 0 {
		t.Fatalf("bad view parse: %+v", view)
	}
	viewCaps := CapabilitiesForWrite(view.CanWrite)
	if !viewCaps.View || viewCaps.Write || viewCaps.PostChannelMessage {
		t.Fatalf("view capabilities can mutate: %+v", viewCaps)
	}

	writeLink, err := FormatWriteLink("https://relay.example", "wr_room", roomKey, writeToken)
	if err != nil {
		t.Fatal(err)
	}
	write, err := ParseJoinLink(writeLink)
	if err != nil {
		t.Fatal(err)
	}
	if !write.CanWrite || !bytes.Equal(write.RoomKey, roomKey) || !bytes.Equal(write.WriteToken, writeToken) {
		t.Fatalf("bad write parse: %+v", write)
	}
	writeCaps := CapabilitiesForWrite(write.CanWrite)
	if !writeCaps.Write || !writeCaps.SendAgentPrompt || !writeCaps.Upload || writeCaps.MentionRoles || writeCaps.MentionEveryone || writeCaps.MentionHere {
		t.Fatalf("bad write capabilities: %+v", writeCaps)
	}
}

func TestTokenHashFingerprintAndVerify(t *testing.T) {
	token := fixedBytes(WriteTokenSize, 7)
	hash := TokenHash(token)
	if len(hash) != 32 {
		t.Fatalf("hash length = %d", len(hash))
	}
	if !VerifyTokenHash(token, hash) {
		t.Fatal("valid token did not verify")
	}
	wrong := append([]byte(nil), token...)
	wrong[0] ^= 0xff
	if VerifyTokenHash(wrong, hash) {
		t.Fatal("wrong token verified")
	}
	if fp := TokenFingerprint(token); !strings.HasPrefix(fp, "sha256:") || len(fp) <= len("sha256:") {
		t.Fatalf("bad fingerprint %q", fp)
	}
}

func TestRoomKeyWrappingUsesLocalMasterKey(t *testing.T) {
	dataDir := t.TempDir()
	roomKey := fixedBytes(RoomKeySize, 33)
	wrapped, err := WrapRoomKey(dataDir, roomKey)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(wrapped, roomKey) {
		t.Fatal("wrapped room key contains plaintext room key")
	}
	unwrapped, err := UnwrapRoomKey(dataDir, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unwrapped, roomKey) {
		t.Fatalf("unwrapped key mismatch")
	}
	again, err := UnwrapRoomKey(dataDir, wrapped)
	if err != nil || !bytes.Equal(again, roomKey) {
		t.Fatalf("stable unwrap failed: %v", err)
	}
	info, err := os.Stat(MasterKeyPath(dataDir))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("master key mode = %o, want 0600", got)
	}
}

func TestStoreActiveUniquenessRevokeChildThreadAndAttachmentScope(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	roomKey := fixedBytes(RoomKeySize, 1)
	writeToken := fixedBytes(WriteTokenSize, 2)
	share, err := store.CreateShare(ctx, CreateShareRequest{
		ShareID: "ws_one", GuildID: "g1", TargetType: TargetChannel, TargetID: "c1", OpenerUserID: "u1",
		OpenerUsername: "Alice", RelayURL: "wss://relay/r", PublicBaseURL: "https://relay", RoomID: "wr_one",
		RoomKey: roomKey, WriteToken: writeToken, Capabilities: WriteCapabilities(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.UnwrapRoomKey(share); err != nil || !bytes.Equal(got, roomKey) {
		t.Fatalf("stored room key unwrap mismatch: %v", err)
	}
	_, err = store.CreateShare(ctx, CreateShareRequest{
		ShareID: "ws_dupe", GuildID: "g1", TargetType: TargetChannel, TargetID: "c1", OpenerUserID: "u1",
		RelayURL: "wss://relay/r", PublicBaseURL: "https://relay", RoomID: "wr_dupe", RoomKey: roomKey, WriteToken: writeToken,
	})
	if !errors.Is(err, ErrActiveShare) {
		t.Fatalf("duplicate active share error = %v", err)
	}
	if err := store.Revoke(ctx, "ws_one", "admin", "done"); err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateShare(ctx, CreateShareRequest{
		ShareID: "ws_two", GuildID: "g1", TargetType: TargetChannel, TargetID: "c1", OpenerUserID: "u1",
		RelayURL: "wss://relay/r", PublicBaseURL: "https://relay", RoomID: "wr_two", RoomKey: roomKey, WriteToken: writeToken,
	})
	if err != nil {
		t.Fatalf("create after revoke: %v", err)
	}
	active, err := store.ListActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ShareID != second.ShareID {
		t.Fatalf("active shares = %+v", active)
	}

	if err := store.RegisterManagedChildThread(ctx, ManagedChildThread{ShareID: second.ShareID, ParentChannelID: "c1", ThreadID: "t1", Name: "child"}); err != nil {
		t.Fatal(err)
	}
	thread, err := store.ResolveManagedChildThread(ctx, second.ShareID, "t1")
	if err != nil || thread.ParentChannelID != "c1" {
		t.Fatalf("resolve child thread = %+v err=%v", thread, err)
	}
	if _, err := store.ResolveManagedChildThread(ctx, "wrong_share", "t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("wrong share child resolve = %v", err)
	}
	if err := store.UnregisterManagedChildThread(ctx, second.ShareID, "t1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveManagedChildThread(ctx, second.ShareID, "t1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unregistered child resolve = %v", err)
	}

	ref, err := store.IssueAttachmentRef(ctx, AttachmentRef{ShareID: second.ShareID, TargetID: "t1", MessageID: "m1", AttachmentID: "a1", Filename: "note.txt", Size: 12, ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := store.ResolveAttachmentRef(ctx, second.ShareID, ref.ID)
	if err != nil || resolved.AttachmentID != "a1" || resolved.TargetID != "t1" {
		t.Fatalf("resolve attachment = %+v err=%v", resolved, err)
	}
	if _, err := store.ResolveAttachmentRef(ctx, "wrong_share", ref.ID); !errors.Is(err, ErrScopeMismatch) {
		t.Fatalf("wrong share attachment resolve = %v", err)
	}
	expired, err := store.IssueAttachmentRef(ctx, AttachmentRef{ShareID: second.ShareID, TargetID: "t1", MessageID: "m2", AttachmentID: "a2", Filename: "old.txt", Size: 1, ExpiresAt: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveAttachmentRef(ctx, second.ShareID, expired.ID); !errors.Is(err, ErrExpiredRef) {
		t.Fatalf("expired attachment resolve = %v", err)
	}
}

func TestStorePruneStaleExpiresAndDeletesWebShareState(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	roomKey := fixedBytes(RoomKeySize, 21)
	writeToken := fixedBytes(WriteTokenSize, 61)
	create := func(id, roomID, targetID string, status Status, at time.Time) {
		t.Helper()
		_, err := store.CreateShare(ctx, CreateShareRequest{
			ShareID: id, GuildID: "g1", TargetType: TargetChannel, TargetID: targetID, OpenerUserID: "u1",
			RelayURL: "wss://relay/r", PublicBaseURL: "https://relay", RoomID: roomID,
			RoomKey: roomKey, WriteToken: writeToken, Capabilities: WriteCapabilities(), Status: status, Now: at,
		})
		if err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	create("ws_old_active", "wr_old_active", "c-old", StatusActive, old)
	create("ws_fresh_active", "wr_fresh_active", "c-fresh", StatusActive, now)
	create("ws_revoked_old", "wr_revoked_old", "c-revoked", StatusRevoked, old)
	if err := store.RegisterManagedChildThread(ctx, ManagedChildThread{ShareID: "ws_revoked_old", ParentChannelID: "c-revoked", ThreadID: "t-revoked", Name: "old child", CreatedAt: old}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordEvent(ctx, Event{ShareID: "ws_revoked_old", Type: "relay.replay", TargetID: "c-revoked", CreatedAt: old}); err != nil {
		t.Fatal(err)
	}
	if accepted, err := store.AcceptPeerSequence(ctx, "ws_revoked_old", 7, 1, old); err != nil || !accepted {
		t.Fatalf("accept peer sequence accepted=%v err=%v", accepted, err)
	}
	if claimed, err := store.ClaimAction(ctx, "ws_revoked_old", "act-1", "prompt", old); err != nil || !claimed {
		t.Fatalf("claim action claimed=%v err=%v", claimed, err)
	}
	expiredRef, err := store.IssueAttachmentRef(ctx, AttachmentRef{ShareID: "ws_fresh_active", TargetID: "c-fresh", MessageID: "m-expired", AttachmentID: "a-expired", Filename: "old.txt", Size: 1, ExpiresAt: old})
	if err != nil {
		t.Fatal(err)
	}
	liveRef, err := store.IssueAttachmentRef(ctx, AttachmentRef{ShareID: "ws_fresh_active", TargetID: "c-fresh", MessageID: "m-live", AttachmentID: "a-live", Filename: "live.txt", Size: 1, ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.PruneStale(ctx, now, 24*time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExpiredShares != 1 || result.DeletedShares != 1 || result.DeletedEvents != 1 || result.DeletedManagedThreads != 1 || result.DeletedAttachmentRefs != 1 || result.DeletedPeerSequences != 1 || result.DeletedActionReceipts != 1 {
		t.Fatalf("unexpected prune result: %+v", result)
	}
	oldActive, err := store.GetShare(ctx, "ws_old_active")
	if err != nil {
		t.Fatal(err)
	}
	if oldActive.Status != StatusExpired {
		t.Fatalf("old active status = %s, want expired", oldActive.Status)
	}
	if _, err := store.GetShare(ctx, "ws_revoked_old"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked old share lookup = %v", err)
	}
	active, err := store.ListActive(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ShareID != "ws_fresh_active" {
		t.Fatalf("active shares = %+v", active)
	}
	if _, err := store.ResolveAttachmentRef(ctx, "ws_fresh_active", expiredRef.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired ref lookup = %v", err)
	}
	if _, err := store.ResolveAttachmentRef(ctx, "ws_fresh_active", liveRef.ID); err != nil {
		t.Fatalf("live ref lookup: %v", err)
	}
	var peerSequences, actionReceipts int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webshare_peer_sequences WHERE share_id=?`, "ws_revoked_old").Scan(&peerSequences); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM webshare_action_receipts WHERE share_id=?`, "ws_revoked_old").Scan(&actionReceipts); err != nil {
		t.Fatal(err)
	}
	if peerSequences != 0 || actionReceipts != 0 {
		t.Fatalf("retained replay/idempotency rows peer=%d action=%d", peerSequences, actionReceipts)
	}
}

func TestDisplayNameSanitizer(t *testing.T) {
	redact := func(s string) string { return strings.ReplaceAll(s, "secret", "") }
	got := SanitizeDisplayName(" \x00<@123456> @everyone Alice secret\n", redact)
	if strings.Contains(got, "@") || strings.Contains(got, "secret") || strings.ContainsAny(got, "\x00\n") {
		t.Fatalf("display name not sanitized: %q", got)
	}
	if got == "web" || !strings.Contains(got, "Alice") {
		t.Fatalf("unexpected sanitized display name: %q", got)
	}
	long := SanitizeDisplayName(strings.Repeat("界", MaxDisplayNameRunes+5), nil)
	if len([]rune(long)) != MaxDisplayNameRunes {
		t.Fatalf("length cap failed: %d", len([]rune(long)))
	}
	if got := SanitizeDisplayName("secret", redact); got != "web" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestAEADEnvelopeDirectionADAndReplayRejection(t *testing.T) {
	roomKey := fixedBytes(RoomKeySize, 9)
	meta := FrameMeta{RoomID: "wr_room", Direction: DirectionGuestToHost, PeerID: 7, Sequence: 1, Type: FrameTypeClientAction}
	env, err := SealEnvelope(roomKey, meta, []byte(`{"type":"hello"}`))
	if err != nil {
		t.Fatal(err)
	}
	replay := NewReplayDetector()
	plain, err := OpenEnvelope(roomKey, "wr_room", DirectionGuestToHost, env, replay)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != `{"type":"hello"}` {
		t.Fatalf("plaintext = %s", plain)
	}
	if _, err := OpenEnvelope(roomKey, "wr_room", DirectionGuestToHost, env, replay); !errors.Is(err, ErrReplayFrame) {
		t.Fatalf("replay error = %v", err)
	}
	if _, err := OpenEnvelope(roomKey, "wr_room", DirectionHostToGuest, env, nil); err == nil {
		t.Fatal("direction change did not break AEAD associated data")
	}
	meta.Sequence = 2
	env2, err := SealEnvelope(roomKey, meta, []byte("next"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenEnvelope(roomKey, "wr_room", DirectionGuestToHost, env2, replay); err != nil {
		t.Fatalf("new sequence rejected: %v", err)
	}
}

func TestSafeAttachmentFilenameAndUploadDir(t *testing.T) {
	if got := SafeAttachmentFilename("../../bad\x00/name.txt"); got != "name.txt" {
		t.Fatalf("safe filename = %q", got)
	}
	if got := UploadDir("/project", "ws_1", "up_1"); !strings.HasSuffix(got, ".kiro-bot/attachments/webshare-ws_1/up_1") {
		t.Fatalf("upload dir = %q", got)
	}
}
