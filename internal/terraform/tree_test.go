package terraform

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
)

func mkdirTF(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTF(t, dir, name, content)
}

func TestDiscoverRoots_SkipsModulesARootCalls(t *testing.T) {
	dir := t.TempDir()
	mkdirTF(t, dir, "main.tf", `module "net" { source = "./modules/net" }`)
	mkdirTF(t, filepath.Join(dir, "modules", "net"), "main.tf", `resource "aws_vpc" "main" {}`)
	mkdirTF(t, filepath.Join(dir, "envs", "prod"), "main.tf", `resource "aws_instance" "web" {}`)

	roots, err := DiscoverRoots(dir)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{dir, filepath.Join(dir, "envs", "prod")}
	if len(roots) != len(want) || roots[0] != want[0] || roots[1] != want[1] {
		t.Errorf("roots = %v, want %v", roots, want)
	}
}

func TestDiscoverRoots_SkipsHiddenAndVendoredDirectories(t *testing.T) {
	dir := t.TempDir()
	mkdirTF(t, dir, "main.tf", `resource "aws_s3_bucket" "b" {}`)
	mkdirTF(t, filepath.Join(dir, ".terraform", "modules", "x"), "main.tf", `resource "aws_instance" "cached" {}`)
	mkdirTF(t, filepath.Join(dir, "node_modules", "pkg"), "main.tf", `resource "aws_instance" "vendored" {}`)

	roots, err := DiscoverRoots(dir)
	if err != nil {
		t.Fatal(err)
	}

	if len(roots) != 1 || roots[0] != dir {
		t.Errorf("roots = %v, want only %s", roots, dir)
	}
}

func TestDiscoverRoots_NoTerraformIsNotAnError(t *testing.T) {
	roots, err := DiscoverRoots(t.TempDir())
	if err != nil || len(roots) != 0 {
		t.Errorf("roots = %v, err = %v; want none and no error", roots, err)
	}
}

func TestDiscoverRoots_MissingDirectoryIsAnError(t *testing.T) {
	if _, err := DiscoverRoots(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("expected an error for a missing directory")
	}
}

func TestDiscoverRoots_UnparseableFileStillCountsAsRoot(t *testing.T) {
	dir := t.TempDir()
	mkdirTF(t, dir, "broken.tf", `resource "aws_instance" {`)

	roots, err := DiscoverRoots(dir)
	if err != nil || len(roots) != 1 {
		t.Errorf("roots = %v, err = %v; want the directory itself", roots, err)
	}
}

func TestParseTree_PricesEachRootOnceWithItsScope(t *testing.T) {
	dir := t.TempDir()
	mkdirTF(t, dir, "main.tf", `module "net" { source = "./modules/net" }`)
	mkdirTF(t, filepath.Join(dir, "modules", "net"), "main.tf", `resource "aws_vpc" "main" { cidr_block = "10.0.0.0/16" }`)
	mkdirTF(t, filepath.Join(dir, "envs", "prod"), "main.tf", `resource "aws_instance" "web" { instance_type = "m5.large" }`)
	mkdirTF(t, filepath.Join(dir, "envs", "staging"), "main.tf", `resource "aws_instance" "web" { instance_type = "t3.micro" }`)

	snapshots, _, err := ParseTree(dir, ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, s := range snapshots {
		got = append(got, s.Scope+" "+SnapshotKey(s))
	}
	sort.Strings(got)
	want := []string{
		". module.net.aws_vpc.main",
		"envs/prod aws_instance.web",
		"envs/staging aws_instance.web",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got %v, want %v", got, want)
			break
		}
	}
}

func TestParseTree_RecordsTheFileOfEveryResource(t *testing.T) {
	dir := t.TempDir()
	mkdirTF(t, dir, "compute.tf", `resource "aws_instance" "web" {}`)
	mkdirTF(t, dir, "storage.tf", `resource "aws_s3_bucket" "logs" {}`)

	snapshots, _, err := ParseTree(dir, ParseOptions{})
	if err != nil {
		t.Fatal(err)
	}

	files := map[string]string{}
	for _, s := range snapshots {
		files[s.Type] = filepath.Base(s.File)
	}
	if files["aws_instance"] != "compute.tf" || files["aws_s3_bucket"] != "storage.tf" {
		t.Errorf("files = %v", files)
	}
}

func TestParseTree_CapsResourcesAcrossRoots(t *testing.T) {
	dir := t.TempDir()
	mkdirTF(t, filepath.Join(dir, "a"), "main.tf", `resource "aws_s3_bucket" "x" {}
resource "aws_s3_bucket" "y" {}`)
	mkdirTF(t, filepath.Join(dir, "b"), "main.tf", `resource "aws_s3_bucket" "z" {}`)

	snapshots, warnings, err := ParseTree(dir, ParseOptions{MaxResources: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 2 || len(warnings) == 0 {
		t.Errorf("got %d snapshots and warnings %v, want 2 and a truncation warning", len(snapshots), warnings)
	}
}

func TestParseTree_MissingDirectoryIsAnError(t *testing.T) {
	if _, _, err := ParseTree(filepath.Join(t.TempDir(), "missing"), ParseOptions{}); err == nil {
		t.Error("expected an error for a missing directory")
	}
}

func TestParseDirectory_ReadsOnlyItsOwnFiles(t *testing.T) {
	dir := t.TempDir()
	mkdirTF(t, dir, "main.tf", `resource "aws_s3_bucket" "top" {}`)
	mkdirTF(t, filepath.Join(dir, "other"), "main.tf", `resource "aws_s3_bucket" "nested" {}`)

	snapshots, _, err := ParseDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 || snapshots[0].Name != "top" {
		t.Errorf("snapshots = %+v, want only aws_s3_bucket.top", snapshots)
	}
}

func TestParseTree_ARootThatFailsToParseIsAnError(t *testing.T) {
	dir := t.TempDir()
	mkdirTF(t, dir, "main.tf", `resource "aws_s3_bucket" "b" {}`)
	orig := parseRoot
	parseRoot = func(string, ParseOptions) ([]api.ResourceSnapshot, []string, error) {
		return nil, nil, errors.New("unreadable")
	}
	defer func() { parseRoot = orig }()

	if _, _, err := ParseTree(dir, ParseOptions{}); err == nil {
		t.Error("expected the root's parse error")
	}
}

func TestDiscoverRoots_OnlyALiteralLocalSourceMarksAModule(t *testing.T) {
	dir := t.TempDir()
	shared := filepath.Join(dir, "shared")
	mkdirTF(t, dir, "main.tf", `
module "no_source" {}
module "computed" { source = var.path }
module "numeric" { source = 3 }
module "remote" { source = "terraform-aws-modules/vpc/aws" }
module "absolute" { source = "`+shared+`" }
`)
	mkdirTF(t, shared, "main.tf", `resource "aws_vpc" "main" {}`)
	mkdirTF(t, filepath.Join(dir, "standalone"), "main.tf", `resource "aws_instance" "web" {}`)

	roots, err := DiscoverRoots(dir)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{dir, filepath.Join(dir, "standalone")}
	if len(roots) != 2 || roots[0] != want[0] || roots[1] != want[1] {
		t.Errorf("roots = %v, want %v", roots, want)
	}
}

func TestDiscoverRoots_AnUnreadableFileCallsNothing(t *testing.T) {
	dir := t.TempDir()
	mkdirTF(t, dir, "main.tf", `resource "aws_s3_bucket" "b" {}`)
	locked := filepath.Join(dir, "locked.tf")
	writeTF(t, dir, "locked.tf", `module "m" { source = "./m" }`)
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(locked, 0o644)
	mkdirTF(t, filepath.Join(dir, "m"), "main.tf", `resource "aws_vpc" "v" {}`)

	roots, err := DiscoverRoots(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 2 {
		t.Errorf("roots = %v, want the directory and m, since the call could not be read", roots)
	}
}
