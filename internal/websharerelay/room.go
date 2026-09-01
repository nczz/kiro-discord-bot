package websharerelay

import (
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Hub struct {
	cfg    Config
	logger *slog.Logger

	mu    sync.Mutex
	rooms map[string]*room
}

type Stats struct {
	Rooms  int
	Guests int
}

type room struct {
	id         string
	host       *peer
	guests     map[uint32]*peer
	nextPeerID uint32
	closed     bool
}

type peer struct {
	id        uint32
	role      string
	roomID    string
	conn      *websocket.Conn
	writeMu   sync.Mutex
	closeOnce sync.Once
}

func NewHub(cfg Config, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{cfg: cfg, logger: logger, rooms: make(map[string]*room)}
}

func (h *Hub) Stats() Stats {
	h.mu.Lock()
	defer h.mu.Unlock()
	stats := Stats{Rooms: len(h.rooms)}
	for _, r := range h.rooms {
		stats.Guests += len(r.guests)
	}
	return stats
}

func (h *Hub) registerHost(roomID string, conn *websocket.Conn) (*peer, int, string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if existing := h.rooms[roomID]; existing != nil && existing.host != nil && !existing.closed {
		return nil, CloseSecondHost, "host already connected"
	}
	if _, ok := h.rooms[roomID]; !ok && len(h.rooms) >= h.cfg.MaxRooms {
		return nil, CloseRoomFull, "room limit reached"
	}

	r := h.rooms[roomID]
	if r == nil || r.closed {
		r = &room{id: roomID, guests: make(map[uint32]*peer), nextPeerID: 1}
		h.rooms[roomID] = r
	}
	p := &peer{id: 0, role: "host", roomID: roomID, conn: conn}
	r.host = p
	h.logger.Info("webshare relay host connected", "room", roomID)
	return p, 0, ""
}

func (h *Hub) registerGuest(roomID string, conn *websocket.Conn) (*peer, int, string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	r := h.rooms[roomID]
	if r == nil || r.closed || r.host == nil {
		return nil, CloseGuestMissingRoom, "room not found"
	}
	if len(r.guests) >= h.cfg.MaxPeersPerRoom {
		return nil, CloseRoomFull, "room is full"
	}
	id := r.nextPeerID
	for id == 0 || r.guests[id] != nil {
		id++
	}
	r.nextPeerID = id + 1
	p := &peer{id: id, role: "guest", roomID: roomID, conn: conn}
	r.guests[id] = p
	h.logger.Info("webshare relay guest connected", "room", roomID, "peer", id)
	return p, 0, ""
}

func (h *Hub) hostGone(p *peer) {
	h.closeRoom(p.roomID, p)
}

func (h *Hub) guestGone(p *peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.rooms[p.roomID]
	if r == nil {
		return
	}
	if r.guests[p.id] == p {
		delete(r.guests, p.id)
		h.logger.Info("webshare relay guest disconnected", "room", p.roomID, "peer", p.id)
	}
}

func (h *Hub) closeRoom(roomID string, host *peer) {
	var guests []*peer
	h.mu.Lock()
	r := h.rooms[roomID]
	if r != nil {
		r.closed = true
		if r.host == host {
			r.host = nil
		}
		for _, guest := range r.guests {
			guests = append(guests, guest)
		}
		delete(h.rooms, roomID)
	}
	h.mu.Unlock()

	for _, guest := range guests {
		guest.closeWith(websocket.CloseNormalClosure, "room-closed", h.cfg.WriteTimeout)
	}
	h.logger.Info("webshare relay room closed", "room", roomID)
}

func (h *Hub) forwardFromGuest(p *peer, frame []byte) error {
	_, payload, err := parsePeerFrame(frame, h.cfg.MaxFrameBytes)
	if err != nil {
		return err
	}
	out := buildPeerFrame(p.id, payload)

	h.mu.Lock()
	r := h.rooms[p.roomID]
	var host *peer
	if r != nil && !r.closed {
		host = r.host
	}
	h.mu.Unlock()
	if host == nil {
		return net.ErrClosed
	}
	return host.writeBinary(out, h.cfg.WriteTimeout)
}

func (h *Hub) forwardFromHost(p *peer, frame []byte) error {
	targetID, _, err := parsePeerFrame(frame, h.cfg.MaxFrameBytes)
	if err != nil {
		return err
	}

	var recipients []*peer
	h.mu.Lock()
	r := h.rooms[p.roomID]
	if r != nil && !r.closed && r.host == p {
		if targetID == 0 {
			for _, guest := range r.guests {
				recipients = append(recipients, guest)
			}
		} else if guest := r.guests[targetID]; guest != nil {
			recipients = append(recipients, guest)
		}
	}
	h.mu.Unlock()

	for _, recipient := range recipients {
		if err := recipient.writeBinary(frame, h.cfg.WriteTimeout); err != nil {
			recipient.closeWith(websocket.CloseInternalServerErr, "write failed", h.cfg.WriteTimeout)
			h.guestGone(recipient)
		}
	}
	return nil
}

func (p *peer) writeBinary(frame []byte, timeout time.Duration) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if timeout > 0 {
		_ = p.conn.SetWriteDeadline(time.Now().Add(timeout))
	}
	return p.conn.WriteMessage(websocket.BinaryMessage, frame)
}

func (p *peer) closeWith(code int, text string, timeout time.Duration) {
	p.closeOnce.Do(func() {
		p.writeMu.Lock()
		defer p.writeMu.Unlock()
		if timeout > 0 {
			_ = p.conn.SetWriteDeadline(time.Now().Add(timeout))
		}
		_ = p.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, text), time.Now().Add(timeout))
		_ = p.conn.Close()
	})
}
