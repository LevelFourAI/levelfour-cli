package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// connect wires a real MCP client to the server over an in-memory transport, so
// every assertion below goes through the protocol rather than around it.
func connect(t *testing.T, f Fetcher) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()

	serverSession, err := NewServer(f, "test").Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })

	client := sdk.NewClient(&sdk.Implementation{Name: "test-client", Version: "test"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestListToolsServesTheWholeCatalogWithItsSchemas(t *testing.T) {
	session := connect(t, &fakeFetcher{})

	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(result.Tools) != len(tools) {
		t.Fatalf("listed %d tools, want %d", len(result.Tools), len(tools))
	}

	byName := map[string]*sdk.Tool{}
	for _, tool := range result.Tools {
		byName[tool.Name] = tool
	}

	breakdown, ok := byName["get-cost-breakdown"]
	if !ok {
		t.Fatal("get-cost-breakdown was not listed")
	}
	if !breakdown.Annotations.ReadOnlyHint {
		t.Error("tools are not advertised as read-only")
	}
	// The schema has to survive the round trip to the wire unchanged, because a
	// client renders its argument form from what arrives here.
	encoded, err := json.Marshal(breakdown.InputSchema)
	if err != nil {
		t.Fatalf("marshal schema: %v", err)
	}
	var got, want map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("unmarshal wire schema: %v", err)
	}
	if err := json.Unmarshal(toolNamed(t, "get-cost-breakdown").schema, &want); err != nil {
		t.Fatalf("unmarshal source schema: %v", err)
	}
	if len(got["properties"].(map[string]any)) != len(want["properties"].(map[string]any)) {
		t.Errorf("wire schema lost properties: %s", encoded)
	}
	if got["title"] != want["title"] {
		t.Errorf("wire schema title = %v, want %v", got["title"], want["title"])
	}
}

func TestCallToolReturnsTheShapedPayload(t *testing.T) {
	session := connect(t, &fakeFetcher{payloads: map[string]any{
		"/api/v1/costs/summary":          map[string]any{"total_spend": float64(4200)},
		"/api/v1/costs/monthly-spending": map[string]any{"months": []any{}},
	}})

	result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "get-cost-summary"})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if result.IsError {
		t.Fatalf("tool reported an error: %v", result.Content)
	}
	text := result.Content[0].(*sdk.TextContent).Text
	if !strings.Contains(text, "4200") {
		t.Errorf("result text = %s", text)
	}
}

func TestCallToolPassesArgumentsThrough(t *testing.T) {
	f := &fakeFetcher{}
	session := connect(t, f)

	_, err := session.CallTool(context.Background(), &sdk.CallToolParams{
		Name:      "list-commitments",
		Arguments: map[string]any{"provider": "azure", "status": "expiring", "page": 2},
	})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if !strings.Contains(f.paths[0], "provider=azure") || !strings.Contains(f.paths[0], "page=2") {
		t.Errorf("arguments did not reach the REST call: %q", f.paths[0])
	}
}

func TestCallToolReportsRESTFailuresToTheModel(t *testing.T) {
	session := connect(t, &fakeFetcher{err: errAPI})

	result, err := session.CallTool(context.Background(), &sdk.CallToolParams{Name: "get-cost-summary"})
	if err != nil {
		t.Fatalf("call tool: %v", err)
	}
	if !result.IsError {
		t.Fatal("a failed call was reported as a success")
	}
	if !strings.Contains(result.Content[0].(*sdk.TextContent).Text, "401") {
		t.Errorf("the model was not told what went wrong: %v", result.Content)
	}
}

func TestCallToolRejectsArgumentsThatAreNotAnObject(t *testing.T) {
	result, err := toolNamed(t, "get-cost-summary").handler(&fakeFetcher{})(
		context.Background(),
		&sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{
			Name:      "get-cost-summary",
			Arguments: json.RawMessage(`["not an object"]`),
		}},
	)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !result.IsError || !strings.Contains(result.Content[0].(*sdk.TextContent).Text, "not a JSON object") {
		t.Errorf("result = %v", result.Content)
	}
}

func TestCallToolReportsAnUnencodableResult(t *testing.T) {
	// A payload the JSON encoder cannot represent has to come back as a tool
	// error, not as a dropped response the client waits on forever.
	f := &fakeFetcher{fallback: map[string]any{"stream": make(chan int)}}
	result, err := toolNamed(t, "get-recommendation").handler(f)(
		context.Background(),
		&sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{
			Name:      "get-recommendation",
			Arguments: json.RawMessage(`{"recommendation_id":"REC-1234"}`),
		}},
	)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !result.IsError || !strings.Contains(result.Content[0].(*sdk.TextContent).Text, "could not be encoded") {
		t.Errorf("result = %v", result.Content)
	}
}

func TestPromptsAreServedWithTheirArguments(t *testing.T) {
	session := connect(t, &fakeFetcher{})
	ctx := context.Background()

	list, err := session.ListPrompts(ctx, nil)
	if err != nil {
		t.Fatalf("list prompts: %v", err)
	}
	if len(list.Prompts) != len(prompts) {
		t.Fatalf("listed %d prompts, want %d", len(list.Prompts), len(prompts))
	}

	got, err := session.GetPrompt(ctx, &sdk.GetPromptParams{
		Name:      "monthly_bill_review",
		Arguments: map[string]string{"month": "2026-07"},
	})
	if err != nil {
		t.Fatalf("get prompt: %v", err)
	}
	text := got.Messages[0].Content.(*sdk.TextContent).Text
	if !strings.Contains(text, "2026-07") {
		t.Errorf("the month argument did not reach the prompt: %s", text)
	}
}

func TestServeSpeaksTheProtocolOverAPipeAndExitsCleanly(t *testing.T) {
	// Real newline-delimited JSON over a pipe, because the framing between the
	// binary and its client is the part this command has to get right and the
	// in-memory transport does not exercise it.
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()

	var notices bytes.Buffer
	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), &fakeFetcher{}, "1.2.3", inReader, outWriter, &notices)
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

	log := notices.String()
	if !strings.Contains(log, "1.2.3") {
		t.Errorf("startup notice does not identify which binary answered: %q", log)
	}
	if !strings.Contains(log, fmt.Sprintf("serving %d tools", len(tools))) {
		t.Errorf("startup notice does not say how much of the surface is served: %q", log)
	}
	// The notice goes in a client's own log, where a user reads it to work out
	// why an agent sees no tools. It must describe this process and nothing
	// else: a URL here sends someone debugging a local server to a remote one.
	if strings.Contains(log, "https://") {
		t.Errorf("startup notice points at a URL this process does not serve: %q", log)
	}
}

func TestServeReportsABrokenStream(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	t.Cleanup(func() { _ = outReader.Close() })

	done := make(chan error, 1)
	go func() {
		done <- Serve(context.Background(), &fakeFetcher{}, "1.2.3", inReader, outWriter, io.Discard)
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
	if isCleanShutdown(errAPI) {
		t.Error("a real failure was swallowed")
	}
}

// TestSummaryCountsTheWholeSurface pins what `l4 mcp status` tells a user. The
// count is the whole catalog and carries no caveat, because there is nothing the
// local server leaves to the hosted one.
func TestSummaryCountsTheWholeSurface(t *testing.T) {
	got := Summary()
	want := fmt.Sprintf("%d tools, %d prompts", len(tools), len(prompts))
	if got != want {
		t.Errorf("Summary = %q, want %q", got, want)
	}
	if strings.Contains(got, "not served") {
		t.Errorf("Summary reports a gap that does not exist: %q", got)
	}
}
