package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func commitmentsServer(t *testing.T, status int, body string) *RawClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	client, err := NewRawClient(srv.URL, "l4_test_key", "test")
	if err != nil {
		t.Fatalf("NewRawClient() error: %v", err)
	}
	return client
}

func TestGetCommitmentsDecodesTheEnvelope(t *testing.T) {
	client := commitmentsServer(t, http.StatusOK,
		`{"success":true,"data":{"basis":"net","totals":{"commitment_count":2},"rows":[{"id":"ri-1"}]}}`)

	portfolio, body, err := GetCommitments[CommitmentPortfolio](client, "/portfolio",
		map[string]string{"provider": "aws", "basis": "net"})
	if err != nil {
		t.Fatalf("GetCommitments() error: %v", err)
	}
	if portfolio.Basis != "net" {
		t.Errorf("Basis = %q, want net", portfolio.Basis)
	}
	if portfolio.Totals.CommitmentCount != 2 {
		t.Errorf("CommitmentCount = %d, want 2", portfolio.Totals.CommitmentCount)
	}
	if len(portfolio.Rows) != 1 || portfolio.Rows[0].ID != "ri-1" {
		t.Errorf("Rows = %+v, want one row ri-1", portfolio.Rows)
	}
	if !strings.Contains(string(body), `"basis":"net"`) {
		t.Errorf("body = %s, want the untouched response", body)
	}
}

func TestGetCommitmentsDecodesAListPayload(t *testing.T) {
	client := commitmentsServer(t, http.StatusOK, `{"data":[{"vendor":"acme","latest_metered_usd":12.5}]}`)

	contracts, _, err := GetCommitments[[]CommitmentContract](client, "/contracts", nil)
	if err != nil {
		t.Fatalf("GetCommitments() error: %v", err)
	}
	if len(contracts) != 1 || contracts[0].Vendor != "acme" {
		t.Errorf("contracts = %+v, want one acme contract", contracts)
	}
}

// A null stays a nil pointer so a caller can tell an unmeasured value from zero.
func TestGetCommitmentsKeepsNullDistinctFromZero(t *testing.T) {
	client := commitmentsServer(t, http.StatusOK,
		`{"data":{"instrument":"sp","services":[{"days":[{"date":"2026-09-01","coverage_pct":null,"capacity_pct":0}]}]}}`)

	utilization, _, err := GetCommitments[CommitmentUtilization](client, "/utilization", nil)
	if err != nil {
		t.Fatalf("GetCommitments() error: %v", err)
	}
	day := utilization.Services[0].Days[0]
	if day.CoveragePct != nil {
		t.Errorf("CoveragePct = %v, want nil for an unmeasured coverage", *day.CoveragePct)
	}
	if day.CapacityPct == nil || *day.CapacityPct != 0 {
		t.Errorf("CapacityPct = %v, want a measured zero", day.CapacityPct)
	}
}

func TestGetCommitmentsReportsAMissingSurface(t *testing.T) {
	client := commitmentsServer(t, http.StatusNotFound, `{"detail":"Not Found"}`)

	_, _, err := GetCommitments[CommitmentPortfolio](client, "/portfolio", nil)
	if !errors.Is(err, ErrCommitmentsUnavailable) {
		t.Errorf("err = %v, want ErrCommitmentsUnavailable", err)
	}
}

func TestGetCommitmentsSurfacesAServerError(t *testing.T) {
	client := commitmentsServer(t, http.StatusInternalServerError, "boom")

	_, _, err := GetCommitments[CommitmentPortfolio](client, "/portfolio", nil)
	if err == nil || errors.Is(err, ErrCommitmentsUnavailable) {
		t.Errorf("err = %v, want a server error", err)
	}
}

func TestGetCommitmentsSurfacesUndecodableJSON(t *testing.T) {
	client := commitmentsServer(t, http.StatusOK, "not json")

	_, _, err := GetCommitments[CommitmentPortfolio](client, "/portfolio", nil)
	if err == nil || !strings.Contains(err.Error(), "unexpected response") {
		t.Errorf("err = %v, want an unexpected response error", err)
	}
}

func TestGetCommitmentsSurfacesATransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	client, err := NewRawClient(srv.URL, "l4_test_key", "test")
	if err != nil {
		t.Fatalf("NewRawClient() error: %v", err)
	}
	srv.Close()

	if _, _, err = GetCommitments[CommitmentPortfolio](client, "/portfolio", nil); err == nil {
		t.Error("expected an error when the server is unreachable")
	}
}
