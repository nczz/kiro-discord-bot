package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrMaterializedDrift = errors.New("materialized skill file drifted")

type MaterializedFile struct {
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	SHA256       string `json:"sha256"`
}

func Materialize(projectCWD, slug, content string, overwrite bool) (MaterializedFile, error) {
	projectCWD = strings.TrimSpace(projectCWD)
	if projectCWD == "" {
		return MaterializedFile{}, fmt.Errorf("project_cwd is required")
	}
	rawSlug := strings.TrimSpace(slug)
	if strings.Contains(rawSlug, "..") || strings.ContainsAny(rawSlug, `/\:`) || strings.HasPrefix(rawSlug, ".") {
		return MaterializedFile{}, fmt.Errorf("unsafe skill slug %q", rawSlug)
	}
	slug = NormalizeSlug(rawSlug)
	if err := ValidateSlug(slug); err != nil {
		return MaterializedFile{}, err
	}
	root, err := filepath.Abs(projectCWD)
	if err != nil {
		return MaterializedFile{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return MaterializedFile{}, fmt.Errorf("project_cwd: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return MaterializedFile{}, fmt.Errorf("project_cwd: %w", err)
	}
	if !info.IsDir() {
		return MaterializedFile{}, fmt.Errorf("project_cwd is not a directory")
	}
	rel := filepath.Join(".kiro-bot", "skills", slug, "SKILL.md")
	path := filepath.Join(root, rel)
	clean := filepath.Clean(path)
	skillsRoot := filepath.Join(root, ".kiro-bot", "skills")
	if !pathWithinRoot(clean, skillsRoot) {
		return MaterializedFile{}, fmt.Errorf("materialized path escapes project skills directory")
	}
	if err := rejectSymlinkAncestors(root, filepath.Dir(clean)); err != nil {
		return MaterializedFile{}, err
	}
	sha := ContentSHA256(content)
	if raw, err := os.ReadFile(clean); err == nil {
		if ContentSHA256(string(raw)) != sha && !overwrite {
			return MaterializedFile{}, ErrMaterializedDrift
		}
	} else if err != nil && !os.IsNotExist(err) {
		return MaterializedFile{}, err
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0755); err != nil {
		return MaterializedFile{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(clean), ".SKILL.md.tmp-*")
	if err != nil {
		return MaterializedFile{}, err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return MaterializedFile{}, err
	}
	if err := tmp.Chmod(0644); err != nil {
		_ = tmp.Close()
		return MaterializedFile{}, err
	}
	if err := tmp.Close(); err != nil {
		return MaterializedFile{}, err
	}
	if err := os.Rename(tmpName, clean); err != nil {
		return MaterializedFile{}, err
	}
	removeTmp = false
	return MaterializedFile{Path: clean, RelativePath: filepath.ToSlash(rel), SHA256: sha}, nil
}

func ValidateProjectCWD(cwd string, allowedRoots []string) (string, error) {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "", fmt.Errorf("project_cwd is required")
	}
	real, err := filepath.Abs(cwd)
	if err != nil {
		return "", err
	}
	real, err = filepath.EvalSymlinks(real)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project_cwd is not a directory")
	}
	if len(allowedRoots) == 0 {
		return real, nil
	}
	for _, root := range allowedRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return "", err
		}
		absRoot, err = filepath.EvalSymlinks(absRoot)
		if err != nil {
			return "", err
		}
		if pathWithinRoot(real, absRoot) {
			return real, nil
		}
	}
	return "", fmt.Errorf("project_cwd is outside allowed roots")
}

func rejectSymlinkAncestors(root, dir string) error {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	cur := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if part == "" || part == "." {
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("materialized skill path contains symlink ancestor %s", cur)
		}
		if !info.IsDir() {
			return fmt.Errorf("materialized skill path ancestor is not a directory: %s", cur)
		}
	}
	return nil
}

func pathWithinRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
