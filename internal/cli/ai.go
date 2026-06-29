package cli

// l4 ai routes a coding agent (Claude Code) through the LevelFour tiering gateway, which downgrades
// the safe turns to a cheaper model and measures the bill-grounded saving. The gateway is a
// separate, privately distributed binary (the data plane); this command only launches it and points
// the agent at it. No routing or pricing logic lives in this public CLI.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/LevelFourAI/levelfour-cli/internal/config"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	agentClaude       = "claude"
	defaultGatewayBin = "l4-gateway"
	policyPath        = "/api/v1/ai-spend/policy"
	savingsPath       = "/api/v1/ai-spend/savings"
)

// errNotEntitled means the control plane returned 403: the org has no active ai_spend module.
var errNotEntitled = errors.New("AI Spend is not enabled for your organization")

// readyAttempts/readyInterval bound the wait for the gateway to start listening. They are vars so
// tests can shrink them.
var (
	readyAttempts = 50
	readyInterval = 100 * time.Millisecond
)

// Test seams: swapped in unit tests so the command logic runs without real processes, sockets, or
// the network.
var (
	lookGatewayFn = lookGateway
	freePortFn    = freePort
	startGwFn     = startGateway
	waitReadyFn   = waitReady
	runAgentFn    = runAgent
	aiHTTPGetFn   = aiHTTPGet
	execCommand   = exec.Command
	lookPathFn    = exec.LookPath
	listenFn      = net.Listen
	sleepFn       = time.Sleep
	randomNonceFn = randomNonce
	healthCheckFn = healthOK
)

var aiCmd = &cobra.Command{
	Use:   "ai",
	Short: "Cut your AI coding bill by routing through the LevelFour gateway",
	Long: "LevelFour AI Spend routes Claude Code through a local gateway that cuts your token bill " +
		"on the turns that do not need the frontier model, and measures the verified savings. The " +
		"gateway runs entirely on your machine; this CLI only launches it.",
}

var aiRunCmd = &cobra.Command{
	Use:   "run <agent> [agent args...]",
	Short: "Run a coding agent through the tiering gateway",
	Long: "Starts the gateway, points the agent at it via ANTHROPIC_BASE_URL, runs the agent, and " +
		"stops the gateway on exit. If the gateway binary is not installed it runs the agent directly " +
		"(fail-open), so your work never blocks on us.",
	Args: cobra.MinimumNArgs(1),
	RunE: runAIRun,
}

func runAIRun(_ *cobra.Command, args []string) error {
	agent := args[0]
	if agent != agentClaude {
		return fmt.Errorf("unsupported agent %q (only %q is supported)", agent, agentClaude)
	}
	rest := args[1:]

	bin, err := lookGatewayFn()
	if err != nil {
		output.Info("Installing the LevelFour gateway (first run)...")
		bin, err = installGatewayFn()
		if err != nil {
			output.Warning("could not install the gateway (" + err.Error() + "); running " + agent + " without tiering")
			return runAgentFn(agent, rest)
		}
		output.Success("Gateway installed at " + bin)
	}

	port, err := freePortFn()
	if err != nil {
		return err
	}
	// Hand the gateway a per-run token and require the port to echo it back, so a local process
	// that raced for the port cannot impersonate the gateway and capture the Anthropic key.
	nonce := randomNonceFn()
	_ = os.Setenv("L4_HEALTH_TOKEN", nonce)
	stop, err := startGwFn(bin, port, config.ResolveAPI(flagAPI), tokenOrEmpty())
	if err != nil {
		return err
	}
	defer stop()

	addr := "127.0.0.1:" + port
	if err := waitReadyFn(addr, nonce); err != nil {
		output.Warning("gateway did not start (" + err.Error() + "); running " + agent + " without tiering")
		return runAgentFn(agent, rest)
	}

	_ = os.Setenv("ANTHROPIC_BASE_URL", "http://"+addr)
	output.Success("Routing " + agent + " through the LevelFour gateway (http://" + addr + ")")
	return runAgentFn(agent, rest)
}

var aiReportCmd = &cobra.Command{
	Use:   "report",
	Short: "Show the bill-grounded tiering savings from the control plane",
	RunE:  runAIReport,
}

type aiSavings struct {
	Data struct {
		TieringSavingsUSD          float64 `json:"tiering_savings_usd"`
		TieringProjectedSavingsUSD float64 `json:"tiering_projected_savings_usd"`
		TieringTreatmentRequests   int     `json:"tiering_treatment_requests"`
		TieringHoldoutRequests     int     `json:"tiering_holdout_requests"`
		TieringBasis               string  `json:"tiering_basis"`
	} `json:"data"`
}

func runAIReport(_ *cobra.Command, _ []string) error {
	body, err := aiHTTPGetFn(config.ResolveAPI(flagAPI)+savingsPath, tokenOrEmpty())
	if errors.Is(err, errNotEntitled) {
		output.Info("AI Spend is not enabled for your organization. Contact sales to turn it on.")
		return nil
	}
	if err != nil {
		return err
	}
	var s aiSavings
	if err := json.Unmarshal(body, &s); err != nil {
		return fmt.Errorf("unexpected response from the control plane: %w", err)
	}
	d := s.Data
	output.Header("LevelFour AI Spend")
	output.KeyValue("Verified savings", "$"+strconv.FormatFloat(d.TieringSavingsUSD, 'f', 2, 64))
	output.KeyValue("Projected (shadow)", "$"+strconv.FormatFloat(d.TieringProjectedSavingsUSD, 'f', 2, 64))
	output.KeyValue("Downgraded requests", strconv.Itoa(d.TieringTreatmentRequests))
	output.KeyValue("Control requests", strconv.Itoa(d.TieringHoldoutRequests))
	output.KeyValue("Basis", d.TieringBasis)
	return nil
}

var aiStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show whether the gateway and control plane are reachable",
	RunE:  runAIStatus,
}

func runAIStatus(_ *cobra.Command, _ []string) error {
	output.Header("AI Spend status")
	if bin, err := lookGatewayFn(); err != nil {
		output.KeyValue("Gateway", output.StatusBadge("not installed"))
	} else {
		output.KeyValue("Gateway", bin)
	}
	cpURL := config.ResolveAPI(flagAPI)
	switch _, err := aiHTTPGetFn(cpURL+policyPath, tokenOrEmpty()); {
	case errors.Is(err, errNotEntitled):
		output.KeyValue("AI Spend", output.StatusBadge("not enabled"))
		output.Info("AI Spend is not enabled for your organization. Contact sales to turn it on.")
	case err != nil:
		output.KeyValue("Control plane", output.StatusBadge("unreachable"))
	default:
		output.KeyValue("Control plane", cpURL)
	}
	return nil
}

// tokenOrEmpty returns the resolved API token, or empty when not authenticated. The gateway runs
// with default policy and no telemetry when the key is empty, so a local pilot needs no login.
func tokenOrEmpty() string {
	key, _ := resolveToken()
	return key
}

func lookGateway() (string, error) {
	if bin := os.Getenv("L4_GATEWAY_BIN"); bin != "" {
		return bin, nil
	}
	if p, err := lookPathFn(defaultGatewayBin); err == nil {
		return p, nil
	}
	cached := gatewayCachePathFn()
	if _, err := statFn(cached); err == nil {
		return cached, nil
	}
	return "", errors.New("gateway not installed")
}

func freePort() (string, error) {
	l, err := listenFn("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer func() { _ = l.Close() }()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port, nil
}

func startGateway(bin, port, controlPlaneURL, token string) (func(), error) {
	cmd := execCommand(bin)
	cmd.Env = append(os.Environ(),
		"L4_PORT="+port,
		"L4_CONTROL_PLANE_URL="+controlPlaneURL,
		"L4_TENANT_KEY="+token,
	)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }, nil
}

func waitReady(addr, token string) error {
	for i := 0; i < readyAttempts; i++ {
		if healthCheckFn(addr, token) {
			return nil
		}
		sleepFn(readyInterval)
	}
	return fmt.Errorf("gateway did not confirm at %s", addr)
}

// healthOK reports whether /healthz at addr echoes the expected per-run token. A bare TCP listener
// or any process that does not know the token fails this, which is what defeats the port race.
func healthOK(addr, token string) bool {
	body, err := aiHTTPGet("http://"+addr+"/healthz", "")
	return err == nil && strings.TrimSpace(string(body)) == token
}

func randomNonce() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func runAgent(name string, args []string) error {
	path, err := lookPathFn(name)
	if err != nil {
		return fmt.Errorf("%s not found on PATH: %w", name, err)
	}
	c := execCommand(path, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

func aiHTTPGet(url, key string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if key != "" {
		req.Header.Set("x-api-key", key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusForbidden {
		return nil, errNotEntitled
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("control plane returned status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

const groupAI = "ai"

func init() {
	aiCmd.AddCommand(aiRunCmd, aiReportCmd, aiStatusCmd, aiInstallCmd)
	rootCmd.AddGroup(&cobra.Group{ID: groupAI, Title: "AI Spend:"})
	aiCmd.GroupID = groupAI
	rootCmd.AddCommand(aiCmd)
}
