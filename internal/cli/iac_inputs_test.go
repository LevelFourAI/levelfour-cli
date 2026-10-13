package cli

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	cfn "github.com/LevelFourAI/levelfour-cli/internal/cloudformation"
	tf "github.com/LevelFourAI/levelfour-cli/internal/terraform"
)

const testToken = "l4_test_testkey123456789a"

// recordingAPI answers every analysis with one CloudFormation estimate and keeps
// the last request body.
func recordingAPI(t *testing.T) (*httptest.Server, *map[string]interface{}) {
	t.Helper()
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body = nil
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"resource_cost_estimates": []map[string]interface{}{{
					"resource_type": "AWS::EC2::Instance", "resource_name": "Web", "change_type": "added",
					"scope": "stacks/app.yaml", "new_monthly_cost": 70.0, "monthly_cost_difference": 70.0,
				}},
				"cost_summary": map[string]interface{}{"total_new_monthly": 70.0, "total_monthly_difference": 70.0, "estimable_count": 1, "total_count": 1},
				"diagnostics":  []map[string]interface{}{{"code": "parameter_defaulted", "message": "Env priced at its default"}},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv, &body
}

func writeAt(t *testing.T, root, rel, content string) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return full
}

func resolvedDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

const appTemplate = `AWSTemplateFormatVersion: "2010-09-09"
Parameters:
  InstanceType:
    Type: String
    Default: m5.large
Resources:
  Web:
    Type: AWS::EC2::Instance
    Properties:
      InstanceType: !Ref InstanceType
  Database:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: ./nested/db.yaml
`

const dbTemplate = `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  Main:
    Type: AWS::RDS::DBInstance
    Properties:
      DBInstanceClass: db.m5.large
`

func fieldList(body map[string]interface{}, key string) []map[string]interface{} {
	var out []map[string]interface{}
	items, _ := body[key].([]interface{})
	for _, item := range items {
		out = append(out, item.(map[string]interface{}))
	}
	return out
}

func filePaths(files interface{}) []string {
	var paths []string
	for _, f := range files.([]interface{}) {
		paths = append(paths, f.(map[string]interface{})["path"].(string))
	}
	sort.Strings(paths)
	return paths
}

func TestEstimate_PricesEachTerraformRootOnce(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	writeAt(t, dir, "main.tf", `module "net" { source = "./modules/net" }`)
	writeAt(t, dir, "modules/net/main.tf", `resource "aws_vpc" "main" { cidr_block = "10.0.0.0/16" }`)
	writeAt(t, dir, "envs/prod/main.tf", `resource "aws_instance" "web" { instance_type = "m5.large" }`)
	writeAt(t, dir, "envs/staging/main.tf", `resource "aws_instance" "web" { instance_type = "t3.micro" }`)

	if _, _, err := executeCommand(t, "estimate", dir, "--api", srv.URL, "--token", testToken); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, c := range fieldList(*body, "resource_changes") {
		got = append(got, c["scope"].(string)+" "+c["file"].(string)+" "+c["resource_type"].(string))
	}
	sort.Strings(got)
	want := []string{
		". modules/net/main.tf aws_vpc",
		"envs/prod envs/prod/main.tf aws_instance",
		"envs/staging envs/staging/main.tf aws_instance",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("changes = %v, want %v", got, want)
	}
	if _, ok := (*body)["region"]; ok {
		t.Errorf("region sent without one being given: %v", (*body)["region"])
	}
}

func TestEstimate_SendsCloudFormationTemplates(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	writeAt(t, dir, "stacks/app.yaml", appTemplate)
	writeAt(t, dir, "stacks/nested/db.yaml", dbTemplate)
	writeAt(t, dir, "stacks/samconfig.toml", "version = 0.1\n")
	writeAt(t, dir, ".github/workflows/deploy.yaml", appTemplate)
	writeAt(t, dir, ".levelfour/config.yml", "excluded_resource_types: [\"AWS::Lambda::*\"]\n")
	chdir(t, dir)

	out, _, err := executeCommand(t, "estimate", "--api", srv.URL, "--token", testToken,
		"--parameter", "InstanceType=m6i.large", "--parameter", "Unused=x", "--config-env", "prod")
	if err != nil {
		t.Fatal(err)
	}

	block := (*body)["cloudformation"].(map[string]interface{})
	files := block["files"].(map[string]interface{})
	if got := filePaths(files["head"]); !reflect.DeepEqual(got, []string{"stacks/app.yaml", "stacks/nested/db.yaml", "stacks/samconfig.toml"}) {
		t.Errorf("head files = %v", got)
	}
	if base := files["base"].([]interface{}); len(base) != 0 {
		t.Errorf("estimate sent a base: %v", base)
	}
	wantParams := map[string]interface{}{"stacks/app.yaml": map[string]interface{}{"InstanceType": "m6i.large"}}
	if !reflect.DeepEqual(block["parameters"], wantParams) {
		t.Errorf("parameters = %v", block["parameters"])
	}
	if block["samconfig_env"] != "prod" || !reflect.DeepEqual(block["excluded_resource_types"], []interface{}{"AWS::Lambda::*"}) {
		t.Errorf("block = %v", block)
	}
	got := out.String()
	for _, want := range []string{"Found 2 CloudFormation templates", "AWS::EC2::Instance Web", "Env priced at its default"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestEstimate_UploadsTheParametersFile(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	writeAt(t, dir, "app.yaml", appTemplate)
	writeAt(t, dir, "nested/db.yaml", dbTemplate)
	writeAt(t, dir, "params/prod.json", `[{"ParameterKey": "InstanceType", "ParameterValue": "c6i.large"}]`)

	_, _, err := executeCommand(t, "estimate", dir, "--api", srv.URL, "--token", testToken,
		"--parameters-file", filepath.Join(dir, "params", "prod.json"))
	if err != nil {
		t.Fatal(err)
	}

	block := (*body)["cloudformation"].(map[string]interface{})
	head := filePaths(block["files"].(map[string]interface{})["head"])
	if !reflect.DeepEqual(head, []string{"app.yaml", "nested/db.yaml", "params/prod.json"}) {
		t.Errorf("head files = %v", head)
	}
	want := map[string]interface{}{"app.yaml": "params/prod.json", "nested/db.yaml": "params/prod.json"}
	if !reflect.DeepEqual(block["parameter_files"], want) {
		t.Errorf("parameter_files = %v", block["parameter_files"])
	}
}

func TestEstimate_RefusesBadCloudFormationInput(t *testing.T) {
	dir := resolvedDir(t)
	writeAt(t, dir, "app.yaml", appTemplate)
	writeAt(t, dir, "notes.yaml", "title: notes\n")
	writeAt(t, dir, "readme.txt", "hello")
	writeAt(t, dir, "big.json", strings.Repeat("x", cfn.MaxFileSize+1))
	outside := writeAt(t, resolvedDir(t), "params.json", "[]")

	cases := map[string][]string{
		"neither a Terraform file":    {filepath.Join(dir, "notes.yaml")},
		"neither a Terraform file ":   {filepath.Join(dir, "readme.txt")},
		"want Key=Value":              {dir, "--parameter", "NoEquals"},
		"outside the project root":    {dir, "--parameters-file", outside},
		"cannot read parameters file": {dir, "--parameters-file", filepath.Join(dir, "missing.json")},
		"exceeds the 1MB limit":       {dir, "--parameters-file", filepath.Join(dir, "big.json")},
	}
	for want, args := range cases {
		args = append([]string{"estimate"}, append(args, "--api", "http://localhost:9999", "--token", testToken)...)
		_, _, err := executeCommand(t, args...)
		if err == nil || !strings.Contains(err.Error(), strings.TrimSpace(want)) {
			t.Errorf("%v: err = %v, want %q", args, err, want)
		}
	}
}

func TestEstimate_AnExplicitTemplateIsPricedAlone(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	writeAt(t, dir, "app.yaml", appTemplate)
	writeAt(t, dir, "nested/db.yaml", dbTemplate)
	writeAt(t, dir, "other.yaml", dbTemplate)

	if _, _, err := executeCommand(t, "estimate", filepath.Join(dir, "app.yaml"), "--api", srv.URL, "--token", testToken); err != nil {
		t.Fatal(err)
	}

	head := filePaths((*body)["cloudformation"].(map[string]interface{})["files"].(map[string]interface{})["head"])
	if !reflect.DeepEqual(head, []string{"app.yaml", "nested/db.yaml"}) {
		t.Errorf("head files = %v", head)
	}
}

func TestEstimate_SkipsTemplatesOutsideTheProject(t *testing.T) {
	srv, _ := recordingAPI(t)
	repo := resolvedDir(t)
	initGitRepo(t, repo)
	writeAt(t, repo, "main.tf", basicTF)
	elsewhere := resolvedDir(t)
	writeAt(t, elsewhere, "app.yaml", appTemplate)

	out, errOut, err := executeCommand(t, "estimate", repo, elsewhere, "--api", srv.URL, "--token", testToken)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String()+errOut.String(), "outside the project root") {
		t.Errorf("no warning for a template outside the project:\n%s%s", out.String(), errOut.String())
	}
}

func commitAll(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
}

func TestDiff_ReportsARemovedResourceInGitMode(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	writeAt(t, dir, "main.tf", basicTF+`resource "aws_ebs_volume" "data" { size = 20 }`)
	commitAll(t, dir)
	writeAt(t, dir, "main.tf", basicTF)
	chdir(t, dir)

	if _, _, err := executeCommand(t, "diff", ".", "--api", srv.URL, "--token", testToken); err != nil {
		t.Fatal(err)
	}

	changes := fieldList(*body, "resource_changes")
	if len(changes) != 1 || changes[0]["change_type"] != "removed" || changes[0]["resource_type"] != "aws_ebs_volume" {
		t.Fatalf("changes = %v, want aws_ebs_volume removed", changes)
	}
	if changes[0]["file"] != "main.tf" || changes[0]["scope"] != "." {
		t.Errorf("file and scope = %v %v", changes[0]["file"], changes[0]["scope"])
	}
}

func TestDiff_AFileTargetLeavesTheOtherFilesAlone(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	writeAt(t, dir, "a.tf", `resource "aws_instance" "a" { instance_type = "t3.small" }`)
	writeAt(t, dir, "b.tf", `resource "aws_instance" "b" { instance_type = "t3.small" }`)
	commitAll(t, dir)
	writeAt(t, dir, "a.tf", `resource "aws_instance" "a" { instance_type = "t3.large" }`)
	writeAt(t, dir, "c.tf", `resource "aws_instance" "c" {}`)
	chdir(t, dir)

	if _, _, err := executeCommand(t, "diff", "a.tf", "c.tf", "--api", srv.URL, "--token", testToken); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, c := range fieldList(*body, "resource_changes") {
		got = append(got, c["resource_name"].(string)+" "+c["change_type"].(string))
	}
	sort.Strings(got)
	if !reflect.DeepEqual(got, []string{"a modified", "c added"}) {
		t.Errorf("changes = %v", got)
	}
}

func TestDiff_ANewDirectoryReadsAsAdded(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	writeAt(t, dir, "infra/main.tf", basicTF)
	commitAll(t, dir)
	writeAt(t, dir, "new/main.tf", `resource "aws_s3_bucket" "logs" {}`)
	chdir(t, dir)

	if _, _, err := executeCommand(t, "diff", "infra", "new", "--api", srv.URL, "--token", testToken); err != nil {
		t.Fatal(err)
	}

	changes := fieldList(*body, "resource_changes")
	if len(changes) != 1 || changes[0]["resource_name"] != "logs" || changes[0]["change_type"] != "added" {
		t.Errorf("changes = %v", changes)
	}
}

func TestDiff_SendsCloudFormationAtBaseAndHead(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	writeAt(t, dir, "stacks/app.yaml", appTemplate)
	writeAt(t, dir, "stacks/nested/db.yaml", dbTemplate)
	writeAt(t, dir, "stacks/gone.yaml", dbTemplate)
	commitAll(t, dir)
	writeAt(t, dir, "stacks/app.yaml", strings.Replace(appTemplate, "m5.large", "m6i.large", 1))
	writeAt(t, dir, "stacks/new.yaml", dbTemplate)
	os.Remove(filepath.Join(dir, "stacks", "gone.yaml"))
	chdir(t, dir)

	if _, _, err := executeCommand(t, "diff", "--api", srv.URL, "--token", testToken); err != nil {
		t.Fatal(err)
	}

	files := (*body)["cloudformation"].(map[string]interface{})["files"].(map[string]interface{})
	if got := filePaths(files["base"]); !reflect.DeepEqual(got, []string{"stacks/app.yaml", "stacks/gone.yaml", "stacks/nested/db.yaml"}) {
		t.Errorf("base files = %v", got)
	}
	if got := filePaths(files["head"]); !reflect.DeepEqual(got, []string{"stacks/app.yaml", "stacks/nested/db.yaml", "stacks/new.yaml"}) {
		t.Errorf("head files = %v", got)
	}
	for _, f := range files["base"].([]interface{}) {
		file := f.(map[string]interface{})
		if file["path"] == "stacks/app.yaml" && !strings.Contains(file["content"].(string), "m5.large") {
			t.Errorf("base app.yaml is not the committed version: %v", file["content"])
		}
	}
}

func TestDiff_CloudFormationAgainstASnapshotIsPricedAsAdded(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	writeAt(t, dir, "app.yaml", dbTemplate)
	baseline := writeAt(t, resolvedDir(t), "baseline.json", "[]")

	if _, _, err := executeCommand(t, "diff", baseline, dir, "--api", srv.URL, "--token", testToken); err != nil {
		t.Fatal(err)
	}

	files := (*body)["cloudformation"].(map[string]interface{})["files"].(map[string]interface{})
	if len(files["base"].([]interface{})) != 0 || len(files["head"].([]interface{})) != 1 {
		t.Errorf("files = %v", files)
	}
}

func TestDiff_AFailedBaseReadIsAnError(t *testing.T) {
	srv, _ := recordingAPI(t)
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	writeAt(t, dir, "app.yaml", dbTemplate)
	commitAll(t, dir)
	orig := baseTree
	baseTree = func(string, string) cfn.Tree { return failingTree{} }
	defer func() { baseTree = orig }()

	_, _, err := executeCommand(t, "diff", dir, "--api", srv.URL, "--token", testToken)
	if err == nil || !strings.Contains(err.Error(), "failed to read CloudFormation templates at") {
		t.Errorf("err = %v", err)
	}
}

type failingTree struct{}

func (failingTree) Walk(string) ([]string, error) { return nil, errors.New("unreadable") }
func (failingTree) Read(string) ([]byte, error)   { return nil, errors.New("unreadable") }
func (failingTree) Tracking(string) cfn.Tracking  { return cfn.Tracked }

func TestDiff_AFailedTerraformBaselineReadsAsAdded(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	writeAt(t, dir, "main.tf", basicTF)
	commitAll(t, dir)
	orig := osMkdirTemp
	osMkdirTemp = func(string, string) (string, error) { return "", errors.New("disk full") }
	defer func() { osMkdirTemp = orig }()

	out, _, err := executeCommand(t, "diff", dir, "--api", srv.URL, "--token", testToken)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Git baseline unavailable (disk full)") {
		t.Errorf("output:\n%s", out.String())
	}
	if changes := fieldList(*body, "resource_changes"); len(changes) != 1 || changes[0]["change_type"] != "added" {
		t.Errorf("changes = %v", changes)
	}
}

func TestGitBaseline_FailuresAreReturned(t *testing.T) {
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	writeAt(t, dir, "main.tf", basicTF)
	commitAll(t, dir)
	sha, err := resolveBaseCommit(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	fileTarget := []iacTarget{{arg: "main.tf", abs: filepath.Join(dir, "main.tf"), rel: "main.tf"}}

	origChdir := osChdir
	osChdir = func(string) error { return errors.New("chdir denied") }
	if _, err := gitBaseline(dir, sha, fileTarget, tf.ParseOptions{}); err == nil {
		t.Error("expected the chdir error")
	}
	osChdir = origChdir

	origExtract := extractGitRef
	extractGitRef = func(string, string, string) (string, error) { return "", errors.New("archive failed") }
	if _, err := gitBaseline(dir, sha, fileTarget, tf.ParseOptions{}); err == nil {
		t.Error("expected the extract error")
	}
	extractGitRef = origExtract

	origParse := parseTerraformFiles
	parseTerraformFiles = func([]string, tf.ParseOptions) ([]api.ResourceSnapshot, []string, error) {
		return nil, nil, errors.New("parse failed")
	}
	if _, err := gitBaseline(dir, sha, fileTarget, tf.ParseOptions{}); err == nil {
		t.Error("expected the parse error")
	}
	if _, _, err := collectTerraform(dir, fileTarget, tf.ParseOptions{}); err == nil {
		t.Error("expected the head parse error")
	}
	parseTerraformFiles = origParse
}

func TestCollectTerraform_AnUnreadableDirectoryIsAnError(t *testing.T) {
	dir := resolvedDir(t)
	locked := filepath.Join(dir, "locked")
	writeAt(t, dir, "locked/main.tf", basicTF)
	os.Chmod(locked, 0o000)
	defer os.Chmod(locked, 0o755)

	_, _, err := collectTerraform(dir, []iacTarget{{arg: dir, abs: dir, rel: ".", isDir: true}}, tf.ParseOptions{})
	if err == nil || !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("err = %v", err)
	}
}

func TestResolveTargets_AnAbsoluteMakeFailureIsAnError(t *testing.T) {
	orig := filepathAbs
	filepathAbs = func(string) (string, error) { return "", errors.New("no cwd") }
	defer func() { filepathAbs = orig }()

	if _, _, err := resolveTargets([]string{"."}); err == nil {
		t.Error("expected the error")
	}
}

func TestGitTree(t *testing.T) {
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	writeAt(t, dir, "stacks/app.yaml", dbTemplate)
	os.Symlink("app.yaml", filepath.Join(dir, "stacks", "link.yaml"))
	commitAll(t, dir)
	sha, _ := resolveBaseCommit(dir, "")
	tree := gitTree{repoRoot: dir, sha: sha}

	paths, err := tree.Walk(".")
	if err != nil || !reflect.DeepEqual(paths, []string{"stacks/app.yaml"}) {
		t.Errorf("Walk = %v, %v", paths, err)
	}
	if content, err := tree.Read("stacks/app.yaml"); err != nil || string(content) != dbTemplate {
		t.Errorf("Read = %q, %v", content, err)
	}
	if _, err := (gitTree{repoRoot: dir, sha: "0000000"}).Walk("."); err == nil {
		t.Error("expected an error for an unknown commit")
	}
}

func TestDetectProviderRegions(t *testing.T) {
	dir := resolvedDir(t)
	noProvider := filepath.Join(dir, "plain")
	writeAt(t, dir, "plain/main.tf", basicTF)
	file := writeAt(t, dir, "eu/main.tf", providerTF+basicTF)
	yamlFile := writeAt(t, dir, "app.yaml", dbTemplate)

	got := detectProviderRegions([]iacTarget{{abs: noProvider, isDir: true}, {abs: file}}, tf.ParseOptions{})
	if got.AWS != "eu-west-1" {
		t.Errorf("region = %q, want the file's provider region", got.AWS)
	}
	if got := detectProviderRegions([]iacTarget{{abs: yamlFile}}, tf.ParseOptions{}); got != (tf.ProviderRegions{}) {
		t.Errorf("a template has no provider block, got %+v", got)
	}
}

func TestPricingRegion(t *testing.T) {
	override := "sa-east-1"
	cases := []struct {
		flag     string
		changed  bool
		override *string
		detected string
		want     string
	}{
		{"eu-west-2", true, &override, "us-west-2", "eu-west-2"},
		{"", false, &override, "us-west-2", "sa-east-1"},
		{"", false, nil, "us-west-2", "us-west-2"},
		{"", false, nil, "", ""},
	}
	for _, c := range cases {
		if got := pricingRegion(c.flag, c.changed, c.override, c.detected); got != c.want {
			t.Errorf("pricingRegion(%+v) = %q, want %q", c, got, c.want)
		}
	}
}

func TestBuildCloudFormationInput(t *testing.T) {
	if input, err := buildCloudFormationInput(".", cfn.Result{}, cfn.Result{}, cfnFlags{}, nil); input != nil || err != nil {
		t.Errorf("no templates gave %v, %v", input, err)
	}

	base := cfn.Result{Files: []api.TemplateFile{{Path: "gone.yaml", Content: appTemplate}}, Templates: []string{"gone.yaml"}}
	input, err := buildCloudFormationInput(".", cfn.Result{}, base, cfnFlags{parameters: []string{"InstanceType=x"}, region: "sa-east-1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Files.Head) != 0 || input.Files.Head == nil {
		t.Errorf("head should be an empty list, got %#v", input.Files.Head)
	}
	if input.Region != "sa-east-1" {
		t.Errorf("region = %q, want the given one", input.Region)
	}
	if input.Parameters["gone.yaml"]["InstanceType"] != "x" {
		t.Errorf("a template only at base still gets its parameters: %v", input.Parameters)
	}

	none, _ := buildCloudFormationInput(".", base, cfn.Result{}, cfnFlags{parameters: []string{"Missing=x"}}, nil)
	if none.Parameters != nil {
		t.Errorf("an undeclared key went somewhere: %v", none.Parameters)
	}
}

func TestWithFileKeepsOneCopyPerPath(t *testing.T) {
	files := []api.TemplateFile{{Path: "params.json"}}
	if got := withFile(files, api.TemplateFile{Path: "params.json"}); len(got) != 1 {
		t.Errorf("withFile = %v", got)
	}
}

func TestReadParametersFile_AnUnreadableFileIsAnError(t *testing.T) {
	dir := resolvedDir(t)
	locked := writeAt(t, dir, "params.json", "[]")
	os.Chmod(locked, 0o000)
	defer os.Chmod(locked, 0o644)

	if _, err := readParametersFile(dir, locked); err == nil {
		t.Error("expected a read error")
	}

	orig := filepathAbs
	filepathAbs = func(string) (string, error) { return "", errors.New("no cwd") }
	defer func() { filepathAbs = orig }()
	if _, err := readParametersFile(dir, "params.json"); err == nil {
		t.Error("expected the abs error")
	}
}

func TestCheckFileTargets_AnUnreadableTemplateIsRefused(t *testing.T) {
	dir := resolvedDir(t)
	locked := writeAt(t, dir, "app.yaml", appTemplate)
	os.Chmod(locked, 0o000)
	defer os.Chmod(locked, 0o644)

	if err := checkFileTargets([]iacTarget{{arg: "app.yaml", abs: locked}}); err == nil {
		t.Error("expected the file to be refused")
	}
}

func TestLoadSnapshotFile(t *testing.T) {
	dir := resolvedDir(t)
	cases := map[string]bool{
		writeAt(t, dir, "snap.json", `[{"type": "aws_instance", "name": "web", "attributes": {}}]`): true,
		writeAt(t, dir, "env.json", `{"head_resources": []}`):                                       true,
		writeAt(t, dir, "template.json", `{"Resources": {"B": {"Type": "AWS::S3::Bucket"}}}`):       false,
		writeAt(t, dir, "snap.yaml", "[]"):                                                          false,
		filepath.Join(dir, "missing.json"):                                                          false,
	}
	for p, want := range cases {
		if _, got := loadSnapshotFile(p); got != want {
			t.Errorf("loadSnapshotFile(%s) = %v, want %v", filepath.Base(p), got, want)
		}
	}
}

func TestReadAnalysisInputs_AFailedTemplateReadIsAnError(t *testing.T) {
	dir := resolvedDir(t)
	writeAt(t, dir, "app.yaml", dbTemplate)
	orig := collectTemplates
	collectTemplates = func(cfn.Tree, []string) (cfn.Result, error) { return cfn.Result{}, errors.New("unreadable") }
	defer func() { collectTemplates = orig }()

	_, _, err := executeCommand(t, "estimate", dir, "--api", "http://localhost:9999", "--token", testToken)
	if err == nil || !strings.Contains(err.Error(), "failed to read CloudFormation templates") {
		t.Errorf("err = %v", err)
	}
}

func TestEstimate_AnUnreadableTerraformDirectoryIsAnError(t *testing.T) {
	dir := resolvedDir(t)
	writeAt(t, dir, "main.tf", basicTF)
	locked := filepath.Join(dir, "locked")
	writeAt(t, dir, "locked/main.tf", basicTF)
	os.Chmod(locked, 0o000)
	defer os.Chmod(locked, 0o755)

	_, _, err := executeCommand(t, "estimate", dir, "--api", "http://localhost:9999", "--token", testToken)
	if err == nil || !strings.Contains(err.Error(), "failed to parse Terraform files") {
		t.Errorf("err = %v", err)
	}
}

func TestEstimate_TerraformAndCloudFormationTogether(t *testing.T) {
	srv, _ := recordingAPI(t)
	dir := resolvedDir(t)
	writeAt(t, dir, "main.tf", basicTF)
	writeAt(t, dir, "app.yaml", dbTemplate)

	out, _, err := executeCommand(t, "estimate", dir, "--api", srv.URL, "--token", testToken)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Found 1 resources and 1 CloudFormation templates") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestDiff_ParametersFileGoesToBothSides(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	writeAt(t, dir, "app.yaml", appTemplate)
	writeAt(t, dir, "nested/db.yaml", dbTemplate)
	writeAt(t, dir, "params.json", `{"Parameters": {"InstanceType": "m6i.large"}}`)
	commitAll(t, dir)
	chdir(t, dir)

	if _, _, err := executeCommand(t, "diff", "app.yaml", "--parameters-file", "params.json", "--api", srv.URL, "--token", testToken); err != nil {
		t.Fatal(err)
	}

	files := (*body)["cloudformation"].(map[string]interface{})["files"].(map[string]interface{})
	for _, side := range []string{"base", "head"} {
		if got := filePaths(files[side]); !reflect.DeepEqual(got, []string{"app.yaml", "nested/db.yaml", "params.json"}) {
			t.Errorf("%s files = %v", side, got)
		}
	}
}

func TestDiff_RefusesABadParameterFlag(t *testing.T) {
	dir := resolvedDir(t)
	writeAt(t, dir, "app.yaml", appTemplate)

	_, _, err := executeCommand(t, "diff", dir, "--parameter", "=x", "--api", "http://localhost:9999", "--token", testToken)
	if err == nil || !strings.Contains(err.Error(), "want Key=Value") {
		t.Errorf("err = %v", err)
	}
}

func TestDiff_AClientErrorStopsTheRun(t *testing.T) {
	dir := resolvedDir(t)
	writeAt(t, dir, "app.yaml", appTemplate)
	orig := newSDKClientFn
	newSDKClientFn = func() (*api.SDKClient, error) { return nil, errors.New("no credential") }
	defer func() { newSDKClientFn = orig }()

	_, _, err := executeCommand(t, "diff", dir)
	if err == nil || !strings.Contains(err.Error(), "no credential") {
		t.Errorf("err = %v", err)
	}
}

func TestDiff_WithoutMainOrMasterEverythingIsAdded(t *testing.T) {
	srv, _ := recordingAPI(t)
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	gitRun(t, dir, "checkout", "-b", "feature")
	writeAt(t, dir, "main.tf", basicTF)
	commitAll(t, dir)

	out, _, err := executeCommand(t, "diff", dir, "--api", srv.URL, "--token", testToken)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "no main or master branch found") {
		t.Errorf("output:\n%s", out.String())
	}
}

func TestDiffSnapshots_AnUnscopedBaselineStillMatches(t *testing.T) {
	baseline := []api.ResourceSnapshot{{Type: "aws_instance", Name: "web", Attributes: map[string]interface{}{"instance_type": "t3.small"}}}
	head := []api.ResourceSnapshot{{Type: "aws_instance", Name: "web", Scope: ".", File: "main.tf", Attributes: map[string]interface{}{"instance_type": "t3.large"}}}

	changes := diffSnapshots(baseline, head)

	if len(changes) != 1 || changes[0].ChangeType != "modified" || changes[0].File != "main.tf" {
		t.Errorf("changes = %+v", changes)
	}
}

func TestResolvedPathKeepsAPathItCannotResolve(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if got := resolvedPath(missing); got != missing {
		t.Errorf("resolvedPath = %q, want %q", got, missing)
	}
}

func TestEstimate_DoesNotUploadAFileGitIgnoresOrDoesNotTrack(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	writeAt(t, dir, ".gitignore", "credentials.json\n")
	writeAt(t, dir, "app.yaml", `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  Db:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: nested/db.yaml
  Local:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: nested/local-only.yaml
  Creds:
    Fn::Transform: {Name: AWS::Include, Parameters: {Location: credentials.json}}
`)
	writeAt(t, dir, "nested/db.yaml", dbTemplate)
	commitAll(t, dir)
	writeAt(t, dir, "credentials.json", `{"aws_secret_access_key": "x"}`)
	writeAt(t, dir, "nested/local-only.yaml", dbTemplate)

	_, errOut, err := executeCommand(t, "estimate", filepath.Join(dir, "app.yaml"), "--api", srv.URL, "--token", testToken)
	if err != nil {
		t.Fatal(err)
	}

	head := filePaths((*body)["cloudformation"].(map[string]interface{})["files"].(map[string]interface{})["head"])
	if !reflect.DeepEqual(head, []string{"app.yaml", "nested/db.yaml"}) {
		t.Errorf("head files = %v", head)
	}
	stderr := errOut.String()
	for _, want := range []string{
		"credentials.json: not tracked by git",
		"nested/local-only.yaml: not tracked by git",
		"Uploading 2 files to the LevelFour API:\n  app.yaml\n  nested/db.yaml\n",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}
}

func TestEstimate_QuietDoesNotListTheUpload(t *testing.T) {
	srv, _ := recordingAPI(t)
	dir := resolvedDir(t)
	writeAt(t, dir, "app.yaml", dbTemplate)

	_, errOut, err := executeCommand(t, "estimate", dir, "--quiet", "--api", srv.URL, "--token", testToken)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(errOut.String(), "Uploading") {
		t.Errorf("--quiet still listed the upload:\n%s", errOut.String())
	}
}

func TestProjectTree_TrackingTakesThePathLiterally(t *testing.T) {
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	writeAt(t, dir, "a.yaml", dbTemplate)
	commitAll(t, dir)
	tree := projectTree{DirTree: cfn.DirTree{Root: dir}, inRepo: true}

	if got := tree.Tracking("a.yaml"); got != cfn.Tracked {
		t.Errorf("a.yaml = %v, want tracked", got)
	}
	if got := tree.Tracking("*.yaml"); got != cfn.Untracked {
		t.Errorf("*.yaml = %v, want untracked: a pattern must not match a.yaml", got)
	}
	if got := (projectTree{DirTree: cfn.DirTree{Root: dir}}).Tracking("a.yaml"); got != cfn.NoRepository {
		t.Errorf("outside a repository = %v", got)
	}
}

func TestEstimate_ATrackedSymlinkNeverUploadsItsTarget(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	initGitRepo(t, dir)
	writeAt(t, dir, ".gitignore", "ignored.json\n")
	writeAt(t, dir, ".secrets/creds.json", `{"password": "hidden"}`)
	writeAt(t, dir, "ignored.json", `{"password": "ignored"}`)
	writeAt(t, dir, "app.yaml", `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  Readme:
    Fn::Transform: {Name: AWS::Include, Parameters: {Location: docs/readme.yaml}}
  Notes:
    Fn::Transform: {Name: AWS::Include, Parameters: {Location: docs/notes.json}}
`)
	os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	os.Symlink("../.secrets/creds.json", filepath.Join(dir, "docs", "readme.yaml"))
	os.Symlink("../ignored.json", filepath.Join(dir, "docs", "notes.json"))
	commitAll(t, dir)

	_, errOut, err := executeCommand(t, "estimate", filepath.Join(dir, "app.yaml"), "--api", srv.URL, "--token", testToken)
	if err != nil {
		t.Fatal(err)
	}

	head := filePaths((*body)["cloudformation"].(map[string]interface{})["files"].(map[string]interface{})["head"])
	if !reflect.DeepEqual(head, []string{"app.yaml"}) {
		t.Errorf("head files = %v", head)
	}
	for _, p := range []string{"docs/readme.yaml", "docs/notes.json"} {
		if !strings.Contains(errOut.String(), "Skipping "+p+": a symlink") {
			t.Errorf("no symlink warning for %s:\n%s", p, errOut.String())
		}
	}
}

func TestEstimate_RefusesASymlinkedParametersFile(t *testing.T) {
	dir := resolvedDir(t)
	writeAt(t, dir, "app.yaml", appTemplate)
	writeAt(t, dir, ".secrets/prod.json", "[]")
	os.Symlink(".secrets/prod.json", filepath.Join(dir, "params.json"))

	_, _, err := executeCommand(t, "estimate", dir, "--parameters-file", filepath.Join(dir, "params.json"),
		"--api", "http://localhost:9999", "--token", testToken)
	if err == nil || !strings.Contains(err.Error(), "a symlink") {
		t.Errorf("err = %v, want the symlink refused", err)
	}
}

func TestEstimate_SendsTheGivenRegionForTemplatesToo(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	writeAt(t, dir, "app.yaml", dbTemplate)
	writeAt(t, dir, "main.tf", providerTF+basicTF)

	if _, _, err := executeCommand(t, "estimate", dir, "--region", "eu-west-2", "--api", srv.URL, "--token", testToken); err != nil {
		t.Fatal(err)
	}

	block := (*body)["cloudformation"].(map[string]interface{})
	if (*body)["region"] != "eu-west-2" || block["region"] != "eu-west-2" {
		t.Errorf("region = %v, cloudformation.region = %v; want eu-west-2 in both", (*body)["region"], block["region"])
	}
}

func TestEstimate_WithoutRegionTemplatesGetNone(t *testing.T) {
	srv, body := recordingAPI(t)
	dir := resolvedDir(t)
	writeAt(t, dir, "app.yaml", dbTemplate)
	writeAt(t, dir, "main.tf", providerTF+basicTF)

	if _, _, err := executeCommand(t, "estimate", dir, "--api", srv.URL, "--token", testToken); err != nil {
		t.Fatal(err)
	}

	block := (*body)["cloudformation"].(map[string]interface{})
	if _, ok := block["region"]; ok {
		t.Errorf("cloudformation.region = %v, want none so samconfig applies", block["region"])
	}
	if (*body)["region"] != "eu-west-1" {
		t.Errorf("region = %v, want the provider block's eu-west-1 for Terraform", (*body)["region"])
	}
}
