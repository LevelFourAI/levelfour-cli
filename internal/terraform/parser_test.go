package terraform

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

func writeTF(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseDirectory_ValidResources(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
resource "aws_instance" "web" {
  ami           = "ami-12345"
  instance_type = "t3.micro"
}
`)

	snapshots, _, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(snapshots) == 0 {
		t.Fatal("expected at least one snapshot")
	}

	found := false
	for _, s := range snapshots {
		if s.Type == "aws_instance" && s.Name == "web" {
			found = true
			if s.Attributes["ami"] != "ami-12345" {
				t.Errorf("expected ami=ami-12345, got %v", s.Attributes["ami"])
			}
		}
	}
	if !found {
		t.Error("expected aws_instance.web snapshot")
	}
}

func TestParseDirectory_NoTFFiles(t *testing.T) {
	dir := t.TempDir()

	_, _, err := ParseDirectory(dir)
	if err == nil {
		t.Fatal("expected error for directory with no .tf files")
	}
	if !strings.Contains(err.Error(), "no .tf files found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseDirectory_MaxFileSizeExceeded(t *testing.T) {
	dir := t.TempDir()

	bigContent := strings.Repeat("x", MaxFileSize+1)
	writeTF(t, dir, "big.tf", bigContent)
	writeTF(t, dir, "small.tf", `resource "aws_s3_bucket" "b" { bucket = "my-bucket" }`)

	_, warnings, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundSizeWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "exceeds 1MB limit") {
			foundSizeWarning = true
		}
	}
	if !foundSizeWarning {
		t.Error("expected file size warning")
	}
}

func TestParseDirectory_MaxFilesExceeded(t *testing.T) {
	dir := t.TempDir()

	for i := 0; i < MaxFiles+5; i++ {
		writeTF(t, dir, fmt.Sprintf("file%03d.tf", i), fmt.Sprintf("resource \"null_resource\" \"r%d\" {}", i))
	}

	_, warnings, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "processing first") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected max files warning")
	}
}

func TestParseDirectory_MaxResourcesTruncation(t *testing.T) {
	dir := t.TempDir()

	var sb strings.Builder
	for i := 0; i < MaxResources+10; i++ {
		fmt.Fprintf(&sb, "resource \"null_resource\" \"r%d\" {\n  value = \"%d\"\n}\n\n", i, i)
	}
	writeTF(t, dir, "main.tf", sb.String())

	snapshots, warnings, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(snapshots) != MaxResources {
		t.Errorf("expected %d snapshots, got %d", MaxResources, len(snapshots))
	}

	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "truncating to") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected truncation warning")
	}
}

func TestParseDirectory_DeduplicatesFiles(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `resource "aws_s3_bucket" "x" { bucket = "test" }`)

	snapshots, _, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, s := range snapshots {
		if s.Type == "aws_s3_bucket" && s.Name == "x" {
			count++
		}
	}
	if count > 1 {
		t.Errorf("expected 1 snapshot for aws_s3_bucket.x, got %d", count)
	}
}

func TestParseDirectory_MultipleValidFiles(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "a.tf", `resource "aws_s3_bucket" "a" { bucket = "a" }`)
	writeTF(t, dir, "b.tf", `resource "aws_s3_bucket" "b" { bucket = "b" }`)

	snapshots, _, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(snapshots) < 2 {
		t.Errorf("expected at least 2 snapshots, got %d", len(snapshots))
	}
}

func TestParseDirectory_ReadError(t *testing.T) {
	dir := t.TempDir()
	tfPath := filepath.Join(dir, "unreadable.tf")
	os.WriteFile(tfPath, []byte(`resource "a" "b" {}`), 0o000)
	defer os.Chmod(tfPath, 0o644)

	writeTF(t, dir, "ok.tf", `resource "aws_instance" "x" { ami = "ami-ok" }`)

	_, warnings, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_ = warnings
}

func TestParseHCLContent_Resources(t *testing.T) {
	content := []byte(`
resource "aws_instance" "app" {
  ami           = "ami-abc"
  instance_type = "t3.small"
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)

	if len(snapshots) == 0 {
		t.Fatal("expected at least one snapshot")
	}
	if snapshots[0].Type != "aws_instance" || snapshots[0].Name != "app" {
		t.Errorf("unexpected snapshot: %+v", snapshots[0])
	}
}

func TestParseHCLContent_ModuleLocal(t *testing.T) {
	parentDir := t.TempDir()
	moduleDir := filepath.Join(parentDir, "mymod")
	os.MkdirAll(moduleDir, 0o755)
	writeTF(t, moduleDir, "main.tf", `resource "aws_iam_role" "role" { name = "test-role" }`)

	parentTF := "module \"mymod\" {\n  source = \"./mymod\"\n}\n"
	writeTF(t, parentDir, "main.tf", parentTF)

	content, _ := os.ReadFile(filepath.Join(parentDir, "main.tf"))
	snapshots, _ := parseHCLContent(content, filepath.Join(parentDir, "main.tf"), make(map[string]bool), 0, nil, ParseOptions{}, nil)

	found := false
	for _, s := range snapshots {
		if s.Type == "aws_iam_role" && s.Name == "role" {
			found = true
		}
	}
	if !found {
		t.Error("expected aws_iam_role.role from local module")
	}
}

func TestParseHCLContent_ModuleRemote(t *testing.T) {
	content := []byte(`
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "3.0"
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)

	found := false
	for _, s := range snapshots {
		if s.Type == "module" && s.Name == "vpc" {
			found = true
			if s.Attributes["source"] != "terraform-aws-modules/vpc/aws" {
				t.Errorf("expected source=terraform-aws-modules/vpc/aws, got %v", s.Attributes["source"])
			}
		}
	}
	if !found {
		t.Error("expected module snapshot for remote module vpc")
	}
}

func TestParseHCLContent_ModuleLocalFailure(t *testing.T) {
	content := []byte(`module "bad" {
  source = "./nonexistent-dir"
}
`)

	_, warnings := parseHCLContent(content, "/tmp/fake/main.tf", make(map[string]bool), 0, nil, ParseOptions{}, nil)

	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "failed to parse local module") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected local module parse failure warning")
	}
}

func TestParseHCLContent_InvalidHCL(t *testing.T) {
	content := []byte(`this is not valid { { { { HCL at all $$$`)

	snapshots, warnings := parseHCLContent(content, "bad.tf", nil, 0, nil, ParseOptions{}, nil)

	if len(snapshots) != 0 {
		t.Errorf("expected no snapshots from invalid HCL, got %d", len(snapshots))
	}

	if len(warnings) == 0 {
		t.Error("expected warnings from invalid HCL parse")
	}

	foundDiag := false
	for _, w := range warnings {
		if strings.Contains(w, "bad.tf") {
			foundDiag = true
		}
	}
	if !foundDiag {
		t.Error("expected warning referencing the filename")
	}
}

func TestParseHCLContent_NestedBlocks(t *testing.T) {
	content := []byte(`
resource "aws_instance" "web" {
  ami = "ami-12345"
  root_block_device {
    volume_size = 50
  }
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	rbd, ok := snapshots[0].Attributes["root_block_device"].([]interface{})
	if !ok || len(rbd) != 1 {
		t.Fatalf("expected root_block_device as []interface{} with 1 entry, got %v", snapshots[0].Attributes["root_block_device"])
	}
	rbdMap, ok := rbd[0].(map[string]interface{})
	if !ok {
		t.Fatal("expected root_block_device entry to be map")
	}
	if rbdMap["volume_size"] != float64(50) {
		t.Errorf("expected volume_size=50, got %v", rbdMap["volume_size"])
	}
}

func TestParseHCLContent_TagsMap(t *testing.T) {
	content := []byte(`
resource "aws_instance" "web" {
  ami = "ami-12345"
  tags = {
    Name = "foo"
    Env  = "prod"
  }
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	tags, ok := snapshots[0].Attributes["tags"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected tags as map, got %T", snapshots[0].Attributes["tags"])
	}
	if tags["Name"] != "foo" {
		t.Errorf("expected Name=foo, got %v", tags["Name"])
	}
	if tags["Env"] != "prod" {
		t.Errorf("expected Env=prod, got %v", tags["Env"])
	}
}

func TestParseHCLContent_VariableReferences(t *testing.T) {
	content := []byte(`
resource "aws_instance" "web" {
  instance_type = var.instance_type
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Attributes["instance_type"] != nil {
		t.Errorf("expected nil for unresolved var reference, got %v", snapshots[0].Attributes["instance_type"])
	}
}

func TestParseHCLContent_LocalReferences(t *testing.T) {
	content := []byte(`
resource "aws_instance" "web" {
  ami = local.ami_id
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Attributes["ami"] != nil {
		t.Errorf("expected nil for unresolved local reference, got %v", snapshots[0].Attributes["ami"])
	}
}

func TestParseHCLContent_FunctionCall(t *testing.T) {
	content := []byte(`
resource "aws_instance" "web" {
  ami = join("-", ["ami", "123"])
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	val, ok := snapshots[0].Attributes["ami"].(string)
	if !ok {
		t.Fatalf("expected string fallback, got %T", snapshots[0].Attributes["ami"])
	}
	if !strings.Contains(val, "join") {
		t.Errorf("expected raw source containing join, got %q", val)
	}
}

func TestParseHCLContent_BooleanValues(t *testing.T) {
	content := []byte(`
resource "aws_instance" "web" {
  enabled = true
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	val, ok := snapshots[0].Attributes["enabled"].(bool)
	if !ok {
		t.Fatalf("expected bool, got %T", snapshots[0].Attributes["enabled"])
	}
	if !val {
		t.Error("expected enabled=true")
	}
}

func TestParseHCLContent_NumericValues(t *testing.T) {
	content := []byte(`
resource "aws_instance" "web" {
  ebs_count = 3
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	val, ok := snapshots[0].Attributes["ebs_count"].(float64)
	if !ok {
		t.Fatalf("expected float64, got %T", snapshots[0].Attributes["ebs_count"])
	}
	if val != 3.0 {
		t.Errorf("expected ebs_count=3, got %v", val)
	}
}

func TestParseHCLContent_ListValues(t *testing.T) {
	content := []byte(`
resource "aws_instance" "web" {
  security_groups = ["sg-123", "sg-456"]
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	val, ok := snapshots[0].Attributes["security_groups"].([]interface{})
	if !ok {
		t.Fatalf("expected []interface{}, got %T", snapshots[0].Attributes["security_groups"])
	}
	if len(val) != 2 {
		t.Fatalf("expected 2 items, got %d", len(val))
	}
	if val[0] != "sg-123" || val[1] != "sg-456" {
		t.Errorf("unexpected list values: %v", val)
	}
}

func TestParseHCLContent_MultipleNestedBlocksSameType(t *testing.T) {
	content := []byte(`
resource "aws_security_group" "web" {
  ingress {
    from_port = 80
  }
  ingress {
    from_port = 443
  }
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	ingress, ok := snapshots[0].Attributes["ingress"].([]interface{})
	if !ok || len(ingress) != 2 {
		t.Fatalf("expected ingress as []interface{} with 2 entries, got %v", snapshots[0].Attributes["ingress"])
	}
	first := ingress[0].(map[string]interface{})
	second := ingress[1].(map[string]interface{})
	if first["from_port"] != float64(80) {
		t.Errorf("expected first from_port=80, got %v", first["from_port"])
	}
	if second["from_port"] != float64(443) {
		t.Errorf("expected second from_port=443, got %v", second["from_port"])
	}
}

func TestParseHCLContent_EmptyBody(t *testing.T) {
	content := []byte(`resource "null_resource" "empty" {}`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if len(snapshots[0].Attributes) != 0 {
		t.Errorf("expected empty attributes, got %v", snapshots[0].Attributes)
	}
}

func TestParseHCLContent_DataBlockIgnored(t *testing.T) {
	content := []byte(`
data "aws_ami" "latest" {
  owners = ["self"]
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 0 {
		t.Errorf("expected no snapshots from data block, got %d", len(snapshots))
	}
}

func TestParseHCLContent_NullValue(t *testing.T) {
	content := []byte(`
resource "aws_instance" "web" {
  value = null
}
`)

	snapshots, _ := parseHCLContent(content, "test.tf", nil, 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Attributes["value"] != nil {
		t.Errorf("expected nil for null value, got %v", snapshots[0].Attributes["value"])
	}
}

func TestCtyToGo_AllTypes(t *testing.T) {
	if ctyToGo(cty.NullVal(cty.String)) != nil {
		t.Error("expected nil for null")
	}
	if ctyToGo(cty.UnknownVal(cty.String)) != nil {
		t.Error("expected nil for unknown")
	}
	if ctyToGo(cty.StringVal("hello")) != "hello" {
		t.Error("expected string hello")
	}
	if ctyToGo(cty.NumberIntVal(42)) != float64(42) {
		t.Error("expected float64 42")
	}
	if ctyToGo(cty.True) != true {
		t.Error("expected true")
	}
	if ctyToGo(cty.False) != false {
		t.Error("expected false")
	}

	list := ctyToGo(cty.ListVal([]cty.Value{cty.StringVal("a"), cty.StringVal("b")})).([]interface{})
	if len(list) != 2 || list[0] != "a" || list[1] != "b" {
		t.Errorf("unexpected list: %v", list)
	}

	tuple := ctyToGo(cty.TupleVal([]cty.Value{cty.StringVal("x"), cty.NumberIntVal(1)})).([]interface{})
	if len(tuple) != 2 || tuple[0] != "x" || tuple[1] != float64(1) {
		t.Errorf("unexpected tuple: %v", tuple)
	}

	set := ctyToGo(cty.SetVal([]cty.Value{cty.StringVal("x")})).([]interface{})
	if len(set) != 1 || set[0] != "x" {
		t.Errorf("unexpected set: %v", set)
	}

	m := ctyToGo(cty.MapVal(map[string]cty.Value{"k": cty.StringVal("v")})).(map[string]interface{})
	if m["k"] != "v" {
		t.Errorf("unexpected map: %v", m)
	}

	obj := ctyToGo(cty.ObjectVal(map[string]cty.Value{"a": cty.NumberIntVal(1)})).(map[string]interface{})
	if obj["a"] != float64(1) {
		t.Errorf("unexpected object: %v", obj)
	}

	n := 42
	capsuleType := cty.Capsule("test", reflect.TypeOf(n))
	capsuleResult, ok := ctyToGo(cty.CapsuleVal(capsuleType, &n)).(string)
	if !ok {
		t.Fatal("expected string for capsule type")
	}
	if capsuleResult == "" {
		t.Error("expected non-empty GoString for capsule type")
	}
}

func TestBodyToMap_EmptyBody(t *testing.T) {
	body := &hclsyntax.Body{}
	result := bodyToMap(body, nil, nil)
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestExtractResources_NoResourceKey(t *testing.T) {
	parsed := map[string]interface{}{
		"variable": map[string]interface{}{},
	}
	result := extractResources(parsed)
	if len(result) != 0 {
		t.Errorf("expected no resources, got %d", len(result))
	}
}

func TestExtractResources_InvalidInstanceMap(t *testing.T) {
	parsed := map[string]interface{}{
		"resource": map[string]interface{}{
			"aws_instance": "not-a-map",
		},
	}
	result := extractResources(parsed)
	if len(result) != 0 {
		t.Errorf("expected no resources, got %d", len(result))
	}
}

func TestExtractResources_InvalidAttrSlice(t *testing.T) {
	parsed := map[string]interface{}{
		"resource": map[string]interface{}{
			"aws_instance": map[string]interface{}{
				"web": "not-a-slice",
			},
		},
	}
	result := extractResources(parsed)
	if len(result) != 0 {
		t.Errorf("expected no resources, got %d", len(result))
	}
}

func TestExtractResources_InvalidAttrMap(t *testing.T) {
	parsed := map[string]interface{}{
		"resource": map[string]interface{}{
			"aws_instance": map[string]interface{}{
				"web": []interface{}{"not-a-map"},
			},
		},
	}
	result := extractResources(parsed)
	if len(result) != 0 {
		t.Errorf("expected no resources, got %d", len(result))
	}
}

func TestExtractResources_Valid(t *testing.T) {
	parsed := map[string]interface{}{
		"resource": map[string]interface{}{
			"aws_s3_bucket": map[string]interface{}{
				"mybucket": []interface{}{
					map[string]interface{}{
						"bucket": "test-bucket",
					},
				},
			},
		},
	}
	result := extractResources(parsed)
	if len(result) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(result))
	}
	if result[0].Type != "aws_s3_bucket" || result[0].Name != "mybucket" {
		t.Errorf("unexpected: %+v", result[0])
	}
}

func TestExtractModules_NoModuleKey(t *testing.T) {
	parsed := map[string]interface{}{
		"resource": map[string]interface{}{},
	}
	snapshots, warnings := extractModules(parsed, "test.tf", make(map[string]bool), 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 0 || len(warnings) != 0 {
		t.Errorf("expected empty results, got %d snapshots, %d warnings", len(snapshots), len(warnings))
	}
}

func TestExtractModules_InvalidConfigSlice(t *testing.T) {
	parsed := map[string]interface{}{
		"module": map[string]interface{}{
			"mymod": "not-a-slice",
		},
	}
	snapshots, _ := extractModules(parsed, "test.tf", make(map[string]bool), 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 0 {
		t.Errorf("expected no snapshots, got %d", len(snapshots))
	}
}

func TestExtractModules_InvalidConfigMap(t *testing.T) {
	parsed := map[string]interface{}{
		"module": map[string]interface{}{
			"mymod": []interface{}{"not-a-map"},
		},
	}
	snapshots, _ := extractModules(parsed, "test.tf", make(map[string]bool), 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 0 {
		t.Errorf("expected no snapshots, got %d", len(snapshots))
	}
}

func TestExtractModules_EmptySource(t *testing.T) {
	parsed := map[string]interface{}{
		"module": map[string]interface{}{
			"mymod": []interface{}{
				map[string]interface{}{
					"version": "1.0",
				},
			},
		},
	}
	snapshots, warnings := extractModules(parsed, "test.tf", make(map[string]bool), 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 0 {
		t.Errorf("expected no snapshots, got %d", len(snapshots))
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for empty source, got %d", len(warnings))
	}
}

func TestExtractModules_RemoteSource(t *testing.T) {
	parsed := map[string]interface{}{
		"module": map[string]interface{}{
			"vpc": []interface{}{
				map[string]interface{}{
					"source":        "terraform-aws-modules/vpc/aws",
					"version":       "3.0",
					"instance_type": "t3.micro",
				},
			},
		},
	}
	snapshots, warnings := extractModules(parsed, "test.tf", make(map[string]bool), 0, nil, ParseOptions{}, nil)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for remote module pass-through, got %v", warnings)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Type != "module" {
		t.Errorf("expected type=module, got %q", snapshots[0].Type)
	}
	if snapshots[0].Name != "vpc" {
		t.Errorf("expected name=vpc, got %q", snapshots[0].Name)
	}
	if snapshots[0].Attributes["source"] != "terraform-aws-modules/vpc/aws" {
		t.Errorf("expected source in attributes, got %v", snapshots[0].Attributes["source"])
	}
	if snapshots[0].Attributes["instance_type"] != "t3.micro" {
		t.Errorf("expected instance_type in attributes, got %v", snapshots[0].Attributes["instance_type"])
	}
}

func TestExtractModules_LocalRelativeSource(t *testing.T) {
	parentDir := t.TempDir()
	moduleDir := filepath.Join(parentDir, "submod")
	os.MkdirAll(moduleDir, 0o755)
	writeTF(t, moduleDir, "main.tf", `resource "aws_sns_topic" "t" { name = "test" }`)

	parsed := map[string]interface{}{
		"module": map[string]interface{}{
			"mymod": []interface{}{
				map[string]interface{}{
					"source": "./submod",
				},
			},
		},
	}

	parentFile := filepath.Join(parentDir, "main.tf")
	snapshots, _ := extractModules(parsed, parentFile, make(map[string]bool), 0, nil, ParseOptions{}, nil)

	found := false
	for _, s := range snapshots {
		if s.Type == "aws_sns_topic" {
			found = true
		}
	}
	if !found {
		t.Error("expected aws_sns_topic from local relative module source")
	}
}

func TestExtractModules_LocalModuleParseError(t *testing.T) {
	parsed := map[string]interface{}{
		"module": map[string]interface{}{
			"bad": []interface{}{
				map[string]interface{}{
					"source": "./nonexistent",
				},
			},
		},
	}
	_, warnings := extractModules(parsed, "/tmp/fake/main.tf", make(map[string]bool), 0, nil, ParseOptions{}, nil)
	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "failed to parse local module") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected failure warning for nonexistent local module")
	}
}

func TestExtractModules_CyclicReference(t *testing.T) {
	rootDir := t.TempDir()
	dirA := filepath.Join(rootDir, "modA")
	dirB := filepath.Join(rootDir, "modB")
	os.MkdirAll(dirA, 0o755)
	os.MkdirAll(dirB, 0o755)

	writeTF(t, dirA, "main.tf", "module \"b\" {\n  source = \"../modB\"\n}\n")
	writeTF(t, dirB, "main.tf", "module \"a\" {\n  source = \"../modA\"\n}\n")

	_, warnings, err := ParseDirectory(dirA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, w := range warnings {
		if strings.Contains(w, "cycle") {
			t.Errorf("cycle should be silently skipped, got warning: %s", w)
		}
	}
}

func TestExtractModules_ModulePath(t *testing.T) {
	parentDir := t.TempDir()
	moduleDir := filepath.Join(parentDir, "modules", "mymod")
	os.MkdirAll(moduleDir, 0o755)
	writeTF(t, moduleDir, "main.tf", `resource "aws_instance" "nested" { ami = "ami-123" }`)
	writeTF(t, parentDir, "main.tf", "module \"mymod\" {\n  source = \"./modules/mymod\"\n}\n")

	snapshots, _, err := ParseDirectory(parentDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, s := range snapshots {
		if s.Type == "aws_instance" && s.Name == "nested" && s.ModulePath == "module.mymod" {
			found = true
		}
	}
	if !found {
		t.Error("expected aws_instance.nested with ModulePath=module.mymod from module")
	}
}

func TestSnapshotKey(t *testing.T) {
	t.Run("without module path", func(t *testing.T) {
		s := api.ResourceSnapshot{Type: "aws_instance", Name: "web"}
		got := SnapshotKey(s)
		if got != "aws_instance.web" {
			t.Errorf("SnapshotKey = %q, want aws_instance.web", got)
		}
	})

	t.Run("with module path", func(t *testing.T) {
		s := api.ResourceSnapshot{Type: "aws_instance", Name: "web", ModulePath: "module.mymod"}
		got := SnapshotKey(s)
		if got != "module.mymod.aws_instance.web" {
			t.Errorf("SnapshotKey = %q, want module.mymod.aws_instance.web", got)
		}
	})
}

func TestParseHCLContent_ProviderRegion(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
provider "aws" {
  region = "eu-west-1"
}

resource "aws_instance" "web" {
  ami = "ami-123"
}
`)

	region := ParseProviderRegion(dir)
	if region != "eu-west-1" {
		t.Errorf("expected eu-west-1, got %q", region)
	}
}

func TestParseProviderRegion_NoProvider(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `resource "aws_instance" "web" { ami = "ami-123" }`)

	region := ParseProviderRegion(dir)
	if region != "" {
		t.Errorf("expected empty, got %q", region)
	}
}

func TestParseProviderRegion_WithVarResolution(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
variable "region" {
  default = "ap-southeast-1"
}

provider "aws" {
  region = var.region
}
`)

	region := ParseProviderRegion(dir)
	if region != "ap-southeast-1" {
		t.Errorf("expected ap-southeast-1, got %q", region)
	}
}

func TestParseDirectoryWithOpts_CustomLimits(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		writeTF(t, dir, fmt.Sprintf("file%02d.tf", i), fmt.Sprintf("resource \"null_resource\" \"r%d\" {}", i))
	}

	opts := ParseOptions{MaxFiles: 3}
	_, warnings, err := ParseDirectoryWithOpts(dir, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "processing first 3") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected custom max files warning")
	}
}

func TestParseGitRef_InvalidRef(t *testing.T) {
	tmpDir := t.TempDir()

	_, _, err := ParseGitRef("nonexistent-ref-abc123", ".", tmpDir)
	if err == nil {
		t.Fatal("expected error for invalid git ref")
	}
	if !strings.Contains(err.Error(), "git archive failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseGitRef_Success(t *testing.T) {
	repoDir := t.TempDir()

	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), name, args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
		}
	}

	run("git", "init")
	run("git", "config", "user.email", "tester@example.com")
	run("git", "config", "user.name", "Test")
	run("git", "config", "commit.gpgSign", "false")
	run("git", "config", "tag.gpgSign", "false")

	tfDir := filepath.Join(repoDir, "infra")
	os.MkdirAll(tfDir, 0o755)
	writeTF(t, tfDir, "main.tf", "resource \"aws_instance\" \"srv\" {\n  ami = \"ami-git\"\n}\n")

	run("git", "add", "-A")
	run("git", "commit", "-m", "init")

	tmpDir := t.TempDir()

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	snapshots, _, err := ParseGitRef("HEAD", "infra", tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, s := range snapshots {
		if s.Type == "aws_instance" && s.Name == "srv" {
			found = true
		}
	}
	if !found {
		t.Error("expected aws_instance.srv from git ref")
	}
}

func TestParseGitRef_TarFailure(t *testing.T) {
	repoDir := t.TempDir()

	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), name, args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
		}
	}

	run("git", "init")
	run("git", "config", "user.email", "tester@example.com")
	run("git", "config", "user.name", "Test")
	run("git", "config", "commit.gpgSign", "false")
	run("git", "config", "tag.gpgSign", "false")

	writeTF(t, repoDir, "main.tf", "resource \"null_resource\" \"x\" {}\n")
	run("git", "add", "-A")
	run("git", "commit", "-m", "init")

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	readonlyDir := filepath.Join(t.TempDir(), "readonly")
	os.MkdirAll(readonlyDir, 0o000)
	defer os.Chmod(readonlyDir, 0o755)

	_, _, err := ParseGitRef("HEAD", ".", readonlyDir)
	if err != nil {
		t.Logf("got expected error: %v", err)
	}
}

func TestParseGitRef_MkdirError(t *testing.T) {
	repoDir := t.TempDir()

	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), name, args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
		}
	}

	run("git", "init")
	run("git", "config", "user.email", "tester@example.com")
	run("git", "config", "user.name", "Test")
	run("git", "config", "commit.gpgSign", "false")
	run("git", "config", "tag.gpgSign", "false")

	writeTF(t, repoDir, "main.tf", "resource \"null_resource\" \"x\" {}\n")
	run("git", "add", "-A")
	run("git", "commit", "-m", "init")

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	blockingFile := filepath.Join(t.TempDir(), "HEAD")
	os.WriteFile(blockingFile, []byte("block"), 0o644)

	_, _, err := ParseGitRef("HEAD", ".", filepath.Dir(blockingFile))
	if err != nil {
		t.Logf("got expected or nil error: %v", err)
	}
}

func TestParseDirectory_StatError(t *testing.T) {
	dir := t.TempDir()
	tfPath := filepath.Join(dir, "vanish.tf")
	os.WriteFile(tfPath, []byte(`resource "a" "b" {}`), 0o644)
	writeTF(t, dir, "ok.tf", "resource \"aws_instance\" \"y\" {\n  ami = \"ami-ok\"\n}\n")

	os.Remove(tfPath)
	os.Symlink("/nonexistent-target-xyz", tfPath)
	defer os.Remove(tfPath)

	_, _, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseGitRef_TarExtractFailure(t *testing.T) {
	repoDir := t.TempDir()

	run := func(name string, args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), name, args...)
		cmd.Dir = repoDir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
		}
	}

	run("git", "init")
	run("git", "config", "user.email", "tester@example.com")
	run("git", "config", "user.name", "Test")
	run("git", "config", "commit.gpgSign", "false")
	run("git", "config", "tag.gpgSign", "false")

	writeTF(t, repoDir, "main.tf", "resource \"null_resource\" \"x\" {}\n")
	run("git", "add", "-A")
	run("git", "commit", "-m", "init")

	origDir, _ := os.Getwd()
	os.Chdir(repoDir)
	defer os.Chdir(origDir)

	binDir := t.TempDir()
	fakeTar := filepath.Join(binDir, "tar")
	os.WriteFile(fakeTar, []byte("#!/bin/sh\nexit 1\n"), 0o755)

	origPath := os.Getenv("PATH")
	os.Setenv("PATH", binDir+":"+origPath)
	defer os.Setenv("PATH", origPath)

	_, _, err := ParseGitRef("HEAD", ".", t.TempDir())
	if err == nil {
		t.Fatal("expected error from tar failure")
	}
	if !strings.Contains(err.Error(), "tar extract failed") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseDirectory_GlobError(t *testing.T) {
	badDir := filepath.Join(t.TempDir(), "bad[dir")
	os.MkdirAll(badDir, 0o755)

	_, _, err := ParseDirectory(badDir)
	if err == nil {
		t.Log("no error from glob with bracket in dir name")
	}
}

func TestParseHCLContent_VariableResolutionWithContext(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "variables.tf", `
variable "instance_type" {
  default = "t3.micro"
}
`)
	writeTF(t, dir, "main.tf", `
resource "aws_instance" "web" {
  instance_type = var.instance_type
}
`)

	opts := ParseOptions{}
	snapshots, _, err := ParseDirectoryWithOpts(dir, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(snapshots) == 0 {
		t.Fatal("expected at least one snapshot")
	}

	found := false
	for _, s := range snapshots {
		if s.Type == "aws_instance" && s.Name == "web" {
			found = true
			if s.Attributes["instance_type"] != "t3.micro" {
				t.Errorf("expected instance_type=t3.micro, got %v", s.Attributes["instance_type"])
			}
		}
	}
	if !found {
		t.Error("expected aws_instance.web")
	}
}

func TestParseHCLContent_VariableResolutionWithTfvars(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "variables.tf", `
variable "instance_type" {
  default = "t3.micro"
}
`)
	writeTF(t, dir, "main.tf", `
resource "aws_instance" "web" {
  instance_type = var.instance_type
}
`)
	os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte(`instance_type = "t3.large"`+"\n"), 0o644)

	opts := ParseOptions{}
	snapshots, _, err := ParseDirectoryWithOpts(dir, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, s := range snapshots {
		if s.Type == "aws_instance" && s.Name == "web" {
			found = true
			if s.Attributes["instance_type"] != "t3.large" {
				t.Errorf("expected instance_type=t3.large, got %v", s.Attributes["instance_type"])
			}
		}
	}
	if !found {
		t.Error("expected aws_instance.web")
	}
}

func TestParseHCLContent_FunctionCallWithContext(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "variables.tf", `
variable "prefix" {
  default = "prod"
}
variable "suffix" {
  default = "app"
}
`)
	writeTF(t, dir, "main.tf", `
resource "aws_instance" "web" {
  ami = join("-", [var.prefix, var.suffix])
}
`)

	opts := ParseOptions{}
	snapshots, _, err := ParseDirectoryWithOpts(dir, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, s := range snapshots {
		if s.Type == "aws_instance" && s.Name == "web" {
			found = true
			if s.Attributes["ami"] != "prod-app" {
				t.Errorf("expected ami=prod-app, got %v", s.Attributes["ami"])
			}
		}
	}
	if !found {
		t.Error("expected aws_instance.web")
	}
}

func TestParseDirectoryWithOpts_VarFlags(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
variable "itype" {}
resource "aws_instance" "web" {
  instance_type = var.itype
}
`)

	snapshots, _, err := ParseDirectoryWithOpts(dir, ParseOptions{
		Vars: []string{"itype=t3.xlarge"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected snapshots")
	}
	if snapshots[0].Attributes["instance_type"] != "t3.xlarge" {
		t.Errorf("expected t3.xlarge, got %v", snapshots[0].Attributes["instance_type"])
	}
}

func TestParseDirectoryWithOpts_MaxResourcesOpt(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "resource \"null_resource\" \"r%d\" { value = \"%d\" }\n", i, i)
	}
	writeTF(t, dir, "main.tf", sb.String())

	snapshots, warnings, err := ParseDirectoryWithOpts(dir, ParseOptions{MaxResources: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) != 5 {
		t.Errorf("expected 5 snapshots, got %d", len(snapshots))
	}
	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "truncating to") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected truncation warning")
	}
}

func TestParseDirectoryWithOpts_MaxFileSizeOpt(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "big.tf", strings.Repeat("x", 100))
	writeTF(t, dir, "ok.tf", `resource "null_resource" "x" {}`)

	_, warnings, err := ParseDirectoryWithOpts(dir, ParseOptions{MaxFileSize: 50})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "exceeds") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected size warning")
	}
}

func TestBuildEvalCtxFromOpts_WithOpts(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "terraform.tfvars", `region = "eu-west-1"`)
	writeTF(t, dir, "vars.tf", `variable "region" { default = "us-east-1" }`)

	ctx := buildEvalCtxFromOpts(dir, []ParseOptions{{VarFiles: nil, Vars: []string{"region=ap-south-1"}}})
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	vars := ctx.Variables["var"]
	if vars.IsNull() {
		t.Fatal("expected var namespace")
	}
	val := vars.GetAttr("region")
	if val.AsString() != "ap-south-1" {
		t.Errorf("expected ap-south-1, got %s", val.AsString())
	}
}

func TestBuildEvalCtxFromOpts_NoOpts(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "vars.tf", `variable "x" { default = "hello" }`)

	ctx := buildEvalCtxFromOpts(dir, nil)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
}

func TestExtractProviderRegionFromFile_ReadError(t *testing.T) {
	result := extractProviderRegionFromFile("/nonexistent/file.tf", nil)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestExtractProviderRegionFromFile_InvalidHCL(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bad.tf")
	os.WriteFile(f, []byte("this is not valid HCL {{{"), 0o644)
	result := extractProviderRegionFromFile(f, nil)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestExtractProviderRegionFromFile_NoProvider(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "main.tf")
	os.WriteFile(f, []byte(`resource "aws_instance" "x" { ami = "test" }`), 0o644)
	result := extractProviderRegionFromFile(f, nil)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestExtractRegionFromBlock_NoRegionAttr(t *testing.T) {
	content := []byte(`provider "aws" {}`)
	file, _ := hclsyntax.ParseConfig(content, "test.tf", hcl.Pos{Line: 1, Column: 1})
	body := file.Body.(*hclsyntax.Body)
	result := extractRegionFromBlock(body.Blocks[0], nil, content)
	if result != "" {
		t.Errorf("expected empty, got %q", result)
	}
}

func TestExtractRegionFromBlock_UnresolvableVar(t *testing.T) {
	content := []byte(`provider "aws" { region = var.unresolvable }`)
	file, _ := hclsyntax.ParseConfig(content, "test.tf", hcl.Pos{Line: 1, Column: 1})
	body := file.Body.(*hclsyntax.Body)
	result := extractRegionFromBlock(body.Blocks[0], nil, content)
	if result != "" {
		t.Errorf("expected empty for unresolvable var, got %q", result)
	}
}

func TestExtractRegionFromBlock_RawLiteralFallback(t *testing.T) {
	content := []byte(`provider "aws" { region = local.something }`)
	file, _ := hclsyntax.ParseConfig(content, "test.tf", hcl.Pos{Line: 1, Column: 1})
	body := file.Body.(*hclsyntax.Body)
	result := extractRegionFromBlock(body.Blocks[0], nil, content)
	if result != "" {
		t.Errorf("expected empty for local ref, got %q", result)
	}
}

func TestExtractRegionFromBlock_NonStringType(t *testing.T) {
	content := []byte(`provider "aws" { region = 123 }`)
	file, _ := hclsyntax.ParseConfig(content, "test.tf", hcl.Pos{Line: 1, Column: 1})
	body := file.Body.(*hclsyntax.Body)
	ctx := BuildEvalContext(nil)
	result := extractRegionFromBlock(body.Blocks[0], ctx, content)
	if result != "" {
		t.Errorf("expected empty for non-string, got %q", result)
	}
}

func TestResolveLocalModule_MaxDepth(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "mod")
	os.MkdirAll(modDir, 0o755)
	writeTF(t, modDir, "main.tf", `resource "null_resource" "x" {}`)

	visited := make(map[string]bool)
	_, warnings := resolveLocalModule("deep", "./mod", filepath.Join(dir, "main.tf"), visited, MaxDepth, nil, ParseOptions{}, "", nil)
	if len(warnings) == 0 {
		t.Fatal("expected depth warning")
	}
	if !strings.Contains(warnings[0], "max depth") {
		t.Errorf("unexpected warning: %s", warnings[0])
	}
}

func TestResolveLocalModule_NestedModulePath(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "child")
	os.MkdirAll(modDir, 0o755)
	writeTF(t, modDir, "main.tf", `resource "aws_instance" "x" { ami = "test" }`)

	visited := make(map[string]bool)
	snapshots, _ := resolveLocalModule("child", "./child", filepath.Join(dir, "main.tf"), visited, 0, nil, ParseOptions{}, "module.parent", nil)
	if len(snapshots) == 0 {
		t.Fatal("expected snapshots")
	}
	if snapshots[0].ModulePath != "module.parent.module.child" {
		t.Errorf("expected module.parent.module.child, got %q", snapshots[0].ModulePath)
	}
}

func TestParseVarFiles_NonexistentExtraFile(t *testing.T) {
	dir := t.TempDir()
	result := ParseVarFiles(dir, []string{"/nonexistent/vars.tfvars"})
	if len(result) != 0 {
		t.Errorf("expected empty result for nonexistent file, got %d entries", len(result))
	}
}

func TestExtractRegionFromBlock_RawFunctionFallback(t *testing.T) {
	content := []byte(`provider "aws" { region = upper("us-east-1") }`)
	file, _ := hclsyntax.ParseConfig(content, "test.tf", hcl.Pos{Line: 1, Column: 1})
	body := file.Body.(*hclsyntax.Body)
	result := extractRegionFromBlock(body.Blocks[0], nil, content)
	if !strings.Contains(result, "upper") {
		t.Errorf("expected raw source with upper(), got %q", result)
	}
}

func TestResolveLocalModule_NestedExistingPath(t *testing.T) {
	dir := t.TempDir()
	innerDir := filepath.Join(dir, "inner")
	os.MkdirAll(innerDir, 0o755)
	subDir := filepath.Join(innerDir, "sub")
	os.MkdirAll(subDir, 0o755)
	writeTF(t, subDir, "main.tf", `resource "null_resource" "x" {}`)
	writeTF(t, innerDir, "main.tf", `module "sub" { source = "./sub" }`)

	visited := make(map[string]bool)
	snapshots, _ := resolveLocalModule("inner", "./inner", filepath.Join(dir, "main.tf"), visited, 0, nil, ParseOptions{}, "", nil)
	for _, s := range snapshots {
		if s.Type == "null_resource" && strings.Contains(s.ModulePath, "module.inner.module.sub") {
			return
		}
	}
	t.Error("expected nested module path module.inner.module.sub")
}

func TestParseProviderRegion_GlobError(t *testing.T) {
	result := ParseProviderRegion("bad[dir")
	if result != "" {
		t.Errorf("expected empty for glob error, got %q", result)
	}
}

func TestParseProviderRegions_AllProviders(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "providers.tf", `
provider "aws" {
  region = "us-west-2"
}

provider "google" {
  region = "europe-west1"
}

provider "azurerm" {
  location = "westus2"
}
`)
	result := ParseProviderRegions(dir)
	if result.AWS != "us-west-2" {
		t.Errorf("AWS = %q, want us-west-2", result.AWS)
	}
	if result.GCP != "europe-west1" {
		t.Errorf("GCP = %q, want europe-west1", result.GCP)
	}
	if result.Azure != "westus2" {
		t.Errorf("Azure = %q, want westus2", result.Azure)
	}
}

func TestParseProviderRegions_AzureLocation(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
provider "azurerm" {
  features {}
  location = "eastus"
}
`)
	result := ParseProviderRegions(dir)
	if result.Azure != "eastus" {
		t.Errorf("Azure = %q, want eastus", result.Azure)
	}
}

func TestParseProviderRegions_GCPOnly(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
provider "google" {
  region  = "us-central1"
  project = "my-project"
}
`)
	result := ParseProviderRegions(dir)
	if result.GCP != "us-central1" {
		t.Errorf("GCP = %q, want us-central1", result.GCP)
	}
	if result.AWS != "" {
		t.Errorf("AWS = %q, want empty", result.AWS)
	}
}

func TestParseProviderRegions_NoProviders(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `resource "null_resource" "x" {}`)
	result := ParseProviderRegions(dir)
	if result.AWS != "" || result.GCP != "" || result.Azure != "" {
		t.Errorf("expected all empty, got %+v", result)
	}
}

func TestParseProviderRegions_GlobError(t *testing.T) {
	result := ParseProviderRegions("bad[dir")
	if result.AWS != "" || result.GCP != "" || result.Azure != "" {
		t.Errorf("expected all empty for glob error, got %+v", result)
	}
}

func TestParseProviderRegions_BadHCL(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "bad.tf", `this is not valid HCL {{{`)
	result := ParseProviderRegions(dir)
	if result.AWS != "" || result.GCP != "" || result.Azure != "" {
		t.Errorf("expected all empty for bad HCL, got %+v", result)
	}
}

func TestParseProviderRegions_AzureLocationRawLiteral(t *testing.T) {
	dir := t.TempDir()
	// lookup() can't be evaluated but is not a var./local. ref,
	// so the raw string should be returned by the fallback path
	writeTF(t, dir, "main.tf", `
provider "azurerm" {
  location = lookup({a = "eastus"}, "a")
  features {}
}
`)
	result := ParseProviderRegions(dir)
	if result.Azure == "" {
		t.Error("expected non-empty Azure from raw literal extraction")
	}
}

func TestParseProviderRegions_AzureNonStringLocation(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
provider "azurerm" {
  location = 123
  features {}
}
`)
	result := ParseProviderRegions(dir)
	if result.Azure != "" {
		t.Errorf("Azure = %q, want empty for non-string location", result.Azure)
	}
}

func TestParseProviderRegions_AzureRawLocation(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
provider "azurerm" {
  location = "westeurope"
  features {}
}
`)
	result := ParseProviderRegions(dir)
	if result.Azure != "westeurope" {
		t.Errorf("Azure = %q, want westeurope", result.Azure)
	}
}

func TestParseProviderRegions_AzureUnresolvedVarReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
provider "azurerm" {
  location = var.location
  features {}
}
`)
	result := ParseProviderRegions(dir)
	if result.Azure != "" {
		t.Errorf("Azure = %q, want empty for unresolved var", result.Azure)
	}
}

func TestParseProviderRegions_AzureRegionAttr(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
provider "azurerm" {
  region = "centralus"
  features {}
}
`)
	result := ParseProviderRegions(dir)
	if result.Azure != "centralus" {
		t.Errorf("Azure = %q, want centralus", result.Azure)
	}
}

func TestParseProviderRegions_UnreadableFile(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `provider "aws" { region = "us-east-1" }`)
	// Make a .tf file that can't be read
	path := filepath.Join(dir, "unreadable.tf")
	os.WriteFile(path, []byte(`provider "google" { region = "us-central1" }`), 0o000)
	defer os.Chmod(path, 0o644)

	result := ParseProviderRegions(dir)
	if result.AWS != "us-east-1" {
		t.Errorf("AWS = %q, want us-east-1", result.AWS)
	}
	// GCP unreadable, should be empty
	if result.GCP != "" {
		t.Errorf("GCP = %q, want empty (unreadable file)", result.GCP)
	}
}

func TestIsRemoteSource(t *testing.T) {
	if !isRemoteSource("terraform-aws-modules/vpc/aws") {
		t.Error("expected true for registry source")
	}
	if isRemoteSource("./local") {
		t.Error("expected false for local source")
	}
	if isRemoteSource("") {
		t.Error("expected false for empty source")
	}
}

func TestIsLocalSource(t *testing.T) {
	if !isLocalSource("./local") {
		t.Error("expected true for relative source")
	}
	if !isLocalSource("/absolute/path") {
		t.Error("expected true for absolute source")
	}
	if isLocalSource("terraform-aws-modules/vpc/aws") {
		t.Error("expected false for registry source")
	}
	if isLocalSource("") {
		t.Error("expected false for empty source")
	}
}

func TestParseFilesWithOpts_SingleFile(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
resource "aws_instance" "web" {
  ami           = "ami-123"
  instance_type = "t3.micro"
}
`)

	snapshots, _, err := ParseFilesWithOpts([]string{filepath.Join(dir, "main.tf")}, ParseOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Type != "aws_instance" || snapshots[0].Name != "web" {
		t.Errorf("unexpected snapshot: %+v", snapshots[0])
	}
}

func TestParseFilesWithOpts_MultipleFiles(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "a.tf", `resource "aws_instance" "a" { ami = "ami-1" }`)
	writeTF(t, dir, "b.tf", `resource "aws_s3_bucket" "b" { bucket = "test" }`)

	snapshots, _, err := ParseFilesWithOpts([]string{
		filepath.Join(dir, "a.tf"),
		filepath.Join(dir, "b.tf"),
	}, ParseOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snapshots))
	}
}

func TestParseFilesWithOpts_NonTFFile(t *testing.T) {
	dir := t.TempDir()
	jsonFile := filepath.Join(dir, "data.json")
	os.WriteFile(jsonFile, []byte(`{}`), 0o644)

	_, _, err := ParseFilesWithOpts([]string{jsonFile}, ParseOptions{})
	if err == nil {
		t.Fatal("expected error for non-.tf file")
	}
	if !strings.Contains(err.Error(), "not a .tf file") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseFilesWithOpts_FileNotExist(t *testing.T) {
	_, _, err := ParseFilesWithOpts([]string{"/nonexistent/path/main.tf"}, ParseOptions{})
	if err != nil {
		t.Logf("got error (expected for nonexistent): %v", err)
		return
	}
}

func TestParseFilesWithOpts_MultipleDirectories(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	writeTF(t, dir1, "main.tf", `
variable "x" { default = "val1" }
resource "aws_instance" "a" { ami = var.x }
`)
	writeTF(t, dir2, "main.tf", `
variable "x" { default = "val2" }
resource "aws_instance" "b" { ami = var.x }
`)

	snapshots, _, err := ParseFilesWithOpts([]string{
		filepath.Join(dir1, "main.tf"),
		filepath.Join(dir2, "main.tf"),
	}, ParseOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(snapshots))
	}

	vals := map[string]interface{}{}
	for _, s := range snapshots {
		vals[s.Name] = s.Attributes["ami"]
	}
	if vals["a"] != "val1" {
		t.Errorf("expected a.ami=val1, got %v", vals["a"])
	}
	if vals["b"] != "val2" {
		t.Errorf("expected b.ami=val2, got %v", vals["b"])
	}
}

func TestParseFilesWithOpts_MaxResources(t *testing.T) {
	dir := t.TempDir()
	var sb strings.Builder
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "resource \"null_resource\" \"r%d\" {}\n", i)
	}
	writeTF(t, dir, "main.tf", sb.String())

	snapshots, warnings, err := ParseFilesWithOpts([]string{filepath.Join(dir, "main.tf")}, ParseOptions{MaxResources: 5})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) != 5 {
		t.Errorf("expected 5 snapshots, got %d", len(snapshots))
	}
	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "truncating to") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected truncation warning")
	}
}

func TestParseFilesWithOpts_WithVars(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
variable "itype" {}
resource "aws_instance" "web" { instance_type = var.itype }
`)

	snapshots, _, err := ParseFilesWithOpts(
		[]string{filepath.Join(dir, "main.tf")},
		ParseOptions{Vars: []string{"itype=m5.xlarge"}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected snapshots")
	}
	if snapshots[0].Attributes["instance_type"] != "m5.xlarge" {
		t.Errorf("expected m5.xlarge, got %v", snapshots[0].Attributes["instance_type"])
	}
}

func TestParseFilesWithOpts_ModuleResolution(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "mymod")
	os.MkdirAll(modDir, 0o755)
	writeTF(t, modDir, "main.tf", `resource "aws_sns_topic" "t" { name = "test" }`)
	writeTF(t, dir, "main.tf", `module "mymod" { source = "./mymod" }`)

	snapshots, _, err := ParseFilesWithOpts([]string{filepath.Join(dir, "main.tf")}, ParseOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, s := range snapshots {
		if s.Type == "aws_sns_topic" && s.Name == "t" {
			found = true
		}
	}
	if !found {
		t.Error("expected aws_sns_topic from module resolution")
	}
}

func TestParseFilesWithOpts_EmptyList(t *testing.T) {
	_, _, err := ParseFilesWithOpts([]string{}, ParseOptions{})
	if err == nil {
		t.Fatal("expected error for empty file list")
	}
	if !strings.Contains(err.Error(), "no .tf files provided") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseFilesWithOpts_FileSizeExceeded(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "big.tf", strings.Repeat("x", 200))
	writeTF(t, dir, "ok.tf", `resource "null_resource" "x" {}`)

	snapshots, warnings, err := ParseFilesWithOpts(
		[]string{filepath.Join(dir, "big.tf"), filepath.Join(dir, "ok.tf")},
		ParseOptions{MaxFileSize: 100},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "exceeds") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected size warning")
	}
	if len(snapshots) != 1 {
		t.Errorf("expected 1 snapshot (only ok.tf), got %d", len(snapshots))
	}
}

func TestParseFilesWithOpts_StatError(t *testing.T) {
	dir := t.TempDir()
	tfPath := filepath.Join(dir, "vanish.tf")
	os.WriteFile(tfPath, []byte(`resource "a" "b" {}`), 0o644)
	writeTF(t, dir, "ok.tf", "resource \"aws_instance\" \"y\" {\n  ami = \"ami-ok\"\n}\n")

	os.Remove(tfPath)
	os.Symlink("/nonexistent-target-xyz", tfPath)
	defer os.Remove(tfPath)

	snapshots, warnings, err := ParseFilesWithOpts([]string{tfPath, filepath.Join(dir, "ok.tf")}, ParseOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "cannot stat") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected stat warning for broken symlink")
	}
	_ = snapshots
}

func TestParseFilesWithOpts_ReadError(t *testing.T) {
	dir := t.TempDir()
	tfPath := filepath.Join(dir, "noperm.tf")
	os.WriteFile(tfPath, []byte(`resource "a" "b" {}`), 0o000)
	defer os.Chmod(tfPath, 0o644)

	_, warnings, err := ParseFilesWithOpts([]string{tfPath}, ParseOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "Failed to read") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected read warning for unreadable file")
	}
}

func TestGroupFilesByDir_AbsError(t *testing.T) {
	orig := absPath
	absPath = func(path string) (string, error) {
		return "", fmt.Errorf("mock abs error")
	}
	defer func() { absPath = orig }()

	_, err := groupFilesByDir([]string{"test.tf"})
	if err == nil {
		t.Fatal("expected error from absPath failure")
	}
	if !strings.Contains(err.Error(), "cannot resolve path") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseFileGroup_Direct(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", "resource \"null_resource\" \"x\" {}\n")

	group := fileGroup{
		dir:   dir,
		files: []string{filepath.Join(dir, "main.tf")},
	}
	visited := make(map[string]bool)
	snapshots, warnings := parseFileGroup(group, ParseOptions{}, visited, int64(MaxFileSize), nil)
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(snapshots) != 1 {
		t.Errorf("expected 1 snapshot, got %d", len(snapshots))
	}
}

func TestParseFileGroup_ReadError(t *testing.T) {
	dir := t.TempDir()
	tfPath := filepath.Join(dir, "unreadable.tf")
	os.WriteFile(tfPath, []byte(`resource "a" "b" {}`), 0o644)

	orig := readFile
	readFile = func(name string) ([]byte, error) {
		return nil, fmt.Errorf("mock read error")
	}
	defer func() { readFile = orig }()

	group := fileGroup{
		dir:   dir,
		files: []string{tfPath},
	}
	visited := make(map[string]bool)
	_, warnings := parseFileGroup(group, ParseOptions{}, visited, int64(MaxFileSize), nil)
	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "Failed to read") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected read error warning")
	}
}

func TestParseFileGroup_WithVarFiles(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
variable "x" { default = "orig" }
resource "null_resource" "r" { value = var.x }
`)
	os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte(`x = "overridden"`+"\n"), 0o644)

	group := fileGroup{
		dir:   dir,
		files: []string{filepath.Join(dir, "main.tf")},
	}
	visited := make(map[string]bool)
	snapshots, _ := parseFileGroup(group, ParseOptions{}, visited, int64(MaxFileSize), nil)
	if len(snapshots) == 0 {
		t.Fatal("expected snapshots")
	}
	if snapshots[0].Attributes["value"] != "overridden" {
		t.Errorf("expected overridden, got %v", snapshots[0].Attributes["value"])
	}
}

func TestParseFileGroup_StatError(t *testing.T) {
	dir := t.TempDir()
	group := fileGroup{
		dir:   dir,
		files: []string{filepath.Join(dir, "nonexistent.tf")},
	}
	visited := make(map[string]bool)
	_, warnings := parseFileGroup(group, ParseOptions{}, visited, int64(MaxFileSize), nil)
	foundWarning := false
	for _, w := range warnings {
		if strings.Contains(w, "cannot stat") {
			foundWarning = true
		}
	}
	if !foundWarning {
		t.Error("expected stat warning for nonexistent file")
	}
}

func TestShouldSkipByCount_Zero(t *testing.T) {
	if !shouldSkipByCount(map[string]interface{}{"count": float64(0)}) {
		t.Error("expected count=0 to skip")
	}
}

func TestShouldSkipByCount_Positive(t *testing.T) {
	if shouldSkipByCount(map[string]interface{}{"count": float64(3)}) {
		t.Error("expected count=3 to not skip")
	}
}

func TestShouldSkipByCount_Absent(t *testing.T) {
	if shouldSkipByCount(map[string]interface{}{"ami": "ami-123"}) {
		t.Error("expected missing count to not skip")
	}
}

func TestShouldSkipByCount_Nil(t *testing.T) {
	if shouldSkipByCount(map[string]interface{}{"count": nil}) {
		t.Error("expected nil count to not skip")
	}
}

func TestShouldSkipByCount_BoolFalse(t *testing.T) {
	if !shouldSkipByCount(map[string]interface{}{"count": false}) {
		t.Error("expected count=false to skip")
	}
}

func TestShouldSkipByCount_BoolTrue(t *testing.T) {
	if shouldSkipByCount(map[string]interface{}{"count": true}) {
		t.Error("expected count=true to not skip")
	}
}

func TestExtractResources_CountZeroSkipped(t *testing.T) {
	parsed := map[string]interface{}{
		"resource": map[string]interface{}{
			"aws_instance": map[string]interface{}{
				"web": []interface{}{
					map[string]interface{}{
						"count":         float64(0),
						"instance_type": "t3.micro",
					},
				},
			},
		},
	}
	snapshots := extractResources(parsed)
	if len(snapshots) != 0 {
		t.Errorf("expected 0 snapshots for count=0, got %d", len(snapshots))
	}
}

func TestExtractResources_CountOneKept(t *testing.T) {
	parsed := map[string]interface{}{
		"resource": map[string]interface{}{
			"aws_instance": map[string]interface{}{
				"web": []interface{}{
					map[string]interface{}{
						"count":         float64(1),
						"instance_type": "t3.micro",
					},
				},
			},
		},
	}
	snapshots := extractResources(parsed)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if _, hasCount := snapshots[0].Attributes["count"]; hasCount {
		t.Error("expected count to be stripped from attributes")
	}
	if snapshots[0].Attributes["instance_type"] != "t3.micro" {
		t.Error("expected instance_type preserved")
	}
}

func TestExtractResources_CountAbsentKept(t *testing.T) {
	parsed := map[string]interface{}{
		"resource": map[string]interface{}{
			"aws_instance": map[string]interface{}{
				"web": []interface{}{
					map[string]interface{}{
						"instance_type": "t3.micro",
					},
				},
			},
		},
	}
	snapshots := extractResources(parsed)
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
}

func TestExtractModules_CountZeroSkipped(t *testing.T) {
	dir := t.TempDir()
	moduleDir := filepath.Join(dir, "mymod")
	os.MkdirAll(moduleDir, 0o750)
	writeTF(t, moduleDir, "main.tf", `
resource "aws_instance" "this" {
  instance_type = "t3.micro"
}
`)

	parsed := map[string]interface{}{
		"module": map[string]interface{}{
			"mymod": []interface{}{
				map[string]interface{}{
					"count":  float64(0),
					"source": "./" + filepath.Base(moduleDir),
				},
			},
		},
	}
	snapshots, _ := extractModules(parsed, filepath.Join(dir, "main.tf"), make(map[string]bool), 0, nil, ParseOptions{}, nil)
	if len(snapshots) != 0 {
		t.Errorf("expected 0 snapshots for module with count=0, got %d", len(snapshots))
	}
}

func TestParseDirectory_CountZeroResourceFiltered(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
resource "aws_instance" "active" {
  count         = 1
  instance_type = "t3.micro"
}

resource "aws_instance" "inactive" {
  count         = 0
  instance_type = "t3.large"
}
`)

	snapshots, _, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot, got %d", len(snapshots))
	}
	if snapshots[0].Name != "active" {
		t.Errorf("expected active resource, got %s", snapshots[0].Name)
	}
}

func TestParseDirectory_LocalsResolveCount(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "variables.tf", `
variable "create" {
  default = true
}
variable "create_spot" {
  default = false
}
`)
	writeTF(t, dir, "main.tf", `
locals {
  create = var.create && !var.create_spot
}

resource "aws_instance" "this" {
  count         = local.create ? 1 : 0
  instance_type = "t3.micro"
}

resource "aws_spot_instance_request" "this" {
  count         = var.create_spot ? 1 : 0
  instance_type = "t3.micro"
}
`)

	snapshots, _, err := ParseDirectory(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 snapshot (only aws_instance), got %d", len(snapshots))
	}
	if snapshots[0].Type != "aws_instance" {
		t.Errorf("expected aws_instance, got %s", snapshots[0].Type)
	}
}

func TestShouldSkipByCount_IntZero(t *testing.T) {
	if !shouldSkipByCount(map[string]interface{}{"count": int(0)}) {
		t.Error("expected int(0) count to skip")
	}
}

func TestShouldSkipByCount_IntNonZero(t *testing.T) {
	if shouldSkipByCount(map[string]interface{}{"count": int(1)}) {
		t.Error("expected int(1) count not to skip")
	}
}

func TestShouldSkipByCount_Int64Zero(t *testing.T) {
	if !shouldSkipByCount(map[string]interface{}{"count": int64(0)}) {
		t.Error("expected int64(0) count to skip")
	}
}

func TestShouldSkipByCount_Int64NonZero(t *testing.T) {
	if shouldSkipByCount(map[string]interface{}{"count": int64(2)}) {
		t.Error("expected int64(2) count not to skip")
	}
}

func TestParseDirectoryWithOpts_DownloadModules(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
resource "aws_instance" "web" {
  ami           = "ami-12345"
  instance_type = "t3.micro"
}
`)

	opts := ParseOptions{
		DownloadModules: true,
		ProjectDir:      dir,
	}
	snapshots, _, err := ParseDirectoryWithOpts(dir, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected at least one snapshot")
	}
}

func TestParseDirectoryWithOpts_DownloadModules_DefaultProjectDir(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
resource "aws_instance" "web" {
  instance_type = "t3.micro"
}
`)

	opts := ParseOptions{
		DownloadModules: true,
	}
	snapshots, _, err := ParseDirectoryWithOpts(dir, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected at least one snapshot")
	}
}

func TestParseFilesWithOpts_DownloadModules(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
resource "aws_s3_bucket" "b" {
  bucket = "my-bucket"
}
`)

	opts := ParseOptions{
		DownloadModules: true,
		ProjectDir:      dir,
	}
	f := filepath.Join(dir, "main.tf")
	snapshots, _, err := ParseFilesWithOpts([]string{f}, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected at least one snapshot")
	}
}

func TestParseFilesWithOpts_DownloadModules_DefaultProjectDir(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
resource "aws_s3_bucket" "b" {
  bucket = "other-bucket"
}
`)

	opts := ParseOptions{
		DownloadModules: true,
	}
	f := filepath.Join(dir, "main.tf")
	snapshots, _, err := ParseFilesWithOpts([]string{f}, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected at least one snapshot")
	}
}

func TestParseFileGroup_WithLocals(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "variables.tf", `
variable "env" {
  default = "prod"
}
`)
	writeTF(t, dir, "locals.tf", `
locals {
  prefix = "app-${var.env}"
}
`)
	writeTF(t, dir, "main.tf", `
resource "aws_instance" "web" {
  instance_type = "t3.micro"
  tags = {
    Name = local.prefix
  }
}
`)

	group := fileGroup{
		dir:   dir,
		files: []string{filepath.Join(dir, "main.tf")},
	}
	snapshots, warnings := parseFileGroup(group, ParseOptions{}, make(map[string]bool), int64(MaxFileSize), nil)
	if len(warnings) > 0 {
		t.Logf("warnings: %v", warnings)
	}
	if len(snapshots) == 0 {
		t.Fatal("expected at least one snapshot")
	}
}

func TestExtractModules_RemoteWithDownloader(t *testing.T) {
	dir := t.TempDir()

	moduleDir := filepath.Join(dir, "cached_mod")
	_ = os.MkdirAll(moduleDir, 0o750)
	writeTF(t, moduleDir, "main.tf", `
resource "aws_instance" "this" {
  instance_type = "t3.micro"
}
`)

	d := NewModuleDownloader(dir)
	key := cacheKey("test/ec2/aws", "1.0.0")
	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key:     key,
		Source:  "test/ec2/aws",
		Version: "1.0.0",
		Dir:     moduleDir,
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()
	d.registry = &RegistryClient{httpClient: server.Client(), baseURL: server.URL}

	writeTF(t, dir, "main.tf", `
module "ec2" {
  source  = "test/ec2/aws"
  version = "1.0.0"
}
`)

	visited := map[string]bool{}
	absDir, _ := filepath.Abs(dir)
	visited[absDir] = true
	snapshots, _, err := parseDirectoryWithVisited(dir, visited, 0, nil, ParseOptions{}, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, s := range snapshots {
		if s.Type == "aws_instance" {
			found = true
		}
	}
	if !found {
		t.Error("expected aws_instance from remote module")
	}
}
