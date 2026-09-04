package bot

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/nczz/kiro-discord-bot/webshare"
)

type webshareHostLoop struct {
	cancel context.CancelFunc
	send   chan webshare.ServerEvent
}

func (b *Bot) startWebShareReconnectLoop(ctx context.Context) {
	if b == nil || b.webshareStore == nil || !b.webshareConfig.ready() {
		return
	}
	shares, err := b.webshareStore.ListActive(ctx)
	if err != nil {
		log.Printf("[webshare] list active shares: %v", err)
		return
	}
	for _, share := range shares {
		b.startWebShareHost(share)
	}
}

func (b *Bot) startWebShareHost(share webshare.Share) {
	if b == nil || b.webshareStore == nil || !b.webshareConfig.ready() || strings.TrimSpace(share.ShareID) == "" {
		return
	}
	b.webshareMu.Lock()
	if b.webshareHosts == nil {
		b.webshareHosts = make(map[string]*webshareHostLoop)
	}
	if _, exists := b.webshareHosts[share.ShareID]; exists {
		b.webshareMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	loop := &webshareHostLoop{cancel: cancel, send: make(chan webshare.ServerEvent, 64)}
	b.webshareHosts[share.ShareID] = loop
	b.webshareMu.Unlock()
	go b.runWebShareHost(ctx, share, loop.send)
}

func (b *Bot) stopWebShareHost(shareID string) {
	b.webshareMu.Lock()
	loop := b.webshareHosts[shareID]
	delete(b.webshareHosts, shareID)
	b.webshareMu.Unlock()
	if loop != nil && loop.cancel != nil {
		loop.cancel()
	}
}

func (b *Bot) stopAllWebShareHosts() {
	b.webshareMu.Lock()
	loops := b.webshareHosts
	b.webshareHosts = nil
	b.webshareMu.Unlock()
	for _, loop := range loops {
		if loop != nil && loop.cancel != nil {
			loop.cancel()
		}
	}
}

func (b *Bot) runWebShareHost(ctx context.Context, share webshare.Share, outbound <-chan webshare.ServerEvent) {
	backoff := b.webshareConfig.reconnectInitial()
	maxBackoff := b.webshareConfig.reconnectMax()
	for ctx.Err() == nil {
		current, err := b.webshareStore.GetShare(ctx, share.ShareID)
		if err != nil || current == nil || !current.Status.ActiveLocking() {
			return
		}
		roomKey, err := b.webshareStore.UnwrapRoomKey(current)
		if err != nil {
			log.Printf("[webshare] unwrap room key share=%s: %v", share.ShareID, err)
			return
		}
		if err := b.hostWebShareOnce(ctx, *current, roomKey, outbound); err != nil && ctx.Err() == nil {
			log.Printf("[webshare] host disconnected share=%s: %v", share.ShareID, err)
			b.recordWebShareAudit(*current, webshare.EventActionRejected, current.OpenerUserID, current.OpenerUsername, current.TargetID, false, "webshare_host_disconnected", map[string]any{"error": err.Error()})
		}
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (b *Bot) hostWebShareOnce(ctx context.Context, share webshare.Share, roomKey []byte, outbound <-chan webshare.ServerEvent) error {
	wsURL, err := webshareRelayURL(share.RelayURL, share.RoomID)
	if err != nil {
		return err
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+b.webshareConfig.resolvedHostToken())
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetReadLimit(b.webshareConfig.frameLimit())
	_ = b.webshareStore.MarkConnected(context.Background(), share.ShareID, time.Now())
	b.recordWebShareAudit(share, webshare.EventConnected, share.OpenerUserID, share.OpenerUsername, share.TargetID, true, "", nil)

	peerReplay := make(map[uint32]uint64)
	relayPeerBindings := make(map[uint32]uint32)
	var seqMu sync.Mutex
	var seq uint64
	var writeMu sync.Mutex
	nextSeq := func() uint64 { seqMu.Lock(); defer seqMu.Unlock(); seq++; return seq }
	sendEvent := func(peerID uint32, event webshare.ServerEvent) error {
		raw, _ := webshare.MarshalServerEvent(event)
		outEnv, err := webshare.SealEnvelope(roomKey, webshare.FrameMeta{RoomID: share.RoomID, Direction: webshare.DirectionHostToGuest, PeerID: peerID, Sequence: nextSeq(), Type: webshare.FrameTypeServerEvent}, raw)
		if err != nil {
			return err
		}
		outRaw, _ := json.Marshal(outEnv)
		writeMu.Lock()
		err = conn.WriteMessage(websocket.BinaryMessage, buildWebSharePeerFrame(peerID, outRaw))
		writeMu.Unlock()
		return err
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case event := <-outbound:
				if err := sendEvent(0, event); err != nil {
					log.Printf("[webshare] broadcast event share=%s: %v", share.ShareID, err)
					b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, share.OpenerUsername, share.TargetID, false, "webshare_broadcast_write_failed", map[string]any{"error": err.Error(), "event_type": event.Type})
					return
				}
			}
		}
	}()
	for ctx.Err() == nil {
		messageType, frame, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if messageType != websocket.BinaryMessage {
			continue
		}
		peerID, payload, err := parseWebSharePeerFrame(frame, b.webshareConfig.frameLimit())
		if err != nil {
			log.Printf("[webshare] ignored malformed relay frame share=%s: %v", share.ShareID, err)
			b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, share.OpenerUsername, share.TargetID, false, "malformed_frame", map[string]any{"error": err.Error()})
			continue
		}
		var env webshare.FrameEnvelope
		if err := json.Unmarshal(payload, &env); err != nil {
			log.Printf("[webshare] ignored malformed envelope share=%s relay_peer=%d: %v", share.ShareID, peerID, err)
			b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, share.OpenerUsername, share.TargetID, false, "malformed_frame", map[string]any{"relay_peer": peerID, "error": err.Error()})
			continue
		}
		if boundEnvelopePeer, ok := relayPeerBindings[peerID]; ok && boundEnvelopePeer != env.PeerID {
			log.Printf("[webshare] ignored peer rebinding share=%s relay_peer=%d bound=%d envelope=%d seq=%d", share.ShareID, peerID, boundEnvelopePeer, env.PeerID, env.Sequence)
			b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, share.OpenerUsername, share.TargetID, false, "peer_mismatch", map[string]any{"relay_peer": peerID, "bound_envelope_peer": boundEnvelopePeer, "envelope_peer": env.PeerID})
			continue
		}
		if last, ok := peerReplay[env.PeerID]; ok && env.Sequence <= last {
			log.Printf("[webshare] ignored replayed guest frame share=%s peer=%d seq=%d", share.ShareID, env.PeerID, env.Sequence)
			b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, share.OpenerUsername, share.TargetID, false, "replayed_frame", map[string]any{"peer": env.PeerID, "seq": env.Sequence})
			continue
		}
		plain, err := webshare.OpenEnvelope(roomKey, share.RoomID, webshare.DirectionGuestToHost, env, nil)
		if err != nil {
			log.Printf("[webshare] ignored bad encrypted envelope share=%s relay_peer=%d envelope_peer=%d seq=%d: %v", share.ShareID, peerID, env.PeerID, env.Sequence, err)
			b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, share.OpenerUsername, share.TargetID, false, "malformed_frame", map[string]any{"relay_peer": peerID, "envelope_peer": env.PeerID, "seq": env.Sequence, "error": err.Error()})
			continue
		}
		if _, ok := relayPeerBindings[peerID]; !ok {
			relayPeerBindings[peerID] = env.PeerID
		}
		accepted, err := b.webshareStore.AcceptPeerSequence(context.Background(), share.ShareID, env.PeerID, env.Sequence, time.Now())
		if err != nil {
			return err
		}
		if !accepted {
			log.Printf("[webshare] ignored persisted replayed guest frame share=%s peer=%d seq=%d", share.ShareID, env.PeerID, env.Sequence)
			b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, share.OpenerUsername, share.TargetID, false, "replayed_frame", map[string]any{"peer": env.PeerID, "seq": env.Sequence})
			continue
		}
		peerReplay[env.PeerID] = env.Sequence
		var action webshare.ClientAction
		if webshare.FrameType(env.Type) == webshare.FrameTypeClientAction || webshare.FrameType(env.Type) == webshare.FrameTypeHello {
			action, err = webshare.UnmarshalClientAction(plain)
			if err != nil {
				log.Printf("[webshare] ignored malformed action share=%s relay_peer=%d envelope_peer=%d seq=%d: %v", share.ShareID, peerID, env.PeerID, env.Sequence, err)
				b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, share.OpenerUsername, share.TargetID, false, "malformed_action", map[string]any{"relay_peer": peerID, "envelope_peer": env.PeerID, "seq": env.Sequence, "error": err.Error()})
				continue
			}
		} else {
			continue
		}
		_ = b.webshareStore.MarkPeerSeen(context.Background(), share.ShareID, time.Now())
		if err := sendEvent(peerID, b.HandleWebShareAction(context.Background(), share.ShareID, action)); err != nil {
			log.Printf("[webshare] write event share=%s: %v", share.ShareID, err)
			b.recordWebShareAudit(share, webshare.EventActionRejected, share.OpenerUserID, action.DisplayName, share.TargetID, false, "webshare_direct_write_failed", map[string]any{"error": err.Error(), "relay_peer": peerID, "envelope_peer": env.PeerID, "action": action.Type})
		}
	}
	return ctx.Err()
}

func (b *Bot) stopWebShareHostSoon(shareID string) {
	go func() {
		time.Sleep(100 * time.Millisecond)
		b.stopWebShareHost(shareID)
	}()
}

func webshareRelayURL(base, roomID string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return "", err
	}
	if u.Scheme == "http" {
		u.Scheme = "ws"
	} else if u.Scheme == "https" {
		u.Scheme = "wss"
	}
	u.Path = webshareRelayPath(u.Path, roomID)
	q := u.Query()
	q.Set("role", "host")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func webshareRelayPath(basePath, roomID string) string {
	prefix := strings.TrimRight(basePath, "/")
	if prefix == "" {
		prefix = "/r"
	} else if !strings.HasSuffix(prefix, "/r") {
		prefix += "/r"
	}
	return prefix + "/" + url.PathEscape(roomID)
}

func parseWebSharePeerFrame(frame []byte, maxBytes int64) (uint32, []byte, error) {
	if maxBytes > 0 && int64(len(frame)) > maxBytes {
		return 0, nil, fmt.Errorf("webshare relay frame exceeds limit")
	}
	if len(frame) < 4 {
		return 0, nil, fmt.Errorf("webshare relay frame too short")
	}
	return binary.BigEndian.Uint32(frame[:4]), frame[4:], nil
}

func buildWebSharePeerFrame(peerID uint32, payload []byte) []byte {
	frame := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(frame[:4], peerID)
	copy(frame[4:], payload)
	return frame
}
