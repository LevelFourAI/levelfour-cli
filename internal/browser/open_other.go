//go:build !darwin && !linux && !windows

package browser

import "fmt"

func OpenURL(url string) error {
	return fmt.Errorf("unsupported platform")
}
