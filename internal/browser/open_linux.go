package browser

import (
	"context"
	"fmt"
	"os/exec"
)

var (
	lookPath  = exec.LookPath
	execStart = func(name, url string) error {
		return exec.CommandContext(context.Background(), name, url).Start()
	}
)

func OpenURL(url string) error {
	for _, cmd := range []string{"xdg-open", "x-www-browser", "www-browser", "wslview"} {
		if path, err := lookPath(cmd); err == nil {
			return execStart(path, url)
		}
	}
	return fmt.Errorf("no browser opener found (tried xdg-open, x-www-browser, www-browser, wslview)")
}
