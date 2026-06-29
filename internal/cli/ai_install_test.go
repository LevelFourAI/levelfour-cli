package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func makeTarGz(t *testing.T, name string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o700, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func assetName() string {
	return fmt.Sprintf("levelfour-ai-gateway_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

func checksumsFor(tarball []byte) []byte {
	return []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(tarball), assetName()))
}

func installResponder(tarball, sums []byte) func(string, string) ([]byte, error) {
	return func(url, _ string) ([]byte, error) {
		if strings.Contains(url, "checksums.txt") {
			return sums, nil
		}
		return tarball, nil
	}
}

func TestInstallGatewaySuccess(t *testing.T) {
	saveAISeams(t)
	tgz := makeTarGz(t, "l4-gateway", []byte("BINARY"))
	aiHTTPGetFn = installResponder(tgz, checksumsFor(tgz))
	dst := filepath.Join(t.TempDir(), "bin", "l4-gateway")
	gatewayCachePathFn = func() string { return dst }
	got, err := installGateway()
	if err != nil || got != dst {
		t.Fatalf("got %q, %v", got, err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "BINARY" {
		t.Fatalf("cached binary = %q", b)
	}
}

func TestInstallGatewayDownloadError(t *testing.T) {
	saveAISeams(t)
	aiHTTPGetFn = func(string, string) ([]byte, error) { return nil, errors.New("offline") }
	if _, err := installGateway(); err == nil {
		t.Fatal("want a download error")
	}
}

func TestInstallGatewayChecksumDownloadError(t *testing.T) {
	saveAISeams(t)
	aiHTTPGetFn = func(url, _ string) ([]byte, error) {
		if strings.Contains(url, "checksums.txt") {
			return nil, errors.New("offline")
		}
		return []byte("tarball"), nil
	}
	if _, err := installGateway(); err == nil {
		t.Fatal("want a checksums download error")
	}
}

func TestInstallGatewayChecksumMismatch(t *testing.T) {
	saveAISeams(t)
	tgz := makeTarGz(t, "l4-gateway", []byte("BINARY"))
	aiHTTPGetFn = installResponder(tgz, []byte("deadbeef  "+assetName()+"\n"))
	if _, err := installGateway(); err == nil {
		t.Fatal("want a checksum mismatch error")
	}
}

func TestInstallGatewayExtractError(t *testing.T) {
	saveAISeams(t)
	bad := []byte("not a gzip tarball")
	aiHTTPGetFn = installResponder(bad, checksumsFor(bad))
	if _, err := installGateway(); err == nil {
		t.Fatal("want an extract error")
	}
}

func TestInstallGatewayMkdirError(t *testing.T) {
	saveAISeams(t)
	tgz := makeTarGz(t, "l4-gateway", []byte("X"))
	aiHTTPGetFn = installResponder(tgz, checksumsFor(tgz))
	f := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	gatewayCachePathFn = func() string { return filepath.Join(f, "sub", "l4-gateway") }
	if _, err := installGateway(); err == nil {
		t.Fatal("want a mkdir error (parent is a file)")
	}
}

func TestInstallGatewayWriteError(t *testing.T) {
	saveAISeams(t)
	tgz := makeTarGz(t, "l4-gateway", []byte("X"))
	aiHTTPGetFn = installResponder(tgz, checksumsFor(tgz))
	dir := t.TempDir()
	gatewayCachePathFn = func() string { return dir } // dst is a directory -> WriteFile fails
	if _, err := installGateway(); err == nil {
		t.Fatal("want a write error")
	}
}

func TestInstallGatewayUsesDistURLOverride(t *testing.T) {
	saveAISeams(t)
	t.Setenv("L4_GATEWAY_DIST_URL", "https://example.test/gw")
	tgz := makeTarGz(t, "l4-gateway", []byte("X"))
	var seen string
	aiHTTPGetFn = func(url, _ string) ([]byte, error) {
		seen = url
		if strings.Contains(url, "checksums.txt") {
			return checksumsFor(tgz), nil
		}
		return tgz, nil
	}
	gatewayCachePathFn = func() string { return filepath.Join(t.TempDir(), "l4-gateway") }
	if _, err := installGateway(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(seen, "https://example.test/gw/") {
		t.Fatalf("dist URL override not used: %q", seen)
	}
}

func TestVerifyChecksumNoEntry(t *testing.T) {
	if err := verifyChecksum([]byte("x"), "asset.tar.gz", []byte("abc  other.tar.gz\n")); err == nil {
		t.Fatal("want a no-checksum error")
	}
}

func TestExtractGatewayNotInArchive(t *testing.T) {
	tgz := makeTarGz(t, "some-other-file", []byte("x"))
	if _, err := extractGateway(tgz); err == nil {
		t.Fatal("want a not-found error")
	}
}

func TestExtractGatewayBadTar(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte("gzipped but not a tar")); err != nil {
		t.Fatal(err)
	}
	_ = gz.Close()
	if _, err := extractGateway(buf.Bytes()); err == nil {
		t.Fatal("want a tar read error")
	}
}

func TestGatewayCachePath(t *testing.T) {
	if filepath.Base(gatewayCachePath()) != gatewayBinName {
		t.Fatalf("unexpected cache path: %q", gatewayCachePath())
	}
}

func TestGatewayCachePathHomeError(t *testing.T) {
	t.Setenv("HOME", "")
	if filepath.Base(gatewayCachePath()) != gatewayBinName {
		t.Fatalf("unexpected fallback cache path: %q", gatewayCachePath())
	}
}

func TestAIInstallCmdSuccess(t *testing.T) {
	saveAISeams(t)
	installGatewayFn = func() (string, error) { return "/cache/l4-gateway", nil }
	if err := aiInstallCmd.RunE(aiInstallCmd, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAIInstallCmdError(t *testing.T) {
	saveAISeams(t)
	installGatewayFn = func() (string, error) { return "", errors.New("nope") }
	if err := aiInstallCmd.RunE(aiInstallCmd, nil); err == nil {
		t.Fatal("want an install error")
	}
}
