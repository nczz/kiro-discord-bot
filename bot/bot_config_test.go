package bot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nczz/kiro-discord-bot/channel"
)

func TestNewFromConfigNormalizesDataDir(t *testing.T) {
	root := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldwd)
	})
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}

	b, err := NewFromConfig(BotConfig{
		DiscordToken:       "token",
		HeartbeatSec:       60,
		DownloadTimeoutSec: 30,
		ManagerConfig: channel.ManagerConfig{
			DataDir: "./runtime-data",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		b.seen.Stop()
		b.manager.StopAll()
	})

	want, err := filepath.Abs("runtime-data")
	if err != nil {
		t.Fatal(err)
	}
	if b.dataDir != want {
		t.Fatalf("bot dataDir = %q, want %q", b.dataDir, want)
	}
}
