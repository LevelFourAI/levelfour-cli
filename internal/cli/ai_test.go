package cli

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"testing"
	"time"
)

// TestAIHelperProcess is the canonical exec stand-in: when invoked as a child with GO_WANT_AI_HELPER
// set, it acts as the "gateway"/"agent" by exiting cleanly.
func TestAIHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_AI_HELPER") != "1" {
		return
	}
	os.Exit(0)
}

func saveAISeams(t *testing.T) {
	t.Helper()
	lg, fp, sg, wr, ra, hg := lookGatewayFn, freePortFn, startGwFn, waitReadyFn, runAgentFn, aiHTTPGetFn
	ec, lp, ln, dl, sl := execCommand, lookPathFn, listenFn, dialFn, sleepFn
	ats, itv := readyAttempts, readyInterval
	t.Cleanup(func() {
		lookGatewayFn, freePortFn, startGwFn, waitReadyFn, runAgentFn, aiHTTPGetFn = lg, fp, sg, wr, ra, hg
		execCommand, lookPathFn, listenFn, dialFn, sleepFn = ec, lp, ln, dl, sl
		readyAttempts, readyInterval = ats, itv
	})
}

func helperCmd(_ string, _ ...string) *exec.Cmd {
	return exec.CommandContext(context.Background(), os.Args[0], "-test.run=TestAIHelperProcess")
}

func TestRunAIRunRejectsOtherAgents(t *testing.T) {
	if err := runAIRun(nil, []string{"cursor"}); err == nil {
		t.Fatal("want error for an unsupported agent")
	}
}

func TestRunAIRunFailsOpenWhenNoGateway(t *testing.T) {
	saveAISeams(t)
	lookGatewayFn = func() (string, error) { return "", errors.New("not found") }
	ran := false
	runAgentFn = func(string, []string) error { ran = true; return nil }
	if err := runAIRun(nil, []string{"claude", "-p", "hi"}); err != nil {
		t.Fatalf("fail-open must not error: %v", err)
	}
	if !ran {
		t.Fatal("the agent must still run without the gateway")
	}
}

func TestRunAIRunFreePortError(t *testing.T) {
	saveAISeams(t)
	lookGatewayFn = func() (string, error) { return "gw", nil }
	freePortFn = func() (string, error) { return "", errors.New("no port") }
	if err := runAIRun(nil, []string{"claude"}); err == nil {
		t.Fatal("want a free-port error")
	}
}

func TestRunAIRunStartError(t *testing.T) {
	saveAISeams(t)
	lookGatewayFn = func() (string, error) { return "gw", nil }
	freePortFn = func() (string, error) { return "1234", nil }
	startGwFn = func(_, _, _, _ string) (func(), error) { return nil, errors.New("boom") }
	if err := runAIRun(nil, []string{"claude"}); err == nil {
		t.Fatal("want a start error")
	}
}

func TestRunAIRunWaitFailsOpen(t *testing.T) {
	saveAISeams(t)
	lookGatewayFn = func() (string, error) { return "gw", nil }
	freePortFn = func() (string, error) { return "1234", nil }
	stopped := false
	startGwFn = func(_, _, _, _ string) (func(), error) { return func() { stopped = true }, nil }
	waitReadyFn = func(string) error { return errors.New("not ready") }
	ran := false
	runAgentFn = func(string, []string) error { ran = true; return nil }
	if err := runAIRun(nil, []string{"claude"}); err != nil {
		t.Fatalf("a wait failure must fail open: %v", err)
	}
	if !ran || !stopped {
		t.Fatalf("agent ran=%v gateway stopped=%v", ran, stopped)
	}
}

func TestRunAIRunSuccess(t *testing.T) {
	saveAISeams(t)
	t.Cleanup(func() { _ = os.Unsetenv("ANTHROPIC_BASE_URL") })
	lookGatewayFn = func() (string, error) { return "gw", nil }
	freePortFn = func() (string, error) { return "1234", nil }
	startGwFn = func(_, _, _, _ string) (func(), error) { return func() {}, nil }
	waitReadyFn = func(string) error { return nil }
	var base string
	runAgentFn = func(string, []string) error { base = os.Getenv("ANTHROPIC_BASE_URL"); return nil }
	if err := runAIRun(nil, []string{"claude"}); err != nil {
		t.Fatalf("success errored: %v", err)
	}
	if base != "http://127.0.0.1:1234" {
		t.Fatalf("ANTHROPIC_BASE_URL = %q", base)
	}
}

func TestRunAIReportHTTPError(t *testing.T) {
	saveAISeams(t)
	aiHTTPGetFn = func(string, string) ([]byte, error) { return nil, errors.New("down") }
	if err := runAIReport(nil, nil); err == nil {
		t.Fatal("want an http error")
	}
}

func TestRunAIReportBadJSON(t *testing.T) {
	saveAISeams(t)
	aiHTTPGetFn = func(string, string) ([]byte, error) { return []byte("not json"), nil }
	if err := runAIReport(nil, nil); err == nil {
		t.Fatal("want a json error")
	}
}

func TestRunAIReportSuccess(t *testing.T) {
	saveAISeams(t)
	aiHTTPGetFn = func(string, string) ([]byte, error) {
		return []byte(`{"data":{"tiering_savings_usd":12.5,"tiering_projected_savings_usd":3,"tiering_treatment_requests":40,"tiering_holdout_requests":7,"tiering_basis":"measured"}}`), nil
	}
	if err := runAIReport(nil, nil); err != nil {
		t.Fatalf("report errored: %v", err)
	}
}

func TestRunAIStatusAllUp(t *testing.T) {
	saveAISeams(t)
	lookGatewayFn = func() (string, error) { return "/usr/local/bin/gw", nil }
	aiHTTPGetFn = func(string, string) ([]byte, error) { return []byte("{}"), nil }
	if err := runAIStatus(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestRunAIStatusAllDown(t *testing.T) {
	saveAISeams(t)
	lookGatewayFn = func() (string, error) { return "", errors.New("nope") }
	aiHTTPGetFn = func(string, string) ([]byte, error) { return nil, errors.New("nope") }
	if err := runAIStatus(nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestLookGatewayEnvOverride(t *testing.T) {
	t.Setenv("L4_GATEWAY_BIN", "/custom/gw")
	got, err := lookGateway()
	if err != nil || got != "/custom/gw" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestLookGatewayPathLookup(t *testing.T) {
	saveAISeams(t)
	t.Setenv("L4_GATEWAY_BIN", "")
	lookPathFn = func(name string) (string, error) {
		if name == defaultGatewayBin {
			return "/found/" + name, nil
		}
		return "", errors.New("no")
	}
	got, err := lookGateway()
	if err != nil || got != "/found/"+defaultGatewayBin {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestFreePortSuccess(t *testing.T) {
	port, err := freePort()
	if err != nil || port == "" {
		t.Fatalf("got %q, %v", port, err)
	}
}

func TestFreePortListenError(t *testing.T) {
	saveAISeams(t)
	listenFn = func(string, string) (net.Listener, error) { return nil, errors.New("no") }
	if _, err := freePort(); err == nil {
		t.Fatal("want a listen error")
	}
}

func TestStartGatewaySuccess(t *testing.T) {
	saveAISeams(t)
	t.Setenv("GO_WANT_AI_HELPER", "1")
	execCommand = helperCmd
	stop, err := startGateway("gw", "1234", "http://cp", "tok")
	if err != nil {
		t.Fatalf("start errored: %v", err)
	}
	stop()
}

func TestStartGatewayStartError(t *testing.T) {
	saveAISeams(t)
	execCommand = func(string, ...string) *exec.Cmd {
		return exec.CommandContext(context.Background(), "/nonexistent/binary/xyz")
	}
	if _, err := startGateway("gw", "1", "cp", "t"); err == nil {
		t.Fatal("want a start error")
	}
}

func TestWaitReadySuccess(t *testing.T) {
	saveAISeams(t)
	dialFn = func(string, string, time.Duration) (net.Conn, error) {
		c, _ := net.Pipe()
		return c, nil
	}
	if err := waitReady("127.0.0.1:1234"); err != nil {
		t.Fatalf("want ready: %v", err)
	}
}

func TestWaitReadyTimeout(t *testing.T) {
	saveAISeams(t)
	readyAttempts = 2
	dialFn = func(string, string, time.Duration) (net.Conn, error) { return nil, errors.New("refused") }
	sleepFn = func(time.Duration) {}
	if err := waitReady("127.0.0.1:1234"); err == nil {
		t.Fatal("want a timeout error")
	}
}

func TestRunAgentLookPathError(t *testing.T) {
	saveAISeams(t)
	lookPathFn = func(string) (string, error) { return "", errors.New("not found") }
	if err := runAgent("claude", nil); err == nil {
		t.Fatal("want a lookpath error")
	}
}

func TestRunAgentRuns(t *testing.T) {
	saveAISeams(t)
	t.Setenv("GO_WANT_AI_HELPER", "1")
	lookPathFn = func(string) (string, error) { return os.Args[0], nil }
	execCommand = helperCmd
	if err := runAgent("claude", []string{"x"}); err != nil {
		t.Fatalf("run errored: %v", err)
	}
}

func TestAIHTTPGetSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	body, err := aiHTTPGet(srv.URL, "k")
	if err != nil || string(body) != "ok" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}

func TestAIHTTPGetNoKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	if _, err := aiHTTPGet(srv.URL, ""); err != nil {
		t.Fatalf("err=%v", err)
	}
}

func TestAIHTTPGetBadURL(t *testing.T) {
	if _, err := aiHTTPGet("http://%zz", "k"); err == nil {
		t.Fatal("want a url error")
	}
}

func TestAIHTTPGetUnreachable(t *testing.T) {
	if _, err := aiHTTPGet("http://127.0.0.1:1", "k"); err == nil {
		t.Fatal("want a dial error")
	}
}

func TestAIHTTPGetNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := aiHTTPGet(srv.URL, "k"); err == nil {
		t.Fatal("want a non-200 error")
	}
}
