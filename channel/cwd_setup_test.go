package channel

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newCWDSetupTestManager(t *testing.T, root string) *Manager {
	t.Helper()
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("new session store: %v", err)
	}
	return NewManager(ManagerConfig{Store: store, DefaultCWD: root, AllowedCwdRoots: filepath.Dir(root)})
}

func TestInitializeChannelCWDRequiresDefaultRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	project := filepath.Join(root, "app")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatalf("mkdir outside: %v", err)
	}
	m := newCWDSetupTestManager(t, root)

	got, err := m.InitializeChannelCWD("channel-1", project)
	if err != nil {
		t.Fatalf("InitializeChannelCWD inside default root: %v", err)
	}
	wantProject, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatalf("EvalSymlinks project: %v", err)
	}
	if got != wantProject {
		t.Fatalf("initialized cwd = %q, want %q", got, wantProject)
	}
	if !m.ChannelInitialized("channel-1") {
		t.Fatal("channel should be initialized after selecting a project")
	}
	if _, err := os.Stat(filepath.Join(project, ".kiro", "steering")); err != nil {
		t.Fatalf("expected steering dir: %v", err)
	}

	if _, err := m.InitializeChannelCWD("channel-2", outside); err == nil {
		t.Fatal("expected outside DEFAULT_CWD to be rejected")
	}
}

func TestCreateDefaultProjectSanitizesAndInitializes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	m := newCWDSetupTestManager(t, root)

	project, err := m.CreateDefaultProject("customer-portal")
	if err != nil {
		t.Fatalf("CreateDefaultProject: %v", err)
	}
	if project.Relative != "customer-portal" {
		t.Fatalf("relative = %q, want customer-portal", project.Relative)
	}
	if _, err := os.Stat(filepath.Join(project.Path, ".kiro", "steering")); err != nil {
		t.Fatalf("expected steering dir: %v", err)
	}
	if _, err := m.CreateDefaultProject("../escape"); err == nil {
		t.Fatal("expected unsafe project name to be rejected")
	}
	if _, err := m.CreateDefaultProject("customer-portal"); err == nil {
		t.Fatal("expected duplicate project to be rejected")
	}
}

func TestCreateDefaultProjectAllowsUnicodeNames(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	m := newCWDSetupTestManager(t, root)

	project, err := m.CreateDefaultProject("專案_2026-測試.v1")
	if err != nil {
		t.Fatalf("CreateDefaultProject unicode name: %v", err)
	}
	if project.Relative != "專案_2026-測試.v1" {
		t.Fatalf("relative = %q, want unicode project name", project.Relative)
	}
	if _, err := os.Stat(filepath.Join(project.Path, ".kiro", "steering")); err != nil {
		t.Fatalf("expected steering dir: %v", err)
	}
}

func TestCreateDefaultProjectRejectsUnsafeUnicodeNames(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	m := newCWDSetupTestManager(t, root)

	for _, name := range []string{
		"../escape",
		"專案/子目錄",
		"專案\\子目錄",
		"專案 名稱",
		"-leading-dash",
		".leading-dot",
		strings.Repeat("專", maxProjectNameRunes+1),
	} {
		if _, err := m.CreateDefaultProject(name); err == nil {
			t.Fatalf("expected project name %q to be rejected", name)
		}
	}
}

func TestListDefaultProjectsListsFirstLevelDirectoriesAndMarksGit(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects")
	app := filepath.Join(root, "app")
	plain := filepath.Join(root, "plain")
	if err := os.MkdirAll(filepath.Join(app, ".git"), 0755); err != nil {
		t.Fatalf("mkdir app marker: %v", err)
	}
	if err := os.MkdirAll(plain, 0755); err != nil {
		t.Fatalf("mkdir plain: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(plain, "nested-project", ".git"), 0755); err != nil {
		t.Fatalf("mkdir nested marker: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("not a project dir"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	m := newCWDSetupTestManager(t, root)

	projects, err := m.ListDefaultProjects()
	if err != nil {
		t.Fatalf("ListDefaultProjects: %v", err)
	}
	if len(projects) != 2 {
		t.Fatalf("projects = %+v, want app and plain", projects)
	}
	if projects[0].Relative != "app" || projects[1].Relative != "plain" {
		t.Fatalf("projects = %+v, want first-level dirs sorted by name", projects)
	}
	if projects[0].Description != "app | .git" {
		t.Fatalf("app description = %q, want git marker", projects[0].Description)
	}
	if projects[1].Description != "plain" {
		t.Fatalf("plain description = %q, want plain relative path", projects[1].Description)
	}
}

func TestListDefaultProjectsReturnsAllFirstLevelDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	for i := 1; i <= 30; i++ {
		name := fmt.Sprintf("project-%02d", i)
		if i == 30 {
			name = "unicode-project-測試-30"
		}
		if err := os.MkdirAll(filepath.Join(root, name), 0755); err != nil {
			t.Fatalf("mkdir project %s: %v", name, err)
		}
	}
	m := newCWDSetupTestManager(t, root)

	projects, err := m.ListDefaultProjects()
	if err != nil {
		t.Fatalf("ListDefaultProjects: %v", err)
	}
	if len(projects) != 30 {
		t.Fatalf("projects len = %d, want all first-level dirs", len(projects))
	}
	if projects[len(projects)-1].Relative != "unicode-project-測試-30" {
		t.Fatalf("last project = %q, want unicode project included", projects[len(projects)-1].Relative)
	}
}

func TestChannelInitializedAcceptsLegacySession(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatalf("mkdir root: %v", err)
	}
	m := newCWDSetupTestManager(t, root)
	if err := m.setChannelSession("channel-1", &Session{SessionID: "legacy-session"}); err != nil {
		t.Fatalf("set channel session: %v", err)
	}
	if !m.ChannelInitialized("channel-1") {
		t.Fatal("legacy active session should be treated as initialized")
	}
	if m.ChannelInitialized("channel-2") {
		t.Fatal("unknown channel should not be initialized")
	}
}

func TestChannelSteeringFileLifecycle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "projects")
	project := filepath.Join(root, "客戶 Portal")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	m := newCWDSetupTestManager(t, root)
	if _, err := m.InitializeChannelCWD("channel-1", project); err != nil {
		t.Fatalf("InitializeChannelCWD: %v", err)
	}

	status, err := m.ChannelSteeringStatus("channel-1")
	if err != nil {
		t.Fatalf("ChannelSteeringStatus: %v", err)
	}
	if status.Exists {
		t.Fatal("steering file should not exist before create")
	}
	if status.FileName != "客戶-Portal.md" {
		t.Fatalf("file name = %q, want sanitized project name", status.FileName)
	}

	status, created, err := m.EnsureChannelSteeringFile("channel-1")
	if err != nil {
		t.Fatalf("EnsureChannelSteeringFile: %v", err)
	}
	if !created || !status.Exists {
		t.Fatalf("created=%v status=%+v, want created file", created, status)
	}
	_, content, err := m.ReadChannelSteeringFile("channel-1")
	if err != nil {
		t.Fatalf("ReadChannelSteeringFile: %v", err)
	}
	if !strings.Contains(content, "inclusion: always") || !strings.Contains(content, "安全與敏感資料") {
		t.Fatalf("unexpected steering template:\n%s", content)
	}

	status, err = m.WriteChannelSteeringFile("channel-1", "---\ninclusion: always\n---\n\n# Updated")
	if err != nil {
		t.Fatalf("WriteChannelSteeringFile: %v", err)
	}
	if status.Size == 0 {
		t.Fatalf("status = %+v, want size", status)
	}
}

func TestBuildSteeringContentUsesDraftInput(t *testing.T) {
	content := BuildSteeringContent("customer-portal", SteeringDraft{
		Background:   "Serve customer support workflows\nReduce manual lookup time",
		WorkingStyle: "Answer in Traditional Chinese\nState assumptions first",
		References:   "docs/faq.md\ngo test ./...",
		Constraints:  "Do not commit generated secrets",
	})

	for _, want := range []string{
		"inclusion: always",
		"# customer-portal Agent Context",
		"## 背景與目標",
		"- Serve customer support workflows",
		"- Reduce manual lookup time",
		"## 希望 agent 記住的工作方式",
		"- Answer in Traditional Chinese",
		"- State assumptions first",
		"## 常用資訊、路徑或驗證方式",
		"- docs/faq.md",
		"- go test ./...",
		"## 限制、禁忌與安全注意事項",
		"- Do not commit generated secrets",
		"不要在 steering 檔案中放入 API key",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("content missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "TODO") {
		t.Fatalf("content should be generated from draft without TODO placeholders:\n%s", content)
	}
	if strings.Contains(content, "其他補充 context") {
		t.Fatalf("empty optional sections should be omitted:\n%s", content)
	}
}
