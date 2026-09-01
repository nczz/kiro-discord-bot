package websharerelay

import (
	"encoding/binary"
	"fmt"
)

const (
	CloseGuestMissingRoom = 4004
	CloseSecondHost       = 4009
	CloseRoomFull         = 4010
	CloseBadFrame         = 4011
)

const framePrefixBytes = 4

func parsePeerFrame(frame []byte, maxBytes int64) (uint32, []byte, error) {
	if int64(len(frame)) > maxBytes {
		return 0, nil, fmt.Errorf("frame size %d exceeds limit %d", len(frame), maxBytes)
	}
	if len(frame) < framePrefixBytes {
		return 0, nil, fmt.Errorf("frame must include 4-byte peer prefix")
	}
	return binary.BigEndian.Uint32(frame[:framePrefixBytes]), frame[framePrefixBytes:], nil
}

func buildPeerFrame(peerID uint32, payload []byte) []byte {
	frame := make([]byte, framePrefixBytes+len(payload))
	binary.BigEndian.PutUint32(frame[:framePrefixBytes], peerID)
	copy(frame[framePrefixBytes:], payload)
	return frame
}
