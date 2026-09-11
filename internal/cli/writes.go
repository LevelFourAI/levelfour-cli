package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
	"golang.org/x/term"
)

// stdinReader is the source the confirmation prompt reads from. Tests swap it.
var stdinReader io.Reader = os.Stdin

// The answer arrives on stdin, so stdin decides whether anybody can give one. isTerminal reads
// stdout, which answers a different question: whether a TUI has somewhere to draw.
var canPrompt = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// requireApproval refuses a write that nobody can approve, for the two tags commands whose
// blast radius reaches months of already-evaluated spend.
func requireApproval(prompt, unattended string) (bool, error) {
	if flagTagsYes {
		return true, nil
	}
	if !canPrompt() {
		return false, fmt.Errorf("%s outside a terminal needs --yes", unattended)
	}
	return confirmAction(prompt), nil
}

// confirmAction asks the operator to approve a write before it is sent. It
// answers yes without prompting when stdout is not a TTY, so piped and CI runs
// never block waiting on input that will not arrive.
func confirmAction(prompt string) bool {
	if !isTerminal() {
		return true
	}
	fmt.Fprintf(output.Stdout, "%s [y/N]: ", prompt)
	line, _ := bufio.NewReader(stdinReader).ReadString('\n')
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == wordYes
}

// postWrite sends an authenticated POST carrying a fresh Idempotency-Key and
// returns the decoded response envelope. The API deduplicates writes on that
// header, so a retried invocation cannot double-apply.
func postWrite(path string, payload interface{}) (map[string]interface{}, error) {
	return sendJSON(http.MethodPost, path, payload, idempotencyHeader())
}

func putWrite(path string, payload interface{}) (map[string]interface{}, error) {
	return sendJSON(http.MethodPut, path, payload, idempotencyHeader())
}

func patchWrite(path string, payload interface{}) (map[string]interface{}, error) {
	return sendJSON(http.MethodPatch, path, payload, idempotencyHeader())
}

// No Idempotency-Key: the API stores responses for POST, PATCH and PUT only.
func deleteWrite(path string) error {
	_, err := sendRequest(http.MethodDelete, path, nil, nil)
	return err
}

func getJSON(path string) (map[string]interface{}, error) {
	raw, err := sendRequest(http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	return decodeEnvelope(raw.Body)
}

func idempotencyHeader() map[string]string {
	return map[string]string{"Idempotency-Key": api.NewIdempotencyKey()}
}

func sendJSON(method, path string, payload interface{}, headers map[string]string) (map[string]interface{}, error) {
	body, _ := json.Marshal(payload)
	raw, err := sendRequest(method, path, bytes.NewReader(body), headers)
	if err != nil {
		return nil, err
	}
	return decodeEnvelope(raw.Body)
}

func sendRequest(method, path string, body io.Reader, headers map[string]string) (*api.RawResponse, error) {
	client, err := newSDKClientFn()
	if err != nil {
		return nil, err
	}
	raw, err := client.Raw().DoRawWithHeaders(method, path, body, headers)
	if err != nil {
		return nil, err
	}
	if raw.StatusCode >= 400 {
		return nil, classifyStatusError(raw.StatusCode, describeAPIError(raw))
	}
	return raw, nil
}

func decodeEnvelope(body []byte) (map[string]interface{}, error) {
	var envelope map[string]interface{}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}
	return envelope, nil
}

func envelopeData(envelope map[string]interface{}) map[string]interface{} {
	data, _ := envelope["data"].(map[string]interface{})
	return data
}

func dataString(data map[string]interface{}, key string) string {
	s, _ := data[key].(string)
	return s
}

const versionConflictHint = "The key changed after it was read. Run the command again to work from the current version."

func versionConflictLine(details interface{}) string {
	if d, ok := details.(map[string]interface{}); ok {
		if current, ok := d["current_version"].(float64); ok {
			return fmt.Sprintf("It is now at version %d. %s", int(current), versionConflictHint)
		}
	}
	return versionConflictHint
}

// The API counts from 0; people count from 1, as `l4 tags show` numbers configs.
var problemPositions = []struct{ field, label string }{
	{"collapsed_index", "collapsed key"},
	{"config_index", "config"},
	{"rule_index", "rule"},
	{"condition_index", "condition"},
	{"split_index", "split"},
}

func describeAPIError(raw *api.RawResponse) error {
	base := raw.DecodeError()
	var envelope struct {
		Error struct {
			Code    string      `json:"code"`
			Details interface{} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw.Body, &envelope)
	lines := errorDetailLines(envelope.Error.Details)
	if envelope.Error.Code == "version_conflict" {
		lines = append(lines, versionConflictLine(envelope.Error.Details))
	}
	if len(lines) == 0 {
		return base
	}
	return fmt.Errorf("%w\n%s", base, strings.Join(lines, "\n"))
}

// Service checks send an object of details; request validation sends a list.
func errorDetailLines(details interface{}) []string {
	var lines []string
	switch d := details.(type) {
	case map[string]interface{}:
		problems, _ := d["problems"].([]interface{})
		for _, p := range problems {
			if m, ok := p.(map[string]interface{}); ok {
				lines = append(lines, "  - "+describeProblem(m))
			}
		}
		if dependents, _ := d["dependents"].([]interface{}); len(dependents) > 0 {
			names := make([]string, 0, len(dependents))
			for _, dep := range dependents {
				names = append(names, refName(dep))
			}
			lines = append(lines, "Read by: "+strings.Join(names, ", "))
		}
	case []interface{}:
		for _, item := range d {
			if m, ok := item.(map[string]interface{}); ok {
				lines = append(lines, "  - "+describeRequestProblem(m))
			}
		}
	}
	return lines
}

func describeProblem(m map[string]interface{}) string {
	code, _ := m["code"].(string)
	var at []string
	if field, _ := m["field"].(string); field != "" {
		at = append(at, field)
	}
	for _, pos := range problemPositions {
		if idx, ok := m[pos.field].(float64); ok {
			at = append(at, fmt.Sprintf("%s %d", pos.label, int(idx)+1))
		}
	}
	if len(at) == 0 {
		return code
	}
	return code + " at " + strings.Join(at, ", ")
}

func describeRequestProblem(m map[string]interface{}) string {
	loc, _ := m["loc"].([]interface{})
	parts := make([]string, 0, len(loc))
	for _, p := range loc {
		parts = append(parts, fmt.Sprint(p))
	}
	msg, _ := m["msg"].(string)
	return strings.Join(parts, ".") + ": " + msg
}

// The contract sends {id, name}. A bare name still renders, rather than Go syntax.
func refName(v interface{}) string {
	if m, ok := v.(map[string]interface{}); ok {
		if name, _ := m["name"].(string); name != "" {
			return name
		}
		id, _ := m["id"].(string)
		return id
	}
	return fmt.Sprint(v)
}
