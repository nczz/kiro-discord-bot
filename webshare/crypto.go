package webshare

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"sync"

	"crypto/sha256"
	"golang.org/x/crypto/hkdf"
)

const (
	ProtocolVersion byte = 1
	FrameInfo            = "kdb-webshare-v1"
)

type Direction string

const (
	DirectionHostToGuest Direction = "host_to_guest"
	DirectionGuestToHost Direction = "guest_to_host"
)

type FrameType uint8

const (
	FrameTypeHello        FrameType = 1
	FrameTypeClientAction FrameType = 2
	FrameTypeServerEvent  FrameType = 3
	FrameTypeAck          FrameType = 4
	FrameTypeBinary       FrameType = 5
)

type FrameMeta struct {
	RoomID    string
	Direction Direction
	PeerID    uint32
	Sequence  uint64
	Type      FrameType
}

type FrameKeys struct {
	HostToGuest []byte
	GuestToHost []byte
}

type FrameEnvelope struct {
	Version    byte   `json:"v"`
	Type       uint8  `json:"t"`
	Sequence   uint64 `json:"seq"`
	PeerID     uint32 `json:"peer"`
	Ciphertext string `json:"p"`
}

var (
	ErrReplayFrame  = errors.New("replayed webshare frame")
	ErrBadFrameMeta = errors.New("invalid webshare frame metadata")
)

func DeriveFrameKeys(roomKey []byte, roomID string) (FrameKeys, error) {
	if len(roomKey) != RoomKeySize {
		return FrameKeys{}, fmt.Errorf("room key must be %d bytes", RoomKeySize)
	}
	out := make([]byte, 64)
	r := hkdf.New(sha256.New, roomKey, []byte(roomID), []byte(FrameInfo))
	if _, err := r.Read(out); err != nil {
		return FrameKeys{}, err
	}
	return FrameKeys{HostToGuest: append([]byte(nil), out[:32]...), GuestToHost: append([]byte(nil), out[32:]...)}, nil
}

func keyForDirection(keys FrameKeys, d Direction) []byte {
	if d == DirectionHostToGuest {
		return keys.HostToGuest
	}
	return keys.GuestToHost
}

func SealFrame(roomKey []byte, meta FrameMeta, plaintext []byte) ([]byte, error) {
	keys, err := DeriveFrameKeys(roomKey, meta.RoomID)
	if err != nil {
		return nil, err
	}
	return sealWithKey(keyForDirection(keys, meta.Direction), meta, plaintext)
}

func OpenFrame(roomKey []byte, meta FrameMeta, ciphertext []byte) ([]byte, error) {
	keys, err := DeriveFrameKeys(roomKey, meta.RoomID)
	if err != nil {
		return nil, err
	}
	return openWithKey(keyForDirection(keys, meta.Direction), meta, ciphertext)
}

func SealEnvelope(roomKey []byte, meta FrameMeta, plaintext []byte) (FrameEnvelope, error) {
	if err := validateFrameMeta(meta); err != nil {
		return FrameEnvelope{}, err
	}
	ciphertext, err := SealFrame(roomKey, meta, plaintext)
	if err != nil {
		return FrameEnvelope{}, err
	}
	return FrameEnvelope{Version: ProtocolVersion, Type: uint8(meta.Type), Sequence: meta.Sequence, PeerID: meta.PeerID, Ciphertext: base64.RawURLEncoding.EncodeToString(ciphertext)}, nil
}

func OpenEnvelope(roomKey []byte, roomID string, direction Direction, env FrameEnvelope, replay *ReplayDetector) ([]byte, error) {
	if env.Version != ProtocolVersion {
		return nil, ErrBadFrameMeta
	}
	meta := FrameMeta{RoomID: roomID, Direction: direction, PeerID: env.PeerID, Sequence: env.Sequence, Type: FrameType(env.Type)}
	if err := validateFrameMeta(meta); err != nil {
		return nil, err
	}
	if replay != nil {
		if err := replay.Check(meta); err != nil {
			return nil, err
		}
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, err
	}
	return OpenFrame(roomKey, meta, ciphertext)
}

func sealWithKey(key []byte, meta FrameMeta, plaintext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("frame key must be 32 bytes")
	}
	if err := validateFrameMeta(meta); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Seal(nil, frameNonce(meta), plaintext, frameAssociatedData(meta)), nil
}

func openWithKey(key []byte, meta FrameMeta, ciphertext []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("frame key must be 32 bytes")
	}
	if err := validateFrameMeta(meta); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, frameNonce(meta), ciphertext, frameAssociatedData(meta))
}

func validateFrameMeta(meta FrameMeta) error {
	if meta.RoomID == "" || meta.Sequence == 0 || meta.Type == 0 {
		return ErrBadFrameMeta
	}
	if meta.Direction != DirectionHostToGuest && meta.Direction != DirectionGuestToHost {
		return ErrBadFrameMeta
	}
	return nil
}

func frameNonce(meta FrameMeta) []byte {
	nonce := make([]byte, 12)
	binary.BigEndian.PutUint32(nonce[:4], meta.PeerID)
	binary.BigEndian.PutUint64(nonce[4:], meta.Sequence)
	return nonce
}

func frameAssociatedData(meta FrameMeta) []byte {
	ad := make([]byte, 0, len(FrameInfo)+len(meta.RoomID)+len(meta.Direction)+32)
	ad = append(ad, FrameInfo...)
	ad = append(ad, 0)
	ad = append(ad, meta.RoomID...)
	ad = append(ad, 0)
	ad = append(ad, string(meta.Direction)...)
	ad = append(ad, 0)
	var u32 [4]byte
	binary.BigEndian.PutUint32(u32[:], meta.PeerID)
	ad = append(ad, u32[:]...)
	ad = append(ad, 0)
	var u64 [8]byte
	binary.BigEndian.PutUint64(u64[:], meta.Sequence)
	ad = append(ad, u64[:]...)
	ad = append(ad, 0)
	ad = append(ad, byte(meta.Type))
	return ad
}

type ReplayDetector struct {
	mu      sync.Mutex
	highest map[string]uint64
}

func NewReplayDetector() *ReplayDetector { return &ReplayDetector{highest: make(map[string]uint64)} }

func (r *ReplayDetector) Check(meta FrameMeta) error {
	if err := validateFrameMeta(meta); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.highest == nil {
		r.highest = make(map[string]uint64)
	}
	key := string(meta.Direction) + ":" + meta.RoomID + ":" + strconv.FormatUint(uint64(meta.PeerID), 10)
	if meta.Sequence <= r.highest[key] {
		return ErrReplayFrame
	}
	r.highest[key] = meta.Sequence
	return nil
}
