package browser

import (
	"context"
	"os/exec"
)

func OpenURL(url string) error {
	return exec.CommandContext(context.Background(), "rundll32", "url.dll,FileProtocolHandler", url).Start()
}
