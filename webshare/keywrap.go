package webshare

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	masterKeyFile       = "master.key"
	wrappedRoomKeyMagic = "wsk1"
)

var ErrInvalidWrappedRoomKey = errors.New("invalid wrapped room key")

func WebShareDir(dataDir string) string {
	if dataDir == "" {
		dataDir = "./data"
	}
	return filepath.Join(dataDir, "webshare")
}

func MasterKeyPath(dataDir string) string { return filepath.Join(WebShareDir(dataDir), masterKeyFile) }
func SQLitePath(dataDir string) string    { return filepath.Join(WebShareDir(dataDir), "webshare.sqlite") }

func LoadOrCreateMasterKey(dataDir string) ([]byte, error) {
	path := MasterKeyPath(dataDir)
	if raw, err := os.ReadFile(path); err == nil {
		if len(raw) != 32 {
			return nil, fmt.Errorf("webshare master key must be 32 bytes")
		}
		return raw, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	key, err := RandomBytes(32)
	if err != nil {
		return nil, err
	}
	fd, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	if _, err := fd.Write(key); err != nil {
		return nil, err
	}
	return key, nil
}

func WrapRoomKey(dataDir string, roomKey []byte) ([]byte, error) {
	master, err := LoadOrCreateMasterKey(dataDir)
	if err != nil {
		return nil, err
	}
	return WrapRoomKeyWithMaster(master, roomKey)
}

func UnwrapRoomKey(dataDir string, wrapped []byte) ([]byte, error) {
	master, err := LoadOrCreateMasterKey(dataDir)
	if err != nil {
		return nil, err
	}
	return UnwrapRoomKeyWithMaster(master, wrapped)
}

func WrapRoomKeyWithMaster(master, roomKey []byte) ([]byte, error) {
	if len(master) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes")
	}
	if len(roomKey) != RoomKeySize {
		return nil, fmt.Errorf("room key must be %d bytes", RoomKeySize)
	}
	block, err := aes.NewCipher(master)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce, err := RandomBytes(gcm.NonceSize())
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(wrappedRoomKeyMagic)+len(nonce)+len(roomKey)+gcm.Overhead())
	out = append(out, wrappedRoomKeyMagic...)
	out = append(out, nonce...)
	out = gcm.Seal(out, nonce, roomKey, []byte("kdb-webshare-room-key-wrap-v1"))
	return out, nil
}

func UnwrapRoomKeyWithMaster(master, wrapped []byte) ([]byte, error) {
	if len(master) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes")
	}
	if len(wrapped) < len(wrappedRoomKeyMagic)+12+16 || !bytes.Equal(wrapped[:len(wrappedRoomKeyMagic)], []byte(wrappedRoomKeyMagic)) {
		return nil, ErrInvalidWrappedRoomKey
	}
	block, err := aes.NewCipher(master)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceStart := len(wrappedRoomKeyMagic)
	nonceEnd := nonceStart + gcm.NonceSize()
	if len(wrapped) <= nonceEnd {
		return nil, ErrInvalidWrappedRoomKey
	}
	roomKey, err := gcm.Open(nil, wrapped[nonceStart:nonceEnd], wrapped[nonceEnd:], []byte("kdb-webshare-room-key-wrap-v1"))
	if err != nil {
		return nil, err
	}
	if len(roomKey) != RoomKeySize {
		return nil, ErrInvalidWrappedRoomKey
	}
	return roomKey, nil
}
