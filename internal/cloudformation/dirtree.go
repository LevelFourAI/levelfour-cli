package cloudformation

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DirTree reads a project from disk. Root must already have its symlinks
// resolved, so a link that leads out of it can be told apart.
type DirTree struct {
	Root string
}

// Walk lists the regular files under target, skipping SkipDir directories below
// it. A symlink is never followed.
func (d DirTree) Walk(target string) ([]string, error) {
	start := filepath.Join(d.Root, filepath.FromSlash(target))
	var paths []string
	err := filepath.WalkDir(start, func(p string, e fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if e.IsDir() {
			if p != start && SkipDir(e.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if e.Type().IsRegular() {
			rel, _ := filepath.Rel(d.Root, p)
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	return paths, err
}

// Read returns a file inside Root. A path that leaves Root, directly or through
// a symlink, is refused.
func (d DirTree) Read(p string) ([]byte, error) {
	full := filepath.Join(d.Root, filepath.FromSlash(path.Clean(p)))
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return nil, err
	}
	rel, _ := filepath.Rel(d.Root, real)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%s is outside the project", p)
	}
	return os.ReadFile(filepath.Clean(real))
}
