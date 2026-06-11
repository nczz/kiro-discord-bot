package channel

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nczz/kiro-discord-bot/acp"
	L "github.com/nczz/kiro-discord-bot/locale"
	_ "modernc.org/sqlite"
)

func mcpServerConfigsContain(servers []acp.MCPServerConfig, name string) bool {
	for _, server := range servers {
		if server.Name == name {
			return true
		}
	}
	return false
}

func TestMCPPolicyDefaultsToDisabled(t *testing.T) {
	s, err := OpenMCPPolicyStore(t.TempDir())
	if err != nil {
		t.Fatalf("open policy store: %v", err)
	}
	defer s.Close()

	p, err := s.GetPolicy(context.Background(), "guild-1", "channel-1", "generic-tools")
	if err != nil {
		t.Fatalf("get policy: %v", err)
	}
	if p.Enabled || !p.ReadOnly || p.AllowDestructive {
		t.Fatalf("unexpected default policy: %+v", p)
	}
}

func TestMCPPolicyStoreMigratesOlderPolicySchema(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "mcp", "policy.sqlite")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		t.Fatalf("mkdir db dir: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE channel_mcp_policies (
		guild_id TEXT NOT NULL,
		channel_id TEXT NOT NULL,
		server_name TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 0,
		allowed_tools_json TEXT NOT NULL DEFAULT '[]',
		PRIMARY KEY (guild_id, channel_id, server_name)
	)`); err != nil {
		t.Fatalf("create legacy policy table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	s, err := OpenMCPPolicyStore(dir)
	if err != nil {
		t.Fatalf("open policy store should migrate legacy schema: %v", err)
	}
	defer s.Close()

	p := defaultMCPPolicy("guild-1", "channel-1", "generic-tools").ApplyPreset("full")
	p.Enabled = true
	p.UpdatedBy = "user-1"
	if err := s.SetPolicy(context.Background(), p); err != nil {
		t.Fatalf("set policy after schema migration: %v", err)
	}
	got, err := s.GetPolicy(context.Background(), "guild-1", "channel-1", "generic-tools")
	if err != nil {
		t.Fatalf("get policy after schema migration: %v", err)
	}
	if !got.Enabled || !got.AllowAllTools || got.UpdatedBy != "user-1" {
		t.Fatalf("unexpected migrated policy: %+v", got)
	}
}

func TestMCPPolicySafeWritePresetIsGenericFailClosed(t *testing.T) {
	p := defaultMCPPolicy("guild-1", "channel-1", "generic-tools").ApplyPreset("safe-write")
	if !p.ReadOnly || p.AllowAllTools || p.AllowDestructive {
		t.Fatalf("safe-write should remain fail-closed without server-specific adapters: %+v", p)
	}
	if tools := p.EffectiveTools(); len(tools) != 0 {
		t.Fatalf("safe-write leaked tools: %+v", tools)
	}
}

func TestMCPPolicyToACPServerPreservesCatalogEnvOnly(t *testing.T) {
	p := defaultMCPPolicy("guild-1", "channel-1", "generic-tools").ApplyPreset("full")
	entry := MCPCatalogEntry{Name: "generic-tools", Command: "/bin/echo", Env: map[string]string{"TOKEN": "secret"}}
	cfg := p.ToACPServer(entry, "/tmp/bot", "guild-1", "channel-1")

	if cfg.Name != "generic-tools" || cfg.Command != "/tmp/bot" || len(cfg.Args) != 1 || cfg.Args[0] != "mcp-proxy" {
		t.Fatalf("unexpected acp config: %+v", cfg)
	}
	var targetEnv map[string]string
	if err := json.Unmarshal([]byte(cfg.Env["MCP_PROXY_ENV_JSON"]), &targetEnv); err != nil {
		t.Fatalf("proxy target env json: %v", err)
	}
	if targetEnv["TOKEN"] != "secret" {
		t.Fatalf("catalog env missing: %+v", targetEnv)
	}
	if len(targetEnv) != 1 {
		t.Fatalf("bot policy should preserve only catalog env, got %+v", targetEnv)
	}
}

func TestRedactedCatalogEntryDoesNotPersistEnvSecrets(t *testing.T) {
	entry := MCPCatalogEntry{
		Name:    "generic-tools",
		Command: "/tmp/generic-tools",
		Env:     map[string]string{"TOKEN": "secret-token"},
	}
	got := redactedCatalogEntry(entry)
	if got.Env["TOKEN"] == "secret-token" {
		t.Fatalf("catalog redaction leaked env value")
	}
	if entry.Env["TOKEN"] != "secret-token" {
		t.Fatalf("redaction should not mutate runtime catalog")
	}
}

func TestReadMCPConfigCatalog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"generic-tools":{"command":"/tmp/generic-tools","args":["--stdio"],"env":{"A":"B"}}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	entries, err := readMCPConfig(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	entry, ok := entries["generic-tools"]
	if !ok {
		t.Fatalf("missing generic entry: %+v", entries)
	}
	if entry.Command != "/tmp/generic-tools" || len(entry.Args) != 1 || entry.Env["A"] != "B" {
		t.Fatalf("unexpected entry: %+v", entry)
	}
}

func TestReadMCPConfigURLType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"meta-ads":{"url":"http://127.0.0.1:18900"},"cli-tool":{"command":"/bin/echo"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	entries, err := readMCPConfig(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(entries), entries)
	}
	meta, ok := entries["meta-ads"]
	if !ok {
		t.Fatalf("missing meta-ads entry")
	}
	if meta.URL != "http://127.0.0.1:18900" || meta.Command != "" {
		t.Fatalf("unexpected meta-ads entry: %+v", meta)
	}
	cli, ok := entries["cli-tool"]
	if !ok {
		t.Fatalf("missing cli-tool entry")
	}
	if cli.Command != "/bin/echo" || cli.URL != "" {
		t.Fatalf("unexpected cli-tool entry: %+v", cli)
	}
}

func TestToACPServerURLType(t *testing.T) {
	p := defaultMCPPolicy("guild-1", "channel-1", "meta-ads").ApplyPreset("full")
	entry := MCPCatalogEntry{Name: "meta-ads", URL: "http://127.0.0.1:18900"}
	cfg := p.ToACPServer(entry, "/tmp/bot", "guild-1", "channel-1")

	if cfg.Name != "meta-ads" || cfg.Command != "/tmp/bot" || len(cfg.Args) != 1 || cfg.Args[0] != "mcp-proxy" {
		t.Fatalf("URL type should still use proxy: %+v", cfg)
	}
	if cfg.Env["MCP_PROXY_URL"] != "http://127.0.0.1:18900" {
		t.Fatalf("expected MCP_PROXY_URL env, got %+v", cfg.Env)
	}
	if _, hasCommand := cfg.Env["MCP_PROXY_COMMAND"]; hasCommand {
		t.Fatalf("URL type should not have MCP_PROXY_COMMAND: %+v", cfg.Env)
	}
}

func TestManagerAgentOptionsApplyChannelMCPPolicy(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"generic-tools":{"command":"/tmp/generic-tools","env":{"TOKEN":"token"}}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	m := NewManager(ManagerConfig{DataDir: dir, GuildID: "guild-1"})
	defer m.StopAll()

	opts := m.agentOptsForChannel("channel-1")
	if len(opts.MCPServers) != 0 {
		t.Fatalf("default policy should not inject MCP servers: %+v", opts.MCPServers)
	}
	if err := m.SetMCPPolicy("channel-1", "user-1", "generic-tools", true, "full"); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	opts = m.agentOptsForChannel("channel-1")
	if len(opts.MCPServers) != 1 || opts.MCPServers[0].Name != "generic-tools" {
		t.Fatalf("expected generic MCP injection: %+v", opts.MCPServers)
	}
	var injectedEnv map[string]string
	if err := json.Unmarshal([]byte(opts.MCPServers[0].Env["MCP_PROXY_ENV_JSON"]), &injectedEnv); err != nil {
		t.Fatalf("proxy target env json: %v", err)
	}
	if injectedEnv["TOKEN"] != "token" {
		t.Fatalf("catalog env missing: %+v", injectedEnv)
	}
	if len(injectedEnv) != 1 {
		t.Fatalf("agent options should preserve only catalog env, got %+v", injectedEnv)
	}
	if len(opts.Env) != 1 || !strings.HasPrefix(opts.Env[0], "KIRO_HOME="+filepath.Join(dir, "kiro-runtime")) {
		t.Fatalf("isolated KIRO_HOME missing: %+v", opts.Env)
	}
}

func TestManagerBuiltinMCPRequiresExplicitPolicy(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(ManagerConfig{DataDir: dir, GuildID: "guild-1"})
	defer m.StopAll()

	m.RegisterBuiltinMCP("bot-tools", []string{"mcp-bot"}, map[string]string{"DATA_DIR": dir})

	opts := m.agentOptsForChannel("channel-1")
	if len(opts.MCPServers) != 0 {
		t.Fatalf("builtin MCP must not be injected without explicit policy: %+v", opts.MCPServers)
	}

	views, err := m.MCPServerViews("channel-1")
	if err != nil {
		t.Fatalf("server views: %v", err)
	}
	var botTools *MCPServerView
	for i := range views {
		if views[i].Name == "bot-tools" {
			botTools = &views[i]
			break
		}
	}
	if botTools == nil || botTools.Policy.Enabled {
		t.Fatalf("builtin should be visible but disabled by default: %+v", views)
	}

	if err := m.SetMCPPolicy("channel-1", "user-1", "bot-tools", true, "full"); err != nil {
		t.Fatalf("enable builtin policy: %v", err)
	}
	opts = m.agentOptsForChannel("channel-1")
	if len(opts.MCPServers) != 1 || opts.MCPServers[0].Name != "bot-tools" {
		t.Fatalf("explicitly enabled builtin should be injected: %+v", opts.MCPServers)
	}
	targetEnv := proxyTargetEnv(t, opts.MCPServers[0].Env)
	if targetEnv["DATA_DIR"] != dir {
		t.Fatalf("builtin env missing data dir: %+v", targetEnv)
	}
	if targetEnv["BOT_TOOLS_CHANNEL_ID"] != "channel-1" || targetEnv["BOT_TOOLS_GUILD_ID"] != "guild-1" {
		t.Fatalf("builtin env missing channel binding: %+v", targetEnv)
	}

	if err := m.SetMCPPolicy("channel-1", "user-1", "bot-tools", false, "full"); err != nil {
		t.Fatalf("disable builtin policy: %v", err)
	}
	if got := m.agentOptsForChannel("channel-1").MCPServers; len(got) != 0 {
		t.Fatalf("explicit disabled builtin must not be injected: %+v", got)
	}
}

func TestLegacyMCPMigrationDoesNotEnableFreshInstall(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"legacy-tools":{"command":"/tmp/legacy-mcp"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	m := NewManager(ManagerConfig{DataDir: dir, Store: store, GuildID: "guild-1", BotID: "bot-1"})
	defer m.StopAll()

	if got := m.agentOptsForChannel("channel-1").MCPServers; len(got) != 0 {
		t.Fatalf("fresh install must not auto-enable MCP servers: %+v", got)
	}

	applied, err := m.mcpPolicies.MigrationApplied(context.Background(), mcpLegacyMigrationV1)
	if err != nil {
		t.Fatalf("migration applied check: %v", err)
	}
	if !applied {
		t.Fatal("fresh install should still mark legacy migration complete")
	}
}

func TestLegacyMCPMigrationEnablesCatalogForKnownChannels(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"legacy-tools":{"command":"/tmp/legacy-mcp"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	if err := store.Set("channel-1", &Session{SessionID: "legacy-session"}); err != nil {
		t.Fatalf("write legacy session: %v", err)
	}

	m := NewManager(ManagerConfig{DataDir: dir, Store: store, GuildID: "guild-1", BotID: "bot-1"})
	defer m.StopAll()

	opts := m.agentOptsForChannel("channel-1")
	if !mcpServerConfigsContain(opts.MCPServers, "legacy-tools") {
		t.Fatalf("legacy channel should get full catalog access for legacy-tools: %+v", opts.MCPServers)
	}
	p, err := m.mcpPolicies.GetPolicy(context.Background(), "guild-1", "channel-1", "legacy-tools")
	if err != nil {
		t.Fatalf("get migrated policy: %v", err)
	}
	if !p.Enabled || !p.AllowAllTools || p.UpdatedBy != "system:migration:"+mcpLegacyMigrationV1 {
		t.Fatalf("unexpected migrated policy: %+v", p)
	}

	if got := m.agentOptsForChannel("new-channel").MCPServers; len(got) != 0 {
		t.Fatalf("new channel must remain disabled after migration: %+v", got)
	}
}

func TestLegacyMCPMigrationRunsAfterBotIDIsKnown(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"legacy-tools":{"command":"/tmp/legacy-mcp"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	if err := store.Set("channel-1", &Session{SessionID: "legacy-session"}); err != nil {
		t.Fatalf("write legacy session: %v", err)
	}

	m := NewManager(ManagerConfig{DataDir: dir, Store: store, GuildID: "guild-1"})
	defer m.StopAll()

	if got := m.agentOptsForChannel("channel-1").MCPServers; len(got) != 0 {
		t.Fatalf("migration should wait for bot id, got %+v", got)
	}
	m.SetBotID("bot-1")
	if got := m.agentOptsForChannel("channel-1").MCPServers; !mcpServerConfigsContain(got, "legacy-tools") {
		t.Fatalf("legacy migration should run after bot id is known: %+v", got)
	}
}

func TestLegacyMCPMigrationSkipsSessionsForOtherBots(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"legacy-tools":{"command":"/tmp/legacy-mcp"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	if err := store.Set("g:guild-1:b:bot-other:channel:channel-other", &Session{
		SessionID:  "other-session",
		GuildID:    "guild-1",
		BotID:      "bot-other",
		TargetType: sessionTargetChannel,
		TargetID:   "channel-other",
	}); err != nil {
		t.Fatalf("write other bot session: %v", err)
	}

	m := NewManager(ManagerConfig{DataDir: dir, Store: store, GuildID: "guild-1", BotID: "bot-1"})
	defer m.StopAll()

	if got := m.agentOptsForChannel("channel-other").MCPServers; len(got) != 0 {
		t.Fatalf("other bot session must not get migrated MCP access: %+v", got)
	}
}

func TestLegacyMCPMigrationUsesThreadParentChannel(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"legacy-tools":{"command":"/tmp/legacy-mcp"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	if err := store.Set("thread:thread-1", &Session{SessionID: "thread-session", TargetType: sessionTargetThread, TargetID: "thread-1", ParentChannelID: "channel-1"}); err != nil {
		t.Fatalf("write thread session: %v", err)
	}

	m := NewManager(ManagerConfig{DataDir: dir, Store: store, GuildID: "guild-1", BotID: "bot-1"})
	defer m.StopAll()

	if got := m.agentOptsForChannel("channel-1").MCPServers; !mcpServerConfigsContain(got, "legacy-tools") {
		t.Fatalf("thread parent should get migrated MCP access: %+v", got)
	}
}

func TestLegacyMCPMigrationDoesNotAutoEnableNewCatalogServersLater(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"legacy-tools":{"command":"/tmp/legacy-mcp"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	if err := store.Set("channel-1", &Session{SessionID: "legacy-session"}); err != nil {
		t.Fatalf("write legacy session: %v", err)
	}
	m := NewManager(ManagerConfig{DataDir: dir, Store: store, GuildID: "guild-1", BotID: "bot-1"})
	m.StopAll()

	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"legacy-tools":{"command":"/tmp/legacy-mcp"},"new-tools":{"command":"/tmp/new-mcp"}}}`), 0644); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	m = NewManager(ManagerConfig{DataDir: dir, Store: store, GuildID: "guild-1", BotID: "bot-1"})
	defer m.StopAll()

	servers := m.agentOptsForChannel("channel-1").MCPServers
	if !mcpServerConfigsContain(servers, "legacy-tools") || mcpServerConfigsContain(servers, "new-tools") {
		t.Fatalf("new catalog server must remain disabled after migration: %+v", servers)
	}
}

func TestLegacyMCPMigrationDoesNotOverrideExplicitPolicy(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"legacy-tools":{"command":"/tmp/legacy-mcp"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	store, err := NewSessionStore(dir)
	if err != nil {
		t.Fatalf("session store: %v", err)
	}
	if err := store.Set("channel-1", &Session{SessionID: "legacy-session"}); err != nil {
		t.Fatalf("write legacy session: %v", err)
	}
	policies, err := OpenMCPPolicyStore(dir)
	if err != nil {
		t.Fatalf("policy store: %v", err)
	}
	if err := policies.SetPolicy(context.Background(), MCPChannelPolicy{
		GuildID:    "guild-1",
		ChannelID:  "channel-1",
		ServerName: "legacy-tools",
		Enabled:    false,
		ReadOnly:   true,
		UpdatedBy:  "user-1",
	}); err != nil {
		t.Fatalf("seed explicit disabled policy: %v", err)
	}
	_ = policies.Close()

	m := NewManager(ManagerConfig{DataDir: dir, Store: store, GuildID: "guild-1", BotID: "bot-1"})
	defer m.StopAll()

	if got := m.agentOptsForChannel("channel-1").MCPServers; mcpServerConfigsContain(got, "legacy-tools") {
		t.Fatalf("explicit disabled policy must not be overridden: %+v", got)
	}
	p, err := m.mcpPolicies.GetPolicy(context.Background(), "guild-1", "channel-1", "legacy-tools")
	if err != nil {
		t.Fatalf("get policy: %v", err)
	}
	if p.Enabled || p.UpdatedBy != "user-1" {
		t.Fatalf("explicit policy was overwritten: %+v", p)
	}
}

func TestManagerPreflightOptionsUseIsolatedKiroHomeWithoutMCP(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"generic-tools":{"command":"/tmp/generic-mcp"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	m := NewManager(ManagerConfig{DataDir: dir, GuildID: "guild-1"})
	defer m.StopAll()

	opts := m.preflightAgentOptions()
	if len(opts.MCPServers) != 0 {
		t.Fatalf("preflight must not inject catalog MCP servers by default: %+v", opts.MCPServers)
	}
	if len(opts.Env) != 1 || !strings.HasPrefix(opts.Env[0], "KIRO_HOME="+filepath.Join(dir, "kiro-runtime")) {
		t.Fatalf("preflight isolated KIRO_HOME missing: %+v", opts.Env)
	}
	if !opts.TrustAllTools || opts.TrustTools != "" || opts.LoadSessionID != "" {
		t.Fatalf("unexpected preflight agent options: %+v", opts)
	}
}

func TestManagerAgentOptionsApplyGenericMCPPolicy(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"generic-tools":{"command":"/tmp/generic-mcp","args":["--stdio"],"env":{"TOKEN":"secret"}}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	m := NewManager(ManagerConfig{DataDir: dir, GuildID: "guild-1"})
	defer m.StopAll()

	if err := m.SetMCPTool("channel-1", "user-1", "generic-tools", "search", true); err != nil {
		t.Fatalf("allow generic tool: %v", err)
	}
	opts := m.agentOptsForChannel("channel-1")
	if len(opts.MCPServers) != 1 || opts.MCPServers[0].Name != "generic-tools" {
		t.Fatalf("expected generic MCP injection: %+v", opts.MCPServers)
	}
	if opts.MCPServers[0].Command != m.mcpProxyCommand || len(opts.MCPServers[0].Args) != 1 || opts.MCPServers[0].Args[0] != "mcp-proxy" {
		t.Fatalf("generic MCP should be proxied: %+v", opts.MCPServers[0])
	}
	var allowed []string
	if err := json.Unmarshal([]byte(opts.MCPServers[0].Env["MCP_PROXY_ALLOWED_TOOLS_JSON"]), &allowed); err != nil {
		t.Fatalf("allowed tools json: %v", err)
	}
	if len(allowed) != 1 || allowed[0] != "search" {
		t.Fatalf("allowed tools = %+v", allowed)
	}
	targetEnv := proxyTargetEnv(t, opts.MCPServers[0].Env)
	if targetEnv["TOKEN"] != "secret" {
		t.Fatalf("target env not preserved: %+v", targetEnv)
	}
}

func TestManagerRejectsGenericReadPresetThatExposesNoTools(t *testing.T) {
	L.Load("en")
	t.Cleanup(func() { L.Load("en") })

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"context7":{"command":"npx","args":["-y","@upstash/context7-mcp"]}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	m := NewManager(ManagerConfig{DataDir: dir, GuildID: "guild-1"})
	defer m.StopAll()

	err := m.SetMCPPolicy("channel-1", "user-1", "context7", true, "read")
	if err == nil || !strings.Contains(err.Error(), "no tools") {
		t.Fatalf("expected no-tools validation error, got %v", err)
	}
	opts := m.agentOptsForChannel("channel-1")
	if len(opts.MCPServers) != 0 {
		t.Fatalf("rejected no-op policy must not inject MCP server: %+v", opts.MCPServers)
	}

	if err := m.SetMCPPolicy("channel-1", "user-1", "context7", true, "full"); err != nil {
		t.Fatalf("full preset should allow generic server: %v", err)
	}
	opts = m.agentOptsForChannel("channel-1")
	if len(opts.MCPServers) != 1 || opts.MCPServers[0].Name != "context7" {
		t.Fatalf("expected generic full policy injection: %+v", opts.MCPServers)
	}
}

func TestManagerDiscoversAndCachesMCPTools(t *testing.T) {
	dir := t.TempDir()
	modCacheRaw, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err != nil {
		t.Fatalf("go env GOMODCACHE: %v", err)
	}
	mockServer := filepath.Join(strings.TrimSpace(string(modCacheRaw)), "github.com/mark3labs/mcp-go@v0.45.0", "testdata", "mockstdio_server.go")
	mockBin := filepath.Join(dir, "mockstdio-server")
	buildMock := exec.Command("go", "build", "-o", mockBin, mockServer)
	if out, err := buildMock.CombinedOutput(); err != nil {
		t.Fatalf("build mock MCP server: %v\n%s", err, out)
	}
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"mock-tools":{"command":"`+mockBin+`"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	m := NewManager(ManagerConfig{DataDir: dir, GuildID: "guild-1"})
	defer m.StopAll()

	tools, err := m.DiscoverMCPTools(context.Background(), "mock-tools")
	if err != nil {
		t.Fatalf("discover tools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "test-tool" {
		t.Fatalf("tools = %+v", tools)
	}
	cached, err := m.MCPToolViews("channel-1", "mock-tools")
	if err != nil {
		t.Fatalf("cached tools: %v", err)
	}
	if len(cached) != 1 || cached[0].Name != "test-tool" || cached[0].Allowed {
		t.Fatalf("cached = %+v", cached)
	}
	if err := m.SetMCPTool("channel-1", "user-1", "mock-tools", "test-tool", true); err != nil {
		t.Fatalf("allow discovered tool: %v", err)
	}
	cached, err = m.MCPToolViews("channel-1", "mock-tools")
	if err != nil {
		t.Fatalf("cached tools after allow: %v", err)
	}
	if !cached[0].Allowed {
		t.Fatalf("tool should be allowed: %+v", cached)
	}
}

func TestManagerRefreshesMCPCatalogForStatusAndInjection(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"generic-tools":{"command":"/tmp/generic-mcp"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	m := NewManager(ManagerConfig{DataDir: dir, GuildID: "guild-1"})
	defer m.StopAll()

	if err := m.SetMCPTool("channel-1", "user-1", "generic-tools", "search", true); err != nil {
		t.Fatalf("allow generic tool: %v", err)
	}
	if len(m.agentOptsForChannel("channel-1").MCPServers) != 1 {
		t.Fatalf("expected enabled generic server to be injected")
	}

	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"new-tools":{"command":"/tmp/new-mcp"}}}`), 0644); err != nil {
		t.Fatalf("rewrite config: %v", err)
	}
	if got := m.MCPStatusChecklist("channel-1"); !strings.Contains(got, "new-tools") || strings.Contains(got, "generic-tools") {
		t.Fatalf("catalog status did not refresh: %s", got)
	}
	if servers := m.agentOptsForChannel("channel-1").MCPServers; len(servers) != 0 {
		t.Fatalf("removed catalog server must not be injected: %+v", servers)
	}
}

func TestManagerMCPStatusUsesLocale(t *testing.T) {
	L.Load("zh-TW")
	t.Cleanup(func() { L.Load("en") })

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"generic-tools":{"command":"/tmp/generic-mcp","args":["--stdio"]}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	m := NewManager(ManagerConfig{DataDir: dir, GuildID: "guild-1"})
	defer m.StopAll()

	checklist := m.MCPStatusChecklist("channel-1")
	if !strings.Contains(checklist, "MCP 檢查清單") || !strings.Contains(checklist, "未勾選") || strings.Contains(checklist, "Unchecked") {
		t.Fatalf("checklist was not localized:\n%s", checklist)
	}
	if !strings.Contains(checklist, "已停用") || !strings.Contains(checklist, "未開放工具") || strings.Contains(checklist, "read-only tools") {
		t.Fatalf("checklist status was not localized:\n%s", checklist)
	}

	status := m.MCPStatus("channel-1", "generic-tools")
	if !strings.Contains(status, "已停用") || !strings.Contains(status, "未開放工具") || strings.Contains(status, "allow all tools") {
		t.Fatalf("status was not localized:\n%s", status)
	}
}

func TestManagerGenericMCPToolPolicy(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"generic-tools":{"command":"/tmp/generic-tools"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	m := NewManager(ManagerConfig{DataDir: dir, GuildID: "guild-1"})
	defer m.StopAll()

	if err := m.SetMCPTool("channel-1", "user-1", "generic-tools", "search", true); err != nil {
		t.Fatalf("allow tool: %v", err)
	}
	opts := m.agentOptsForChannel("channel-1")
	var allowed []string
	if err := json.Unmarshal([]byte(opts.MCPServers[0].Env["MCP_PROXY_ALLOWED_TOOLS_JSON"]), &allowed); err != nil {
		t.Fatalf("allowed tools json: %v", err)
	}
	if len(allowed) != 1 || allowed[0] != "search" {
		t.Fatalf("allowed tools = %+v", allowed)
	}
	if err := m.SetMCPTool("channel-1", "user-1", "generic-tools", "search", false); err != nil {
		t.Fatalf("deny tool: %v", err)
	}
	opts = m.agentOptsForChannel("channel-1")
	if len(opts.MCPServers) != 0 {
		t.Fatalf("empty custom allowlist should stop injecting MCP server: %+v", opts.MCPServers)
	}
}

func proxyTargetEnv(t *testing.T, env map[string]string) map[string]string {
	t.Helper()
	var out map[string]string
	if err := json.Unmarshal([]byte(env["MCP_PROXY_ENV_JSON"]), &out); err != nil {
		t.Fatalf("proxy target env json: %v", err)
	}
	return out
}

func TestManagerMCPPolicyUpdateRecordsAuditEvent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(cfgPath, []byte(`{"mcpServers":{"generic-tools":{"command":"/tmp/generic-tools"}}}`), 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("KIRO_MCP_CONFIG", cfgPath)
	sink := &recordingAuditSink{}
	m := NewManager(ManagerConfig{DataDir: dir, GuildID: "guild-1", Audit: sink})
	defer m.StopAll()

	if err := m.SetMCPPolicy("channel-1", "user-1", "generic-tools", true, "full"); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	events := sink.Snapshot()
	if len(events) != 1 {
		t.Fatalf("expected one audit event, got %+v", events)
	}
	evt := events[0]
	if evt.Type != "mcp_policy_updated" || evt.ChannelID != "channel-1" || evt.UserID != "user-1" {
		t.Fatalf("unexpected audit event: %+v", evt)
	}
	if evt.Metadata["server_name"] != "generic-tools" || evt.Metadata["enabled"] != true || evt.Metadata["read_only"] != false {
		t.Fatalf("unexpected audit metadata: %+v", evt.Metadata)
	}
}
