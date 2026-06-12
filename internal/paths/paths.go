package paths

import (
	"path/filepath"
	"strings"
)

// DataDir returns an absolute data directory path.
func DataDir(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "./data"
	}
	return filepath.Abs(path)
}
