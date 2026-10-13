package terraform

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// skipDir reports directories that never hold a root module of the project:
// tool caches and checkouts (hidden directories) and vendored packages.
func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

// DiscoverRoots returns, sorted, every directory under dir that holds .tf
// files and that no other of them calls as a local module. A module is priced
// through the root that calls it, so reading it as a root too would count it twice.
func DiscoverRoots(dir string) ([]string, error) {
	absDir, _ := filepath.Abs(dir)
	tfDirs, err := directoriesWithTerraform(absDir)
	if err != nil {
		return nil, err
	}
	called := make(map[string]bool)
	for _, d := range tfDirs {
		for _, m := range localModuleDirs(d) {
			called[m] = true
		}
	}
	var roots []string
	for _, d := range tfDirs {
		if !called[d] {
			roots = append(roots, d)
		}
	}
	sort.Strings(roots)
	return roots, nil
}

func directoriesWithTerraform(root string) ([]string, error) {
	seen := make(map[string]bool)
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path != root && skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		dir := filepath.Dir(path)
		if filepath.Ext(path) == tfExt && !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
		return nil
	})
	return dirs, err
}

// localModuleDirs returns the directories the module blocks in dir call by a
// local source. A file that does not parse calls nothing.
func localModuleDirs(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var called []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != tfExt {
			continue
		}
		path := filepath.Join(dir, e.Name())
		content, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			continue
		}
		file, diags := hclsyntax.ParseConfig(content, path, hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			continue
		}
		for _, block := range file.Body.(*hclsyntax.Body).Blocks {
			if source := moduleSource(block); isLocalSource(source) {
				called = append(called, resolveSource(dir, source))
			}
		}
	}
	return called
}

func moduleSource(block *hclsyntax.Block) string {
	if block.Type != blockModule {
		return ""
	}
	attr, ok := block.Body.Attributes["source"]
	if !ok {
		return ""
	}
	val, diags := attr.Expr.Value(nil)
	if diags.HasErrors() || val.Type() != cty.String {
		return ""
	}
	return val.AsString()
}

func resolveSource(dir, source string) string {
	if filepath.IsAbs(source) {
		return filepath.Clean(source)
	}
	return filepath.Join(dir, source)
}

// parseRoot is a variable so a test can make one root fail after discovery found it.
var parseRoot = ParseDirectoryWithOpts

// ParseTree parses every root module under dir (see DiscoverRoots). Each
// snapshot's Scope is its root's path relative to dir, "." for dir itself, so two
// roots that declare the same address stay apart.
func ParseTree(dir string, opts ParseOptions) ([]api.ResourceSnapshot, []string, error) {
	roots, err := DiscoverRoots(dir)
	if err != nil {
		return nil, nil, err
	}
	absDir, _ := filepath.Abs(dir)
	var snapshots []api.ResourceSnapshot
	var warnings []string
	for _, root := range roots {
		snaps, warns, parseErr := parseRoot(root, opts)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		rel, _ := filepath.Rel(absDir, root)
		for i := range snaps {
			snaps[i].Scope = filepath.ToSlash(rel)
		}
		snapshots = append(snapshots, snaps...)
		warnings = append(warnings, warns...)
	}

	maxResources := MaxResources
	if opts.MaxResources > 0 {
		maxResources = opts.MaxResources
	}
	if len(snapshots) > maxResources {
		warnings = append(warnings, fmt.Sprintf("Found %d resources, truncating to %d", len(snapshots), maxResources))
		snapshots = snapshots[:maxResources]
	}
	return snapshots, warnings, nil
}
