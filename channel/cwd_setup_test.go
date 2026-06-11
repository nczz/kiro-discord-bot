package channel

import (
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
