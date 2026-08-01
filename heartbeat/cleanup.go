package heartbeat

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// CleanupTask deletes attachment files older than retainDays.
type CleanupTask struct {
	dataDir    string
	retainDays int
	lastRun    time.Time
}

type cleanupSession struct {
	CWD string `json:"cwd"`
}

func NewCleanupTask(dataDir string, retainDays int) *CleanupTask {
	return &CleanupTask{dataDir: dataDir, retainDays: retainDays}
}

func (c *CleanupTask) Name() string { return "cleanup" }

func (c *CleanupTask) ShouldRun(now time.Time) bool {
	if c.retainDays <= 0 {
		return false
	}
	return c.lastRun.IsZero() || now.Sub(c.lastRun) >= 24*time.Hour
}

func (c *CleanupTask) Run() error {
	c.lastRun = time.Now()
	cutoff := time.Now().AddDate(0, 0, -c.retainDays)
	count := 0

	entries, err := os.ReadDir(c.dataDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		count += cleanupAttachmentTree(filepath.Join(c.dataDir, e.Name(), "attachments"), cutoff)
	}
	for _, root := range c.projectAttachmentRoots() {
		count += cleanupAttachmentTree(root, cutoff)
	}
	if count > 0 {
		log.Printf("[cleanup] removed %d expired attachments (retain=%d days)", count, c.retainDays)
	}
	return nil
}

func (c *CleanupTask) projectAttachmentRoots() []string {
	raw, err := os.ReadFile(filepath.Join(c.dataDir, "sessions.json"))
	if err != nil {
		return nil
	}
	var sessions map[string]cleanupSession
	if err := json.Unmarshal(raw, &sessions); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var roots []string
	for _, sess := range sessions {
		cwd := strings.TrimSpace(sess.CWD)
		if cwd == "" {
			continue
		}
		root := filepath.Join(filepath.Clean(cwd), ".kiro-bot", "attachments")
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		roots = append(roots, root)
	}
	return roots
}

func cleanupAttachmentTree(root string, cutoff time.Time) int {
	var count int
	var dirs []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			dirs = append(dirs, path)
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().Before(cutoff) {
			if os.Remove(path) == nil {
				count++
			}
		}
		return nil
	}); err != nil {
		return 0
	}
	slices.Reverse(dirs)
	for _, dir := range dirs {
		_ = os.Remove(dir)
	}
	return count
}
