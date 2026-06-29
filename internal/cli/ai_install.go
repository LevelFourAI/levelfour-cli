package cli

// Auto-install of the private gateway binary, so `l4 ai run claude` is a single command with no
// prior setup. We fetch the compiled binary for this platform, verify its checksum, and cache it.
// Only the compiled artifact moves; no source or routing logic ships in this public CLI.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	gatewayBinName        = "l4-gateway"
	defaultGatewayDistURL = "https://dl.levelfour.ai/ai-gateway/latest"
	maxGatewayBytes       = 256 << 20 // bound extraction; the binary is a few MB
)

// install seams.
var (
	installGatewayFn   = installGateway
	gatewayCachePathFn = gatewayCachePath
	statFn             = os.Stat
)

var aiInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Download and cache the LevelFour gateway binary",
	RunE: func(_ *cobra.Command, _ []string) error {
		path, err := installGatewayFn()
		if err != nil {
			return err
		}
		output.Success("Gateway installed at " + path)
		return nil
	},
}

// gatewayCachePath is where the CLI caches the auto-installed gateway binary.
func gatewayCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".levelfour", "bin", gatewayBinName)
}

func installGateway() (string, error) {
	base := os.Getenv("L4_GATEWAY_DIST_URL")
	if base == "" {
		base = defaultGatewayDistURL
	}
	asset := fmt.Sprintf("levelfour-ai-gateway_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	tarball, err := aiHTTPGetFn(base+"/"+asset, "")
	if err != nil {
		return "", fmt.Errorf("download gateway: %w", err)
	}
	sums, err := aiHTTPGetFn(base+"/checksums.txt", "")
	if err != nil {
		return "", fmt.Errorf("download checksums: %w", err)
	}
	if err := verifyChecksum(tarball, asset, sums); err != nil {
		return "", err
	}
	bin, err := extractGateway(tarball)
	if err != nil {
		return "", err
	}
	dst := gatewayCachePathFn()
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, bin, 0o700); err != nil { //nolint:gosec // G306: an executable must carry the exec bit; 0700 is owner-only
		return "", err
	}
	return dst, nil
}

func verifyChecksum(tarball []byte, asset string, sums []byte) error {
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == asset {
			want = f[0]
		}
	}
	if want == "" {
		return fmt.Errorf("no checksum published for %s", asset)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(tarball)); got != want {
		return errors.New("gateway checksum mismatch, refusing to install")
	}
	return nil
}

func extractGateway(tarball []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(tarball))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s not found in the archive", gatewayBinName)
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(h.Name) == gatewayBinName {
			return io.ReadAll(io.LimitReader(tr, maxGatewayBytes))
		}
	}
}
