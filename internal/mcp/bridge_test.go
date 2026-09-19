package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const testKey = "l4_test_bridgekey123456789"

// hosted stands in for the hosted server: a real go-sdk server behind the same
// stateless streamable HTTP transport, refusing any request without the key.
type hosted struct {
	server *sdk.Server
	http   *httptest.Server

	mu       sync.Mutex
	accepted string
	headers  http.Header
	lastArgs json.RawMessage
}

// accept changes the one key the stand-in takes, the way a revocation does.
func (h *hosted) accept(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.accepted = key
}

func (h *hosted) endpoint() string { return h.http.URL + "/mcp" }

func (h *hosted) upstream() Upstream {
	return Upstream{Endpoint: h.endpoint(), Key: testKey, Version: "1.2.3"}
}

func (h *hosted) seenHeaders() http.Header {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.headers
}

func (h *hosted) seenArgs() json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastArgs
}

// newHosted builds the stand-in. Methods named in failing answer with an error,
// so a listing that breaks part way through can be reproduced.
func newHosted(t *testing.T, failing ...string) *hosted {
	t.Helper()
	h := &hosted{accepted: testKey, server: sdk.NewServer(&sdk.Implementation{Name: "hosted", Version: "test"},
		&sdk.ServerOptions{Instructions: "Route on the Purpose line of each tool."})}
	h.server.AddReceivingMiddleware(failingOn(failing))
	h.server.AddTool(&sdk.Tool{
		Name:         "get-cost-summary",
		Description:  "**Purpose:** Spend for a period.",
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"period":{"type":"string"}}}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"total":{"type":"number"}}}`),
		Annotations:  &sdk.ToolAnnotations{ReadOnlyHint: true, Title: "Cost summary"},
	}, func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		h.mu.Lock()
		h.lastArgs = req.Params.Arguments
		h.mu.Unlock()
		return &sdk.CallToolResult{
			Content:           []sdk.Content{&sdk.TextContent{Text: string(req.Params.Arguments)}},
			StructuredContent: map[string]any{"total": 1234.5},
		}, nil
	})
	h.server.AddTool(&sdk.Tool{
		Name:        "get-commitment",
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		return errorResult("commitment not found"), nil
	})
	h.server.AddPrompt(&sdk.Prompt{Name: "monthly_bill_review", Arguments: []*sdk.PromptArgument{{Name: "month"}}},
		func(_ context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
			return &sdk.GetPromptResult{Messages: []*sdk.PromptMessage{{
				Role: "user", Content: &sdk.TextContent{Text: "Review " + req.Params.Arguments["month"]},
			}}}, nil
		})
	h.server.AddResource(&sdk.Resource{URI: "levelfour://providers", Name: "providers", MIMEType: "application/json"},
		func(_ context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: req.Params.URI, Text: `["aws"]`}}}, nil
		})
	h.server.AddResourceTemplate(&sdk.ResourceTemplate{URITemplate: "levelfour://filters/{kind}", Name: "filters"},
		func(_ context.Context, req *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: req.Params.URI, Text: "{}"}}}, nil
		})
	h.serve(t)
	return h
}

func failingOn(methods []string) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			for _, m := range methods {
				if m == method {
					return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: method + " is down"}
				}
			}
			return next(ctx, method, req)
		}
	}
}

func (h *hosted) serve(t *testing.T) {
	t.Helper()
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return h.server },
		&sdk.StreamableHTTPOptions{Stateless: true})
	h.http = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.headers = r.Header.Clone()
		accepted := h.accepted
		h.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+accepted {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(h.http.Close)
}

// direct is a client of the stand-in with nothing in between, which is what an
// agent pointed at the hosted server sees. The bridge has to match it exactly.
func direct(t *testing.T, h *hosted) *sdk.ClientSession {
	t.Helper()
	session, err := dial(context.Background(), h.upstream())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// throughBridge is a client of the bridge, the way Claude Desktop reaches it.
func throughBridge(t *testing.T, h *hosted) *sdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverTransport, clientTransport := sdk.NewInMemoryTransports()
	serverSession, err := newBridgeServer(direct(t, h), "1.2.3").Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("bridge connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Wait() })

	session, err := sdk.NewClient(&sdk.Implementation{Name: "desktop", Version: "test"}, nil).Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func sameResult(t *testing.T, what string, want, got any, wantErr, gotErr error) {
	t.Helper()
	if wantErr != nil || gotErr != nil {
		t.Fatalf("%s: direct err = %v, bridged err = %v", what, wantErr, gotErr)
	}
	if !reflect.DeepEqual(want, got) {
		wantJSON, _ := json.Marshal(want)
		gotJSON, _ := json.Marshal(got)
		t.Errorf("%s differs through the bridge:\n direct:  %s\n bridged: %s", what, wantJSON, gotJSON)
	}
}

func TestTheBridgeServesExactlyWhatTheHostedServerServes(t *testing.T) {
	h := newHosted(t)
	ctx := context.Background()
	hosted, bridged := direct(t, h), throughBridge(t, h)

	wantTools, wantErr := hosted.ListTools(ctx, nil)
	gotTools, gotErr := bridged.ListTools(ctx, nil)
	sameResult(t, "tools/list", wantTools, gotTools, wantErr, gotErr)

	wantPrompts, wantErr := hosted.ListPrompts(ctx, nil)
	gotPrompts, gotErr := bridged.ListPrompts(ctx, nil)
	sameResult(t, "prompts/list", wantPrompts, gotPrompts, wantErr, gotErr)

	get := &sdk.GetPromptParams{Name: "monthly_bill_review", Arguments: map[string]string{"month": "2026-08"}}
	wantPrompt, wantErr := hosted.GetPrompt(ctx, get)
	gotPrompt, gotErr := bridged.GetPrompt(ctx, get)
	sameResult(t, "prompts/get", wantPrompt, gotPrompt, wantErr, gotErr)

	wantResources, wantErr := hosted.ListResources(ctx, nil)
	gotResources, gotErr := bridged.ListResources(ctx, nil)
	sameResult(t, "resources/list", wantResources, gotResources, wantErr, gotErr)

	read := &sdk.ReadResourceParams{URI: "levelfour://providers"}
	wantRead, wantErr := hosted.ReadResource(ctx, read)
	gotRead, gotErr := bridged.ReadResource(ctx, read)
	sameResult(t, "resources/read", wantRead, gotRead, wantErr, gotErr)

	wantTemplates, wantErr := hosted.ListResourceTemplates(ctx, nil)
	gotTemplates, gotErr := bridged.ListResourceTemplates(ctx, nil)
	sameResult(t, "resources/templates/list", wantTemplates, gotTemplates, wantErr, gotErr)

	for _, call := range []*sdk.CallToolParams{
		{Name: "get-cost-summary", Arguments: map[string]any{"period": "2026-08"}},
		{Name: "get-commitment"},
	} {
		wantCall, wantErr := hosted.CallTool(ctx, call)
		gotCall, gotErr := bridged.CallTool(ctx, call)
		sameResult(t, "tools/call "+call.Name, wantCall, gotCall, wantErr, gotErr)
	}
}

// A tool shipped on the hosted server mid-session has to appear without a
// restart, which is the property a registered copy of the list could not give.
func TestTheBridgeReadsTheToolListFreshOnEveryRequest(t *testing.T) {
	h := newHosted(t)
	bridged := throughBridge(t, h)

	h.server.AddTool(&sdk.Tool{Name: "list-commitment-contracts", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{}, nil
		})

	tools, err := bridged.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range tools.Tools {
		names = append(names, tool.Name)
	}
	if !strings.Contains(strings.Join(names, ","), "list-commitment-contracts") {
		t.Errorf("tools = %v, want the one added after the bridge started", names)
	}
}

func TestTheBridgeCarriesTheHostedInstructionsAndCapabilities(t *testing.T) {
	init := throughBridge(t, newHosted(t)).InitializeResult()
	if init.Instructions != "Route on the Purpose line of each tool." {
		t.Errorf("instructions = %q, want the hosted server's", init.Instructions)
	}
	caps := init.Capabilities
	if caps.Tools == nil || caps.Prompts == nil || caps.Resources == nil {
		t.Fatalf("capabilities = %+v, want tools, prompts and resources", caps)
	}
	if caps.Resources.Subscribe {
		t.Error("the bridge advertises subscriptions it cannot relay")
	}
	if init.ServerInfo.Name != ServerName || init.ServerInfo.Version != "1.2.3" {
		t.Errorf("server info = %+v, want this binary", init.ServerInfo)
	}
}

// A hosted server offering tools alone is relayed as offering tools alone, so a
// client never asks for a list the hosted server would refuse.
func TestTheBridgeAdvertisesOnlyWhatTheHostedServerDoes(t *testing.T) {
	h := &hosted{accepted: testKey, server: sdk.NewServer(&sdk.Implementation{Name: "hosted", Version: "test"}, nil)}
	h.server.AddTool(&sdk.Tool{Name: "get-identity", InputSchema: json.RawMessage(`{"type":"object"}`)},
		func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return &sdk.CallToolResult{}, nil
		})
	h.serve(t)

	caps := throughBridge(t, h).InitializeResult().Capabilities
	if caps.Tools == nil || caps.Prompts != nil || caps.Resources != nil {
		t.Errorf("capabilities = %+v, want tools alone", caps)
	}

	surface, err := Probe(context.Background(), h.upstream())
	if err != nil {
		t.Fatal(err)
	}
	if surface != (Surface{Tools: 1}) {
		t.Errorf("surface = %+v, want one tool and nothing else", surface)
	}
}

func TestCallToolArgumentsReachTheHostedServerByteForByte(t *testing.T) {
	h := newHosted(t)
	bridged := throughBridge(t, h)
	ctx := context.Background()

	// A number past float64's exact range survives only if nothing decodes it.
	raw := json.RawMessage(`{"period":"2026-08","limit":12345678901234567890}`)
	if _, err := bridged.CallTool(ctx, &sdk.CallToolParams{Name: "get-cost-summary", Arguments: raw}); err != nil {
		t.Fatal(err)
	}
	if got := string(h.seenArgs()); got != string(raw) {
		t.Errorf("arguments arrived as %s, want %s", got, raw)
	}
}

// The SDK's own client always sends arguments, so a request that omits them is
// built by hand, the way a client written against the bare protocol sends one.
func TestCallToolWithNoArgumentsSendsAnEmptyObject(t *testing.T) {
	h := newHosted(t)
	req := &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: "get-cost-summary"}}
	if _, err := callTool(context.Background(), direct(t, h), req); err != nil {
		t.Fatal(err)
	}
	if got := string(h.seenArgs()); got != "{}" {
		t.Errorf("a call with no arguments arrived with %s, want {}", got)
	}
}

// The hosted server's own refusal of a call is its answer, so it reaches the
// client as the same protocol error rather than being reworded.
func TestCallToolKeepsTheHostedProtocolError(t *testing.T) {
	h := newHosted(t)
	ctx := context.Background()
	call := &sdk.CallToolParams{Name: "get-potential-savings"}

	_, wantErr := direct(t, h).CallTool(ctx, call)
	_, gotErr := throughBridge(t, h).CallTool(ctx, call)

	var want, got *jsonrpc.Error
	if !errors.As(wantErr, &want) || !errors.As(gotErr, &got) {
		t.Fatalf("direct err = %v, bridged err = %v, want protocol errors from both", wantErr, gotErr)
	}
	if got.Code != want.Code || !strings.Contains(got.Message, want.Message) {
		t.Errorf("bridged error = %d %q, want %d %q", got.Code, got.Message, want.Code, want.Message)
	}
}

// A model is shown a tool result, not a transport error, so it can tell the
// user why the answer is missing. Other requests fail as the protocol expects.
func TestAnUnreachableHostedServerIsReportedToTheModel(t *testing.T) {
	h := newHosted(t)
	bridged := throughBridge(t, h)
	h.http.Close()
	ctx := context.Background()

	result, err := bridged.CallTool(ctx, &sdk.CallToolParams{Name: "get-cost-summary"})
	if err != nil {
		t.Fatalf("call returned a protocol error: %v", err)
	}
	text := result.Content[0].(*sdk.TextContent).Text
	if !result.IsError || !strings.Contains(text, "could not be reached for get-cost-summary") {
		t.Errorf("result = %+v %q, want an error result naming the tool", result, text)
	}

	if _, err := bridged.ListTools(ctx, nil); err == nil {
		t.Error("listing tools against a server that is gone succeeded")
	}
}

// A failure that is not a JSON-RPC error at all, such as the caller giving up,
// never came from the hosted server either.
func TestCallToolReportsAnAbandonedCallToTheModel(t *testing.T) {
	up := direct(t, newHosted(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := callTool(ctx, up, &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Name: "get-cost-summary"}})
	if err != nil {
		t.Fatalf("err = %v, want an error result", err)
	}
	if !result.(*sdk.CallToolResult).IsError {
		t.Errorf("result = %+v, want IsError", result)
	}
}

// A key revoked while Claude Desktop is running fails the call it lands on with
// the cause, and does not break the session: the SDK marks a refusal raised by
// the transport as a rejected request rather than a dead connection.
func TestAKeyRefusedMidSessionFailsThatCallAndNotTheNext(t *testing.T) {
	h := newHosted(t)
	bridged := throughBridge(t, h)
	ctx := context.Background()
	call := &sdk.CallToolParams{Name: "get-cost-summary"}

	h.accept("l4_test_rotated")
	result, err := bridged.CallTool(ctx, call)
	if err != nil {
		t.Fatalf("call returned a protocol error: %v", err)
	}
	text := result.Content[0].(*sdk.TextContent).Text
	if !result.IsError || !strings.Contains(text, "refused this credential while running get-cost-summary") {
		t.Errorf("result = %q, want the refusal named for the model", text)
	}

	h.accept(testKey)
	if result, err := bridged.CallTool(ctx, call); err != nil || result.IsError {
		t.Errorf("the call after the key was restored failed: %v %+v", err, result)
	}
}

func TestDialSendsTheKeyAndNamesTheBinary(t *testing.T) {
	h := newHosted(t)
	direct(t, h)

	headers := h.seenHeaders()
	if headers.Get("Authorization") != "Bearer "+testKey {
		t.Errorf("authorization = %q", headers.Get("Authorization"))
	}
	if headers.Get("User-Agent") != "levelfour-cli/1.2.3" {
		t.Errorf("user agent = %q", headers.Get("User-Agent"))
	}
}

func TestDialTellsARefusedKeyFromAnUnreachableServer(t *testing.T) {
	h := newHosted(t)
	ctx := context.Background()

	u := h.upstream()
	u.Key = "l4_test_revoked"
	if _, err := dial(ctx, u); !errors.Is(err, ErrCredentialRefused) {
		t.Errorf("wrong key: err = %v, want ErrCredentialRefused", err)
	}

	gone := h.upstream()
	h.http.Close()
	_, err := dial(ctx, gone)
	if err == nil || errors.Is(err, ErrCredentialRefused) {
		t.Fatalf("closed server: err = %v, want a connection failure", err)
	}
	if !strings.Contains(err.Error(), "connecting to the LevelFour MCP server at "+gone.Endpoint) {
		t.Errorf("err = %v, want it to name the endpoint", err)
	}
}

func TestDialRefusesAnEndpointTheKeyShouldNotTravelTo(t *testing.T) {
	ctx := context.Background()
	for endpoint, want := range map[string]string{
		"http://mcp.example.test/mcp": "insecure HTTP",
		"http://[::1/mcp":             "invalid MCP endpoint",
	} {
		if _, err := dial(ctx, Upstream{Endpoint: endpoint, Key: testKey}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", endpoint, err, want)
		}
	}
}

// The credential is attached to every request, so a redirect to another host
// would hand it over. Only a redirect within the endpoint's own origin is followed.
func TestDialFollowsARedirectOnlyWithinTheEndpointsOrigin(t *testing.T) {
	h := newHosted(t)

	var leaked sync.Map
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store("authorization", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(elsewhere.Close)

	redirecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, elsewhere.URL+"/mcp", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirecting.Close)

	_, err := dial(context.Background(), Upstream{Endpoint: redirecting.URL + "/mcp", Key: testKey})
	if err == nil || !strings.Contains(err.Error(), "refusing to follow a redirect") {
		t.Errorf("err = %v, want the cross-host redirect refused", err)
	}
	if value, sent := leaked.Load("authorization"); sent {
		t.Errorf("the other host received a request carrying %q", value)
	}

	// Same host, other scheme: an https endpoint sent to http would put the key
	// on the wire in cleartext, so a scheme change is refused like a host change.
	switching := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://"+r.Host+"/mcp", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(switching.Close)
	_, err = dial(context.Background(), Upstream{Endpoint: switching.URL + "/mcp", Key: testKey})
	if err == nil || !strings.Contains(err.Error(), "to https://") {
		t.Errorf("err = %v, want the change of scheme refused", err)
	}

	sameHost := http.NewServeMux()
	sameHost.Handle("/old", http.RedirectHandler("/mcp", http.StatusTemporaryRedirect))
	sameHost.Handle("/mcp", h.http.Config.Handler)
	moved := httptest.NewServer(sameHost)
	t.Cleanup(moved.Close)
	if _, err := dial(context.Background(), Upstream{Endpoint: moved.URL + "/old", Key: testKey}); err != nil {
		t.Errorf("a redirect within the host was refused: %v", err)
	}
}

// Every client in the SDK sends capabilities, so this is reproduced by hand: a
// server that answers initialize without them has nothing the bridge can relay.
func TestDialRefusesAServerThatAdvertisesNothing(t *testing.T) {
	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(req.ID) + `,"result":` +
				`{"protocolVersion":"2025-06-18","serverInfo":{"name":"bare","version":"1"}}}`))
		case "server/discover":
			// What a server predating discovery answers, which sends the client to initialize.
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(req.ID) + `,"error":` +
				`{"code":-32601,"message":"Method not found"}}`))
		default:
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	t.Cleanup(bare.Close)

	_, err := dial(context.Background(), Upstream{Endpoint: bare.URL + "/mcp", Key: testKey})
	if err == nil || !strings.Contains(err.Error(), "advertised no capabilities") {
		t.Errorf("err = %v, want the empty server refused", err)
	}
}

// A server that accepts the connection and then says nothing has to fail the
// probe on the caller's deadline, not on the far longer one a tool call gets.
func TestProbeGivesUpOnAServerThatStalls(t *testing.T) {
	// Notifications are answered so the SDK's own cancellation notice, which it
	// bounds separately at five seconds, stays out of what is being measured.
	release := make(chan struct{})
	stalled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"notifications/`) {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		<-release
	}))
	t.Cleanup(stalled.Close)
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	began := time.Now()
	if _, err := Probe(ctx, Upstream{Endpoint: stalled.URL + "/mcp", Key: testKey}); err == nil {
		t.Fatal("a server that never answered was probed successfully")
	}
	if waited := time.Since(began); waited > 5*time.Second {
		t.Errorf("gave up after %s, want the caller's deadline honored", waited)
	}
}

func TestProbeCountsWhatTheHostedServerOffersTheKey(t *testing.T) {
	surface, err := Probe(context.Background(), newHosted(t).upstream())
	if err != nil {
		t.Fatal(err)
	}
	if surface != (Surface{Tools: 2, Prompts: 1, Resources: 1}) {
		t.Errorf("surface = %+v", surface)
	}
	if surface.String() != "2 tools, 1 prompts, 1 resources" {
		t.Errorf("String() = %q", surface.String())
	}
}

func TestProbeReportsAListingThatFails(t *testing.T) {
	for _, method := range []string{"tools/list", "prompts/list", "resources/list"} {
		_, err := Probe(context.Background(), newHosted(t, method).upstream())
		if err == nil || !strings.Contains(err.Error(), method+" is down") {
			t.Errorf("%s failing: err = %v, want it reported", method, err)
		}
	}

	h := newHosted(t)
	u := h.upstream()
	u.Key = "l4_test_revoked"
	if _, err := Probe(context.Background(), u); !errors.Is(err, ErrCredentialRefused) {
		t.Errorf("err = %v, want ErrCredentialRefused", err)
	}
}
