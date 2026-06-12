package kirosettings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var allowedCLISettingPrefixes = []string{
	"app.",
	"chat.",
	"inline.",
}

// EnsureRuntimeSettings prepares an isolated Kiro runtime settings directory.
// MCP config is always bot-managed and empty; selected non-MCP CLI feature
// settings are copied into the runtime so Kiro built-ins such as todo and
// knowledge keep the same behavior as the user's normal CLI.
func EnsureRuntimeSettings(runtimeHome string) (string, error) {
	runtimeHome = strings.TrimSpace(runtimeHome)
	if runtimeHome == "" {
		return "", fmt.Errorf("runtime home is required")
	}
	settingsDir := filepath.Join(runtimeHome, "settings")
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		return "", err
	}
	if err := syncCLISettings(settingsDir, runtimeHome); err != nil {
		return "", err
	}
	if err := syncAgentConfigs(runtimeHome); err != nil {
		return "", err
	}
	mcpPath := filepath.Join(settingsDir, "mcp.json")
	if err := writeFileAtomic(mcpPath, []byte("{\"mcpServers\":{}}\n"), 0644); err != nil {
		return "", err
	}
	return mcpPath, nil
}

func syncAgentConfigs(runtimeHome string) error {
	sourceDir := findSourceAgentDir(runtimeHome)
	if sourceDir == "" {
		return nil
	}
	targetDir := filepath.Join(runtimeHome, "agents")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return fmt.Errorf("read source agents: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			continue
		}
		sanitized, err := sanitizeAgentConfig(raw)
		if err != nil {
			continue
		}
		if err := writeFileAtomic(filepath.Join(targetDir, entry.Name()), sanitized, 0644); err != nil {
			return err
		}
	}
	return nil
}

func findSourceAgentDir(runtimeHome string) string {
	candidates := make([]string, 0, 3)
	if home := strings.TrimSpace(os.Getenv("KIRO_HOME")); home != "" && filepath.Clean(home) != filepath.Clean(runtimeHome) {
		candidates = append(candidates, filepath.Join(home, "agents"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".kiro", "agents"))
		candidates = append(candidates, filepath.Join(home, ".kiro", ".kiro", "agents"))
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			return path
		}
	}
	return ""
}

func sanitizeAgentConfig(raw []byte) ([]byte, error) {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	delete(cfg, "mcpServers")
	delete(cfg, "allowedTools")
	cfg["mcpServers"] = json.RawMessage(`{}`)
	cfg["includeMcpJson"] = json.RawMessage(`false`)
	cfg["useLegacyMcpJson"] = json.RawMessage(`false`)
	if toolsRaw, ok := cfg["tools"]; ok {
		var tools []string
		if err := json.Unmarshal(toolsRaw, &tools); err == nil {
			filtered := make([]string, 0, len(tools))
			for _, tool := range tools {
				if strings.HasPrefix(strings.TrimSpace(tool), "@") {
					continue
				}
				filtered = append(filtered, tool)
			}
			raw, err := json.Marshal(filtered)
			if err != nil {
				return nil, err
			}
			cfg["tools"] = raw
		}
	}
	return marshalStableObject(cfg)
}

func syncCLISettings(settingsDir, runtimeHome string) error {
	dstPath := filepath.Join(settingsDir, "cli.json")
	merged := map[string]json.RawMessage{}
	if raw, err := os.ReadFile(dstPath); err == nil {
		_ = json.Unmarshal(raw, &merged)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read runtime cli settings: %w", err)
	}
	sourcePath := findSourceCLISettings(runtimeHome)
	if sourcePath != "" {
		raw, err := os.ReadFile(sourcePath)
		if err != nil {
			return fmt.Errorf("read source cli settings: %w", err)
		}
		var source map[string]json.RawMessage
		if err := json.Unmarshal(raw, &source); err != nil {
			return fmt.Errorf("parse source cli settings: %w", err)
		}
		for key, value := range source {
			if allowedCLISetting(key) {
				merged[key] = value
			}
		}
	}
	raw, err := marshalStableObject(merged)
	if err != nil {
		return err
	}
	return writeFileAtomic(dstPath, raw, 0644)
}

func findSourceCLISettings(runtimeHome string) string {
	candidates := make([]string, 0, 3)
	if home := strings.TrimSpace(os.Getenv("KIRO_HOME")); home != "" && filepath.Clean(home) != filepath.Clean(runtimeHome) {
		candidates = append(candidates, filepath.Join(home, "settings", "cli.json"))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".kiro", "settings", "cli.json"))
		candidates = append(candidates, filepath.Join(home, ".kiro", ".kiro", "settings", "cli.json"))
	}
	for _, path := range candidates {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

func allowedCLISetting(key string) bool {
	key = strings.TrimSpace(key)
	for _, prefix := range allowedCLISettingPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func marshalStableObject(values map[string]json.RawMessage) ([]byte, error) {
	if len(values) == 0 {
		return []byte("{}\n"), nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, key := range keys {
		k, _ := json.Marshal(key)
		buf.WriteString("  ")
		buf.Write(k)
		buf.WriteString(": ")
		buf.Write(bytes.TrimSpace(values[key]))
		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}

func writeFileAtomic(path string, raw []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
