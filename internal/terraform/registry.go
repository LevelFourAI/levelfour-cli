package terraform

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
)

type RegistryModule struct {
	Namespace string
	Name      string
	Provider  string
}

type RegistryClient struct {
	httpClient *http.Client
	baseURL    string
}

func NewRegistryClient() *RegistryClient {
	return &RegistryClient{
		httpClient: http.DefaultClient,
		baseURL:    "https://registry.terraform.io",
	}
}

func ParseRegistrySource(source string) (RegistryModule, string, bool) {
	subdir := ""
	main := source

	if idx := strings.Index(source, "//"); idx != -1 {
		main = source[:idx]
		subdir = source[idx+2:]
	}

	parts := strings.Split(main, "/")
	if len(parts) != 3 {
		return RegistryModule{}, "", false
	}

	for _, p := range parts {
		if p == "" {
			return RegistryModule{}, "", false
		}
	}

	return RegistryModule{
		Namespace: parts[0],
		Name:      parts[1],
		Provider:  parts[2],
	}, subdir, true
}

func (rc *RegistryClient) ListVersions(mod RegistryModule) ([]string, error) {
	url := fmt.Sprintf("%s/v1/modules/%s/%s/%s/versions", rc.baseURL, mod.Namespace, mod.Name, mod.Provider)
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		return nil, err
	}
	if token := rc.tokenForHost(rc.hostFromURL(rc.baseURL)); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := rc.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry returned status %d for %s/%s/%s", resp.StatusCode, mod.Namespace, mod.Name, mod.Provider)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Modules []struct {
			Versions []struct {
				Version string `json:"version"`
			} `json:"versions"`
		} `json:"modules"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse registry response: %w", err)
	}

	var versions []string
	if len(result.Modules) > 0 {
		for _, v := range result.Modules[0].Versions {
			versions = append(versions, v.Version)
		}
	}
	return versions, nil
}

func (rc *RegistryClient) GetDownloadURL(mod RegistryModule, version string) (string, error) {
	url := fmt.Sprintf("%s/v1/modules/%s/%s/%s/%s/download", rc.baseURL, mod.Namespace, mod.Name, mod.Provider, version)
	req, err := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	if err != nil {
		return "", err
	}
	if token := rc.tokenForHost(rc.hostFromURL(rc.baseURL)); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download URL request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusFound {
		return "", fmt.Errorf("registry returned status %d for download URL", resp.StatusCode)
	}

	downloadURL := resp.Header.Get("X-Terraform-Get")
	if downloadURL == "" {
		return "", fmt.Errorf("no X-Terraform-Get header in response")
	}
	return downloadURL, nil
}

func (rc *RegistryClient) ResolveVersion(mod RegistryModule, constraint string) (string, error) {
	versions, err := rc.ListVersions(mod)
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions found for %s/%s/%s", mod.Namespace, mod.Name, mod.Provider)
	}

	if constraint == "" {
		sort.Slice(versions, func(i, j int) bool {
			return compareVersions(versions[i], versions[j]) > 0
		})
		return versions[0], nil
	}

	var matching []string
	for _, v := range versions {
		if matchesConstraint(v, constraint) {
			matching = append(matching, v)
		}
	}

	if len(matching) == 0 {
		return "", fmt.Errorf("no version matching %q for %s/%s/%s", constraint, mod.Namespace, mod.Name, mod.Provider)
	}

	sort.Slice(matching, func(i, j int) bool {
		return compareVersions(matching[i], matching[j]) > 0
	})
	return matching[0], nil
}

func matchesConstraint(version, constraint string) bool {
	constraint = strings.TrimSpace(constraint)

	parts := strings.Split(constraint, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !matchesSingleConstraint(version, part) {
			return false
		}
	}
	return true
}

var constraintPrefixes = []struct {
	prefix string
	op     string
	trim   int
}{
	{"~>", "~>", 2},
	{">=", ">=", 2},
	{"<=", "<=", 2},
	{"!=", "!=", 2},
	{">", ">", 1},
	{"<", "<", 1},
	{"=", "=", 1},
}

func parseConstraintOp(constraint string) (op, ver string) {
	for _, p := range constraintPrefixes {
		if strings.HasPrefix(constraint, p.prefix) {
			return p.op, strings.TrimSpace(constraint[p.trim:])
		}
	}
	if !strings.ContainsAny(constraint, "~><!=") {
		return "=", constraint
	}
	return "", constraint
}

func matchesSingleConstraint(version, constraint string) bool {
	constraint = strings.TrimSpace(constraint)
	op, ver := parseConstraintOp(constraint)

	switch op {
	case "~>":
		return matchesPessimistic(version, ver)
	case "=":
		return compareVersions(version, ver) == 0
	case "!=":
		return compareVersions(version, ver) != 0
	case ">":
		return compareVersions(version, ver) > 0
	case ">=":
		return compareVersions(version, ver) >= 0
	case "<":
		return compareVersions(version, ver) < 0
	case "<=":
		return compareVersions(version, ver) <= 0
	default:
		return false
	}
}

func matchesPessimistic(version, constraint string) bool {
	vParts := parseVersion(version)
	cParts := parseVersion(constraint)

	if len(vParts) < len(cParts) {
		return false
	}

	if compareVersions(version, constraint) < 0 {
		return false
	}

	upperParts := make([]int, len(cParts))
	copy(upperParts, cParts)
	if len(upperParts) > 1 {
		upperParts[len(upperParts)-2]++
		upperParts[len(upperParts)-1] = 0
	} else {
		upperParts[0]++
	}

	for i := 0; i < len(upperParts) && i < len(vParts); i++ {
		if vParts[i] < upperParts[i] {
			return true
		}
		if vParts[i] > upperParts[i] {
			return false
		}
	}
	return false
}

func parseVersion(v string) []int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	result := make([]int, len(parts))
	for i, p := range parts {
		n, _ := strconv.Atoi(p)
		result[i] = n
	}
	return result
}

func compareVersions(a, b string) int {
	aParts := parseVersion(a)
	bParts := parseVersion(b)

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		av, bv := 0, 0
		if i < len(aParts) {
			av = aParts[i]
		}
		if i < len(bParts) {
			bv = bParts[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func (rc *RegistryClient) tokenForHost(host string) string {
	envKey := "TF_TOKEN_" + strings.ReplaceAll(strings.ReplaceAll(host, ".", "_"), "-", "__")
	return os.Getenv(envKey)
}

func (rc *RegistryClient) hostFromURL(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	if idx := strings.Index(url, "/"); idx != -1 {
		url = url[:idx]
	}
	return url
}
