//go:build !windows

package tuicommon

import (
	"syscall"
	"testing"
)

func TestDrainFD(t *testing.T) {
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		t.Fatal(err)
	}
	_, _ = syscall.Write(fds[1], []byte("\x1b]11;rgb:1919/2020/1919\x1b\\"))
	_ = syscall.Close(fds[1])
	drainFD(fds[0])
	_ = syscall.Close(fds[0])
}

func TestDrainFDInvalidFD(t *testing.T) {
	drainFD(-1)
}

func TestDrainTermInputMissingTTY(t *testing.T) {
	orig := ttyPath
	ttyPath = t.TempDir() + "/no-such-tty"
	defer func() { ttyPath = orig }()
	DrainTermInput()
}

func TestDrainTermInputWithFIFO(t *testing.T) {
	fifo := t.TempDir() + "/fake-tty"
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Skip("cannot create FIFO:", err)
	}
	orig := ttyPath
	ttyPath = fifo
	defer func() { ttyPath = orig }()

	go func() {
		fd, _ := syscall.Open(fifo, syscall.O_WRONLY, 0)
		_, _ = syscall.Write(fd, []byte("\x1b]11;rgb:1919/2020\x1b\\"))
		_ = syscall.Close(fd)
	}()
	DrainTermInput()
}
