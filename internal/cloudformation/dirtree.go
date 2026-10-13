package cloudformation

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ErrSymlink refuses a path that is, or lies under, a symlink. The exclusions
// and the git check judge the path a template names, so following a link to a
// hidden or ignored file would get around both.
var ErrSymlink = errors.New("a symlink, which is never followed")

// DirTree reads a project from disk. Root must already have its symlinks
// resolved, so a link below it can be told apart.
type DirTree struct {
	Root string
}

// Walk lists the files under target, skipping SkipDir directories below it. A
// symlink is listed, never followed, so Read can refuse it by name.
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
		if e.Type().IsRegular() || e.Type()&fs.ModeSymlink != 0 {
			rel, _ := filepath.Rel(d.Root, p)
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	return paths, err
}

// Read returns a regular file inside Root reached without following any
// symlink. A path above Root, or through a symlink, is refused, and so is
// anything but a regular file, before it is opened: opening a named pipe would
// block. A file over MaxFileSize is refused from its size alone.
func (d DirTree) Read(p string) ([]byte, error) {
	full := filepath.Join(d.Root, filepath.FromSlash(path.Clean(p)))
	rel, _ := filepath.Rel(d.Root, full)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("%s is outside the project", p)
	}
	info, err := os.Lstat(full)
	if err != nil {
		return nil, err
	}
	// A path that cannot be resolved cannot be shown to be free of links.
	if real, evalErr := filepath.EvalSymlinks(full); evalErr != nil || real != full {
		return nil, fmt.Errorf("%s: %w", p, ErrSymlink)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", p)
	}
	if info.Size() > MaxFileSize {
		return nil, fmt.Errorf("%s: %w", p, ErrTooLarge)
	}
	f, err := os.Open(filepath.Clean(full))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return ReadCapped(f)
}
