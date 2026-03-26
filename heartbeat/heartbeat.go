package heartbeat

import (
	"context"
	"log"
	"time"
)

// Heartbeat runs registered Tasks on a fixed interval.
type Heartbeat struct {
	interval time.Duration
	tasks    []Task
}

func New(intervalSec int) *Heartbeat {
	if intervalSec <= 0 {
		intervalSec = 60
	}
	return &Heartbeat{interval: time.Duration(intervalSec) * time.Second}
}

func (h *Heartbeat) Register(t Task) {
	h.tasks = append(h.tasks, t)
}

// Start blocks until ctx is cancelled. Call with `go h.Start(ctx)`.
func (h *Heartbeat) Start(ctx context.Context) {
	log.Printf("[heartbeat] started, interval=%s, tasks=%d", h.interval, len(h.tasks))
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[heartbeat] stopped")
			return
		case now := <-ticker.C:
			for _, t := range h.tasks {
				if t.ShouldRun(now) {
					if err := t.Run(); err != nil {
						log.Printf("[heartbeat] task %s error: %v", t.Name(), err)
					}
				}
			}
		}
	}
}
