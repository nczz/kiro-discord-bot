package bot

import (
	"sync"
	"time"
)

const setupPromptCooldownDuration = 5 * time.Minute

type setupPromptCooldown struct {
	mu      sync.Mutex
	entries map[string]time.Time
	now     func() time.Time
}

func newSetupPromptCooldown(now func() time.Time) *setupPromptCooldown {
	if now == nil {
		now = time.Now
	}
	return &setupPromptCooldown{
		entries: make(map[string]time.Time),
		now:     now,
	}
}

func (c *setupPromptCooldown) Allow(key string) bool {
	if c == nil || key == "" {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	for existingKey, sentAt := range c.entries {
		if now.Sub(sentAt) >= setupPromptCooldownDuration {
			delete(c.entries, existingKey)
		}
	}
	if sentAt, ok := c.entries[key]; ok && now.Sub(sentAt) < setupPromptCooldownDuration {
		return false
	}
	c.entries[key] = now
	return true
}
