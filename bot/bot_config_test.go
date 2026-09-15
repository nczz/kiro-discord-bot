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

func TestNewFromConfigParsesGMUserIDs(t *testing.T) {
	b, err := NewFromConfig(BotConfig{
		DiscordToken:       "token",
		HeartbeatSec:       60,
		DownloadTimeoutSec: 30,
		GMUserIDs:          "gm-1, gm-2",
		ManagerConfig: channel.ManagerConfig{
			DataDir: t.TempDir(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		b.seen.Stop()
		b.manager.StopAll()
	})

	if !b.userIsGM("gm-1") || !b.userIsGM("gm-2") || b.userIsGM("viewer") {
		t.Fatalf("gmUserIDs = %#v, want gm-1/gm-2 only", b.gmUserIDs)
	}
}
