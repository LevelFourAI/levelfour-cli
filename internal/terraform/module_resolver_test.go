package terraform

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zclconf/go-cty/cty"
)

func TestGoToCty_String(t *testing.T) {
	val := goToCty("hello")
	if val.Type() != cty.String || val.AsString() != "hello" {
		t.Errorf("expected string hello, got %v", val)
	}
}

func TestGoToCty_Float64(t *testing.T) {
	val := goToCty(float64(42.5))
	if val.Type() != cty.Number {
		t.Errorf("expected number type, got %v", val.Type().FriendlyName())
	}
	f, _ := val.AsBigFloat().Float64()
	if f != 42.5 {
		t.Errorf("expected 42.5, got %v", f)
	}
}

func TestGoToCty_Int(t *testing.T) {
	val := goToCty(int(10))
	if val.Type() != cty.Number {
		t.Errorf("expected number type, got %v", val.Type().FriendlyName())
	}
}

func TestGoToCty_Int64(t *testing.T) {
	val := goToCty(int64(100))
	if val.Type() != cty.Number {
		t.Errorf("expected number type, got %v", val.Type().FriendlyName())
	}
}

func TestGoToCty_Bool(t *testing.T) {
	val := goToCty(true)
	if val.Type() != cty.Bool || val.True() != true {
		t.Errorf("expected bool true, got %v", val)
	}
}

func TestGoToCty_Nil(t *testing.T) {
	val := goToCty(nil)
	if val != cty.NilVal {
		t.Errorf("expected NilVal, got %v", val)
	}
}

func TestGoToCty_EmptySlice(t *testing.T) {
	val := goToCty([]interface{}{})
	if !val.Type().IsTupleType() {
		t.Errorf("expected tuple type, got %v", val.Type().FriendlyName())
	}
}

func TestGoToCty_Slice(t *testing.T) {
	val := goToCty([]interface{}{"a", "b"})
	if !val.Type().IsTupleType() {
		t.Errorf("expected tuple type, got %v", val.Type().FriendlyName())
	}
	elems := val.AsValueSlice()
	if len(elems) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(elems))
	}
	if elems[0].AsString() != "a" {
		t.Errorf("expected a, got %s", elems[0].AsString())
	}
}

func TestGoToCty_EmptyMap(t *testing.T) {
	val := goToCty(map[string]interface{}{})
	if !val.Type().IsObjectType() {
		t.Errorf("expected object type, got %v", val.Type().FriendlyName())
	}
}

func TestGoToCty_Map(t *testing.T) {
	val := goToCty(map[string]interface{}{"key": "value"})
	if !val.Type().IsObjectType() {
		t.Errorf("expected object type, got %v", val.Type().FriendlyName())
	}
	v := val.GetAttr("key")
	if v.AsString() != "value" {
		t.Errorf("expected value, got %s", v.AsString())
	}
}

func TestGoToCty_UnknownType(t *testing.T) {
	val := goToCty(struct{ X int }{X: 1})
	if val.Type() != cty.String {
		t.Errorf("expected string fallback, got %v", val.Type().FriendlyName())
	}
}

func TestFilterModuleInputs(t *testing.T) {
	input := map[string]interface{}{
		"source":     "terraform-aws-modules/rds/aws",
		"version":    "6.13.1",
		"providers":  map[string]interface{}{},
		"depends_on": []interface{}{},
		"count":      1,
		"for_each":   nil,
		"identifier": "my-db",
		"engine":     "postgres",
	}
	result := filterModuleInputs(input)
	if len(result) != 2 {
		t.Fatalf("expected 2 inputs, got %d: %v", len(result), result)
	}
	if result["identifier"] != "my-db" {
		t.Errorf("expected identifier=my-db, got %v", result["identifier"])
	}
	if result["engine"] != "postgres" {
		t.Errorf("expected engine=postgres, got %v", result["engine"])
	}
}

func TestFilterModuleInputs_Empty(t *testing.T) {
	result := filterModuleInputs(map[string]interface{}{})
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

func TestFilterModuleInputs_OnlyMeta(t *testing.T) {
	input := map[string]interface{}{
		"source":  "terraform-aws-modules/rds/aws",
		"version": "6.13.1",
	}
	result := filterModuleInputs(input)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

func TestResolveRemoteModule_DownloadFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()
	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}

	configMap := map[string]interface{}{
		"source":  "test/mod/aws",
		"version": "1.0.0",
	}

	snapshots, warnings := resolveRemoteModule("mymod", "test/mod/aws", "1.0.0", configMap, filepath.Join(dir, "main.tf"), make(map[string]bool), 0, nil, ParseOptions{}, "", d)

	if len(snapshots) != 1 {
		t.Fatalf("expected 1 opaque snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Type != "module" {
		t.Errorf("expected type module, got %s", snapshots[0].Type)
	}
	if len(warnings) == 0 {
		t.Error("expected warning about download failure")
	}
}

func TestResolveRemoteModule_Success(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "downloaded_mod")
	_ = os.MkdirAll(moduleDir, 0o750)
	writeTF(t, moduleDir, "main.tf", `
resource "aws_db_instance" "this" {
  engine         = "postgres"
  instance_class = "db.t3.micro"
}
`)
	writeTF(t, moduleDir, "variables.tf", `
variable "engine" {
  default = "mysql"
}
variable "instance_class" {
  default = "db.t3.small"
}
`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}

	key := cacheKey("test/mod/aws", "1.0.0")
	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key:     key,
		Source:  "test/mod/aws",
		Version: "1.0.0",
		Dir:     moduleDir,
	})

	configMap := map[string]interface{}{
		"source":         "test/mod/aws",
		"version":        "1.0.0",
		"engine":         "postgres",
		"instance_class": "db.t3.micro",
	}

	snapshots, warnings := resolveRemoteModule("mydb", "test/mod/aws", "1.0.0", configMap, filepath.Join(dir, "main.tf"), make(map[string]bool), 0, nil, ParseOptions{}, "", d)

	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Type != "aws_db_instance" {
		t.Errorf("expected aws_db_instance, got %s", snapshots[0].Type)
	}
	if snapshots[0].ModulePath != "module.mydb" {
		t.Errorf("expected module.mydb, got %s", snapshots[0].ModulePath)
	}
}

func TestResolveRemoteModule_ModulePath(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "downloaded_mod")
	_ = os.MkdirAll(moduleDir, 0o750)
	writeTF(t, moduleDir, "main.tf", `
resource "aws_instance" "this" {
  instance_type = "t3.micro"
}
`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	key := cacheKey("test/mod/aws", "1.0.0")
	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key: key, Source: "test/mod/aws", Version: "1.0.0", Dir: moduleDir,
	})

	configMap := map[string]interface{}{"source": "test/mod/aws", "version": "1.0.0"}
	snapshots, _ := resolveRemoteModule("inner", "test/mod/aws", "1.0.0", configMap, filepath.Join(dir, "main.tf"), make(map[string]bool), 0, nil, ParseOptions{}, "module.outer", d)

	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].ModulePath != "module.outer.module.inner" {
		t.Errorf("expected module.outer.module.inner, got %s", snapshots[0].ModulePath)
	}
}

func TestResolveRemoteModule_IsolatedVisited(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "downloaded_mod")
	_ = os.MkdirAll(moduleDir, 0o750)
	writeTF(t, moduleDir, "main.tf", `resource "aws_instance" "x" {}`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	key := cacheKey("test/mod/aws", "1.0.0")
	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key: key, Source: "test/mod/aws", Version: "1.0.0", Dir: moduleDir,
	})

	absModuleDir, _ := filepath.Abs(moduleDir)
	visited := map[string]bool{absModuleDir: true}
	configMap := map[string]interface{}{"source": "test/mod/aws", "version": "1.0.0"}
	snapshots, warnings := resolveRemoteModule("mymod", "test/mod/aws", "1.0.0", configMap, filepath.Join(dir, "main.tf"), visited, 0, nil, ParseOptions{}, "", d)

	if len(snapshots) != 1 {
		t.Errorf("expected 1 snapshot (isolated visited), got %d", len(snapshots))
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings with isolated visited, got %v", warnings)
	}
}

func TestResolveRemoteModule_MaxDepth(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "downloaded_mod")
	_ = os.MkdirAll(moduleDir, 0o750)
	writeTF(t, moduleDir, "main.tf", `resource "aws_instance" "x" {}`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	key := cacheKey("test/mod/aws", "1.0.0")
	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key: key, Source: "test/mod/aws", Version: "1.0.0", Dir: moduleDir,
	})

	configMap := map[string]interface{}{"source": "test/mod/aws", "version": "1.0.0"}
	snapshots, warnings := resolveRemoteModule("mymod", "test/mod/aws", "1.0.0", configMap, filepath.Join(dir, "main.tf"), make(map[string]bool), MaxDepth, nil, ParseOptions{}, "", d)

	if len(snapshots) != 0 {
		t.Errorf("expected 0 snapshots for max depth, got %d", len(snapshots))
	}
	if len(warnings) == 0 {
		t.Error("expected max depth warning")
	}
}

func TestResolveRemoteModule_VariableMerging(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "downloaded_mod")
	_ = os.MkdirAll(moduleDir, 0o750)
	writeTF(t, moduleDir, "main.tf", `
resource "aws_instance" "this" {
  instance_type = var.instance_type
}
`)
	writeTF(t, moduleDir, "variables.tf", `
variable "instance_type" {
  default = "t3.small"
}
`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	key := cacheKey("test/mod/aws", "1.0.0")
	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key: key, Source: "test/mod/aws", Version: "1.0.0", Dir: moduleDir,
	})

	configMap := map[string]interface{}{
		"source":        "test/mod/aws",
		"version":       "1.0.0",
		"instance_type": "t3.micro",
	}

	snapshots, warnings := resolveRemoteModule("mymod", "test/mod/aws", "1.0.0", configMap, filepath.Join(dir, "main.tf"), make(map[string]bool), 0, nil, ParseOptions{}, "", d)

	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Attributes["instance_type"] != "t3.micro" {
		t.Errorf("expected caller override t3.micro, got %v", snapshots[0].Attributes["instance_type"])
	}
}

func TestResolveRemoteModule_DefaultsUsed(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "downloaded_mod")
	_ = os.MkdirAll(moduleDir, 0o750)
	writeTF(t, moduleDir, "main.tf", `
resource "aws_instance" "this" {
  instance_type = var.instance_type
}
`)
	writeTF(t, moduleDir, "variables.tf", `
variable "instance_type" {
  default = "t3.small"
}
`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	key := cacheKey("test/mod/aws", "1.0.0")
	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key: key, Source: "test/mod/aws", Version: "1.0.0", Dir: moduleDir,
	})

	configMap := map[string]interface{}{
		"source":  "test/mod/aws",
		"version": "1.0.0",
	}

	snapshots, warnings := resolveRemoteModule("mymod", "test/mod/aws", "1.0.0", configMap, filepath.Join(dir, "main.tf"), make(map[string]bool), 0, nil, ParseOptions{}, "", d)

	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Attributes["instance_type"] != "t3.small" {
		t.Errorf("expected default t3.small, got %v", snapshots[0].Attributes["instance_type"])
	}
}

func TestResolveRemoteModule_ParseDirectoryFails(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "empty_mod")
	_ = os.MkdirAll(moduleDir, 0o750)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	key := cacheKey("test/empty/aws", "1.0.0")
	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key: key, Source: "test/empty/aws", Version: "1.0.0", Dir: moduleDir,
	})

	configMap := map[string]interface{}{"source": "test/empty/aws", "version": "1.0.0"}
	snapshots, warnings := resolveRemoteModule("mymod", "test/empty/aws", "1.0.0", configMap, filepath.Join(dir, "main.tf"), make(map[string]bool), 0, nil, ParseOptions{}, "", d)

	if len(snapshots) != 1 {
		t.Fatalf("expected 1 opaque snapshot on parse failure, got %d", len(snapshots))
	}
	if snapshots[0].Type != "module" {
		t.Errorf("expected type module, got %s", snapshots[0].Type)
	}
	if len(warnings) == 0 {
		t.Error("expected warning about parse failure")
	}
	if !strings.Contains(warnings[0], "failed to parse downloaded module") {
		t.Errorf("expected parse failure warning, got %s", warnings[0])
	}
}

func TestResolveRemoteModule_NestedModulePath(t *testing.T) {
	dir := t.TempDir()

	innerModuleDir := filepath.Join(dir, "inner_mod")
	_ = os.MkdirAll(innerModuleDir, 0o750)
	writeTF(t, innerModuleDir, "main.tf", `
resource "aws_instance" "inner" {
  instance_type = "t3.micro"
}
`)

	outerModuleDir := filepath.Join(dir, "outer_mod")
	_ = os.MkdirAll(outerModuleDir, 0o750)

	innerKey := cacheKey("test/inner/aws", "1.0.0")
	outerD := NewModuleDownloader(dir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()
	outerD.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	outerD.manifest.Entries = append(outerD.manifest.Entries, ManifestEntry{
		Key: innerKey, Source: "test/inner/aws", Version: "1.0.0", Dir: innerModuleDir,
	})

	writeTF(t, outerModuleDir, "main.tf", `
module "inner" {
  source  = "test/inner/aws"
  version = "1.0.0"
}
`)

	outerKey := cacheKey("test/outer/aws", "1.0.0")
	outerD.manifest.Entries = append(outerD.manifest.Entries, ManifestEntry{
		Key: outerKey, Source: "test/outer/aws", Version: "1.0.0", Dir: outerModuleDir,
	})

	configMap := map[string]interface{}{"source": "test/outer/aws", "version": "1.0.0"}
	snapshots, _ := resolveRemoteModule("outer", "test/outer/aws", "1.0.0", configMap, filepath.Join(dir, "main.tf"), make(map[string]bool), 0, nil, ParseOptions{DownloadModules: true}, "", outerD)

	found := false
	for _, s := range snapshots {
		if strings.HasPrefix(s.ModulePath, "module.outer.module.inner") {
			found = true
		}
	}
	if !found {
		for _, s := range snapshots {
			t.Logf("snapshot: type=%s modulePath=%s", s.Type, s.ModulePath)
		}
		t.Error("expected nested module path containing module.outer.module.inner")
	}
}

func TestResolveRemoteModule_CountZeroResourcesFiltered(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "downloaded_mod")
	_ = os.MkdirAll(moduleDir, 0o750)
	writeTF(t, moduleDir, "variables.tf", `
variable "create" {
  default = true
}
variable "create_spot_instance" {
  default = false
}
`)
	writeTF(t, moduleDir, "main.tf", `
locals {
  create = var.create && !var.create_spot_instance
}

resource "aws_instance" "this" {
  count         = local.create ? 1 : 0
  instance_type = "t3.micro"
}

resource "aws_spot_instance_request" "this" {
  count         = var.create_spot_instance ? 1 : 0
  instance_type = "t3.micro"
}
`)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()

	d := NewModuleDownloader(dir)
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	key := cacheKey("test/ec2/aws", "1.0.0")
	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key: key, Source: "test/ec2/aws", Version: "1.0.0", Dir: moduleDir,
	})

	configMap := map[string]interface{}{
		"source":  "test/ec2/aws",
		"version": "1.0.0",
	}

	snapshots, warnings := resolveRemoteModule("ec2", "test/ec2/aws", "1.0.0", configMap, filepath.Join(dir, "main.tf"), make(map[string]bool), 0, nil, ParseOptions{}, "", d)

	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot (only aws_instance), got %d", len(snapshots))
	}
	if snapshots[0].Type != "aws_instance" {
		t.Errorf("expected aws_instance, got %s", snapshots[0].Type)
	}
	if snapshots[0].ModulePath != "module.ec2" {
		t.Errorf("expected module.ec2, got %s", snapshots[0].ModulePath)
	}
}
