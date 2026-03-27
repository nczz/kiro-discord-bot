package channel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Session holds the mapping between a Discord channel and a kiro-cli agent.
type Session struct {
	AgentName string `json:"agentName"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Model     string `json:"model,omitempty"`
}

// SessionStore persists channel→session mappings to a JSON file.
type SessionStore struct {
	mu       sync.RWMutex
	path     string
	sessions map[string]*Session // key: channelID
}

func NewSessionStore(dataDir string) (*SessionStore, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, err
	}
	s := &SessionStore{
		path:     filepath.Join(dataDir, "sessions.json"),
		sessions: make(map[string]*Session),
	}
	_ = s.load() // ignore error on first run
	return s, nil
}

func (s *SessionStore) Get(channelID string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[channelID]
	return sess, ok
}

func (s *SessionStore) Set(channelID string, sess *Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[channelID] = sess
	return s.save()
}

func (s *SessionStore) Delete(channelID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, channelID)
	return s.save()
}

func (s *SessionStore) All() map[string]*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*Session, len(s.sessions))
	for k, v := range s.sessions {
		out[k] = v
	}
	return out
}

func (s *SessionStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, &s.sessions)
}

func (s *SessionStore) save() error {
	data, err := json.MarshalIndent(s.sessions, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
