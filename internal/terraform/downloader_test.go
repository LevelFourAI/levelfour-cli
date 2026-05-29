package terraform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewModuleDownloader(t *testing.T) {
	dir := t.TempDir()
	d := NewModuleDownloader(dir)
	if d.cacheDir != filepath.Join(dir, ".levelfour", "modules") {
		t.Errorf("unexpected cacheDir: %s", d.cacheDir)
	}
	if d.registry == nil {
		t.Error("expected non-nil registry")
	}
	if _, err := os.Stat(d.cacheDir); err != nil {
		t.Errorf("expected cache dir to exist: %v", err)
	}
}

func TestCacheKey(t *testing.T) {
	key := cacheKey("terraform-aws-modules/rds/aws", "6.13.1")
	if key == "" {
		t.Fatal("expected non-empty key")
	}
	if !strings.Contains(key, "6.13.1") {
		t.Errorf("expected version in key, got %s", key)
	}
	key2 := cacheKey("terraform-aws-modules/rds/aws", "6.12.0")
	if key == key2 {
		t.Error("expected different keys for different versions")
	}
}

func TestCacheKey_Deterministic(t *testing.T) {
	key1 := cacheKey("terraform-aws-modules/vpc/aws", "3.0.0")
	key2 := cacheKey("terraform-aws-modules/vpc/aws", "3.0.0")
	if key1 != key2 {
		t.Error("expected same key for same input")
	}
}

func TestParseGitURL(t *testing.T) {
	tests := []struct {
		input    string
		wantRepo string
		wantRef  string
	}{
		{
			"git::https://github.com/terraform-aws-modules/terraform-aws-rds?ref=v6.13.1",
			"https://github.com/terraform-aws-modules/terraform-aws-rds",
			"v6.13.1",
		},
		{
			"https://github.com/example/repo",
			"https://github.com/example/repo",
			"",
		},
		{
			"git::https://github.com/example/repo?ref=main&depth=1",
			"https://github.com/example/repo",
			"main",
		},
	}
	for _, tt := range tests {
		repo, ref := parseGitURL(tt.input)
		if repo != tt.wantRepo {
			t.Errorf("parseGitURL(%q) repo = %q, want %q", tt.input, repo, tt.wantRepo)
		}
		if ref != tt.wantRef {
			t.Errorf("parseGitURL(%q) ref = %q, want %q", tt.input, ref, tt.wantRef)
		}
	}
}

func TestModuleDownloader_ManifestPersistence(t *testing.T) {
	dir := t.TempDir()
	d := NewModuleDownloader(dir)

	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key:     "test-key",
		Source:  "test/source/aws",
		Version: "1.0.0",
		Dir:     "/tmp/test",
	})
	d.saveManifest()

	d2 := NewModuleDownloader(dir)
	if len(d2.manifest.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(d2.manifest.Entries))
	}
	if d2.manifest.Entries[0].Key != "test-key" {
		t.Errorf("expected test-key, got %s", d2.manifest.Entries[0].Key)
	}
}

func TestModuleDownloader_ManifestLoadNoFile(t *testing.T) {
	dir := t.TempDir()
	d := NewModuleDownloader(dir)
	if len(d.manifest.Entries) != 0 {
		t.Errorf("expected empty manifest, got %d entries", len(d.manifest.Entries))
	}
}

func TestModuleDownloader_Resolve_CacheHit(t *testing.T) {
	dir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}

	cachedDir := filepath.Join(d.cacheDir, cacheKey("test/mod/aws", "1.0.0"))
	_ = os.MkdirAll(cachedDir, 0o750)
	writeTF(t, cachedDir, "main.tf", `resource "aws_instance" "x" {}`)

	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key:     cacheKey("test/mod/aws", "1.0.0"),
		Source:  "test/mod/aws",
		Version: "1.0.0",
		Dir:     cachedDir,
	})

	result, err := d.Resolve("test/mod/aws", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != cachedDir {
		t.Errorf("expected cached dir %s, got %s", cachedDir, result)
	}
}

func TestModuleDownloader_Resolve_BadSource(t *testing.T) {
	dir := t.TempDir()
	d := NewModuleDownloader(dir)
	_, err := d.Resolve("invalid-source", "1.0.0")
	if err == nil {
		t.Fatal("expected error for bad source")
	}
}

func TestModuleDownloader_Resolve_RegistryError(t *testing.T) {
	dir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}

	_, err := d.Resolve("test/mod/aws", "1.0.0")
	if err == nil {
		t.Fatal("expected error for registry failure")
	}
}

func TestModuleDownloader_DownloadGit_BadURL(t *testing.T) {
	dir := t.TempDir()
	d := NewModuleDownloader(dir)

	origExecCommand := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.CommandContext(context.Background(), "false")
	}
	defer func() { execCommand = origExecCommand }()

	err := d.downloadGit("git::https://github.com/example/nonexistent?ref=v1.0.0", filepath.Join(dir, "dest"))
	if err == nil {
		t.Fatal("expected error for failed git clone")
	}
}

func TestModuleDownloader_Resolve_WithSubdir(t *testing.T) {
	dir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}

	key := cacheKey("test/mod/aws", "1.0.0")
	cachedDir := filepath.Join(d.cacheDir, key)
	subDir := filepath.Join(cachedDir, "modules", "sub")
	_ = os.MkdirAll(subDir, 0o750)
	writeTF(t, subDir, "main.tf", `resource "aws_instance" "sub" {}`)

	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key:     key,
		Source:  "test/mod/aws",
		Version: "1.0.0",
		Dir:     cachedDir,
	})

	result, err := d.Resolve("test/mod/aws//modules/sub", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != subDir {
		t.Errorf("expected %s, got %s", subDir, result)
	}
}

func TestManifestEntry_JSON(t *testing.T) {
	entry := ManifestEntry{
		Key:         "test-key",
		Source:      "terraform-aws-modules/rds/aws",
		Version:     "6.13.1",
		Dir:         "/tmp/test",
		DownloadURL: "git::https://example.com?ref=v1",
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded ManifestEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Key != entry.Key || decoded.Source != entry.Source {
		t.Errorf("round-trip failed: %+v", decoded)
	}
}

func TestSaveManifest_MarshalError(t *testing.T) {
	dir := t.TempDir()
	d := NewModuleDownloader(dir)

	origMarshal := jsonMarshalIndent
	jsonMarshalIndent = func(v any, prefix, indent string) ([]byte, error) {
		return nil, fmt.Errorf("marshal error")
	}
	defer func() { jsonMarshalIndent = origMarshal }()

	d.saveManifest()

	manifestPath := filepath.Join(d.cacheDir, "manifest.json")
	if _, err := os.Stat(manifestPath); err == nil {
		t.Error("expected manifest.json to not exist after marshal error")
	}
}

func TestDownloadGit_EmptyURL(t *testing.T) {
	dir := t.TempDir()
	d := NewModuleDownloader(dir)
	err := d.downloadGit("", filepath.Join(dir, "dest"))
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if !strings.Contains(err.Error(), "cannot parse git URL") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDownloadGit_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	d := NewModuleDownloader(dir)

	origMkdirAll := osMkdirAll
	osMkdirAll = func(path string, perm os.FileMode) error {
		return fmt.Errorf("mkdir failed")
	}
	defer func() { osMkdirAll = origMkdirAll }()

	err := d.downloadGit("https://github.com/example/repo", filepath.Join(dir, "dest"))
	if err == nil {
		t.Fatal("expected error for mkdir failure")
	}
}

func TestDownloadGit_ShallowCloneSuccess(t *testing.T) {
	dir := t.TempDir()
	d := NewModuleDownloader(dir)
	destDir := filepath.Join(dir, "dest")

	origExecCommand := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.CommandContext(context.Background(), "true")
	}
	defer func() { execCommand = origExecCommand }()

	err := d.downloadGit("git::https://github.com/example/repo?ref=v1.0.0", destDir)
	if err != nil {
		t.Fatalf("expected success for shallow clone, got: %v", err)
	}
}

func TestDownloadGit_FullCloneWithCheckoutSuccess(t *testing.T) {
	dir := t.TempDir()
	d := NewModuleDownloader(dir)
	destDir := filepath.Join(dir, "dest")

	callCount := 0
	origExecCommand := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		callCount++
		if callCount == 1 {
			return exec.CommandContext(context.Background(), "false")
		}
		return exec.CommandContext(context.Background(), "true")
	}
	defer func() { execCommand = origExecCommand }()

	err := d.downloadGit("git::https://github.com/example/repo?ref=v1.0.0", destDir)
	if err != nil {
		t.Fatalf("expected success after full clone with checkout, got: %v", err)
	}
}

func TestDownloadGit_FullCloneWithCheckoutFailure(t *testing.T) {
	dir := t.TempDir()
	d := NewModuleDownloader(dir)
	destDir := filepath.Join(dir, "dest")

	callCount := 0
	origExecCommand := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		callCount++
		if callCount == 1 {
			return exec.CommandContext(context.Background(), "false")
		}
		if callCount == 2 {
			return exec.CommandContext(context.Background(), "true")
		}
		return exec.CommandContext(context.Background(), "false")
	}
	defer func() { execCommand = origExecCommand }()

	err := d.downloadGit("git::https://github.com/example/repo?ref=v1.0.0", destDir)
	if err == nil {
		t.Fatal("expected error for checkout failure")
	}
	if !strings.Contains(err.Error(), "checkout") {
		t.Errorf("expected checkout error, got: %v", err)
	}
}

func TestModuleDownloader_Resolve_DownloadPath(t *testing.T) {
	dir := t.TempDir()
	destDir := filepath.Join(dir, ".levelfour", "modules", cacheKey("test/mod/aws", "1.0.0"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "versions") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
		} else {
			w.Header().Set("X-Terraform-Get", "git::https://github.com/example/repo?ref=v1.0.0")
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}

	origExecCommand := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		if len(args) >= 2 && args[1] == "clone" && len(args) > 4 && args[2] == "--depth" {
			return exec.CommandContext(context.Background(), "true")
		}
		return exec.CommandContext(context.Background(), "true")
	}
	defer func() { execCommand = origExecCommand }()

	result, err := d.Resolve("test/mod/aws", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != destDir {
		t.Errorf("expected %s, got %s", destDir, result)
	}
}

func TestModuleDownloader_Resolve_DownloadPath_WithSubdir(t *testing.T) {
	dir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "versions") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
		} else {
			w.Header().Set("X-Terraform-Get", "git::https://github.com/example/repo?ref=v1.0.0")
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}

	origExecCommand := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.CommandContext(context.Background(), "true")
	}
	defer func() { execCommand = origExecCommand }()

	baseKey := cacheKey("test/mod/aws", "1.0.0")
	expectedBase := filepath.Join(dir, ".levelfour", "modules", baseKey)
	result, err := d.Resolve("test/mod/aws//modules/sub", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(expectedBase, "modules/sub")
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

func TestModuleDownloader_Resolve_GetDownloadURLError(t *testing.T) {
	dir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "versions") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}

	_, err := d.Resolve("test/mod/aws", "1.0.0")
	if err == nil {
		t.Fatal("expected error for GetDownloadURL failure")
	}
}

func TestModuleDownloader_Resolve_GitCloneError(t *testing.T) {
	dir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "versions") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
		} else {
			w.Header().Set("X-Terraform-Get", "git::https://github.com/example/repo?ref=v1.0.0")
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}

	origExecCommand := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.CommandContext(context.Background(), "false")
	}
	defer func() { execCommand = origExecCommand }()

	_, err := d.Resolve("test/mod/aws", "1.0.0")
	if err == nil {
		t.Fatal("expected error for git clone failure")
	}
	if !strings.Contains(err.Error(), "git clone failed") {
		t.Errorf("expected git clone error, got: %v", err)
	}
}
