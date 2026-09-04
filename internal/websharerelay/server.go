package websharerelay

import (
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

type Server struct {
	cfg      Config
	hub      *Hub
	logger   *slog.Logger
	upgrader websocket.Upgrader
}

func NewServer(cfg Config, logger *slog.Logger) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	hub := NewHub(cfg, logger)
	return &Server{
		cfg:    cfg,
		hub:    hub,
		logger: logger,
		upgrader: websocket.Upgrader{
			EnableCompression: false,
			CheckOrigin:       func(*http.Request) bool { return true },
		},
	}, nil
}

func (s *Server) Hub() *Hub { return s.hub }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/healthz":
		s.handleHealthz(w, r)
	case strings.HasPrefix(r.URL.Path, "/r/"):
		s.handleRelay(w, r)
	default:
		serveStaticFallback(w, r)
	}
}

func (s *Server) MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			http.NotFound(w, r)
			return
		}
		stats := s.hub.Stats()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		writeMetric(w, "webshare_relay_rooms", "Current relay rooms.", "gauge", intString(stats.Rooms))
		writeMetric(w, "webshare_relay_hosts", "Current relay host peers.", "gauge", intString(stats.Hosts))
		writeMetric(w, "webshare_relay_guests", "Current relay guest peers.", "gauge", intString(stats.Guests))
		writeMetric(w, "webshare_relay_frames_guest_to_host_total", "Relay frames forwarded from guests to hosts.", "counter", uint64String(stats.FramesGuestToHost))
		writeMetric(w, "webshare_relay_frames_host_to_guest_total", "Relay frames forwarded from hosts to guests.", "counter", uint64String(stats.FramesHostToGuest))
		writeMetric(w, "webshare_relay_bytes_guest_to_host_total", "Relay bytes forwarded from guests to hosts.", "counter", uint64String(stats.BytesGuestToHost))
		writeMetric(w, "webshare_relay_bytes_host_to_guest_total", "Relay bytes forwarded from hosts to guests.", "counter", uint64String(stats.BytesHostToGuest))
		writeMetric(w, "webshare_relay_malformed_frames_total", "Relay binary frames rejected as malformed.", "counter", uint64String(stats.MalformedFrames))
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	stats := s.hub.Stats()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":                     true,
		"rooms":                  stats.Rooms,
		"guests":                 stats.Guests,
		"hosts":                  stats.Hosts,
		"frames_guest_to_host":   stats.FramesGuestToHost,
		"frames_host_to_guest":   stats.FramesHostToGuest,
		"bytes_guest_to_host":    stats.BytesGuestToHost,
		"bytes_host_to_guest":    stats.BytesHostToGuest,
		"malformed_frames_total": stats.MalformedFrames,
	})
}

func (s *Server) handleRelay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	roomID := strings.TrimPrefix(r.URL.Path, "/r/")
	if roomID == "" || strings.Contains(roomID, "/") {
		http.Error(w, "invalid room", http.StatusBadRequest)
		return
	}
	role := r.URL.Query().Get("role")
	if role != "host" && role != "guest" {
		http.Error(w, "invalid role", http.StatusBadRequest)
		return
	}
	if role == "host" && !s.authorizeHost(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Debug("webshare relay upgrade failed", "room", roomID, "role", role, "err", err)
		return
	}
	conn.EnableWriteCompression(false)
	conn.SetReadLimit(s.cfg.MaxFrameBytes)

	var p *peer
	var closeCode int
	var closeText string
	if role == "host" {
		p, closeCode, closeText = s.hub.registerHost(roomID, conn)
	} else {
		p, closeCode, closeText = s.hub.registerGuest(roomID, conn)
	}
	if closeCode != 0 {
		(&peer{conn: conn}).closeWith(closeCode, closeText, s.cfg.WriteTimeout)
		return
	}

	if role == "host" {
		s.readHost(p)
	} else {
		s.readGuest(p)
	}
}

func (s *Server) authorizeHost(r *http.Request) bool {
	if s.cfg.HostToken == "" {
		return false
	}
	auth := strings.TrimSpace(r.Header.Get("Authorization"))
	fields := strings.Fields(auth)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return false
	}
	if len(fields[1]) != len(s.cfg.HostToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(fields[1]), []byte(s.cfg.HostToken)) == 1
}

func (s *Server) readHost(p *peer) {
	defer p.conn.Close()
	defer s.hub.hostGone(p)
	s.readLoop(p, s.cfg.HostIdleTimeout, s.hub.forwardFromHost)
}

func (s *Server) readGuest(p *peer) {
	defer p.conn.Close()
	defer s.hub.guestGone(p)
	s.readLoop(p, s.cfg.GuestIdleTimeout, s.hub.forwardFromGuest)
}

func (s *Server) readLoop(p *peer, idleTimeout time.Duration, forward func(*peer, []byte) error) {
	for {
		if idleTimeout > 0 {
			_ = p.conn.SetReadDeadline(time.Now().Add(idleTimeout))
			p.conn.SetPongHandler(func(string) error {
				return p.conn.SetReadDeadline(time.Now().Add(idleTimeout))
			})
		}
		messageType, frame, err := p.conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.BinaryMessage {
			p.closeWith(CloseBadFrame, "binary frames only", s.cfg.WriteTimeout)
			return
		}
		if _, _, err := parsePeerFrame(frame, s.cfg.MaxFrameBytes); err != nil {
			s.hub.recordMalformedFrame()
			p.closeWith(CloseBadFrame, "bad frame", s.cfg.WriteTimeout)
			return
		}
		if err := forward(p, frame); err != nil {
			return
		}
	}
}

func writeMetric(w http.ResponseWriter, name, help, metricType, value string) {
	_, _ = w.Write([]byte("# HELP "))
	_, _ = w.Write([]byte(name))
	_, _ = w.Write([]byte(" "))
	_, _ = w.Write([]byte(help))
	_, _ = w.Write([]byte("\n# TYPE "))
	_, _ = w.Write([]byte(name))
	_, _ = w.Write([]byte(" "))
	_, _ = w.Write([]byte(metricType))
	_, _ = w.Write([]byte("\n"))
	_, _ = w.Write([]byte(name))
	_, _ = w.Write([]byte(" "))
	_, _ = w.Write([]byte(value))
	_, _ = w.Write([]byte("\n"))
}

func intString(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func uint64String(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
