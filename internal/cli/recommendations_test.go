package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	kr "github.com/zalando/go-keyring"
)

func recommendationsListServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/recommendations/overview"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_spend":       31443.84,
					"available_savings": 13287.04,
					"pending_savings":   1900.00,
					"saved_itd":         691.20,
				},
			})
		case strings.HasSuffix(r.URL.Path, "/recommendations"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_savings": 15187.04,
					"items": []interface{}{
						map[string]interface{}{
							"recommendation_id":  "rec-001",
							"service":            "EC2",
							"environment":        "production",
							"account":            "123456789012",
							"tag":                "Squad-Platform",
							"monthly_savings":    150.00,
							"savings_percentage": 30.0,
							"status":             "available",
							"saving_accepted_by": "bruno@levelfour.ai",
						},
						map[string]interface{}{
							"recommendation_id":  "rec-002",
							"service":            "RDS",
							"environment":        "staging",
							"monthly_savings":    250.00,
							"savings_percentage": 45.0,
							"status":             "pending",
						},
					},
					"pagination": map[string]interface{}{
						"current_page": 1,
						"total_pages":  3,
						"total_items":  52,
						"page_size":    20,
						"has_next":     true,
						"has_previous": false,
					},
				},
			})
		default:
			w.WriteHeader(404)
		}
	}))
}

func recommendationsEmptyServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/recommendations/overview"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_spend":       0,
					"available_savings": 0,
					"pending_savings":   0,
					"saved_itd":         0,
				},
			})
		case strings.HasSuffix(r.URL.Path, "/recommendations"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_savings": 0,
					"items":         []interface{}{},
					"pagination": map[string]interface{}{
						"current_page": 1,
						"total_pages":  0,
						"total_items":  0,
						"page_size":    20,
						"has_next":     false,
						"has_previous": false,
					},
				},
			})
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestRecommendationsListUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, _, err := executeCommand(t, "recommendations", "list")
	if err == nil {
		t.Error("expected error when not authenticated")
	}
}

func TestRecommendationsViewUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, _, err := executeCommand(t, "recommendations", "view", "rec-001")
	if err == nil {
		t.Error("expected error when not authenticated")
	}
}

func TestRecommendationsList(t *testing.T) {
	srv := recommendationsListServer()
	defer srv.Close()

	t.Run("table output", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "recommendations", "list")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "rec-001") || !strings.Contains(got, "EC2") {
			t.Errorf("output missing data: %q", got)
		}
		if !strings.Contains(got, "rec-002") || !strings.Contains(got, "RDS") {
			t.Errorf("output missing second recommendation: %q", got)
		}
	})

	t.Run("json output", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "recommendations", "list", "--json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(outBuf.String(), "rec-001") {
			t.Errorf("JSON output missing recommendation data: %q", outBuf.String())
		}
	})

	t.Run("empty results", func(t *testing.T) {
		emptySrv := recommendationsEmptyServer()
		defer emptySrv.Close()

		flagAPI = emptySrv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "recommendations", "list")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(outBuf.String(), "No recommendations found") {
			t.Errorf("expected empty message, got %q", outBuf.String())
		}
	})
}

func TestRecommendationsListAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "recommendations", "list")
	if err == nil {
		t.Error("expected error when API fails")
	}
}

func TestRecommendationsListKPIHeader(t *testing.T) {
	srv := recommendationsListServer()
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "recommendations", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "Total Spend") {
		t.Errorf("output missing Total Spend KPI: %q", got)
	}
	if !strings.Contains(got, "Available Savings") {
		t.Errorf("output missing Available Savings KPI: %q", got)
	}
	if !strings.Contains(got, "Pending Savings") {
		t.Errorf("output missing Pending Savings KPI: %q", got)
	}
	if !strings.Contains(got, "Saved CTD") {
		t.Errorf("output missing Saved CTD KPI: %q", got)
	}
	if !strings.Contains(got, "31443.84") {
		t.Errorf("output missing total spend value: %q", got)
	}
}

func TestRecommendationsListPaginationFooter(t *testing.T) {
	srv := recommendationsListServer()
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "recommendations", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "Page 1 of 3") {
		t.Errorf("output missing pagination info: %q", got)
	}
	if !strings.Contains(got, "52 total") {
		t.Errorf("output missing total items count: %q", got)
	}
	if !strings.Contains(got, "--page 2") {
		t.Errorf("output missing next page hint: %q", got)
	}
}

func TestRecommendationsListPaginationNoHint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/recommendations/overview"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_spend": 1000, "available_savings": 200,
					"pending_savings": 50, "saved_itd": 100,
				},
			})
		case strings.HasSuffix(r.URL.Path, "/recommendations"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_savings": 200,
					"items": []interface{}{
						map[string]interface{}{
							"recommendation_id": "rec-001", "service": "EC2",
							"monthly_savings": 200, "savings_percentage": 20,
							"status": "available",
						},
					},
					"pagination": map[string]interface{}{
						"current_page": 1, "total_pages": 1, "total_items": 1,
						"page_size": 20, "has_next": false, "has_previous": false,
					},
				},
			})
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "recommendations", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if strings.Contains(got, "--page") {
		t.Errorf("should not show next page hint when on last page: %q", got)
	}
}

func TestRecommendationsListProviderFlag(t *testing.T) {
	var mu sync.Mutex
	var requestedPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestedPaths = append(requestedPaths, r.URL.Path)
		mu.Unlock()
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.Contains(r.URL.Path, "/providers/gcp/recommendations/overview"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_spend": 5000, "available_savings": 1000,
					"pending_savings": 200, "saved_itd": 300,
				},
			})
		case strings.Contains(r.URL.Path, "/providers/gcp/recommendations"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_savings": 1000,
					"items": []interface{}{
						map[string]interface{}{
							"recommendation_id": "GCP-001", "service": "Compute",
							"monthly_savings": 1000, "savings_percentage": 20,
							"status": "available",
						},
					},
					"pagination": map[string]interface{}{
						"current_page": 1, "total_pages": 1, "total_items": 1,
						"page_size": 20, "has_next": false, "has_previous": false,
					},
				},
			})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "recommendations", "list", "--provider", "gcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "GCP-001") {
		t.Errorf("output missing GCP recommendation: %q", got)
	}
	for _, p := range requestedPaths {
		if p == "/api/v1/providers" {
			t.Error("should not call /providers when --provider flag is set")
		}
	}
}

func TestRecommendationsListNoProviders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/providers" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{},
			})
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "recommendations", "list")
	if err == nil {
		t.Error("expected error when no providers are connected")
	}
	if err != nil && !strings.Contains(err.Error(), "no providers") {
		t.Errorf("expected 'no providers' error, got: %v", err)
	}
}

func TestRecommendationsListFilterFlags(t *testing.T) {
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/recommendations/overview"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_spend": 1000, "available_savings": 200,
					"pending_savings": 50, "saved_itd": 100,
				},
			})
		case strings.HasSuffix(r.URL.Path, "/recommendations"):
			capturedQuery = r.URL.RawQuery
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_savings": 200,
					"items": []interface{}{
						map[string]interface{}{
							"recommendation_id": "rec-001", "service": "RDS",
							"monthly_savings": 200, "savings_percentage": 20,
							"status": "available",
						},
					},
					"pagination": map[string]interface{}{
						"current_page": 1, "total_pages": 1, "total_items": 1,
						"page_size": 20, "has_next": false, "has_previous": false,
					},
				},
			})
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "recommendations", "list",
		"--service", "RDS",
		"--status", "available",
		"--sort-by", "monthly_savings",
		"--sort-order", "desc",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedQuery, "service=RDS") {
		t.Errorf("query missing service filter: %q", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "display_status=available") {
		t.Errorf("query missing status filter: %q", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "sort_by=monthly_savings") {
		t.Errorf("query missing sort_by: %q", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "sort_order=desc") {
		t.Errorf("query missing sort_order: %q", capturedQuery)
	}
}

func TestRecommendationsListAllFilterFlags(t *testing.T) {
	var capturedQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/recommendations/overview"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_spend": 1000, "available_savings": 200,
					"pending_savings": 50, "saved_itd": 100,
				},
			})
		case strings.HasSuffix(r.URL.Path, "/recommendations"):
			capturedQuery = r.URL.RawQuery
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_savings": 200,
					"items": []interface{}{
						map[string]interface{}{
							"recommendation_id": "rec-001", "service": "RDS",
							"monthly_savings": 200, "savings_percentage": 20,
							"status": "available",
						},
					},
					"pagination": map[string]interface{}{
						"current_page": 1, "total_pages": 1, "total_items": 1,
						"page_size": 20, "has_next": false, "has_previous": false,
					},
				},
			})
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "recommendations", "list",
		"--environment", "production",
		"--account", "123456789012",
		"--tag", "Squad-Platform",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedQuery, "environment=production") {
		t.Errorf("query missing environment filter: %q", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "account=123456789012") {
		t.Errorf("query missing account filter: %q", capturedQuery)
	}
	if !strings.Contains(capturedQuery, "tag=Squad-Platform") {
		t.Errorf("query missing tag filter: %q", capturedQuery)
	}
}

func TestRecommendationsListSearchFilter(t *testing.T) {
	srv := recommendationsListServer()
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "recommendations", "list", "--search", "RDS")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "rec-002") {
		t.Errorf("search should include RDS recommendation: %q", got)
	}
	if strings.Contains(got, "rec-001") {
		t.Errorf("search should exclude EC2 recommendation: %q", got)
	}
}

func TestRecommendationsListSearchHiddenColumn(t *testing.T) {
	srv := recommendationsListServer()
	defer srv.Close()

	origTermWidth := terminalWidth
	terminalWidth = func() int { return 80 }
	defer func() { terminalWidth = origTermWidth }()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "recommendations", "list", "--search", "123456789012")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "rec-001") {
		t.Errorf("search by hidden Account column should still match rec-001: %q", got)
	}
}

func TestRecommendationsListWideColumns(t *testing.T) {
	srv := recommendationsListServer()
	defer srv.Close()

	origTermWidth := terminalWidth
	terminalWidth = func() int { return 200 }
	defer func() { terminalWidth = origTermWidth }()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "recommendations", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "Account") {
		t.Errorf("wide output should contain Account column: %q", got)
	}
	if !strings.Contains(got, "Author") {
		t.Errorf("wide output should contain Author column: %q", got)
	}
	if !strings.Contains(got, "123456789012") {
		t.Errorf("wide output should contain account value: %q", got)
	}
	if !strings.Contains(got, "bruno@levelfour.ai") {
		t.Errorf("wide output should contain author value: %q", got)
	}
}

func TestRecommendationsListNarrowColumns(t *testing.T) {
	srv := recommendationsListServer()
	defer srv.Close()

	origTermWidth := terminalWidth
	terminalWidth = func() int { return 80 }
	defer func() { terminalWidth = origTermWidth }()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "recommendations", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if strings.Contains(got, "Account") {
		t.Errorf("narrow output should not contain Account column: %q", got)
	}
	if strings.Contains(got, "Author") {
		t.Errorf("narrow output should not contain Author column: %q", got)
	}
	if !strings.Contains(got, "rec-001") {
		t.Errorf("narrow output should still contain recommendation ID: %q", got)
	}
	if !strings.Contains(got, "EC2") {
		t.Errorf("narrow output should still contain service: %q", got)
	}
}

func TestRecommendationsListStatusColumn(t *testing.T) {
	srv := recommendationsListServer()
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "recommendations", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "Status") {
		t.Errorf("output should contain Status column header: %q", got)
	}
}

func TestRecommendationsView(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/rec-001/details") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"recommendation_id":    "rec-001",
				"service":              "EC2",
				"resource_type":        "Right Sizing",
				"account":              "123456789012",
				"region":               "us-east-1",
				"environment":          "production",
				"status":               "pending",
				"analysis_period":      "2025-10-24",
				"created_at":           "2025-10-24T10:00:00Z",
				"monthly_savings":      150.00,
				"annual_savings":       1800.00,
				"current_spending":     500.00,
				"savings_percentage":   30.0,
				"resource_console_url": "https://console.aws.amazon.com/ec2",
				"actions": map[string]interface{}{
					"description":            "Rightsize i-12345",
					"key_takeaway":           "**Risk Level:** Low",
					"implementation_process": "## Steps\n\nModify instance type.",
					"execution_method":       "### Terraform\n\n```hcl\nresource \"aws_instance\" {}\n```",
				},
				"comparison_data": []interface{}{
					map[string]interface{}{"label": "Instance Class", "current_value": "m5.2xlarge", "new_value": "m6g.2xlarge"},
					"bad-item",
					map[string]interface{}{"label": "Monthly Cost", "current_value": "$500.00", "new_value": "$350.00"},
				},
				"risk_assessment": map[string]interface{}{
					"level": "LOW",
					"factors": []interface{}{
						map[string]interface{}{"factor": "CPU headroom", "detail": "P99 at 42% leaves ample margin"},
						"bad-factor",
					},
				},
				"implementation_steps": []interface{}{
					map[string]interface{}{"action": "Create snapshot", "detail": "Safety snapshot before resize"},
					42,
					map[string]interface{}{"action": "Modify instance", "detail": "Change to m6g.2xlarge"},
				},
			},
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	t.Run("default output is static", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "recommendations", "view", "rec-001")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "rec-001") {
			t.Errorf("output missing recommendation: %q", got)
		}
		if !strings.Contains(got, "EC2") {
			t.Errorf("output missing service: %q", got)
		}
		if !strings.Contains(got, "500.00") {
			t.Errorf("output missing current spending: %q", got)
		}
		if !strings.Contains(got, "Rightsize i-12345") {
			t.Errorf("output missing description: %q", got)
		}
		if !strings.Contains(got, "Steps") {
			t.Errorf("output missing implementation: %q", got)
		}
		if !strings.Contains(got, "123456789012") {
			t.Errorf("output missing account: %q", got)
		}
		if !strings.Contains(got, "us-east-1") {
			t.Errorf("output missing region: %q", got)
		}
		if !strings.Contains(got, "m5.2xlarge") {
			t.Errorf("output missing comparison current_value: %q", got)
		}
		if !strings.Contains(got, "m6g.2xlarge") {
			t.Errorf("output missing comparison new_value: %q", got)
		}
		if !strings.Contains(got, "LOW") {
			t.Errorf("output missing risk level: %q", got)
		}
		if !strings.Contains(got, "CPU headroom") {
			t.Errorf("output missing risk factor: %q", got)
		}
		if !strings.Contains(got, "Create snapshot") {
			t.Errorf("output missing implementation step: %q", got)
		}
	})

	t.Run("json output", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "recommendations", "view", "rec-001", "--json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(outBuf.String(), "rec-001") {
			t.Errorf("JSON output missing recommendation: %q", outBuf.String())
		}
	})
}

func TestRecommendationsViewStaticByDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"recommendation_id": "rec-001",
				"service":           "EC2",
				"environment":       "production",
				"status":            "pending",
				"current_spending":  500.00,
				"monthly_savings":   150.00,
				"annual_savings":    1800.00,
			},
		})
	}))
	defer srv.Close()

	origIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	defer func() { isTerminal = origIsTerminal }()

	tuiCalled := false
	origRunTUI := runRecommendationViewTUI
	runRecommendationViewTUI = func(data map[string]interface{}) error {
		tuiCalled = true
		return nil
	}
	defer func() { runRecommendationViewTUI = origRunTUI }()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "recommendations", "view", "rec-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tuiCalled {
		t.Error("TUI should NOT be invoked by default (static output is default)")
	}
	got := outBuf.String()
	if !strings.Contains(got, "Recommendation rec-001") {
		t.Errorf("static output missing header: %q", got)
	}
}

func TestRecommendationsViewTUIOptIn(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"recommendation_id": "rec-001",
				"service":           "EC2",
				"environment":       "production",
				"status":            "pending",
			},
		})
	}))
	defer srv.Close()

	origIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	defer func() { isTerminal = origIsTerminal }()

	tuiCalled := false
	origRunTUI := runRecommendationViewTUI
	runRecommendationViewTUI = func(data map[string]interface{}) error {
		tuiCalled = true
		return nil
	}
	defer func() { runRecommendationViewTUI = origRunTUI }()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "recommendations", "view", "rec-001", "--tui")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tuiCalled {
		t.Error("expected TUI to be invoked when --tui is passed")
	}
}

func TestRecommendationsViewJSONNoANSI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"recommendation_id": "rec-001",
				"service":           "EC2",
			},
		})
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "recommendations", "view", "rec-001", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if len(got) == 0 || got[0] != '{' {
		t.Errorf("--json output should start with '{', got %q", got[:min(20, len(got))])
	}
}

func TestRecommendationsViewFormattingPrecedence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"recommendation_id": "rec-001",
				"service":           "EC2",
				"environment":       "production",
				"status":            "pending",
			},
		})
	}))
	defer srv.Close()

	origIsTerminal := isTerminal
	isTerminal = func() bool { return true }
	defer func() { isTerminal = origIsTerminal }()

	tuiCalled := false
	origRunTUI := runRecommendationViewTUI
	runRecommendationViewTUI = func(data map[string]interface{}) error {
		tuiCalled = true
		return nil
	}
	defer func() { runRecommendationViewTUI = origRunTUI }()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "recommendations", "view", "rec-001", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tuiCalled {
		t.Error("TUI should not be invoked when --json is passed")
	}
	if !strings.Contains(outBuf.String(), "rec-001") {
		t.Errorf("JSON output missing data: %q", outBuf.String())
	}
}

func TestRecommendationsViewAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "recommendations", "view", "nonexistent")
	if err == nil {
		t.Error("expected error when recommendation not found")
	}
}

func TestRecommendationsListWebFlag(t *testing.T) {
	origBrowser := openBrowser
	var openedURL string
	openBrowser = func(url string) error {
		openedURL = url
		return nil
	}
	defer func() { openBrowser = origBrowser }()

	flagAPI = "https://api.levelfour.ai"
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "recommendations", "list", "--web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(openedURL, "/savings-recommendations") {
		t.Errorf("expected savings URL, got %q", openedURL)
	}
}

func TestRecommendationsViewWebFlag(t *testing.T) {
	origBrowser := openBrowser
	var openedURL string
	openBrowser = func(url string) error {
		openedURL = url
		return nil
	}
	defer func() { openBrowser = origBrowser }()

	flagAPI = "https://api.levelfour.ai"
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "recommendations", "view", "rec-001", "--web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(openedURL, "rec-001") {
		t.Errorf("expected rec-001 in URL, got %q", openedURL)
	}
}
