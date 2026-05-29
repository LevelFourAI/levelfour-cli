package context

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var getwd = os.Getwd

func DetectRepo() (string, error) {
	dir, err := getwd()
	if err != nil {
		return "", err
	}

	for {
		gitConfig := filepath.Join(dir, ".git", "config")
		if _, err := os.Stat(gitConfig); err == nil {
			return parseRemoteURL(gitConfig)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("not a git repository")
}

func parseRemoteURL(configPath string) (string, error) {
	f, err := os.Open(filepath.Clean(configPath))
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	inRemoteOrigin := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == `[remote "origin"]` {
			inRemoteOrigin = true
			continue
		}
		if inRemoteOrigin {
			if strings.HasPrefix(line, "[") {
				break
			}
			if strings.HasPrefix(line, "url = ") {
				return strings.TrimPrefix(line, "url = "), nil
			}
		}
	}
	return "", fmt.Errorf("no remote origin found")
}

func DetectTerraformRoot() (string, error) {
	dir, err := getwd()
	if err != nil {
		return "", err
	}

	for {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.tf"))
		if len(matches) > 0 {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", fmt.Errorf("no Terraform files found")
}
