package cloudformation

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// mapTree is a project held in memory, keyed by slash path.
type mapTree map[string]string

func (m mapTree) Walk(target string) ([]string, error) {
	if target == "missing" {
		return nil, errors.New("no such directory")
	}
	if _, ok := m[target]; ok {
		return []string{target}, nil
	}
	var paths []string
	for p := range m {
		if target == "." || strings.HasPrefix(p, target+"/") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (m mapTree) Read(p string) ([]byte, error) {
	content, ok := m[p]
	if !ok {
		return nil, errors.New("not found")
	}
	return []byte(content), nil
}

const ec2Template = `AWSTemplateFormatVersion: "2010-09-09"
Parameters:
  InstanceType:
    Type: String
    Default: m5.large
Resources:
  Web:
    Type: AWS::EC2::Instance
    Properties:
      InstanceType: !Ref InstanceType
`

func paths(r Result) []string {
	var out []string
	for _, f := range r.Files {
		out = append(out, f.Path)
	}
	return out
}

func TestLooksLikeTemplate(t *testing.T) {
	cases := map[string]bool{
		ec2Template: true,
		"Resources:\n  Fn:\n    Type: AWS::Serverless::Function\n":                       true,
		`{"Resources": {"Bucket": {"Type": "AWS::S3::Bucket"}}}`:                         true,
		`{ "AWSTemplateFormatVersion": "2010-09-09" }`:                                   true,
		"apiVersion: v1\nkind: ConfigMap\nResources:\n  x:\n    Type: AWS::S3::Bucket\n": false,
		"name: ci\non: push\njobs: {}\n":                                                 false,
		"Resources:\n  thing:\n    Type: Custom::Thing\n":                                false,
	}
	for content, want := range cases {
		if got := LooksLikeTemplate([]byte(content)); got != want {
			t.Errorf("LooksLikeTemplate(%q) = %v, want %v", content, got, want)
		}
	}
}

func TestReferences(t *testing.T) {
	content := []byte(`Resources:
  Db:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: ./nested/db.yaml
  App:
    Type: AWS::Serverless::Application
    Properties:
      Location:
        ApplicationId: arn:aws:serverlessrepo:us-east-1:123456789012:applications/x
  Remote:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: https://bucket.s3.amazonaws.com/x.yaml
  Snippet:
    Fn::Transform: {Name: AWS::Include, Parameters: {Location: "snippets/tags.json"}}
  Computed:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: !Sub ${Bucket}/x.yaml
  Code:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: ./build/artifact.zip
`)
	got := References(content)
	want := []string{"./nested/db.yaml", "snippets/tags.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("References = %v, want %v", got, want)
	}
}

func TestCollect_FindsTemplatesAndSkipsTheRest(t *testing.T) {
	tree := mapTree{
		"stacks/app.yaml":               ec2Template,
		"stacks/notes.md":               "# notes",
		"stacks/config.yaml":            "key: value\n",
		".github/workflows/deploy.yaml": ec2Template,
		".aws-sam/build/template.yaml":  ec2Template,
		"node_modules/pkg/template.yml": ec2Template,
		"cdk.out/Stack.template.json":   ec2Template,
		"package.json":                  `{"name": "x"}`,
		"tsconfig.build.json":           `{}`,
		"serverless.yml":                ec2Template,
	}

	result, err := Collect(tree, []string{".", "stacks/app.yaml"})
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(paths(result), []string{"stacks/app.yaml"}) {
		t.Errorf("files = %v", paths(result))
	}
	if !reflect.DeepEqual(result.Templates, []string{"stacks/app.yaml"}) {
		t.Errorf("templates = %v", result.Templates)
	}
}

func TestCollect_AnExplicitFileIsReadEvenInAHiddenDirectory(t *testing.T) {
	tree := mapTree{".aws-sam/build/template.yaml": ec2Template}

	result, err := Collect(tree, []string{".aws-sam/build/template.yaml"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Templates) != 1 {
		t.Errorf("templates = %v, want the file named on the command line", result.Templates)
	}
}

func TestCollect_FollowsReferencesInsideTheProjectOnly(t *testing.T) {
	tree := mapTree{
		"stacks/root.yaml": `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  Db:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: ./nested/db.yaml
  Tags:
    Fn::Transform: {Name: AWS::Include, Parameters: {Location: ../shared/tags.yaml}}
  Gone:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: ./nested/missing.yaml
  Outside:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: ../../elsewhere.yaml
`,
		"stacks/nested/db.yaml": `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  Cache:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: ../../shared/cache.yaml
`,
		"shared/tags.yaml": "Key: team\n",
		"shared/cache.yaml": `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  Back:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: ../stacks/nested/db.yaml
`,
	}

	result, err := Collect(tree, []string{"stacks/root.yaml"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"shared/cache.yaml", "shared/tags.yaml", "stacks/nested/db.yaml", "stacks/root.yaml"}
	if !reflect.DeepEqual(paths(result), want) {
		t.Errorf("files = %v, want %v", paths(result), want)
	}
	if !reflect.DeepEqual(result.Templates, []string{"shared/cache.yaml", "stacks/nested/db.yaml", "stacks/root.yaml"}) {
		t.Errorf("templates = %v", result.Templates)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "outside the project") {
		t.Errorf("warnings = %v", result.Warnings)
	}
}

func TestCollect_AddsTheSamconfigBesideATemplate(t *testing.T) {
	tree := mapTree{
		"app/template.yaml":  ec2Template,
		"app/samconfig.toml": "version = 0.1\n",
		"app/samconfig.yaml": "version: 0.1\n",
	}

	result, err := Collect(tree, []string{"app"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"app/samconfig.toml", "app/samconfig.yaml", "app/template.yaml"}
	if !reflect.DeepEqual(paths(result), want) {
		t.Errorf("files = %v, want %v", paths(result), want)
	}
}

func TestCollect_SkipsOversizedFiles(t *testing.T) {
	big := ec2Template + strings.Repeat("#", MaxFileSize)
	tree := mapTree{
		"big.yaml":  big,
		"root.yaml": "AWSTemplateFormatVersion: x\nResources:\n  N:\n    Type: AWS::CloudFormation::Stack\n    Properties:\n      TemplateURL: big2.yaml\n",
		"big2.yaml": big,
	}

	result, err := Collect(tree, []string{"."})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(paths(result), []string{"root.yaml"}) || len(result.Warnings) != 2 {
		t.Errorf("files = %v, warnings = %v", paths(result), result.Warnings)
	}
}

func TestCollect_CapsTheNumberOfTemplates(t *testing.T) {
	tree := mapTree{}
	for i := 0; i <= MaxTemplates; i++ {
		tree[filepath.ToSlash(filepath.Join("stacks", strings.Repeat("a", i+1)+".yaml"))] = ec2Template
	}

	result, err := Collect(tree, []string{"stacks"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Templates) != MaxTemplates || len(result.Warnings) != 1 {
		t.Errorf("got %d templates and %d warnings", len(result.Templates), len(result.Warnings))
	}
}

func TestCollect_AWalkErrorStopsTheRun(t *testing.T) {
	if _, err := Collect(mapTree{}, []string{"missing"}); err == nil {
		t.Error("expected the walk error")
	}
}

func TestCollect_AnUnreadableCandidateIsSkipped(t *testing.T) {
	tree := failingReadTree{mapTree{"a.yaml": ec2Template}}

	result, err := Collect(tree, []string{"."})
	if err != nil || len(result.Files) != 0 {
		t.Errorf("files = %v, err = %v", result.Files, err)
	}
}

type failingReadTree struct{ mapTree }

func (failingReadTree) Read(string) ([]byte, error) { return nil, errors.New("denied") }

func TestDeclaredParameters(t *testing.T) {
	cases := map[string][]string{
		ec2Template: {"InstanceType"},
		`{"Parameters": {"Env": {"Type": "String"}, "Size": {"Type": "Number"}}}`: {"Env", "Size"},
		"Resources: {}\n":    nil,
		"Parameters: none\n": nil,
		"- a list\n":         nil,
		"key: [unclosed\n":   nil,
		"":                   nil,
	}
	for content, want := range cases {
		var got []string
		for name := range DeclaredParameters([]byte(content)) {
			got = append(got, name)
		}
		sort.Strings(got)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("DeclaredParameters(%q) = %v, want %v", content, got, want)
		}
	}
}

func TestDirTree_WalksRegularFilesAndSkipsHiddenDirectories(t *testing.T) {
	root := resolvedTempDir(t)
	writeFile(t, root, "stacks/app.yaml", ec2Template)
	writeFile(t, root, ".git/config", "x")
	writeFile(t, root, "stacks/.cache/x.yaml", ec2Template)
	if err := os.Symlink(filepath.Join(root, "stacks", "app.yaml"), filepath.Join(root, "stacks", "link.yaml")); err != nil {
		t.Fatal(err)
	}

	got, err := DirTree{Root: root}.Walk(".")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"stacks/app.yaml"}) {
		t.Errorf("Walk = %v", got)
	}

	if _, err := (DirTree{Root: root}).Walk("missing"); err == nil {
		t.Error("expected an error walking a missing directory")
	}
}

func TestDirTree_ReadRefusesToLeaveTheRoot(t *testing.T) {
	outside := resolvedTempDir(t)
	writeFile(t, outside, "secret.yaml", "token: x")
	root := resolvedTempDir(t)
	writeFile(t, root, "app.yaml", ec2Template)
	if err := os.Symlink(filepath.Join(outside, "secret.yaml"), filepath.Join(root, "escape.yaml")); err != nil {
		t.Fatal(err)
	}
	tree := DirTree{Root: root}

	if content, err := tree.Read("app.yaml"); err != nil || string(content) != ec2Template {
		t.Errorf("Read(app.yaml) = %q, %v", content, err)
	}
	if _, err := tree.Read("escape.yaml"); err == nil {
		t.Error("a symlink out of the root was read")
	}
	if _, err := tree.Read("missing.yaml"); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
