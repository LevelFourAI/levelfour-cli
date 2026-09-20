package mcp

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestServeSpeaksTheProtocolOverAPipeAndExitsCleanly(t *testing.T) {
	h := newHosted(t)

	// Real newline-delimited JSON over a pipe, because the framing between the
	// binary and its client is the part this command has to get right and the
	// in-memory transport does not exercise it.
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	var notices bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), Session{Upstream: h.upstream(), In: inReader, Out: outWriter, Notices: &notices})
	}()

	request := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":` +
		`{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}` + "\n"
	if _, err := io.WriteString(inWriter, request); err != nil {
		t.Fatalf("write request: %v", err)
	}

	scanner := bufio.NewScanner(outReader)
	if !scanner.Scan() {
		t.Fatalf("no response line: %v", scanner.Err())
	}
	response := scanner.Text()

	// Closing stdin is how a client says it is finished, and it must not look
	// like a failure.
	_ = inWriter.Close()
	if err := <-done; err != nil {
		t.Fatalf("a client hanging up was reported as an error: %v", err)
	}
	_ = outReader.Close()

	if !strings.Contains(response, `"serverInfo"`) || !strings.Contains(response, ServerName) {
		t.Errorf("initialize response = %s", response)
	}

	// The notice lands in the client's own log, where a user reads it to work
	// out why an agent sees no tools: which binary, what it relays, and from where.
	log := notices.String()
	for _, want := range []string{"1.2.3", "2 tools, 1 prompts, 1 resources", h.endpoint()} {
		if !strings.Contains(log, want) {
			t.Errorf("startup notice is missing %q: %q", want, log)
		}
	}
	if strings.Contains(log, testKey) {
		t.Errorf("startup notice printed the credential: %q", log)
	}
}

// A key the hosted server refuses stops the process before it speaks, so the
// client logs the reason instead of showing a server with no tools.
func TestServeStopsOnARefusedKey(t *testing.T) {
	u := newHosted(t).upstream()
	u.Key = "l4_test_revoked"
	err := Serve(context.Background(), Session{Upstream: u, In: io.NopCloser(strings.NewReader("")), Notices: io.Discard})
	if !errors.Is(err, ErrCredentialRefused) {
		t.Errorf("err = %v, want ErrCredentialRefused", err)
	}
}

func TestServeStopsWhenTheSurfaceCannotBeRead(t *testing.T) {
	h := newHosted(t, "tools/list")
	err := Serve(context.Background(), Session{Upstream: h.upstream(), In: io.NopCloser(strings.NewReader("")), Notices: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "listing tools") {
		t.Errorf("err = %v, want the failed listing", err)
	}
}

func TestServeReportsABrokenStream(t *testing.T) {
	h := newHosted(t)
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	t.Cleanup(func() { _ = outReader.Close() })

	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), Session{Upstream: h.upstream(), In: inReader, Out: outWriter, Notices: io.Discard})
	}()

	// A pipe that fails rather than ending is a genuine fault, and has to reach
	// the caller so the process exits non-zero.
	_ = inWriter.CloseWithError(errors.New("stdin went away"))

	if err := <-done; err == nil {
		t.Fatal("a broken stream was reported as a clean exit")
	}
}

func TestIsCleanShutdown(t *testing.T) {
	if !isCleanShutdown(nil) || !isCleanShutdown(io.EOF) || !isCleanShutdown(context.Canceled) {
		t.Error("a normal ending was reported as a failure")
	}
	if !isCleanShutdown(errServerClosing) {
		t.Error("the SDK's closing error was reported as a failure")
	}
	if isCleanShutdown(errors.New("stdin went away")) {
		t.Error("a real failure was swallowed")
	}
}
