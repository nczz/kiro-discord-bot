package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscordUploadDenyDefaultDataDir(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)

	secretPath := filepath.Join(dataDir, "sessions.json")
	if err := os.WriteFile(secretPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if !discordUploadDenied(secretPath) {
		t.Fatal("DATA_DIR file was not blocked by default upload denylist")
	}
}

func TestDiscordUploadDenyDefaultDataDirTreatsDollarLiterally(t *testing.T) {
	root := filepath.Join(t.TempDir(), "$UNSET_LITERAL_SEGMENT", "data")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_DIR", root)

	secretPath := filepath.Join(root, "sessions.json")
	if err := os.WriteFile(secretPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if !discordUploadDenied(secretPath) {
		t.Fatal("DATA_DIR with literal dollar segment was not blocked")
	}
}

func TestDiscordUploadDenyDefaultDataDirTreatsTildeLiterally(t *testing.T) {
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	root := filepath.Join("~", "bot-data")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_DIR", root)

	secretPath := filepath.Join(root, "sessions.json")
	if err := os.WriteFile(secretPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if !discordUploadDenied(secretPath) {
		t.Fatal("DATA_DIR with literal tilde segment was not blocked")
	}
}

func TestOpenDiscordUploadFileBlocksDeniedPathWithoutLeakingPath(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)

	secretPath := filepath.Join(dataDir, ".env")
	if err := os.WriteFile(secretPath, []byte("DISCORD_TOKEN=secret-token"), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := openDiscordUploadFile(secretPath)
	if !errors.Is(err, errDiscordUploadDenied) {
		t.Fatalf("openDiscordUploadFile error = %v, want denylist error", err)
	}
	msg := "file could not be opened for upload: " + safeDiscordUploadError(err)
	if strings.Contains(msg, secretPath) || strings.Contains(msg, dataDir) {
		t.Fatalf("denylist error leaked path: %q", msg)
	}
	if !strings.Contains(msg, errDiscordUploadDenied.Error()) {
		t.Fatalf("denylist error missing stable reason: %q", msg)
	}
}

func TestDiscordUploadDenyCustomWildcard(t *testing.T) {
	root := t.TempDir()
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("MCP_DISCORD_UPLOAD_DENY_CASE_INSENSITIVE", "false")
	t.Setenv("MCP_DISCORD_UPLOAD_DENY_PATHS", filepath.Join(root, "**", "*.secret"))

	rootBlocked := filepath.Join(root, "token.secret")
	if err := os.WriteFile(rootBlocked, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if !discordUploadDenied(rootBlocked) {
		t.Fatal("custom wildcard denylist did not block root-level file")
	}

	blocked := filepath.Join(root, "nested", "token.secret")
	if err := os.MkdirAll(filepath.Dir(blocked), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocked, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if !discordUploadDenied(blocked) {
		t.Fatal("custom wildcard denylist did not block matching file")
	}

	allowed := filepath.Join(root, "nested", "public.txt")
	if err := os.WriteFile(allowed, []byte("public"), 0644); err != nil {
		t.Fatal(err)
	}
	if discordUploadDenied(allowed) {
		t.Fatal("custom wildcard denylist blocked non-matching file")
	}
}

func TestDiscordUploadDenyIgnoresMissingOrEmptyEnvExpansion(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("EMPTY_DISCORD_UPLOAD_DENY_TEST", "")
	t.Setenv("MCP_DISCORD_UPLOAD_DENY_PATHS", "$UNSET_DISCORD_UPLOAD_DENY_TEST/**,$EMPTY_DISCORD_UPLOAD_DENY_TEST/**")
	publicDir := t.TempDir()
	publicPath := filepath.Join(publicDir, "report.txt")
	if err := os.WriteFile(publicPath, []byte("public"), 0644); err != nil {
		t.Fatal(err)
	}
	if discordUploadDenied(publicPath) {
		t.Fatal("unset env expansion in denylist blocked unrelated absolute path")
	}
}

func TestDiscordUploadDenyResolvesConfigSymlinkTarget(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())

	configDir := t.TempDir()
	targetPath := filepath.Join(configDir, "mcp.json")
	if err := os.WriteFile(targetPath, []byte(`{"mcpServers":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(t.TempDir(), "linked-mcp.json")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", linkPath)

	if !discordUploadDenied(targetPath) {
		t.Fatal("upload denylist did not block real target of KIRO_MCP_CONFIG symlink")
	}
}

func TestDiscordUploadDenyResolvesCustomSymlinkRoot(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	t.Setenv("MCP_DISCORD_UPLOAD_DENY_CASE_INSENSITIVE", "false")

	targetRoot := t.TempDir()
	linkRoot := filepath.Join(t.TempDir(), "private-link")
	if err := os.Symlink(targetRoot, linkRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	t.Setenv("MCP_DISCORD_UPLOAD_DENY_PATHS", filepath.Join(linkRoot, "**"))

	targetFile := filepath.Join(targetRoot, "secret.txt")
	if err := os.WriteFile(targetFile, []byte("secret"), 0644); err != nil {
		t.Fatal(err)
	}
	if !discordUploadDenied(targetFile) {
		t.Fatal("custom denylist symlink root did not block direct target path")
	}
}

func TestDiscordUploadDenyFollowsSymlinkTarget(t *testing.T) {
	dataDir := t.TempDir()
	outside := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)

	secretPath := filepath.Join(dataDir, "settings", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(secretPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte(`{"mcpServers":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(outside, "safe-name.json")
	if err := os.Symlink(secretPath, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if !discordUploadDenied(linkPath) {
		t.Fatal("upload denylist did not block symlink to denied DATA_DIR file")
	}
}

func TestOpenDiscordUploadFileAllowsOutsideDenylist(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())
	publicDir := t.TempDir()
	publicPath := filepath.Join(publicDir, "report.bin")
	want := []byte{0, 1, 2, 3}
	if err := os.WriteFile(publicPath, want, 0644); err != nil {
		t.Fatal(err)
	}

	f, displayName, cleanup, err := openDiscordUploadFile(publicPath)
	if err != nil {
		t.Fatalf("openDiscordUploadFile: %v", err)
	}
	defer cleanup()
	if displayName != "report.bin" {
		t.Fatalf("displayName = %q, want report.bin", displayName)
	}
	got, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("opened bytes = %v, want %v", got, want)
	}
}

func TestOpenDiscordUploadFileOpensResolvedAllowedSymlink(t *testing.T) {
	t.Setenv("DATA_DIR", t.TempDir())

	publicDir := t.TempDir()
	targetPath := filepath.Join(publicDir, "target.txt")
	if err := os.WriteFile(targetPath, []byte("public"), 0644); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(t.TempDir(), "display-name.txt")
	if err := os.Symlink(targetPath, linkPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	f, displayName, cleanup, err := openDiscordUploadFile(linkPath)
	if err != nil {
		t.Fatalf("openDiscordUploadFile: %v", err)
	}
	defer cleanup()
	if displayName != "display-name.txt" {
		t.Fatalf("displayName = %q, want original symlink basename", displayName)
	}
	wantOpenPath, err := filepath.EvalSymlinks(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(f.Name()) != filepath.Clean(wantOpenPath) {
		t.Fatalf("opened path = %q, want resolved target %q", f.Name(), wantOpenPath)
	}
}
