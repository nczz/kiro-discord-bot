package channel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// MemoryStore manages persistent per-channel memory rules stored on disk.
type MemoryStore struct {
	mu      sync.RWMutex
	dataDir string
	cache   map[string][]string // channelID → entries
}

func NewMemoryStore(dataDir string) *MemoryStore {
	return &MemoryStore{
		dataDir: dataDir,
		cache:   make(map[string][]string),
	}
}

func (s *MemoryStore) path(channelID string) string {
	return filepath.Join(s.dataDir, "ch-"+channelID, "memory.json")
}

func (s *MemoryStore) load(channelID string) []string {
	if entries, ok := s.cache[channelID]; ok {
		return entries
	}
	data, err := os.ReadFile(s.path(channelID))
	if err != nil {
		return nil
	}
	var entries []string
	if json.Unmarshal(data, &entries) != nil {
		return nil
	}
	s.cache[channelID] = entries
	return entries
}

func (s *MemoryStore) save(channelID string) error {
	entries := s.cache[channelID]
	dir := filepath.Dir(s.path(channelID))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(channelID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(channelID))
}

func (s *MemoryStore) Add(channelID, entry string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.load(channelID)
	s.cache[channelID] = append(s.cache[channelID], entry)
	return s.save(channelID)
}

func (s *MemoryStore) List(channelID string) []string {
	s.mu.RLock()
	if entries, ok := s.cache[channelID]; ok {
		out := make([]string, len(entries))
		copy(out, entries)
		s.mu.RUnlock()
		return out
	}
	s.mu.RUnlock()

	// Cache miss — upgrade to write lock and load from disk
	s.mu.Lock()
	defer s.mu.Unlock()
	// Double-check after acquiring write lock
	if entries, ok := s.cache[channelID]; ok {
		out := make([]string, len(entries))
		copy(out, entries)
		return out
	}
	entries := s.load(channelID)
	out := make([]string, len(entries))
	copy(out, entries)
	return out
}

func (s *MemoryStore) Remove(channelID string, index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.load(channelID)
	if index < 0 || index >= len(entries) {
		return fmt.Errorf("index out of range: %d", index)
	}
	s.cache[channelID] = append(entries[:index], entries[index+1:]...)
	return s.save(channelID)
}

func (s *MemoryStore) Clear(channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[channelID] = nil
	return s.save(channelID)
}
