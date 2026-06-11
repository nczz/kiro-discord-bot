package channel

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const (
	maxProjectOptions = 25
)

var safeProjectNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

type ProjectOption struct {
	Name        string
	Path        string
	Relative    string
	Description string
}

type SteeringFileStatus struct {
	ProjectName string
	FileName    string
	Path        string
	Exists      bool
	Size        int64
}

func (m *Manager) ChannelInitialized(channelID string) bool {
	sess, ok := m.getChannelSession(channelID)
	if !ok || sess == nil {
		return false
	}
	return strings.TrimSpace(sess.CWD) != "" ||
		strings.TrimSpace(sess.SessionID) != "" ||
		strings.TrimSpace(sess.AgentName) != ""
}

func (m *Manager) DefaultProjectRoot() (string, error) {
	root := strings.TrimSpace(m.defaultCWD)
	if root == "" {
		return "", fmt.Errorf("DEFAULT_CWD is not configured")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve DEFAULT_CWD: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("DEFAULT_CWD not found: %s", root)
	}
	fi, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("DEFAULT_CWD not found: %s", root)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("DEFAULT_CWD is not a directory: %s", root)
	}
	return real, nil
}

func (m *Manager) ValidateInitialCWD(cwd string) (string, error) {
	root, err := m.DefaultProjectRoot()
	if err != nil {
		return "", err
	}
	real, err := m.validateExistingDir(cwd)
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(real, root) {
		return "", fmt.Errorf("initial channel project must be inside DEFAULT_CWD: %s", root)
	}
	return real, nil
}

func (m *Manager) InitializeChannelCWD(channelID, cwd string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	real, err := m.ValidateInitialCWD(cwd)
	if err != nil {
		return "", err
	}
	if err := EnsureProjectSteering(real); err != nil {
		return "", err
	}
	existing, _ := m.getChannelSession(channelID)
	newSess := &Session{CWD: real}
	if existing != nil {
		newSess.Model = existing.Model
	}
	if err := m.setChannelSession(channelID, newSess); err != nil {
		return "", err
	}
	return real, nil
}

func (m *Manager) ListDefaultProjects() ([]ProjectOption, error) {
	root, err := m.DefaultProjectRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	projects := make([]ProjectOption, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(root, name)
		projects = append(projects, ProjectOption{
			Name:        name,
			Path:        path,
			Relative:    name,
			Description: projectDescription(path, name),
		})
		if len(projects) >= maxProjectOptions {
			break
		}
	}
	sort.Slice(projects, func(i, j int) bool {
		return strings.ToLower(projects[i].Relative) < strings.ToLower(projects[j].Relative)
	})
	return projects, nil
}

func (m *Manager) CreateDefaultProject(name string) (ProjectOption, error) {
	root, err := m.DefaultProjectRoot()
	if err != nil {
		return ProjectOption{}, err
	}
	name = strings.TrimSpace(name)
	if !safeProjectNameRE.MatchString(name) || strings.Contains(name, "..") {
		return ProjectOption{}, fmt.Errorf("project name may only contain letters, numbers, dot, dash, and underscore")
	}
	path := filepath.Join(root, name)
	clean := filepath.Clean(path)
	if !pathWithinRoot(clean, root) {
		return ProjectOption{}, fmt.Errorf("project path escapes DEFAULT_CWD")
	}
	if _, err := os.Stat(clean); err == nil {
		return ProjectOption{}, fmt.Errorf("project already exists: %s", name)
	} else if !os.IsNotExist(err) {
		return ProjectOption{}, err
	}
	if err := os.MkdirAll(clean, 0755); err != nil {
		return ProjectOption{}, err
	}
	if err := EnsureProjectSteering(clean); err != nil {
		return ProjectOption{}, err
	}
	real, err := filepath.EvalSymlinks(clean)
	if err != nil {
		return ProjectOption{}, err
	}
	if !pathWithinRoot(real, root) {
		return ProjectOption{}, fmt.Errorf("project path escapes DEFAULT_CWD")
	}
	return ProjectOption{
		Name:        name,
		Path:        real,
		Relative:    name,
		Description: projectDescription(real, name),
	}, nil
}

func EnsureProjectSteering(projectPath string) error {
	return os.MkdirAll(filepath.Join(projectPath, ".kiro", "steering"), 0755)
}

func (m *Manager) ChannelSteeringStatus(channelID string) (SteeringFileStatus, error) {
	path, err := m.channelSteeringPath(channelID)
	if err != nil {
		return SteeringFileStatus{}, err
	}
	status := steeringFileStatus(path)
	if fi, err := os.Stat(path); err == nil {
		status.Exists = true
		status.Size = fi.Size()
	} else if !os.IsNotExist(err) {
		return SteeringFileStatus{}, err
	}
	return status, nil
}

func (m *Manager) EnsureChannelSteeringFile(channelID string) (SteeringFileStatus, bool, error) {
	path, err := m.channelSteeringPath(channelID)
	if err != nil {
		return SteeringFileStatus{}, false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return SteeringFileStatus{}, false, err
	}
	if fi, err := os.Stat(path); err == nil {
		status := steeringFileStatus(path)
		status.Exists = true
		status.Size = fi.Size()
		return status, false, nil
	} else if !os.IsNotExist(err) {
		return SteeringFileStatus{}, false, err
	}
	content := defaultSteeringTemplate(filepath.Base(m.CWDPath(channelID)))
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return SteeringFileStatus{}, false, err
	}
	status := steeringFileStatus(path)
	status.Exists = true
	status.Size = int64(len([]byte(content)))
	return status, true, nil
}

func (m *Manager) ReadChannelSteeringFile(channelID string) (SteeringFileStatus, string, error) {
	status, err := m.ChannelSteeringStatus(channelID)
	if err != nil {
		return SteeringFileStatus{}, "", err
	}
	if !status.Exists {
		return status, "", fmt.Errorf("project steering file does not exist")
	}
	raw, err := os.ReadFile(status.Path)
	if err != nil {
		return SteeringFileStatus{}, "", err
	}
	status.Size = int64(len(raw))
	return status, string(raw), nil
}

func (m *Manager) WriteChannelSteeringFile(channelID, content string) (SteeringFileStatus, error) {
	path, err := m.channelSteeringPath(channelID)
	if err != nil {
		return SteeringFileStatus{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return SteeringFileStatus{}, err
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return SteeringFileStatus{}, fmt.Errorf("project steering content is empty")
	}
	if err := os.WriteFile(path, []byte(content+"\n"), 0644); err != nil {
		return SteeringFileStatus{}, err
	}
	status := steeringFileStatus(path)
	status.Exists = true
	status.Size = int64(len([]byte(content + "\n")))
	return status, nil
}

func (m *Manager) channelSteeringPath(channelID string) (string, error) {
	if !m.ChannelInitialized(channelID) {
		return "", fmt.Errorf("channel is not initialized")
	}
	cwd, err := m.ValidateCWD(m.CWDPath(channelID))
	if err != nil {
		return "", err
	}
	fileName := projectSteeringFileName(filepath.Base(cwd))
	path := filepath.Join(cwd, ".kiro", "steering", fileName)
	clean := filepath.Clean(path)
	if !pathWithinRoot(clean, cwd) {
		return "", fmt.Errorf("project steering path escapes working directory")
	}
	return clean, nil
}

func steeringFileStatus(path string) SteeringFileStatus {
	return SteeringFileStatus{
		ProjectName: filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path)))),
		FileName:    filepath.Base(path),
		Path:        path,
	}
}

func projectSteeringFileName(projectName string) string {
	projectName = strings.TrimSpace(projectName)
	var sb strings.Builder
	lastDash := false
	for _, r := range projectName {
		keep := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-'
		if keep {
			sb.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			sb.WriteRune('-')
			lastDash = true
		}
	}
	stem := strings.Trim(sb.String(), ".-_")
	if stem == "" {
		stem = "project"
	}
	return stem + ".md"
}

func defaultSteeringTemplate(projectName string) string {
	projectName = strings.TrimSpace(projectName)
	if projectName == "" {
		projectName = "Project"
	}
	return fmt.Sprintf(`---
inclusion: always
---

# %s 專案規範

## 專案目標
- TODO: 描述此專案的產品目標、主要使用者與成功條件。

## 技術棧
- TODO: 記錄主要語言、框架、資料庫、外部服務與版本限制。

## 架構原則
- TODO: 說明目錄分層、模組邊界、命名慣例與不可違反的設計約束。

## 開發與驗證
- TODO: 記錄常用建置、測試、lint、部署或本機啟動指令。

## 安全與敏感資料
- 不要在 steering 檔案中放入 API key、token、password 或其他機敏資料。
`, projectName)
}

func (m *Manager) validateExistingDir(cwd string) (string, error) {
	if strings.TrimSpace(cwd) == "" {
		return "", fmt.Errorf("working directory is empty")
	}
	abs, err := filepath.Abs(cwd)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("working directory not found: %s", cwd)
	}
	if fi, err := os.Stat(real); err != nil {
		return "", fmt.Errorf("working directory not found: %s", cwd)
	} else if !fi.IsDir() {
		return "", fmt.Errorf("working directory is not a directory: %s", cwd)
	}
	return real, nil
}

func projectDescription(path, rel string) string {
	markers := []string{}
	for _, marker := range []string{".kiro", ".git", "go.mod", "package.json", "composer.json", "pyproject.toml"} {
		if _, err := os.Stat(filepath.Join(path, marker)); err == nil {
			markers = append(markers, marker)
		}
	}
	if len(markers) == 0 {
		return rel
	}
	return rel + " | " + strings.Join(markers, ", ")
}
