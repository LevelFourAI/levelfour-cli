package terraform

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type ManifestEntry struct {
	Key         string `json:"key"`
	Source      string `json:"source"`
	Version     string `json:"version"`
	Dir         string `json:"dir"`
	DownloadURL string `json:"download_url"`
}

type ModuleManifest struct {
	Entries []ManifestEntry `json:"entries"`
}

type ModuleDownloader struct {
	cacheDir string
	registry *RegistryClient
	manifest *ModuleManifest
}

var execCommand = exec.Command
var osMkdirAll = os.MkdirAll
var osRemoveAll = os.RemoveAll
var jsonMarshalIndent = json.MarshalIndent

func NewModuleDownloader(projectDir string) *ModuleDownloader {
	cacheDir := filepath.Join(projectDir, ".levelfour", "modules")
	_ = osMkdirAll(cacheDir, 0o750)

	d := &ModuleDownloader{
		cacheDir: cacheDir,
		registry: NewRegistryClient(),
		manifest: &ModuleManifest{},
	}
	d.loadManifest()
	return d
}

func (d *ModuleDownloader) Resolve(source, version string) (string, error) {
	mod, subdir, ok := ParseRegistrySource(source)
	if !ok {
		return "", fmt.Errorf("cannot parse registry source: %s", source)
	}

	resolvedVersion, err := d.registry.ResolveVersion(mod, version)
	if err != nil {
		return "", err
	}

	baseSource := mod.Namespace + "/" + mod.Name + "/" + mod.Provider
	key := cacheKey(baseSource, resolvedVersion)

	for _, entry := range d.manifest.Entries {
		if entry.Key == key {
			fullPath := entry.Dir
			if subdir != "" {
				fullPath = filepath.Join(fullPath, subdir)
			}
			if _, err := os.Stat(fullPath); err == nil {
				return fullPath, nil
			}
		}
	}

	downloadURL, err := d.registry.GetDownloadURL(mod, resolvedVersion)
	if err != nil {
		return "", err
	}

	destDir := filepath.Join(d.cacheDir, key)
	if err := d.downloadGit(downloadURL, destDir); err != nil {
		return "", fmt.Errorf("git clone failed for %s: %w", source, err)
	}

	d.manifest.Entries = append(d.manifest.Entries, ManifestEntry{
		Key:         key,
		Source:      source,
		Version:     resolvedVersion,
		Dir:         destDir,
		DownloadURL: downloadURL,
	})
	d.saveManifest()

	fullPath := destDir
	if subdir != "" {
		fullPath = filepath.Join(destDir, subdir)
	}
	return fullPath, nil
}

func (d *ModuleDownloader) downloadGit(url, destDir string) error {
	repoURL, ref := parseGitURL(url)
	if repoURL == "" {
		return fmt.Errorf("cannot parse git URL: %s", url)
	}

	_ = osRemoveAll(destDir)
	if err := osMkdirAll(destDir, 0o750); err != nil {
		return err
	}

	if ref != "" {
		cmd := execCommand("git", "clone", "--depth", "1", "--branch", ref, repoURL, destDir)
		if output, err := cmd.CombinedOutput(); err == nil {
			return nil
		} else {
			_ = osRemoveAll(destDir)
			_ = osMkdirAll(destDir, 0o750)
			_ = output
		}
	}

	cloneCmd := execCommand("git", "clone", repoURL, destDir)
	if output, err := cloneCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s: %s", err, string(output))
	}

	if ref != "" {
		checkoutCmd := execCommand("git", "-C", destDir, "checkout", ref)
		if output, err := checkoutCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("checkout %s failed: %s: %s", ref, err, string(output))
		}
	}

	return nil
}

func parseGitURL(url string) (repo, ref string) {
	url = strings.TrimPrefix(url, "git::")

	if idx := strings.Index(url, "?"); idx != -1 {
		query := url[idx+1:]
		url = url[:idx]
		for _, param := range strings.Split(query, "&") {
			kv := strings.SplitN(param, "=", 2)
			if len(kv) == 2 && kv[0] == "ref" {
				ref = kv[1]
			}
		}
	}

	return url, ref
}

func cacheKey(source, version string) string {
	safe := strings.NewReplacer("/", "_", ".", "_").Replace(source)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(source+"@"+version)))[:8]
	return fmt.Sprintf("%s_%s_%s", safe, version, hash)
}

func (d *ModuleDownloader) saveManifest() {
	data, err := jsonMarshalIndent(d.manifest, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(d.cacheDir, "manifest.json"), data, 0o600)
}

func (d *ModuleDownloader) loadManifest() {
	data, err := os.ReadFile(filepath.Join(d.cacheDir, "manifest.json"))
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, d.manifest)
}
