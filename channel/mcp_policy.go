package channel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/nczz/kiro-discord-bot/acp"
	"github.com/nczz/kiro-discord-bot/mcpproxy"
	_ "modernc.org/sqlite"
)

type MCPCatalogEntry struct {
	Name    string
	Command string
	Args    []string
	Env     map[string]string
	URL     string
	Source  string
}

type MCPChannelPolicy struct {
	GuildID          string
	ChannelID        string
	ServerName       string
	Enabled          bool
	AllowedTools     []string
	AllowAllTools    bool
	Preset           string
	ReadOnly         bool
	AllowDestructive bool
	UpdatedBy        string
	UpdatedAt        time.Time
}

type MCPToolInfo struct {
	ServerName   string
	Name         string
	Description  string
	InputSchema  string
	DiscoveredAt time.Time
}

type MCPPolicyStore struct {
	mu      sync.RWMutex
	db      *sql.DB
	catalog map[string]MCPCatalogEntry
}

func OpenMCPPolicyStore(dataDir string) (*MCPPolicyStore, error) {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = "./data"
	}
	path := filepath.Join(dataDir, "mcp", "policy.sqlite")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &MCPPolicyStore{db: db, catalog: loadMCPCatalog()}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	_ = s.upsertCatalog(context.Background(), s.catalog)
	return s, nil
}

func (s *MCPPolicyStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *MCPPolicyStore) init() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA busy_timeout=5000`,
		`CREATE TABLE IF NOT EXISTS mcp_catalog (
			server_name TEXT PRIMARY KEY,
			config_json TEXT NOT NULL,
			source TEXT NOT NULL,
			discovered_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS channel_mcp_policies (
			guild_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			server_name TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 0,
			allowed_tools_json TEXT NOT NULL DEFAULT '[]',
			allow_all_tools INTEGER NOT NULL DEFAULT 0,
			preset TEXT NOT NULL DEFAULT '',
			read_only INTEGER NOT NULL DEFAULT 1,
			allow_destructive INTEGER NOT NULL DEFAULT 0,
			updated_by TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (guild_id, channel_id, server_name)
		)`,
		`CREATE TABLE IF NOT EXISTS mcp_policy_versions (
			guild_id TEXT NOT NULL,
			channel_id TEXT NOT NULL,
			version INTEGER NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (guild_id, channel_id)
		)`,
		`CREATE TABLE IF NOT EXISTS mcp_tool_catalog (
			server_name TEXT NOT NULL,
			tool_name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			input_schema_json TEXT NOT NULL DEFAULT '{}',
			discovered_at TEXT NOT NULL,
			PRIMARY KEY (server_name, tool_name)
		)`,
		`CREATE TABLE IF NOT EXISTS mcp_policy_migrations (
			migration_key TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL,
			catalog_fingerprint TEXT NOT NULL,
			server_names_json TEXT NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	return s.ensureChannelMCPPolicyColumns()
}

func (s *MCPPolicyStore) ensureChannelMCPPolicyColumns() error {
	columns := []struct {
		name string
		def  string
	}{
		{name: "allow_all_tools", def: "INTEGER NOT NULL DEFAULT 0"},
		{name: "preset", def: "TEXT NOT NULL DEFAULT ''"},
		{name: "read_only", def: "INTEGER NOT NULL DEFAULT 1"},
		{name: "allow_destructive", def: "INTEGER NOT NULL DEFAULT 0"},
		{name: "updated_by", def: "TEXT NOT NULL DEFAULT ''"},
		{name: "updated_at", def: "TEXT NOT NULL DEFAULT ''"},
	}
	for _, column := range columns {
		stmt := fmt.Sprintf("ALTER TABLE channel_mcp_policies ADD COLUMN %s %s", column.name, column.def)
		if _, err := s.db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column") {
			return err
		}
	}
	return nil
}

func (s *MCPPolicyStore) CachedTools(ctx context.Context, serverName string) ([]MCPToolInfo, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT server_name, tool_name, description, input_schema_json, discovered_at
		FROM mcp_tool_catalog WHERE server_name=? ORDER BY tool_name`, strings.TrimSpace(serverName))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MCPToolInfo
	for rows.Next() {
		var item MCPToolInfo
		var ts string
		if err := rows.Scan(&item.ServerName, &item.Name, &item.Description, &item.InputSchema, &ts); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			item.DiscoveredAt = t
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *MCPPolicyStore) DiscoverTools(ctx context.Context, serverName string) ([]MCPToolInfo, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("mcp policy store is not available")
	}
	serverName = strings.TrimSpace(serverName)
	if err := s.RefreshCatalog(ctx); err != nil {
		return nil, err
	}
	entry, ok := s.CatalogEntry(serverName)
	if !ok {
		return nil, fmt.Errorf("mcp server %q was not found in catalog", serverName)
	}

	var c *mcpclient.Client
	var err error
	if entry.URL != "" {
		c, err = mcpclient.NewStreamableHttpClient(entry.URL)
	} else {
		env := make([]string, 0, len(entry.Env))
		for k, v := range entry.Env {
			env = append(env, k+"="+v)
		}
		sort.Strings(env)
		c, err = mcpclient.NewStdioMCPClient(entry.Command, env, entry.Args...)
	}
	if err != nil {
		return nil, err
	}
	defer c.Close()
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "kiro-discord-bot-mcp-discovery", Version: "1"}
	initReq.Params.Capabilities = mcp.ClientCapabilities{}
	if _, err := c.Initialize(ctx, initReq); err != nil {
		return nil, err
	}
	result, err := c.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tools := make([]MCPToolInfo, 0, len(result.Tools))
	for _, tool := range result.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			continue
		}
		rawSchema, _ := json.Marshal(tool.InputSchema)
		tools = append(tools, MCPToolInfo{
			ServerName:   serverName,
			Name:         strings.TrimSpace(tool.Name),
			Description:  strings.TrimSpace(tool.Description),
			InputSchema:  string(rawSchema),
			DiscoveredAt: now,
		})
	}
	if err := s.replaceTools(ctx, serverName, tools, now); err != nil {
		return nil, err
	}
	log.Printf("[mcp-policy] discovered tools server=%s count=%d", serverName, len(tools))
	return tools, nil
}

func (s *MCPPolicyStore) replaceTools(ctx context.Context, serverName string, tools []MCPToolInfo, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_tool_catalog WHERE server_name=?`, serverName); err != nil {
		return err
	}
	for _, tool := range tools {
		if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_tool_catalog(server_name, tool_name, description, input_schema_json, discovered_at)
			VALUES(?, ?, ?, ?, ?)`,
			serverName, tool.Name, tool.Description, tool.InputSchema, now.Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *MCPPolicyStore) RefreshCatalog(ctx context.Context) error {
	if s == nil {
		return nil
	}
	catalog := loadMCPCatalog()
	s.mu.Lock()
	s.catalog = catalog
	s.mu.Unlock()
	return s.upsertCatalog(ctx, catalog)
}

func (s *MCPPolicyStore) upsertCatalog(ctx context.Context, catalog map[string]MCPCatalogEntry) error {
	for name, entry := range catalog {
		raw, err := json.Marshal(redactedCatalogEntry(entry))
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO mcp_catalog(server_name, config_json, source, discovered_at)
			VALUES(?, ?, ?, ?)
			ON CONFLICT(server_name) DO UPDATE SET config_json=excluded.config_json, source=excluded.source, discovered_at=excluded.discovered_at`,
			name, string(raw), entry.Source, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}

func redactedCatalogEntry(entry MCPCatalogEntry) MCPCatalogEntry {
	cp := entry
	if len(cp.Env) > 0 {
		cp.Env = make(map[string]string, len(entry.Env))
		for k := range entry.Env {
			cp.Env[k] = "<redacted>"
		}
	}
	return cp
}

func (s *MCPPolicyStore) Catalog() []MCPCatalogEntry {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MCPCatalogEntry, 0, len(s.catalog))
	for _, entry := range s.catalog {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *MCPPolicyStore) CatalogEntry(name string) (MCPCatalogEntry, bool) {
	if s == nil {
		return MCPCatalogEntry{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.catalog[strings.TrimSpace(name)]
	return entry, ok
}

func (s *MCPPolicyStore) GetPolicy(ctx context.Context, guildID, channelID, serverName string) (MCPChannelPolicy, error) {
	p := defaultMCPPolicy(guildID, channelID, serverName)
	if s == nil || s.db == nil {
		return p, nil
	}
	var enabled, readOnly, destructive int
	var allowAll int
	var toolsRaw, updatedAt string
	err := s.db.QueryRowContext(ctx, `SELECT enabled, allowed_tools_json, allow_all_tools, preset, read_only, allow_destructive, updated_by, updated_at
		FROM channel_mcp_policies WHERE guild_id=? AND channel_id=? AND server_name=?`,
		guildID, channelID, serverName).Scan(&enabled, &toolsRaw, &allowAll, &p.Preset, &readOnly, &destructive, &p.UpdatedBy, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	p.Enabled = enabled != 0
	p.AllowAllTools = allowAll != 0
	p.ReadOnly = readOnly != 0
	p.AllowDestructive = destructive != 0
	_ = json.Unmarshal([]byte(toolsRaw), &p.AllowedTools)
	if t, err := time.Parse(time.RFC3339, updatedAt); err == nil {
		p.UpdatedAt = t
	}
	return p, nil
}

func (s *MCPPolicyStore) SetPolicy(ctx context.Context, p MCPChannelPolicy) error {
	if s == nil || s.db == nil {
		return errors.New("mcp policy store is not available")
	}
	p.ServerName = strings.TrimSpace(p.ServerName)
	if p.ServerName == "" {
		return errors.New("server name is empty")
	}
	toolsRaw, err := json.Marshal(normalizeStrings(p.AllowedTools))
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO channel_mcp_policies(
		guild_id, channel_id, server_name, enabled, allowed_tools_json, allow_all_tools, preset, read_only, allow_destructive, updated_by, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(guild_id, channel_id, server_name) DO UPDATE SET
		enabled=excluded.enabled,
		allowed_tools_json=excluded.allowed_tools_json,
		allow_all_tools=excluded.allow_all_tools,
		preset=excluded.preset,
		read_only=excluded.read_only,
		allow_destructive=excluded.allow_destructive,
		updated_by=excluded.updated_by,
		updated_at=excluded.updated_at`,
		p.GuildID, p.ChannelID, p.ServerName, boolInt(p.Enabled), string(toolsRaw), boolInt(p.AllowAllTools), p.Preset, boolInt(p.ReadOnly), boolInt(p.AllowDestructive), p.UpdatedBy, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO mcp_policy_versions(guild_id, channel_id, version, updated_at)
		VALUES(?, ?, 1, ?)
		ON CONFLICT(guild_id, channel_id) DO UPDATE SET version=version+1, updated_at=excluded.updated_at`,
		p.GuildID, p.ChannelID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MCPPolicyStore) MigrationApplied(ctx context.Context, key string) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	var appliedAt string
	err := s.db.QueryRowContext(ctx, `SELECT applied_at FROM mcp_policy_migrations WHERE migration_key=?`, strings.TrimSpace(key)).Scan(&appliedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *MCPPolicyStore) MarkMigrationApplied(ctx context.Context, key string, catalog []MCPCatalogEntry) error {
	if s == nil || s.db == nil {
		return nil
	}
	names := make([]string, 0, len(catalog))
	for _, entry := range catalog {
		names = append(names, entry.Name)
	}
	sort.Strings(names)
	raw, err := json.Marshal(names)
	if err != nil {
		return err
	}
	fingerprint := strings.Join(names, "\n")
	_, err = s.db.ExecContext(ctx, `INSERT INTO mcp_policy_migrations(migration_key, applied_at, catalog_fingerprint, server_names_json)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(migration_key) DO NOTHING`,
		strings.TrimSpace(key), time.Now().UTC().Format(time.RFC3339), fingerprint, string(raw))
	return err
}

func (s *MCPPolicyStore) HasExplicitPolicy(ctx context.Context, guildID, channelID, serverName string) (bool, error) {
	if s == nil || s.db == nil {
		return false, nil
	}
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM channel_mcp_policies WHERE guild_id=? AND channel_id=? AND server_name=?`,
		guildID, channelID, serverName).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (s *MCPPolicyStore) EnabledPolicies(ctx context.Context, guildID, channelID string) ([]MCPChannelPolicy, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT server_name FROM channel_mcp_policies WHERE guild_id=? AND channel_id=? AND enabled=1`, guildID, channelID)
	if err != nil {
		return nil, err
	}
	var names []string
	for rows.Next() {
		var serverName string
		if err := rows.Scan(&serverName); err != nil {
			return nil, err
		}
		names = append(names, serverName)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var out []MCPChannelPolicy
	for _, serverName := range names {
		p, err := s.GetPolicy(ctx, guildID, channelID, serverName)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func defaultMCPPolicy(guildID, channelID, serverName string) MCPChannelPolicy {
	return MCPChannelPolicy{
		GuildID:    guildID,
		ChannelID:  channelID,
		ServerName: serverName,
		ReadOnly:   true,
	}
}

func (p MCPChannelPolicy) EffectiveTools() []string {
	if p.AllowAllTools {
		return nil
	}
	if p.ReadOnly {
		return nil
	}
	return normalizeStrings(p.AllowedTools)
}

func (p MCPChannelPolicy) ApplyPreset(preset string) MCPChannelPolicy {
	switch strings.TrimSpace(preset) {
	case "read":
		p.Preset = "read"
		p.ReadOnly = true
		p.AllowDestructive = false
		p.AllowAllTools = false
		p.AllowedTools = nil
	case "safe-write":
		p.Preset = "safe-write"
		p.ReadOnly = true
		p.AllowDestructive = false
		p.AllowAllTools = false
		p.AllowedTools = nil
	case "full":
		p.Preset = "full"
		p.ReadOnly = false
		p.AllowDestructive = true
		p.AllowAllTools = true
		p.AllowedTools = nil
	default:
		p.Preset = ""
	}
	return p
}

func (p MCPChannelPolicy) ToACPServer(entry MCPCatalogEntry, proxyCommand string, guildID, channelID string) acp.MCPServerConfig {
	allowedTools := p.EffectiveTools()
	proxyEnv := map[string]string{}

	var envItems []string
	if entry.URL != "" {
		envItems = mcpproxy.ConfigEnvURL(entry.URL, allowedTools, p.AllowAllTools)
	} else {
		env := make(map[string]string, len(entry.Env))
		for k, v := range entry.Env {
			env[k] = v
		}
		envItems = mcpproxy.ConfigEnv(entry.Command, entry.Args, env, allowedTools, p.AllowAllTools)
	}
	for _, item := range envItems {
		k, v, ok := strings.Cut(item, "=")
		if ok {
			proxyEnv[k] = v
		}
	}
	return acp.MCPServerConfig{
		Name:    entry.Name,
		Command: proxyCommand,
		Args:    []string{"mcp-proxy"},
		Env:     proxyEnv,
	}
}

func loadMCPCatalog() map[string]MCPCatalogEntry {
	out := make(map[string]MCPCatalogEntry)
	for _, path := range candidateMCPConfigPaths() {
		entries, err := readMCPConfig(path)
		if err != nil {
			continue
		}
		for name, entry := range entries {
			entry.Source = path
			out[name] = entry
		}
	}
	return out
}

func candidateMCPConfigPaths() []string {
	var paths []string
	if p := strings.TrimSpace(os.Getenv("KIRO_MCP_CONFIG")); p != "" {
		paths = append(paths, p)
	}
	if home := strings.TrimSpace(os.Getenv("KIRO_HOME")); home != "" {
		paths = append(paths, filepath.Join(home, "settings", "mcp.json"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		paths = append(paths, filepath.Join(home, ".kiro", "settings", "mcp.json"))
	}
	return paths
}

func readMCPConfig(path string) (map[string]MCPCatalogEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc struct {
		MCPServers map[string]struct {
			Command string            `json:"command"`
			Args    []string          `json:"args"`
			Env     map[string]string `json:"env"`
			URL     string            `json:"url"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make(map[string]MCPCatalogEntry, len(doc.MCPServers))
	for name, cfg := range doc.MCPServers {
		name = strings.TrimSpace(name)
		if name == "" || (strings.TrimSpace(cfg.Command) == "" && strings.TrimSpace(cfg.URL) == "") {
			continue
		}
		env := make(map[string]string, len(cfg.Env))
		for k, v := range cfg.Env {
			env[k] = v
		}
		out[name] = MCPCatalogEntry{Name: name, Command: cfg.Command, Args: cfg.Args, Env: env, URL: strings.TrimSpace(cfg.URL)}
	}
	return out, nil
}

func normalizeStrings(in []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
