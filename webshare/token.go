package webshare

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	LinkSecretVersion byte = 1
	RoomKeySize            = 32
	WriteTokenSize         = 32
	shareIDRandomBytes     = 16
	roomIDRandomBytes      = 16
)

var (
	ErrInvalidLink   = errors.New("invalid webshare link")
	ErrInvalidSecret = errors.New("invalid webshare secret")
)

type SecretMaterial struct {
	ShareID    string
	RoomID     string
	RoomKey    []byte
	WriteToken []byte
}

type ParsedLink struct {
	RoomID     string
	RoomKey    []byte
	WriteToken []byte
	CanWrite   bool
}

func GenerateSecretMaterial() (SecretMaterial, error) {
	shareID, err := GenerateShareID()
	if err != nil {
		return SecretMaterial{}, err
	}
	roomID, err := GenerateRoomID()
	if err != nil {
		return SecretMaterial{}, err
	}
	roomKey, err := RandomBytes(RoomKeySize)
	if err != nil {
		return SecretMaterial{}, err
	}
	writeToken, err := RandomBytes(WriteTokenSize)
	if err != nil {
		return SecretMaterial{}, err
	}
	return SecretMaterial{ShareID: shareID, RoomID: roomID, RoomKey: roomKey, WriteToken: writeToken}, nil
}

func GenerateShareID() (string, error) { return randomID("ws_", shareIDRandomBytes) }
func GenerateRoomID() (string, error)  { return randomID("wr_", roomIDRandomBytes) }

func randomID(prefix string, n int) (string, error) {
	b, err := RandomBytes(n)
	if err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func RandomBytes(n int) ([]byte, error) {
	if n <= 0 {
		return nil, fmt.Errorf("random byte count must be positive")
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

func FormatViewLink(publicBaseURL, roomID string, roomKey []byte) (string, error) {
	secret, err := EncodeLinkSecret(roomKey, nil)
	if err != nil {
		return "", err
	}
	return FormatJoinLink(publicBaseURL, roomID, secret), nil
}

func FormatWriteLink(publicBaseURL, roomID string, roomKey, writeToken []byte) (string, error) {
	secret, err := EncodeLinkSecret(roomKey, writeToken)
	if err != nil {
		return "", err
	}
	return FormatJoinLink(publicBaseURL, roomID, secret), nil
}

func FormatJoinLink(publicBaseURL, roomID string, encodedSecret string) string {
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	return base + "#/join/" + roomID + "." + encodedSecret
}

func EncodeLinkSecret(roomKey, writeToken []byte) (string, error) {
	if len(roomKey) != RoomKeySize {
		return "", fmt.Errorf("room key must be %d bytes", RoomKeySize)
	}
	if writeToken != nil && len(writeToken) != WriteTokenSize {
		return "", fmt.Errorf("write token must be %d bytes", WriteTokenSize)
	}
	secret := make([]byte, 1+len(roomKey)+len(writeToken))
	secret[0] = LinkSecretVersion
	copy(secret[1:], roomKey)
	copy(secret[1+len(roomKey):], writeToken)
	return base64.RawURLEncoding.EncodeToString(secret), nil
}

func ParseJoinLink(raw string) (ParsedLink, error) {
	frag := strings.TrimSpace(raw)
	if frag == "" {
		return ParsedLink{}, ErrInvalidLink
	}
	if u, err := url.Parse(frag); err == nil && u.Fragment != "" {
		frag = "#" + u.Fragment
	}
	frag = strings.TrimPrefix(frag, "#")
	frag = strings.TrimPrefix(frag, "/")
	if !strings.HasPrefix(frag, "join/") {
		return ParsedLink{}, ErrInvalidLink
	}
	payload := strings.TrimPrefix(frag, "join/")
	roomID, encoded, ok := strings.Cut(payload, ".")
	if !ok || roomID == "" || encoded == "" || strings.Contains(roomID, "/") {
		return ParsedLink{}, ErrInvalidLink
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return ParsedLink{}, ErrInvalidSecret
	}
	parsed, err := DecodeLinkSecret(decoded)
	if err != nil {
		return ParsedLink{}, err
	}
	parsed.RoomID = roomID
	return parsed, nil
}

func DecodeLinkSecret(secret []byte) (ParsedLink, error) {
	if len(secret) != 1+RoomKeySize && len(secret) != 1+RoomKeySize+WriteTokenSize {
		return ParsedLink{}, ErrInvalidSecret
	}
	if secret[0] != LinkSecretVersion {
		return ParsedLink{}, ErrInvalidSecret
	}
	roomKey := append([]byte(nil), secret[1:1+RoomKeySize]...)
	var writeToken []byte
	if len(secret) == 1+RoomKeySize+WriteTokenSize {
		writeToken = append([]byte(nil), secret[1+RoomKeySize:]...)
	}
	return ParsedLink{RoomKey: roomKey, WriteToken: writeToken, CanWrite: len(writeToken) > 0}, nil
}

func TokenHash(token []byte) []byte {
	sum := sha256.Sum256(token)
	return sum[:]
}

func TokenFingerprint(token []byte) string {
	sum := sha256.Sum256(token)
	return "sha256:" + hex.EncodeToString(sum[:16])
}

func VerifyTokenHash(token, storedHash []byte) bool {
	if len(storedHash) != sha256.Size {
		return false
	}
	sum := sha256.Sum256(token)
	return hmac.Equal(sum[:], storedHash)
}
