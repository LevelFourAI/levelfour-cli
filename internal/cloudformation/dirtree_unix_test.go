//go:build !windows

package cloudformation

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestDirTree_ReadRefusesANamedPipeWithoutBlocking(t *testing.T) {
	root := resolvedTempDir(t)
	if err := syscall.Mkfifo(filepath.Join(root, "pipe.yaml"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := (DirTree{Root: root}).Read("pipe.yaml"); err == nil {
		t.Error("a named pipe was read")
	}
}
