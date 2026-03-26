package heartbeat

import (
	"log"
	"os"
	"path/filepath"
	"time"
)

// CleanupTask deletes attachment files older than retainDays.
type CleanupTask struct {
	dataDir    string
	retainDays int
	lastRun    time.Time
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

	// Walk all ch-*/attachments/ directories
	entries, err := os.ReadDir(c.dataDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		attDir := filepath.Join(c.dataDir, e.Name(), "attachments")
		files, err := os.ReadDir(attDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			info, err := f.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				_ = os.Remove(filepath.Join(attDir, f.Name()))
				count++
			}
		}
		// Remove empty dir
		remaining, _ := os.ReadDir(attDir)
		if len(remaining) == 0 {
			_ = os.Remove(attDir)
		}
	}
	if count > 0 {
		log.Printf("[cleanup] removed %d expired attachments (retain=%d days)", count, c.retainDays)
	}
	return nil
}
