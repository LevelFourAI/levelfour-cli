package cli

import (
	"testing"
)

func TestCompletion(t *testing.T) {
	shells := []string{"bash", "zsh", "fish", "powershell"}
	for _, shell := range shells {
		t.Run(shell, func(t *testing.T) {
			outBuf, _, err := executeCommand(t, "completion", shell)
			if err != nil {
				t.Fatalf("completion %s error: %v", shell, err)
			}
			if outBuf.Len() == 0 {
				t.Errorf("completion %s produced no output", shell)
			}
		})
	}
}

func TestCompletionFallthrough(t *testing.T) {
	err := completionCmd.RunE(completionCmd, []string{"unknown"})
	if err != nil {
		t.Errorf("unexpected error for unknown shell: %v", err)
	}
}
