package terraform

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestParseRegistrySource_ThreeParts(t *testing.T) {
	mod, subdir, ok := ParseRegistrySource("terraform-aws-modules/rds/aws")
	if !ok {
		t.Fatal("expected ok")
	}
	if mod.Namespace != "terraform-aws-modules" || mod.Name != "rds" || mod.Provider != "aws" {
		t.Errorf("unexpected module: %+v", mod)
	}
	if subdir != "" {
		t.Errorf("expected empty subdir, got %q", subdir)
	}
}

func TestParseRegistrySource_WithSubdir(t *testing.T) {
	mod, subdir, ok := ParseRegistrySource("terraform-aws-modules/rds/aws//modules/db_instance")
	if !ok {
		t.Fatal("expected ok")
	}
	if mod.Namespace != "terraform-aws-modules" || mod.Name != "rds" || mod.Provider != "aws" {
		t.Errorf("unexpected module: %+v", mod)
	}
	if subdir != "modules/db_instance" {
		t.Errorf("expected subdir=modules/db_instance, got %q", subdir)
	}
}

func TestParseRegistrySource_TwoParts(t *testing.T) {
	_, _, ok := ParseRegistrySource("hashicorp/consul")
	if ok {
		t.Fatal("expected not ok for two-part source")
	}
}

func TestParseRegistrySource_FourParts(t *testing.T) {
	_, _, ok := ParseRegistrySource("a/b/c/d")
	if ok {
		t.Fatal("expected not ok for four-part source")
	}
}

func TestParseRegistrySource_EmptyPart(t *testing.T) {
	_, _, ok := ParseRegistrySource("a//b/c")
	if ok {
		t.Fatal("expected not ok for empty part")
	}
}

func TestParseRegistrySource_Empty(t *testing.T) {
	_, _, ok := ParseRegistrySource("")
	if ok {
		t.Fatal("expected not ok for empty source")
	}
}

func TestRegistryClient_ListVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/modules/terraform-aws-modules/rds/aws/versions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"6.13.1"},{"version":"6.12.0"},{"version":"5.0.0"}]}]}`))
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	versions, err := rc.ListVersions(RegistryModule{Namespace: "terraform-aws-modules", Name: "rds", Provider: "aws"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions))
	}
	if versions[0] != "6.13.1" {
		t.Errorf("expected first version 6.13.1, got %s", versions[0])
	}
}

func TestRegistryClient_ListVersions_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	_, err := rc.ListVersions(RegistryModule{Namespace: "nope", Name: "nope", Provider: "nope"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRegistryClient_ListVersions_EmptyModules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[]}`))
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	versions, err := rc.ListVersions(RegistryModule{Namespace: "a", Name: "b", Provider: "c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("expected 0 versions, got %d", len(versions))
	}
}

func TestRegistryClient_ListVersions_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	_, err := rc.ListVersions(RegistryModule{Namespace: "a", Name: "b", Provider: "c"})
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestRegistryClient_GetDownloadURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/modules/terraform-aws-modules/rds/aws/6.13.1/download" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("X-Terraform-Get", "git::https://github.com/terraform-aws-modules/terraform-aws-rds?ref=v6.13.1")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	url, err := rc.GetDownloadURL(RegistryModule{Namespace: "terraform-aws-modules", Name: "rds", Provider: "aws"}, "6.13.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "git::https://github.com/terraform-aws-modules/terraform-aws-rds?ref=v6.13.1" {
		t.Errorf("unexpected URL: %s", url)
	}
}

func TestRegistryClient_GetDownloadURL_NoHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	_, err := rc.GetDownloadURL(RegistryModule{Namespace: "a", Name: "b", Provider: "c"}, "1.0.0")
	if err == nil {
		t.Fatal("expected error for missing header")
	}
}

func TestRegistryClient_GetDownloadURL_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	_, err := rc.GetDownloadURL(RegistryModule{Namespace: "a", Name: "b", Provider: "c"}, "1.0.0")
	if err == nil {
		t.Fatal("expected error for server error")
	}
}

func TestRegistryClient_ResolveVersion_NoConstraint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"5.0.0"},{"version":"6.13.1"},{"version":"6.12.0"}]}]}`))
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	version, err := rc.ResolveVersion(RegistryModule{Namespace: "a", Name: "b", Provider: "c"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "6.13.1" {
		t.Errorf("expected 6.13.1, got %s", version)
	}
}

func TestRegistryClient_ResolveVersion_PessimisticConstraint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"5.0.0"},{"version":"6.13.1"},{"version":"6.12.0"},{"version":"7.0.0"}]}]}`))
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	version, err := rc.ResolveVersion(RegistryModule{Namespace: "a", Name: "b", Provider: "c"}, "~> 6.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "6.13.1" {
		t.Errorf("expected 6.13.1, got %s", version)
	}
}

func TestRegistryClient_ResolveVersion_ExactConstraint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"5.0.0"},{"version":"6.13.1"},{"version":"6.12.0"}]}]}`))
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	version, err := rc.ResolveVersion(RegistryModule{Namespace: "a", Name: "b", Provider: "c"}, "6.12.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "6.12.0" {
		t.Errorf("expected 6.12.0, got %s", version)
	}
}

func TestRegistryClient_ResolveVersion_NoMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"5.0.0"}]}]}`))
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	_, err := rc.ResolveVersion(RegistryModule{Namespace: "a", Name: "b", Provider: "c"}, "~> 6.0")
	if err == nil {
		t.Fatal("expected error for no matching version")
	}
}

func TestRegistryClient_ResolveVersion_NoVersions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[]}]}`))
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	_, err := rc.ResolveVersion(RegistryModule{Namespace: "a", Name: "b", Provider: "c"}, "")
	if err == nil {
		t.Fatal("expected error for no versions")
	}
}

func TestMatchesConstraint_GreaterOrEqual(t *testing.T) {
	if !matchesConstraint("6.13.1", ">= 6.0.0") {
		t.Error("expected 6.13.1 >= 6.0.0")
	}
	if matchesConstraint("5.9.9", ">= 6.0.0") {
		t.Error("expected 5.9.9 not >= 6.0.0")
	}
}

func TestMatchesConstraint_LessThan(t *testing.T) {
	if !matchesConstraint("5.9.9", "< 6.0.0") {
		t.Error("expected 5.9.9 < 6.0.0")
	}
	if matchesConstraint("6.0.0", "< 6.0.0") {
		t.Error("expected 6.0.0 not < 6.0.0")
	}
}

func TestMatchesConstraint_LessOrEqual(t *testing.T) {
	if !matchesConstraint("6.0.0", "<= 6.0.0") {
		t.Error("expected 6.0.0 <= 6.0.0")
	}
	if matchesConstraint("6.0.1", "<= 6.0.0") {
		t.Error("expected 6.0.1 not <= 6.0.0")
	}
}

func TestMatchesConstraint_NotEqual(t *testing.T) {
	if !matchesConstraint("6.0.1", "!= 6.0.0") {
		t.Error("expected 6.0.1 != 6.0.0")
	}
	if matchesConstraint("6.0.0", "!= 6.0.0") {
		t.Error("expected 6.0.0 not != 6.0.0")
	}
}

func TestMatchesConstraint_GreaterThan(t *testing.T) {
	if !matchesConstraint("6.0.1", "> 6.0.0") {
		t.Error("expected 6.0.1 > 6.0.0")
	}
	if matchesConstraint("6.0.0", "> 6.0.0") {
		t.Error("expected 6.0.0 not > 6.0.0")
	}
}

func TestMatchesConstraint_Pessimistic(t *testing.T) {
	if !matchesConstraint("6.13.1", "~> 6.0") {
		t.Error("expected 6.13.1 ~> 6.0")
	}
	if matchesConstraint("7.0.0", "~> 6.0") {
		t.Error("expected 7.0.0 not ~> 6.0")
	}
	if !matchesConstraint("6.0.5", "~> 6.0.0") {
		t.Error("expected 6.0.5 ~> 6.0.0")
	}
	if matchesConstraint("6.1.0", "~> 6.0.0") {
		t.Error("expected 6.1.0 not ~> 6.0.0")
	}
}

func TestMatchesConstraint_Comma(t *testing.T) {
	if !matchesConstraint("6.5.0", ">= 6.0.0, < 7.0.0") {
		t.Error("expected 6.5.0 in range >= 6.0.0, < 7.0.0")
	}
	if matchesConstraint("7.0.0", ">= 6.0.0, < 7.0.0") {
		t.Error("expected 7.0.0 not in range >= 6.0.0, < 7.0.0")
	}
}

func TestMatchesConstraint_EmptyConstraint(t *testing.T) {
	if !matchesConstraint("6.0.0", "") {
		t.Error("expected empty constraint to match anything")
	}
}

func TestMatchesPessimistic_TwoPartConstraint(t *testing.T) {
	if !matchesPessimistic("6.99.0", "6.0") {
		t.Error("expected 6.99.0 matches ~> 6.0")
	}
	if matchesPessimistic("7.0.0", "6.0") {
		t.Error("expected 7.0.0 does not match ~> 6.0")
	}
}

func TestMatchesPessimistic_ThreePartConstraint(t *testing.T) {
	if !matchesPessimistic("6.0.9", "6.0.0") {
		t.Error("expected 6.0.9 matches ~> 6.0.0")
	}
	if matchesPessimistic("6.1.0", "6.0.0") {
		t.Error("expected 6.1.0 does not match ~> 6.0.0")
	}
}

func TestMatchesPessimistic_VersionTooShort(t *testing.T) {
	if matchesPessimistic("6", "6.0.0") {
		t.Error("expected short version to not match longer constraint")
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "2.0.0", -1},
		{"1.1.0", "1.0.0", 1},
		{"1.0.1", "1.0.0", 1},
		{"1.0", "1.0.0", 0},
		{"v1.0.0", "1.0.0", 0},
	}
	for _, tt := range tests {
		got := compareVersions(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	parts := parseVersion("v6.13.1")
	if len(parts) != 3 || parts[0] != 6 || parts[1] != 13 || parts[2] != 1 {
		t.Errorf("unexpected: %v", parts)
	}
}

func TestRegistryClient_TokenForHost(t *testing.T) {
	rc := NewRegistryClient()
	os.Setenv("TF_TOKEN_app_terraform_io", "my-token")
	defer os.Unsetenv("TF_TOKEN_app_terraform_io")

	token := rc.tokenForHost("app.terraform.io")
	if token != "my-token" {
		t.Errorf("expected my-token, got %q", token)
	}
}

func TestRegistryClient_TokenForHost_NoToken(t *testing.T) {
	rc := NewRegistryClient()
	token := rc.tokenForHost("no-such-host.example.com")
	if token != "" {
		t.Errorf("expected empty, got %q", token)
	}
}

func TestRegistryClient_HostFromURL(t *testing.T) {
	rc := NewRegistryClient()
	host := rc.hostFromURL("https://registry.terraform.io/v1/modules")
	if host != "registry.terraform.io" {
		t.Errorf("expected registry.terraform.io, got %q", host)
	}
}

func TestRegistryClient_HostFromURL_NoPath(t *testing.T) {
	rc := NewRegistryClient()
	host := rc.hostFromURL("https://example.com")
	if host != "example.com" {
		t.Errorf("expected example.com, got %q", host)
	}
}

func TestRegistryClient_ListVersions_WithAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %q", auth)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"1.0.0"}]}]}`))
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	host := rc.hostFromURL(server.URL)
	envKey := "TF_TOKEN_" + strings.ReplaceAll(strings.ReplaceAll(host, ".", "_"), "-", "__")
	os.Setenv(envKey, "test-token")
	defer os.Unsetenv(envKey)

	_, err := rc.ListVersions(RegistryModule{Namespace: "a", Name: "b", Provider: "c"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewRegistryClient(t *testing.T) {
	rc := NewRegistryClient()
	if rc.baseURL != "https://registry.terraform.io" {
		t.Errorf("unexpected baseURL: %s", rc.baseURL)
	}
	if rc.httpClient == nil {
		t.Error("expected non-nil httpClient")
	}
}

func TestParseRegistrySource_EmptyPartInThreePart(t *testing.T) {
	_, _, ok := ParseRegistrySource("/a/b")
	if ok {
		t.Fatal("expected not ok for source with empty first part")
	}
}

func TestRegistryClient_ListVersions_RequestError(t *testing.T) {
	rc := &RegistryClient{
		httpClient: http.DefaultClient,
		baseURL:    "http://127.0.0.1:1",
	}
	_, err := rc.ListVersions(RegistryModule{Namespace: "a", Name: "b", Provider: "c"})
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestRegistryClient_ListVersions_BodyReadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	_, err := rc.ListVersions(RegistryModule{Namespace: "a", Name: "b", Provider: "c"})
	if err == nil {
		t.Fatal("expected error for body read failure")
	}
}

func TestRegistryClient_GetDownloadURL_RequestError(t *testing.T) {
	rc := &RegistryClient{
		httpClient: http.DefaultClient,
		baseURL:    "http://127.0.0.1:1",
	}
	_, err := rc.GetDownloadURL(RegistryModule{Namespace: "a", Name: "b", Provider: "c"}, "1.0.0")
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestMatchesSingleConstraint_ExplicitEquals(t *testing.T) {
	if !matchesSingleConstraint("6.0.0", "= 6.0.0") {
		t.Error("expected = 6.0.0 to match 6.0.0")
	}
	if matchesSingleConstraint("6.0.1", "= 6.0.0") {
		t.Error("expected = 6.0.0 to not match 6.0.1")
	}
}

func TestMatchesSingleConstraint_UnknownOperator(t *testing.T) {
	if matchesSingleConstraint("1.0.0", "% 1.0.0") {
		t.Error("expected unknown operator to return false")
	}
	if matchesSingleConstraint("1.0.0", "1>0.0") {
		t.Error("expected malformed constraint with embedded operator to return false")
	}
}

func TestMatchesPessimistic_PatchConstraint(t *testing.T) {
	if !matchesPessimistic("1.2.5", "1.2.3") {
		t.Error("expected 1.2.5 to match ~> 1.2.3")
	}
	if matchesPessimistic("1.3.0", "1.2.3") {
		t.Error("expected 1.3.0 to not match ~> 1.2.3")
	}
}

func TestMatchesPessimistic_VersionFewerPartsThanConstraint(t *testing.T) {
	if matchesPessimistic("1.2", "1.2.3") {
		t.Error("expected short version 1.2 to not match longer constraint 1.2.3")
	}
}

func TestMatchesPessimistic_SinglePartConstraint(t *testing.T) {
	if !matchesPessimistic("6", "6") {
		t.Error("expected 6 to match ~> 6 (single-part)")
	}
	if matchesPessimistic("7", "6") {
		t.Error("expected 7 to not match ~> 6 (single-part)")
	}
}

func TestMatchesPessimistic_ExceedsUpperBound(t *testing.T) {
	if matchesPessimistic("7.0.0", "6.0.0") {
		t.Error("expected 7.0.0 to not match ~> 6.0.0 (exceeds upper bound)")
	}
}

func TestRegistryClient_ListVersions_BadURL(t *testing.T) {
	rc := &RegistryClient{
		httpClient: http.DefaultClient,
		baseURL:    "http://\x00invalid",
	}
	_, err := rc.ListVersions(RegistryModule{Namespace: "a", Name: "b", Provider: "c"})
	if err == nil {
		t.Fatal("expected error for bad URL")
	}
}

func TestRegistryClient_GetDownloadURL_BadURL(t *testing.T) {
	rc := &RegistryClient{
		httpClient: http.DefaultClient,
		baseURL:    "http://\x00invalid",
	}
	_, err := rc.GetDownloadURL(RegistryModule{Namespace: "a", Name: "b", Provider: "c"}, "1.0.0")
	if err == nil {
		t.Fatal("expected error for bad URL in GetDownloadURL")
	}
}

func TestRegistryClient_GetDownloadURL_WithAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-dl-token" {
			t.Errorf("expected Bearer test-dl-token, got %q", auth)
		}
		w.Header().Set("X-Terraform-Get", "git::https://github.com/example/repo?ref=v1.0.0")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	host := rc.hostFromURL(server.URL)
	envKey := "TF_TOKEN_" + strings.ReplaceAll(strings.ReplaceAll(host, ".", "_"), "-", "__")
	os.Setenv(envKey, "test-dl-token")
	defer os.Unsetenv(envKey)

	url, err := rc.GetDownloadURL(RegistryModule{Namespace: "a", Name: "b", Provider: "c"}, "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url == "" {
		t.Error("expected non-empty URL")
	}
}

func TestRegistryClient_GetDownloadURL_Redirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Terraform-Get", "git::https://github.com/example/repo?ref=v1.0.0")
		w.Header().Set("Location", "https://example.com/redirect")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	url, err := rc.GetDownloadURL(RegistryModule{Namespace: "a", Name: "b", Provider: "c"}, "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "git::https://github.com/example/repo?ref=v1.0.0" {
		t.Errorf("unexpected URL: %s", url)
	}
}

func TestRegistryClient_ResolveVersion_CommaConstraint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"modules":[{"versions":[{"version":"5.0.0"},{"version":"6.5.0"},{"version":"7.0.0"}]}]}`))
	}))
	defer server.Close()

	rc := &RegistryClient{httpClient: server.Client(), baseURL: server.URL}
	version, err := rc.ResolveVersion(RegistryModule{Namespace: "a", Name: "b", Provider: "c"}, ">= 6.0.0, < 7.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "6.5.0" {
		t.Errorf("expected 6.5.0, got %s", version)
	}
}
