// Package cloudformation finds the CloudFormation templates `l4 estimate` and
// `l4 diff` send to the LevelFour API, together with the files those templates
// need: nested stack templates, included snippets and samconfig files.
//
// Nothing here evaluates a template. Paths are slash-separated and relative to
// the project root, and nothing outside that root is ever read.
package cloudformation

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"gopkg.in/yaml.v3"
)

const (
	// MaxTemplates caps how many templates one run uploads.
	MaxTemplates = 200
	// MaxFileSize caps the size of any one uploaded file.
	MaxFileSize = 1 << 20
)

// Tracking says whether version control holds a file.
type Tracking int

const (
	// Untracked: the project is a git repository that does not track the file
	// (an ignored file is never tracked).
	Untracked Tracking = iota
	Tracked
	// NoRepository: the project is not a git repository.
	NoRepository
)

// Tree reads a project by slash-separated paths relative to its root.
type Tree interface {
	// Walk lists every file under target: a directory, recursively, or the file itself.
	Walk(target string) ([]string, error)
	Read(path string) ([]byte, error)
	Tracking(path string) Tracking
}

// Result is what Collect found.
type Result struct {
	// Files holds every file to upload, sorted by path.
	Files []api.TemplateFile
	// Templates lists the paths among Files that are templates.
	Templates []string
	Warnings  []string
}

var (
	formatVersion = regexp.MustCompile(`(?m)(^AWSTemplateFormatVersion[ \t]*:|"AWSTemplateFormatVersion"\s*:)`)
	topResources  = regexp.MustCompile(`(?m)(^Resources[ \t]*:|"Resources"\s*:)`)
	awsType       = regexp.MustCompile(`"?Type"?[ \t]*:[ \t]*["']?AWS::`)
	k8sAPIVersion = regexp.MustCompile(`(?m)^apiVersion[ \t]*:`)
	k8sKind       = regexp.MustCompile(`(?m)^kind[ \t]*:`)
	// A literal TemplateURL or Location on the same line: nested stacks,
	// AWS::Serverless::Application and AWS::Include.
	reference = regexp.MustCompile(`["']?(?:TemplateURL|Location)["']?[ \t]*:[ \t]*["']?([^\s"'{}\[\],#!]+)`)
)

var candidateExtensions = map[string]bool{".yaml": true, ".yml": true, ".json": true, ".template": true}

var samconfigNames = []string{"samconfig.toml", "samconfig.yaml", "samconfig.yml"}

// LooksLikeTemplate is a cheap content check: a template declares
// AWSTemplateFormatVersion, or has a top-level Resources section with AWS types.
func LooksLikeTemplate(content []byte) bool {
	if k8sAPIVersion.Match(content) && k8sKind.Match(content) {
		return false
	}
	return formatVersion.Match(content) || (topResources.Match(content) && awsType.Match(content))
}

// IsCandidatePath reports whether a file may be a template by its path alone.
func IsCandidatePath(p string) bool {
	return candidateExtensions[path.Ext(p)]
}

// SkipDir reports directories a template is never found under: hidden ones
// (version control, CI and build output such as .aws-sam), vendored packages and
// CDK output.
func SkipDir(name string) bool {
	return name == "node_modules" || name == "cdk.out" || (strings.HasPrefix(name, ".") && name != "." && name != "..")
}

// excludedPath reports hidden files, paths under a skipped directory, and
// package or Serverless Framework manifests. A hidden file such as .env.json
// is where secrets live, so it is never read for upload.
func excludedPath(p string) bool {
	for _, segment := range strings.Split(p, "/") {
		if SkipDir(segment) {
			return true
		}
	}
	name := path.Base(p)
	switch {
	case name == "package.json", name == "package-lock.json", strings.HasPrefix(name, "tsconfig"):
		return true
	case name == "serverless.yml", name == "serverless.yaml":
		return true
	}
	return false
}

// Collect finds the templates under targets (directories, walked recursively,
// or single files named by the user) and every local file they reference.
func Collect(tree Tree, targets []string) (Result, error) {
	c := collector{tree: tree, contents: make(map[string][]byte), rejected: make(map[string]bool)}
	for _, target := range targets {
		if err := c.walk(target); err != nil {
			return Result{}, err
		}
	}
	c.followReferences()
	c.addSamconfigs()
	sort.Slice(c.files, func(i, j int) bool { return c.files[i].Path < c.files[j].Path })
	sort.Strings(c.templates)
	return Result{Files: c.files, Templates: c.templates, Warnings: c.warnings}, nil
}

type collector struct {
	tree      Tree
	contents  map[string][]byte
	rejected  map[string]bool
	files     []api.TemplateFile
	templates []string
	queue     []string
	warnings  []string
}

func (c *collector) walk(target string) error {
	paths, err := c.tree.Walk(target)
	if err != nil {
		return err
	}
	explicit := len(paths) == 1 && paths[0] == target
	for _, p := range paths {
		if !IsCandidatePath(p) || (!explicit && excludedPath(p)) {
			continue
		}
		content, ok := c.read(p)
		if ok && LooksLikeTemplate(content) {
			c.addTemplate(p, content)
		}
	}
	return nil
}

func (c *collector) read(p string) ([]byte, bool) {
	if c.rejected[p] {
		return nil, false
	}
	content, err := c.tree.Read(p)
	if err == nil && len(content) > MaxFileSize {
		c.warnings = append(c.warnings, fmt.Sprintf("Skipping %s: file exceeds 1MB limit", p))
	}
	if err != nil || len(content) > MaxFileSize {
		c.rejected[p] = true
		return nil, false
	}
	return content, true
}

func (c *collector) addTemplate(p string, content []byte) {
	if c.contents[p] != nil {
		return
	}
	if len(c.templates) >= MaxTemplates {
		c.warnings = append(c.warnings, fmt.Sprintf("Skipping %s: more than %d templates found", p, MaxTemplates))
		return
	}
	c.add(p, content)
	c.templates = append(c.templates, p)
}

func (c *collector) add(p string, content []byte) {
	c.contents[p] = content
	c.files = append(c.files, api.TemplateFile{Path: p, Content: string(content)})
	c.queue = append(c.queue, p)
}

func (c *collector) followReferences() {
	for len(c.queue) > 0 {
		p := c.queue[0]
		c.queue = c.queue[1:]
		for _, ref := range References(c.contents[p]) {
			c.addReferenced(path.Join(path.Dir(p), ref))
		}
	}
}

// addReferenced adds a file a template names. A template can come from someone
// else, so what it names is held to more than a discovered file: never hidden,
// vendored or a manifest, and in a git repository tracked by git, so an ignored
// credentials file cannot be pulled into the upload. An included snippet is not
// a template, so outside a repository nothing vouches for it and it stays out.
func (c *collector) addReferenced(p string) {
	if c.contents[p] != nil || c.rejected[p] {
		return
	}
	reason := c.refusal(p)
	if reason != "" {
		c.reject(p, reason)
		return
	}
	content, ok := c.read(p)
	if !ok {
		return
	}
	if LooksLikeTemplate(content) {
		c.addTemplate(p, content)
		return
	}
	if c.tree.Tracking(p) == NoRepository {
		c.reject(p, "an included file is uploaded only when git tracks it")
		return
	}
	c.add(p, content)
}

// refusal says why a referenced path is never read, or returns "".
func (c *collector) refusal(p string) string {
	switch {
	case p == ".." || strings.HasPrefix(p, "../"):
		return "outside the project"
	case excludedPath(p):
		return "hidden, vendored or a package manifest"
	case c.tree.Tracking(p) == Untracked:
		return "not tracked by git"
	}
	return ""
}

func (c *collector) reject(p, reason string) {
	c.rejected[p] = true
	c.warnings = append(c.warnings, fmt.Sprintf("Skipping %s: %s", p, reason))
}

func (c *collector) addSamconfigs() {
	dirs := make(map[string]bool)
	for _, t := range c.templates {
		dirs[path.Dir(t)] = true
	}
	for dir := range dirs {
		for _, name := range samconfigNames {
			c.addSamconfig(path.Join(dir, name))
		}
	}
}

// addSamconfig adds a samconfig beside a template. It can carry parameter
// overrides, so in a git repository it is uploaded only when git tracks it.
func (c *collector) addSamconfig(p string) {
	content, err := c.tree.Read(p)
	if err != nil || c.contents[p] != nil {
		return
	}
	if c.tree.Tracking(p) == Untracked {
		c.reject(p, "not tracked by git")
		return
	}
	c.contents[p] = content
	c.files = append(c.files, api.TemplateFile{Path: p, Content: string(content)})
}

// References returns the local paths a template names in a literal
// TemplateURL or Location: relative paths with a template extension, never URLs.
func References(content []byte) []string {
	var refs []string
	for _, m := range reference.FindAllSubmatch(content, -1) {
		ref := string(m[1])
		if strings.Contains(ref, "://") || strings.HasPrefix(ref, "$") || !IsCandidatePath(ref) {
			continue
		}
		refs = append(refs, ref)
	}
	return refs
}

// DeclaredParameters returns the names in a template's Parameters section.
func DeclaredParameters(content []byte) map[string]bool {
	declared := make(map[string]bool)
	var doc yaml.Node
	if yaml.Unmarshal(content, &doc) != nil || len(doc.Content) == 0 {
		return declared
	}
	parameters := mappingValue(doc.Content[0], "Parameters")
	if parameters == nil {
		return declared
	}
	for i := 0; i+1 < len(parameters.Content); i += 2 {
		declared[parameters.Content[i].Value] = true
	}
	return declared
}

func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key && node.Content[i+1].Kind == yaml.MappingNode {
			return node.Content[i+1]
		}
	}
	return nil
}
