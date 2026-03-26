package heartbeat

import "time"

// Task is a periodic maintenance job managed by the Heartbeat loop.
type Task interface {
	Name() string
	ShouldRun(now time.Time) bool
	Run() error
}
