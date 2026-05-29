package browser

import (
	"context"
	"os/exec"
)

func OpenURL(url string) error {
	return exec.CommandContext(context.Background(), "open", url).Start()
}
