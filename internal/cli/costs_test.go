package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kr "github.com/zalando/go-keyring"
)

func costsSummaryServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/costs/summary"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"provider_id":                  "aws",
					"provider_name":                "AWS",
					"monthly_spending":             1234.56,
					"monthly_spending_percentage":  8.3,
					"forecasted_monthly_costs":     1500.00,
					"potential_savings":            200.00,
					"potential_savings_percentage": 16.2,
					"top_services": []interface{}{
						map[string]interface{}{"service": "EC2", "cost": 500.00, "change_percentage": 5.0},
						map[string]interface{}{"service": "RDS", "cost": 300.00, "change_percentage": -2.0},
					},
				},
			})
		case strings.Contains(r.URL.Path, "/recommendations/audit/summary"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"total_monthly_savings": 200.00,
					"total_annual_savings":  2400.00,
				},
			})
		default:
			w.WriteHeader(404)
		}
	}))
}

func costsBreakdownServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/costs/breakdown"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"provider_id":       "aws",
					"provider_name":     "AWS",
					"period":            "2026-04",
					"start_date":        "2026-04-01",
					"end_date":          "2026-04-30",
					"total_period_cost": 800.00,
					"items": []interface{}{
						map[string]interface{}{
							"service":           "EC2",
							"cost":              500.00,
							"previous_cost":     475.00,
							"change_percentage": 5.2,
						},
						map[string]interface{}{
							"service":           "RDS",
							"cost":              300.00,
							"previous_cost":     306.00,
							"change_percentage": -2.1,
						},
					},
					"pagination": map[string]interface{}{
						"current_page": 1,
						"total_pages":  1,
						"total_items":  2,
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

func TestCostsSummaryUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, _, err := executeCommand(t, "costs", "summary")
	if err == nil {
		t.Error("expected error when not authenticated")
	}
}

func TestCostsBreakdownUnauthenticated(t *testing.T) {
	kr.MockInit()
	flagToken = ""
	t.Setenv("LEVELFOUR_TOKEN", "")
	defer resetFlags()

	_, _, err := executeCommand(t, "costs", "breakdown")
	if err == nil {
		t.Error("expected error when not authenticated")
	}
}

func TestCostsSummarySpendingError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/costs/summary"):
			w.WriteHeader(500)
			w.Write([]byte("spending error"))
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{"total_monthly_savings": 100.0},
			})
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "costs", "summary")
	if err == nil {
		t.Error("expected error when spending API fails")
	}
}

func TestCostsSummarySavingsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.Contains(r.URL.Path, "/recommendations/audit/summary"):
			w.WriteHeader(500)
			w.Write([]byte("savings error"))
		default:
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"provider_id":      "aws",
					"provider_name":    "AWS",
					"monthly_spending": 100.0,
				},
			})
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "costs", "summary")
	if err == nil {
		t.Error("expected error when savings API fails")
	}
}

func TestCostsBreakdownAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/providers" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
			return
		}
		w.WriteHeader(500)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "costs", "breakdown")
	if err == nil {
		t.Error("expected error when API fails")
	}
}

func TestCostsSummary(t *testing.T) {
	srv := costsSummaryServer()
	defer srv.Close()

	t.Run("table output", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "costs", "summary")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "This Month") {
			t.Errorf("output missing 'This Month': %q", got)
		}
		if !strings.Contains(got, "1234.56") {
			t.Errorf("output missing monthly value: %q", got)
		}
		if !strings.Contains(got, "Top Services") {
			t.Errorf("output missing 'Top Services': %q", got)
		}
	})

	t.Run("json output", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "costs", "summary", "--json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "provider") || !strings.Contains(got, "savings") {
			t.Errorf("JSON output missing keys: %q", got)
		}
	})
}

func TestCostsBreakdown(t *testing.T) {
	srv := costsBreakdownServer()
	defer srv.Close()

	t.Run("table output", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "costs", "breakdown")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "EC2") || !strings.Contains(got, "RDS") {
			t.Errorf("output missing services: %q", got)
		}
		if !strings.Contains(got, "500.00") {
			t.Errorf("output missing dollar amount: %q", got)
		}
		if !strings.Contains(got, "Period Total") {
			t.Errorf("output missing 'Period Total' KPI: %q", got)
		}
	})

	t.Run("json output", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "costs", "breakdown", "--json")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "items") {
			t.Errorf("JSON output missing items: %q", got)
		}
	})

	t.Run("empty results", func(t *testing.T) {
		emptySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/api/v1/providers":
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": []interface{}{
						map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
					},
				})
			case strings.HasSuffix(r.URL.Path, "/costs/breakdown"):
				json.NewEncoder(w).Encode(map[string]interface{}{
					"data": map[string]interface{}{
						"provider_id":       "aws",
						"provider_name":     "AWS",
						"period":            "2026-04",
						"start_date":        "2026-04-01",
						"end_date":          "2026-04-30",
						"total_period_cost": 0.0,
						"items":             []interface{}{},
						"pagination": map[string]interface{}{
							"current_page": 1,
							"total_pages":  1,
							"total_items":  0,
							"page_size":    20,
							"has_next":     false,
							"has_previous": false,
						},
					},
				})
			}
		}))
		defer emptySrv.Close()

		flagAPI = emptySrv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "costs", "breakdown")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		if !strings.Contains(got, "No cost breakdown data found") {
			t.Errorf("expected empty message, got %q", got)
		}
	})
}

func TestCostsSummaryWebFlag(t *testing.T) {
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

	_, _, err := executeCommand(t, "costs", "summary", "--web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(openedURL, "/spending") {
		t.Errorf("expected /spending URL, got %q", openedURL)
	}
}

func TestCostsBreakdownWebFlag(t *testing.T) {
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

	_, _, err := executeCommand(t, "costs", "breakdown", "--web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(openedURL, "/spending") {
		t.Errorf("expected /spending URL, got %q", openedURL)
	}
}

func TestCostsBreakdownCSVFormat(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/costs/breakdown"):
			gotPath = r.URL.String()
			if r.URL.Query().Get("format") != "csv" {
				t.Errorf("expected format=csv, got %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "text/csv")
			w.Write([]byte("Service,Cost\nEC2,500.00\n"))
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "costs", "breakdown", "--format", "csv")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotPath, "/providers/aws/costs/breakdown") {
		t.Errorf("CSV path should hit per-provider endpoint, got %q", gotPath)
	}
	got := outBuf.String()
	if !strings.Contains(got, "EC2") {
		t.Errorf("CSV output missing EC2: %q", got)
	}
}

func TestCostsBreakdownCSVAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/providers" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
			return
		}
		w.WriteHeader(500)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "costs", "breakdown", "--format", "csv")
	if err == nil {
		t.Error("expected error for CSV API error")
	}
}

func TestCostsBreakdownWithFlags(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/costs/breakdown"):
			gotPath = r.URL.String()
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"provider_id":       "aws",
					"provider_name":     "AWS",
					"period":            "2026-01",
					"start_date":        "2026-01-01",
					"end_date":          "2026-01-31",
					"total_period_cost": 500.00,
					"items": []interface{}{
						map[string]interface{}{"service": "EC2", "cost": 500.00, "change_percentage": 5.2},
					},
					"pagination": map[string]interface{}{"current_page": 1, "total_pages": 1, "total_items": 1, "page_size": 20, "has_next": false, "has_previous": false},
				},
			})
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "costs", "breakdown",
		"--start", "2026-01-01",
		"--end", "2026-01-31",
		"--preset", "30D",
		"--granularity", "daily",
		"--group-by", "service",
		"--group-by", "region",
		"--sort-by", "cost",
		"--sort-order", "desc",
		"--provider", "aws",
		"--service", "EC2",
		"--environment", "prod",
		"--account", "acct-1",
		"--region", "us-east-1",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{
		"start=2026-01-01",
		"end=2026-01-31",
		"preset=30D",
		"granularity=daily",
		"group_by=service",
		"group_by=region",
		"sort_by=cost",
		"sort_order=desc",
		"service=EC2",
		"environment=prod",
		"account_id=acct-1",
		"region=us-east-1",
	} {
		if !strings.Contains(gotPath, want) {
			t.Errorf("missing query param %q in: %q", want, gotPath)
		}
	}
}

func TestCostsBreakdownGroupByTagRequiresKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/providers" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "costs", "breakdown", "--group-by", "tag")
	if err == nil {
		t.Error("expected error when group-by=tag without --tag-key")
	} else if !strings.Contains(err.Error(), "tag-key") {
		t.Errorf("expected tag-key error, got %v", err)
	}
}

func TestCostsBreakdownInvalidPreset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/providers" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	_, _, err := executeCommand(t, "costs", "breakdown", "--preset", "invalid")
	if err == nil {
		t.Error("expected error for invalid preset")
	}
}

func TestCostsDaily(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/costs/daily/breakdown") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"start_date": "2026-04-01",
					"end_date":   "2026-04-30",
					"total":      1500.00,
					"data_points": []interface{}{
						map[string]interface{}{"date": "2026-04-01", "amount": 50.00},
						map[string]interface{}{"date": "2026-04-02", "amount": 55.00},
					},
				},
			})
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "costs", "daily", "--start", "2026-04-01", "--end", "2026-04-30")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "2026-04-01") {
		t.Errorf("output missing date: %q", got)
	}
	if !strings.Contains(got, "Total") {
		t.Errorf("output missing Total KPI: %q", got)
	}
}

func TestCostsMonthly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/costs/monthly-spending") {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"data_points": []interface{}{
						map[string]interface{}{"month": "2026-03", "amount": 1200.00},
						map[string]interface{}{"month": "2026-04", "amount": 1500.00},
					},
				},
			})
		}
	}))
	defer srv.Close()

	flagAPI = srv.URL
	flagToken = "l4_test_testkey123456789a"
	defer resetFlags()

	outBuf, _, err := executeCommand(t, "costs", "monthly")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := outBuf.String()
	if !strings.Contains(got, "2026-04") {
		t.Errorf("output missing month: %q", got)
	}
	if !strings.Contains(got, "2700.00") {
		t.Errorf("output missing total (1200+1500): %q", got)
	}
}

func TestCostsFilters(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/providers":
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []interface{}{
					map[string]interface{}{"provider_id": "aws", "provider_name": "AWS"},
				},
			})
		case strings.HasSuffix(r.URL.Path, "/costs/filter-options"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"provider_id": "aws",
					"services":    []string{"EC2", "RDS", "S3"},
					"regions":     []string{"us-east-1", "us-west-2"},
					"accounts":    []string{"111", "222"},
					"tag_keys":    []string{"Environment", "Team"},
					"tag_values":  []string{"prod", "staging"},
				},
			})
		}
	}))
	defer srv.Close()

	t.Run("list all dimensions", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "costs", "filters")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		for _, want := range []string{"service", "region", "account", "tag-key", "tag-value"} {
			if !strings.Contains(got, want) {
				t.Errorf("output missing dimension %q: %q", want, got)
			}
		}
	})

	t.Run("single dimension", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		outBuf, _, err := executeCommand(t, "costs", "filters", "service")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := outBuf.String()
		for _, want := range []string{"EC2", "RDS", "S3"} {
			if !strings.Contains(got, want) {
				t.Errorf("output missing service %q: %q", want, got)
			}
		}
	})

	t.Run("unknown dimension", func(t *testing.T) {
		flagAPI = srv.URL
		flagToken = "l4_test_testkey123456789a"
		defer resetFlags()

		_, _, err := executeCommand(t, "costs", "filters", "bogus")
		if err == nil {
			t.Error("expected error for unknown dimension")
		}
	})
}
