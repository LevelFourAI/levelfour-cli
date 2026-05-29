package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	checkInterval = 24 * time.Hour
	releasesURL   = "https://api.github.com/repos/LevelFourAI/levelfour-cli/releases/latest"
)

type cachedCheck struct {
	LastCheck time.Time `json:"last_check"`
	Latest    string    `json:"latest"`
}

func cachePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".levelfour", "update-check")
}

var httpGet = func(url string) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

func isCI() bool {
	for _, key := range []string{"CI", "BUILD_NUMBER", "RUN_ID", "GITHUB_ACTIONS"} {
		if os.Getenv(key) != "" {
			return true
		}
	}
	return false
}

func CheckForUpdate(current string) string {
	if isCI() {
		return ""
	}
	if current == "dev" {
		return ""
	}

	path := cachePath()
	data, err := os.ReadFile(filepath.Clean(path))
	if err == nil {
		var c cachedCheck
		if json.Unmarshal(data, &c) == nil && time.Since(c.LastCheck) < checkInterval {
			if c.Latest != "" && c.Latest != current && isNewer(c.Latest, current) {
				return formatMessage(current, c.Latest)
			}
			return ""
		}
	}

	latest := fetchLatest()
	if latest == "" {
		return ""
	}

	c := cachedCheck{LastCheck: time.Now(), Latest: latest}
	if b, err := json.Marshal(c); err == nil {
		_ = os.MkdirAll(filepath.Dir(path), 0o700)
		_ = os.WriteFile(path, b, 0o600)
	}

	if latest != current && isNewer(latest, current) {
		return formatMessage(current, latest)
	}
	return ""
}

func fetchLatest() string {
	resp, err := httpGet(releasesURL)
	if err != nil || resp.StatusCode != 200 {
		return ""
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"`
	}
	if json.NewDecoder(resp.Body).Decode(&release) != nil {
		return ""
	}
	return strings.TrimPrefix(release.TagName, "v")
}

func isNewer(latest, current string) bool {
	lp := strings.Split(latest, ".")
	cp := strings.Split(current, ".")
	for i := 0; i < len(lp) && i < len(cp); i++ {
		l, _ := strconv.Atoi(lp[i])
		c, _ := strconv.Atoi(cp[i])
		if l > c {
			return true
		}
		if l < c {
			return false
		}
	}
	return len(lp) > len(cp)
}

const (
	ansiDim    = "\033[2m"
	ansiCyan   = "\033[38;5;159m"
	ansiYellow = "\033[38;5;173m"
	ansiReset  = "\033[0m"
)

func formatMessage(current, latest string) string {
	if noColor() {
		return fmt.Sprintf("\nA new version of l4 is available: %s → %s\nRun 'brew upgrade levelfour' or download from https://github.com/LevelFourAI/levelfour-cli/releases\n", current, latest)
	}
	return fmt.Sprintf("\n%sA new version of l4 is available: %s%s%s → %s%s%s\n%sRun '%sbrew upgrade levelfour%s' or download from https://github.com/LevelFourAI/levelfour-cli/releases%s\n",
		ansiDim, ansiYellow, current, ansiDim, ansiCyan, latest, ansiDim,
		ansiDim, ansiCyan, ansiDim, ansiReset)
}

func noColor() bool {
	return os.Getenv("NO_COLOR") != ""
}
