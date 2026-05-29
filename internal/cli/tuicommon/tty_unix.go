//go:build !windows

package tuicommon

import "syscall"

var ttyPath = "/dev/tty"

func DrainTermInput() {
	fd, err := syscall.Open(ttyPath, syscall.O_RDONLY|syscall.O_NOCTTY, 0)
	if err != nil {
		return
	}
	defer func() { _ = syscall.Close(fd) }()
	drainFD(fd)
}

func drainFD(fd int) {
	if err := syscall.SetNonblock(fd, true); err != nil {
		return
	}
	buf := make([]byte, 4096)
	for {
		n, _ := syscall.Read(fd, buf)
		if n <= 0 {
			break
		}
	}
}
