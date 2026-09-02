package websharerelay

import (
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const testHostToken = "test-relay-token"

func TestStaticAssetsAreNotImmutableCached(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/assets/App.js", nil)
	rec := httptest.NewRecorder()
	serveStaticFallback(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestHostRequiresBearerAuth(t *testing.T) {
	server := newTestRelay(t)
	defer server.Close()

	_, resp, err := dialRelay(server.URL, "auth-room", "host", "")
	if err == nil {
		t.Fatalf("host without bearer token unexpectedly connected")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		if resp == nil {
			t.Fatalf("host without bearer token response = nil err=%v", err)
		}
		t.Fatalf("host without bearer token status = %d err=%v", resp.StatusCode, err)
	}

	host, resp, err := dialRelay(server.URL, "auth-room", "host", testHostToken)
	if err != nil {
		t.Fatalf("host with bearer token: %v status=%s", err, responseStatus(resp))
	}
	defer host.Close()
}

func TestSecondHostRejectedWithClose4009(t *testing.T) {
	server := newTestRelay(t)
	defer server.Close()

	host, _, err := dialRelay(server.URL, "one-host", "host", testHostToken)
	if err != nil {
		t.Fatalf("first host: %v", err)
	}
	defer host.Close()
	waitForRelayRooms(t, server.URL, 1)

	second, resp, err := dialRelay(server.URL, "one-host", "host", testHostToken)
	if err != nil {
		t.Fatalf("second host handshake should upgrade before close: %v status=%s", err, responseStatus(resp))
	}
	defer second.Close()
	expectCloseCode(t, second, CloseSecondHost)
}

func TestGuestMissingRoomRejectedWithClose4004(t *testing.T) {
	server := newTestRelay(t)
	defer server.Close()

	guest, resp, err := dialRelay(server.URL, "missing", "guest", "")
	if err != nil {
		t.Fatalf("guest missing room handshake should upgrade before close: %v status=%s", err, responseStatus(resp))
	}
	defer guest.Close()
	expectCloseCode(t, guest, CloseGuestMissingRoom)
}

func TestMalformedBinaryFrameClosesConnection(t *testing.T) {
	server := newTestRelay(t)
	defer server.Close()

	host, _, err := dialRelay(server.URL, "bad-frame", "host", testHostToken)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	defer host.Close()
	guest, _, err := dialRelay(server.URL, "bad-frame", "guest", "")
	if err != nil {
		t.Fatalf("guest: %v", err)
	}
	defer guest.Close()

	if err := guest.WriteMessage(websocket.BinaryMessage, []byte{0x01, 0x02, 0x03}); err != nil {
		t.Fatalf("guest write short frame: %v", err)
	}
	expectCloseCode(t, guest, CloseBadFrame)
}

func TestBinaryFramesRouteBetweenGuestAndHost(t *testing.T) {
	server := newTestRelay(t)
	defer server.Close()

	host, _, err := dialRelay(server.URL, "route", "host", testHostToken)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	defer host.Close()
	guest, _, err := dialRelay(server.URL, "route", "guest", "")
	if err != nil {
		t.Fatalf("guest: %v", err)
	}
	defer guest.Close()

	if err := guest.WriteMessage(websocket.BinaryMessage, frame(99, []byte{0xde, 0xad, 0xbe, 0xef})); err != nil {
		t.Fatalf("guest write: %v", err)
	}
	messageType, got, err := readMessage(host)
	if err != nil {
		t.Fatalf("host read forwarded guest frame: %v", err)
	}
	if messageType != websocket.BinaryMessage {
		t.Fatalf("host message type = %d", messageType)
	}
	peerID, payload, err := parsePeerFrame(got, DefaultMaxFrameBytes)
	if err != nil {
		t.Fatalf("parse forwarded guest frame: %v", err)
	}
	if peerID != 1 || string(payload) != string([]byte{0xde, 0xad, 0xbe, 0xef}) {
		t.Fatalf("host got peer=%d payload=%x", peerID, payload)
	}

	reply := frame(1, []byte("opaque-reply"))
	if err := host.WriteMessage(websocket.BinaryMessage, reply); err != nil {
		t.Fatalf("host targeted write: %v", err)
	}
	messageType, got, err = readMessage(guest)
	if err != nil {
		t.Fatalf("guest read targeted host frame: %v", err)
	}
	if messageType != websocket.BinaryMessage || string(got) != string(reply) {
		t.Fatalf("guest targeted got type=%d frame=%x", messageType, got)
	}

	secondGuest, _, err := dialRelay(server.URL, "route", "guest", "")
	if err != nil {
		t.Fatalf("second guest: %v", err)
	}
	defer secondGuest.Close()

	broadcast := frame(0, []byte("opaque-broadcast"))
	if err := host.WriteMessage(websocket.BinaryMessage, broadcast); err != nil {
		t.Fatalf("host broadcast write: %v", err)
	}
	for name, conn := range map[string]*websocket.Conn{"guest": guest, "secondGuest": secondGuest} {
		messageType, got, err = readMessage(conn)
		if err != nil {
			t.Fatalf("%s read broadcast: %v", name, err)
		}
		if messageType != websocket.BinaryMessage || string(got) != string(broadcast) {
			t.Fatalf("%s broadcast got type=%d frame=%x", name, messageType, got)
		}
	}
}

func TestHostDisconnectClosesGuests(t *testing.T) {
	server := newTestRelay(t)
	defer server.Close()

	host, _, err := dialRelay(server.URL, "closing", "host", testHostToken)
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	guest, _, err := dialRelay(server.URL, "closing", "guest", "")
	if err != nil {
		t.Fatalf("guest: %v", err)
	}
	defer guest.Close()

	if err := host.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done")); err != nil {
		t.Fatalf("host close write: %v", err)
	}
	_ = host.Close()

	closeErr := expectCloseCode(t, guest, websocket.CloseNormalClosure)
	if closeErr.Text != "room-closed" {
		t.Fatalf("guest close reason = %q", closeErr.Text)
	}
}

func newTestRelay(t *testing.T) *httptest.Server {
	t.Helper()
	cfg := DefaultConfig()
	cfg.HostToken = testHostToken
	cfg.WriteTimeout = time.Second
	relay, err := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return httptest.NewServer(relay)
}

func waitForRelayRooms(t *testing.T, baseURL string, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var last Stats
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/healthz")
		if err != nil {
			lastErr = err
			time.Sleep(10 * time.Millisecond)
			continue
		}
		func() {
			defer resp.Body.Close()
			lastErr = json.NewDecoder(resp.Body).Decode(&last)
		}()
		if lastErr == nil && last.Rooms == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("relay rooms did not reach %d: last err=%v", want, lastErr)
	}
	t.Fatalf("relay rooms = %d guests=%d, want rooms=%d", last.Rooms, last.Guests, want)
}

func dialRelay(baseURL, roomID, role, token string) (*websocket.Conn, *http.Response, error) {
	url := "ws" + strings.TrimPrefix(baseURL, "http") + "/r/" + roomID + "?role=" + role
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	return websocket.DefaultDialer.Dial(url, header)
}

func readMessage(conn *websocket.Conn) (int, []byte, error) {
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	return conn.ReadMessage()
}

func expectCloseCode(t *testing.T, conn *websocket.Conn, code int) *websocket.CloseError {
	t.Helper()
	_, _, err := readMessage(conn)
	if err == nil {
		t.Fatalf("expected close code %d, got message", code)
	}
	closeErr, ok := err.(*websocket.CloseError)
	if !ok {
		t.Fatalf("expected close code %d, got %T: %v", code, err, err)
	}
	if closeErr.Code != code {
		t.Fatalf("close code = %d text=%q, want %d", closeErr.Code, closeErr.Text, code)
	}
	return closeErr
}

func frame(peerID uint32, payload []byte) []byte {
	out := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(out[:4], peerID)
	copy(out[4:], payload)
	return out
}

func responseStatus(resp *http.Response) string {
	if resp == nil {
		return "<nil>"
	}
	return resp.Status
}
