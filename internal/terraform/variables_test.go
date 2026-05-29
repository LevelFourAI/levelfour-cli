package terraform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

func TestParseVarFiles_TerraformTfvars(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte("region = \"us-west-2\"\ncount = 3\n"), 0o644)

	result := ParseVarFiles(dir, nil)

	if result["region"].AsString() != "us-west-2" {
		t.Errorf("expected region=us-west-2, got %v", result["region"])
	}
	f, _ := result["count"].AsBigFloat().Float64()
	if f != 3 {
		t.Errorf("expected count=3, got %v", f)
	}
}

func TestParseVarFiles_AutoTfvars(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "prod.auto.tfvars"), []byte("env = \"production\"\n"), 0o644)

	result := ParseVarFiles(dir, nil)

	if result["env"].AsString() != "production" {
		t.Errorf("expected env=production, got %v", result["env"])
	}
}

func TestParseVarFiles_ExtraVarFiles(t *testing.T) {
	dir := t.TempDir()
	extra := filepath.Join(dir, "extra.tfvars")
	os.WriteFile(extra, []byte("name = \"custom\"\n"), 0o644)

	result := ParseVarFiles(dir, []string{extra})

	if result["name"].AsString() != "custom" {
		t.Errorf("expected name=custom, got %v", result["name"])
	}
}

func TestParseVarFiles_Precedence(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte("val = \"base\"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "override.auto.tfvars"), []byte("val = \"auto\"\n"), 0o644)

	result := ParseVarFiles(dir, nil)

	if result["val"].AsString() != "auto" {
		t.Errorf("expected val=auto (auto overrides base), got %v", result["val"])
	}
}

func TestParseVarFiles_ExtraOverridesAuto(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "override.auto.tfvars"), []byte("val = \"auto\"\n"), 0o644)
	extra := filepath.Join(dir, "extra.tfvars")
	os.WriteFile(extra, []byte("val = \"explicit\"\n"), 0o644)

	result := ParseVarFiles(dir, []string{extra})

	if result["val"].AsString() != "explicit" {
		t.Errorf("expected val=explicit (extra overrides auto), got %v", result["val"])
	}
}

func TestParseVarFiles_NoFiles(t *testing.T) {
	dir := t.TempDir()
	result := ParseVarFiles(dir, nil)
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestParseVarFiles_InvalidHCL(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "terraform.tfvars"), []byte("not valid { { {"), 0o644)

	result := ParseVarFiles(dir, nil)
	if len(result) != 0 {
		t.Errorf("expected empty map for invalid HCL, got %d", len(result))
	}
}

func TestParseVarFlags_KeyValue(t *testing.T) {
	result := ParseVarFlags([]string{"region=us-west-2", "name=test"})

	if result["region"].AsString() != "us-west-2" {
		t.Errorf("expected region=us-west-2, got %v", result["region"])
	}
	if result["name"].AsString() != "test" {
		t.Errorf("expected name=test, got %v", result["name"])
	}
}

func TestParseVarFlags_ValueWithEquals(t *testing.T) {
	result := ParseVarFlags([]string{"tag=key=value"})

	if result["tag"].AsString() != "key=value" {
		t.Errorf("expected tag=key=value, got %v", result["tag"])
	}
}

func TestParseVarFlags_NoEquals(t *testing.T) {
	result := ParseVarFlags([]string{"invalid"})
	if len(result) != 0 {
		t.Errorf("expected empty map for flag without =, got %d", len(result))
	}
}

func TestParseVarFlags_Empty(t *testing.T) {
	result := ParseVarFlags(nil)
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d", len(result))
	}
}

func TestParseVariableDefaults(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "variables.tf", `
variable "region" {
  default = "us-east-1"
}

variable "instance_type" {
  default = "t3.micro"
}

variable "no_default" {
  type = string
}
`)

	result := ParseVariableDefaults(dir)

	if result["region"].AsString() != "us-east-1" {
		t.Errorf("expected region=us-east-1, got %v", result["region"])
	}
	if result["instance_type"].AsString() != "t3.micro" {
		t.Errorf("expected instance_type=t3.micro, got %v", result["instance_type"])
	}
	if _, ok := result["no_default"]; ok {
		t.Error("expected no_default to be absent")
	}
}

func TestParseVariableDefaults_NoTFFiles(t *testing.T) {
	dir := t.TempDir()
	result := ParseVariableDefaults(dir)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

func TestParseVariableDefaults_NumericDefault(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "variables.tf", `
variable "count" {
  default = 5
}
`)

	result := ParseVariableDefaults(dir)
	f, _ := result["count"].AsBigFloat().Float64()
	if f != 5 {
		t.Errorf("expected count=5, got %v", f)
	}
}

func TestBuildEvalContext_WithVars(t *testing.T) {
	vars := map[string]cty.Value{
		"region":        cty.StringVal("us-west-2"),
		"instance_type": cty.StringVal("t3.large"),
	}

	ctx := BuildEvalContext(vars)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	varObj := ctx.Variables["var"]
	if varObj.IsNull() {
		t.Fatal("expected non-null var object")
	}

	region := varObj.GetAttr("region")
	if region.AsString() != "us-west-2" {
		t.Errorf("expected region=us-west-2, got %v", region)
	}

	if _, ok := ctx.Functions["join"]; !ok {
		t.Error("expected join function in context")
	}
	if _, ok := ctx.Functions["lower"]; !ok {
		t.Error("expected lower function in context")
	}
}

func TestBuildEvalContext_EmptyVars(t *testing.T) {
	ctx := BuildEvalContext(map[string]cty.Value{})
	if ctx != nil {
		t.Error("expected nil context for empty vars")
	}
}

func TestBuildEvalContext_NilVars(t *testing.T) {
	ctx := BuildEvalContext(nil)
	if ctx != nil {
		t.Error("expected nil context for nil vars")
	}
}

func TestParseVarFiles_RelativePath(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "custom.tfvars"), []byte("key = \"val\"\n"), 0o644)

	result := ParseVarFiles(dir, []string{"custom.tfvars"})
	if result["key"].AsString() != "val" {
		t.Errorf("expected key=val, got %v", result["key"])
	}
}

func TestBuildEvalContextWithLocals(t *testing.T) {
	vars := map[string]cty.Value{"region": cty.StringVal("us-east-1")}
	locals := map[string]cty.Value{"create": cty.BoolVal(true)}

	ctx := BuildEvalContextWithLocals(vars, locals)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}

	varObj := ctx.Variables["var"]
	if varObj.GetAttr("region").AsString() != "us-east-1" {
		t.Error("expected var.region = us-east-1")
	}

	localObj := ctx.Variables["local"]
	if localObj.GetAttr("create").True() != true {
		t.Error("expected local.create = true")
	}
}

func TestBuildEvalContextWithLocals_NilLocals(t *testing.T) {
	vars := map[string]cty.Value{"x": cty.StringVal("y")}
	ctx := BuildEvalContextWithLocals(vars, nil)
	if ctx == nil {
		t.Fatal("expected non-nil context")
	}
	if _, ok := ctx.Variables["local"]; ok {
		t.Error("expected no local namespace when locals is nil")
	}
}

func TestBuildEvalContextWithLocals_BothEmpty(t *testing.T) {
	ctx := BuildEvalContextWithLocals(nil, nil)
	if ctx != nil {
		t.Error("expected nil context when both are empty")
	}
}

func TestParseLocals_Simple(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
locals {
  region = "us-west-2"
  env    = "prod"
}
`)

	result := ParseLocals(dir, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 locals, got %d", len(result))
	}
	if result["region"].AsString() != "us-west-2" {
		t.Errorf("expected region=us-west-2, got %v", result["region"])
	}
	if result["env"].AsString() != "prod" {
		t.Errorf("expected env=prod, got %v", result["env"])
	}
}

func TestParseLocals_InterDependencies(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
locals {
  base   = "my-app"
  prefix = "${local.base}-prod"
}
`)

	result := ParseLocals(dir, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 locals, got %d", len(result))
	}
	if result["prefix"].AsString() != "my-app-prod" {
		t.Errorf("expected prefix=my-app-prod, got %v", result["prefix"])
	}
}

func TestParseLocals_ReferencesVars(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
locals {
  create = var.create && var.putin_khuylo
}
`)
	writeTF(t, dir, "variables.tf", `
variable "create" {
  default = true
}
variable "putin_khuylo" {
  default = true
}
`)

	vars := ParseVariableDefaults(dir)
	evalCtx := BuildEvalContext(vars)

	result := ParseLocals(dir, evalCtx)
	if len(result) != 1 {
		t.Fatalf("expected 1 local, got %d", len(result))
	}
	if result["create"].True() != true {
		t.Errorf("expected create=true, got %v", result["create"])
	}
}

func TestParseLocals_Unresolvable(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "main.tf", `
locals {
  resolvable   = "hello"
  unresolvable = data.aws_ami.latest.id
}
`)

	result := ParseLocals(dir, nil)
	if len(result) != 1 {
		t.Fatalf("expected 1 resolved local, got %d", len(result))
	}
	if result["resolvable"].AsString() != "hello" {
		t.Errorf("expected resolvable=hello, got %v", result["resolvable"])
	}
	if _, ok := result["unresolvable"]; ok {
		t.Error("expected unresolvable to not be present")
	}
}

func TestParseLocals_NoTFFiles(t *testing.T) {
	dir := t.TempDir()
	result := ParseLocals(dir, nil)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

func TestParseLocals_MultipleBlocks(t *testing.T) {
	dir := t.TempDir()
	writeTF(t, dir, "locals1.tf", `
locals {
  a = "one"
}
`)
	writeTF(t, dir, "locals2.tf", `
locals {
  b = "two"
}
`)

	result := ParseLocals(dir, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 locals, got %d", len(result))
	}
}

func TestExtractVars_NonObjectType(t *testing.T) {
	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"var": cty.StringVal("not-an-object"),
		},
	}
	result := extractVars(ctx)
	if result != nil {
		t.Errorf("expected nil for non-object var, got %v", result)
	}
}

func TestExtractVars_NullVar(t *testing.T) {
	ctx := &hcl.EvalContext{
		Variables: map[string]cty.Value{
			"var": cty.NullVal(cty.DynamicPseudoType),
		},
	}
	result := extractVars(ctx)
	if result != nil {
		t.Errorf("expected nil for null var, got %v", result)
	}
}
