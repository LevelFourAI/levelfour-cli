package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	cfn "github.com/LevelFourAI/levelfour-cli/internal/cloudformation"
	tf "github.com/LevelFourAI/levelfour-cli/internal/terraform"
)

// iacTarget is one path argument of estimate or diff.
type iacTarget struct {
	arg   string
	abs   string
	rel   string // slash path relative to the project root
	isDir bool
}

// resolveTargets resolves the path arguments (the current directory when there
// are none) and the project root they are reported against: the git repository
// holding the first one, or outside a repository the first one's directory.
func resolveTargets(args []string) (string, []iacTarget, error) {
	if len(args) == 0 {
		args = []string{"."}
	}
	targets := make([]iacTarget, 0, len(args))
	for _, a := range args {
		info, err := os.Stat(a)
		if err != nil {
			return "", nil, fmt.Errorf("path not found: %s", a)
		}
		abs, err := filepathAbs(a)
		if err != nil {
			return "", nil, err
		}
		targets = append(targets, iacTarget{arg: a, abs: resolvedPath(abs), isDir: info.IsDir()})
	}

	root := projectRoot(targets[0])
	for i := range targets {
		targets[i].rel = relativeSlash(root, targets[i].abs)
	}
	return root, targets, nil
}

func projectRoot(first iacTarget) string {
	dir := first.abs
	if !first.isDir {
		dir = filepath.Dir(dir)
	}
	root, err := gitRepoRoot(dir)
	if err != nil {
		return dir
	}
	return resolvedPath(root)
}

// resolvedPath follows symlinks, so a path and the git root it sits in compare
// equal (on macOS a temporary directory lives behind /var -> /private/var).
func resolvedPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return p
}

func relativeSlash(root, abs string) string {
	rel, _ := filepath.Rel(root, abs)
	return filepath.ToSlash(rel)
}

// Variables so a test can make parsing and extraction fail.
var (
	parseTerraformFiles = tf.ParseFilesWithOpts
	extractGitRef       = tf.ExtractGitRef
)

func isTerraformFile(p string) bool {
	return filepath.Ext(p) == ".tf"
}

// checkFileTargets refuses a file argument that is neither Terraform nor a
// CloudFormation template, so a typo does not silently price nothing.
func checkFileTargets(targets []iacTarget) error {
	for _, t := range targets {
		if t.isDir || isTerraformFile(t.abs) {
			continue
		}
		if !cfn.IsCandidatePath(t.abs) {
			return fmt.Errorf("%s is neither a Terraform file nor a CloudFormation template", t.arg)
		}
		// t.abs is resolved, so its directory is a root DirTree accepts.
		content, err := cfn.DirTree{Root: filepath.Dir(t.abs)}.Read(filepath.Base(t.abs))
		if errors.Is(err, cfn.ErrTooLarge) {
			return fmt.Errorf("%s exceeds the 1MB limit", t.arg)
		}
		if err != nil || !cfn.LooksLikeTemplate(content) {
			return fmt.Errorf("%s is neither a Terraform file nor a CloudFormation template", t.arg)
		}
	}
	return nil
}

// collectTerraform parses the Terraform targets. File paths and scopes come out
// relative to the project root, so a resource reads the same at base and head.
func collectTerraform(root string, targets []iacTarget, opts tf.ParseOptions) ([]api.ResourceSnapshot, []string, error) {
	var snapshots []api.ResourceSnapshot
	var warnings []string
	filesByDir := make(map[string][]string)
	for _, t := range targets {
		switch {
		case t.isDir:
			snaps, warns, err := tf.ParseTree(t.abs, opts)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to parse %s: %w", t.arg, err)
			}
			snapshots = append(snapshots, placeTree(snaps, root, t.rel)...)
			warnings = append(warnings, warns...)
		case isTerraformFile(t.abs):
			dir := filepath.Dir(t.abs)
			filesByDir[dir] = append(filesByDir[dir], t.abs)
		}
	}
	for _, dir := range sortedKeys(filesByDir) {
		snaps, warns, err := parseTerraformFiles(filesByDir[dir], opts)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse files: %w", err)
		}
		snapshots = append(snapshots, placeRoot(snaps, root, relativeSlash(root, dir))...)
		warnings = append(warnings, warns...)
	}
	return snapshots, warnings, nil
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// placeTree reports snapshots parsed from the tree at treeRel (see tf.ParseTree)
// against the project: files relative to fileRoot, scopes under treeRel.
func placeTree(snaps []api.ResourceSnapshot, fileRoot, treeRel string) []api.ResourceSnapshot {
	for i := range snaps {
		snaps[i].File = relativeSlash(fileRoot, snaps[i].File)
		snaps[i].Scope = path.Join(treeRel, snaps[i].Scope)
	}
	return snaps
}

// placeRoot reports snapshots of one root module at scope against the project.
func placeRoot(snaps []api.ResourceSnapshot, fileRoot, scope string) []api.ResourceSnapshot {
	for i := range snaps {
		snaps[i].File = relativeSlash(fileRoot, snaps[i].File)
		snaps[i].Scope = scope
	}
	return snaps
}

// resolveBaseCommit returns the merge-base of baseRef (main or master when
// empty) and HEAD in the repository at root.
func resolveBaseCommit(root, baseRef string) (string, error) {
	repoRoot, err := gitRepoRoot(root)
	if err != nil {
		return "", err
	}
	if baseRef == "" {
		baseRef = gitDefaultBranch(repoRoot)
		if baseRef == "" {
			return "", fmt.Errorf("no main or master branch found")
		}
	}
	return gitMergeBase(repoRoot, baseRef)
}

// gitBaseline parses the Terraform targets as they were at sha. A target the
// commit does not have yet contributes nothing, so its resources read as added.
func gitBaseline(root, sha string, targets []iacTarget, opts tf.ParseOptions) ([]api.ResourceSnapshot, error) {
	tmpDir, err := osMkdirTemp("", "l4-git-baseline-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	origDir, _ := os.Getwd()
	if chErr := osChdir(root); chErr != nil {
		return nil, chErr
	}
	defer func() { _ = osChdir(origDir) }()

	extractRoot := filepath.Join(tmpDir, sha)
	var snapshots []api.ResourceSnapshot
	for _, t := range targets {
		snaps, baseErr := baselineOf(t, sha, tmpDir, extractRoot, opts)
		if baseErr != nil {
			return nil, baseErr
		}
		snapshots = append(snapshots, snaps...)
	}
	return snapshots, nil
}

func baselineOf(t iacTarget, sha, tmpDir, extractRoot string, opts tf.ParseOptions) ([]api.ResourceSnapshot, error) {
	if !t.isDir && !isTerraformFile(t.abs) {
		return nil, nil
	}
	dirRel := t.rel
	if !t.isDir {
		dirRel = path.Dir(t.rel)
	}
	if !gitPathExists(sha, dirRel) {
		return nil, nil
	}
	if t.isDir {
		opts.ProjectDir = t.abs
		snaps, _, err := tf.ParseGitRefWithOpts(sha, dirRel, tmpDir, opts)
		return placeTree(snaps, extractRoot, t.rel), err
	}

	opts.ProjectDir = filepath.Dir(t.abs)
	if _, err := extractGitRef(sha, dirRel, tmpDir); err != nil {
		return nil, err
	}
	file := filepath.Join(extractRoot, filepath.FromSlash(t.rel))
	if !fileExists(file) {
		return nil, nil
	}
	snaps, _, err := parseTerraformFiles([]string{file}, opts)
	return placeRoot(snaps, extractRoot, dirRel), err
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// gitPathExists reports whether the commit has rel, run from the repository root.
func gitPathExists(sha, rel string) bool {
	if rel == "." {
		return true
	}
	return exec.CommandContext(context.Background(), "git", "cat-file", "-e", sha+":"+rel).Run() == nil
}

// gitTree reads a project as it was at one commit (see cfn.Tree).
type gitTree struct {
	repoRoot string
	sha      string
}

func (g gitTree) Walk(target string) ([]string, error) {
	out, err := exec.CommandContext(context.Background(), "git", "-C", g.repoRoot, "ls-tree", "-r", "-z", g.sha, "--", target).Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-tree failed for %s: %w", g.sha, err)
	}
	var paths []string
	for _, entry := range strings.Split(string(out), "\x00") {
		meta, p, ok := strings.Cut(entry, "\t")
		// A regular file's mode starts with 100; a symlink (120000) is never followed.
		if ok && strings.HasPrefix(meta, "100") {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// Read returns a file as it was at the commit. Its size is asked first, so a
// blob over the cap is never read.
func (g gitTree) Read(p string) ([]byte, error) {
	object := g.sha + ":" + p
	size, err := exec.CommandContext(context.Background(), "git", "-C", g.repoRoot, "cat-file", "-s", object).Output()
	if err != nil {
		return nil, fmt.Errorf("git cat-file failed for %s: %w", p, err)
	}
	if n, _ := strconv.Atoi(strings.TrimSpace(string(size))); n > cfn.MaxFileSize {
		return nil, fmt.Errorf("%s: %w", p, cfn.ErrTooLarge)
	}
	return exec.CommandContext(context.Background(), "git", "-C", g.repoRoot, "cat-file", "blob", object).Output()
}

// Tracking is always Tracked: a commit only holds tracked files.
func (gitTree) Tracking(string) cfn.Tracking {
	return cfn.Tracked
}

// projectTree reads the project from disk and asks git which files it tracks.
type projectTree struct {
	cfn.DirTree
	inRepo bool
}

func newProjectTree(root string) projectTree {
	_, err := gitRepoRoot(root)
	return projectTree{DirTree: cfn.DirTree{Root: root}, inRepo: err == nil}
}

// Tracking asks `git ls-files --error-unmatch`, which fails for a file git does
// not track, an ignored one included. Literal pathspecs keep a name holding * or
// ? from matching some other, tracked file.
func (p projectTree) Tracking(rel string) cfn.Tracking {
	if !p.inRepo {
		return cfn.NoRepository
	}
	cmd := exec.CommandContext(context.Background(), "git", "--literal-pathspecs", "-C", p.Root, "ls-files", "--error-unmatch", "--", rel)
	if cmd.Run() != nil {
		return cfn.Untracked
	}
	return cfn.Tracked
}

// cloudFormationTargets returns the targets, as project paths, that may hold
// templates. Nothing outside the project root is read.
func cloudFormationTargets(targets []iacTarget) ([]string, []string) {
	var rels, warnings []string
	for _, t := range targets {
		if !t.isDir && isTerraformFile(t.abs) {
			continue
		}
		if t.rel == ".." || strings.HasPrefix(t.rel, "../") {
			warnings = append(warnings, fmt.Sprintf("Skipping %s for CloudFormation: outside the project root", t.arg))
			continue
		}
		rels = append(rels, t.rel)
	}
	return rels, warnings
}
