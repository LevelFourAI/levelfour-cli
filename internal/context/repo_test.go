package context

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectTerraformRootWithTFFiles(t *testing.T) {
	tmp := t.TempDir()
	tfFile := filepath.Join(tmp, "main.tf")
	os.WriteFile(tfFile, []byte("resource \"aws_instance\" \"test\" {}"), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	root, err := DetectTerraformRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolvedTmp, _ := filepath.EvalSymlinks(tmp)
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if resolvedRoot != resolvedTmp {
		t.Errorf("root = %q, want %q", resolvedRoot, resolvedTmp)
	}
}

func TestDetectTerraformRootNoTFFiles(t *testing.T) {
	tmp := t.TempDir()

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	_, err := DetectTerraformRoot()
	if err == nil {
		t.Error("expected error when no .tf files exist")
	}
}

func TestDetectTerraformRootInSubdir(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "main.tf"), []byte(""), 0o644)
	sub := filepath.Join(tmp, "modules", "vpc")
	os.MkdirAll(sub, 0o755)

	origDir, _ := os.Getwd()
	os.Chdir(sub)
	defer os.Chdir(origDir)

	root, err := DetectTerraformRoot()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resolvedTmp, _ := filepath.EvalSymlinks(tmp)
	resolvedRoot, _ := filepath.EvalSymlinks(root)
	if resolvedRoot != resolvedTmp {
		t.Errorf("root = %q, want %q", resolvedRoot, resolvedTmp)
	}
}

func TestParseRemoteURLValid(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config")
	content := `[core]
	repositoryformatversion = 0
[remote "origin"]
	url = git@github.com:LevelFourAI/levelfour-cli.git
	fetch = +refs/heads/*:refs/remotes/origin/*
[branch "main"]
	remote = origin
`
	os.WriteFile(configPath, []byte(content), 0o644)

	url, err := parseRemoteURL(configPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "git@github.com:LevelFourAI/levelfour-cli.git" {
		t.Errorf("url = %q, want %q", url, "git@github.com:LevelFourAI/levelfour-cli.git")
	}
}

func TestParseRemoteURLNoOrigin(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config")
	content := `[core]
	repositoryformatversion = 0
[branch "main"]
	remote = origin
`
	os.WriteFile(configPath, []byte(content), 0o644)

	_, err := parseRemoteURL(configPath)
	if err == nil {
		t.Error("expected error when no remote origin exists")
	}
	if !strings.Contains(err.Error(), "no remote origin found") {
		t.Errorf("error = %q, want 'no remote origin found'", err.Error())
	}
}

func TestParseRemoteURLFileNotFound(t *testing.T) {
	_, err := parseRemoteURL("/nonexistent/path/config")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParseRemoteURLSectionBreakBeforeURL(t *testing.T) {
	tmp := t.TempDir()
	configPath := filepath.Join(tmp, "config")
	content := `[remote "origin"]
[branch "main"]
	remote = origin
`
	os.WriteFile(configPath, []byte(content), 0o644)

	_, err := parseRemoteURL(configPath)
	if err == nil {
		t.Error("expected error when origin section has no url")
	}
}

func TestDetectRepoWithGitConfig(t *testing.T) {
	tmp := t.TempDir()
	gitDir := filepath.Join(tmp, ".git")
	os.MkdirAll(gitDir, 0o755)
	content := `[remote "origin"]
	url = https://github.com/LevelFourAI/levelfour-cli.git
`
	os.WriteFile(filepath.Join(gitDir, "config"), []byte(content), 0o644)

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	url, err := DetectRepo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "https://github.com/LevelFourAI/levelfour-cli.git" {
		t.Errorf("url = %q, want %q", url, "https://github.com/LevelFourAI/levelfour-cli.git")
	}
}

func TestDetectRepoFromSubdir(t *testing.T) {
	tmp := t.TempDir()
	gitDir := filepath.Join(tmp, ".git")
	os.MkdirAll(gitDir, 0o755)
	content := `[remote "origin"]
	url = git@github.com:org/repo.git
`
	os.WriteFile(filepath.Join(gitDir, "config"), []byte(content), 0o644)
	sub := filepath.Join(tmp, "src", "pkg")
	os.MkdirAll(sub, 0o755)

	origDir, _ := os.Getwd()
	os.Chdir(sub)
	defer os.Chdir(origDir)

	url, err := DetectRepo()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "git@github.com:org/repo.git" {
		t.Errorf("url = %q, want %q", url, "git@github.com:org/repo.git")
	}
}

func TestDetectRepoNotGitRepo(t *testing.T) {
	tmp := t.TempDir()

	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	_, err := DetectRepo()
	if err == nil {
		t.Error("expected error when not in a git repo")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error = %q, want 'not a git repository'", err.Error())
	}
}

func TestDetectRepoGetwdError(t *testing.T) {
	origGetwd := getwd
	defer func() { getwd = origGetwd }()
	getwd = func() (string, error) {
		return "", fmt.Errorf("getwd failed")
	}

	_, err := DetectRepo()
	if err == nil {
		t.Error("expected error when getwd fails")
	}
	if !strings.Contains(err.Error(), "getwd failed") {
		t.Errorf("error = %q, want 'getwd failed'", err.Error())
	}
}

func TestDetectTerraformRootGetwdError(t *testing.T) {
	origGetwd := getwd
	defer func() { getwd = origGetwd }()
	getwd = func() (string, error) {
		return "", fmt.Errorf("getwd failed")
	}

	_, err := DetectTerraformRoot()
	if err == nil {
		t.Error("expected error when getwd fails")
	}
	if !strings.Contains(err.Error(), "getwd failed") {
		t.Errorf("error = %q, want 'getwd failed'", err.Error())
	}
}
