package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/LevelFourAI/levelfour-cli/internal/output"
)

// stdinReader is the source the confirmation prompt reads from. Tests swap it.
var stdinReader io.Reader = os.Stdin

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
	return sendWrite("POST", path, payload)
}

// sendWrite is postWrite for the other write verbs. A nil payload sends no
// body, which is what DELETE needs.
func sendWrite(method, path string, payload interface{}) (map[string]interface{}, error) {
	client, err := newSDKClientFn()
	if err != nil {
		return nil, err
	}

	var body io.Reader
	if payload != nil {
		encoded, _ := json.Marshal(payload)
		body = bytes.NewReader(encoded)
	}
	raw, err := client.Raw().DoRawWithHeaders(method, path, body, map[string]string{
		"Idempotency-Key": api.NewIdempotencyKey(),
	})
	if err != nil {
		return nil, err
	}
	if raw.StatusCode >= 400 {
		return nil, classifyStatusError(raw.StatusCode, raw.DecodeError())
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal(raw.Body, &envelope); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}
	return envelope, nil
}

func readEnvelope(path string) (map[string]interface{}, error) {
	client, err := newSDKClientFn()
	if err != nil {
		return nil, err
	}

	raw, err := client.Raw().DoRaw("GET", path, nil)
	if err != nil {
		return nil, err
	}
	if raw.StatusCode >= 400 {
		return nil, classifyStatusError(raw.StatusCode, raw.DecodeError())
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal(raw.Body, &envelope); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}
	return envelope, nil
}

// envelopeList pulls the "data" array out of a response envelope.
func envelopeList(envelope map[string]interface{}) []map[string]interface{} {
	raw, _ := envelope["data"].([]interface{})
	items := make([]map[string]interface{}, 0, len(raw))
	for _, entry := range raw {
		if item, ok := entry.(map[string]interface{}); ok {
			items = append(items, item)
		}
	}
	return items
}

// envelopeData pulls the "data" object out of a response envelope.
func envelopeData(envelope map[string]interface{}) map[string]interface{} {
	data, _ := envelope["data"].(map[string]interface{})
	return data
}

// dataString reads one string field out of a response data object.
func dataString(data map[string]interface{}, key string) string {
	s, _ := data[key].(string)
	return s
}
