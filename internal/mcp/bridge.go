package mcp

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"net/url"
	"time"

	"github.com/LevelFourAI/levelfour-cli/internal/api"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	// The hosted server. `l4 mcp serve` relays it rather than carrying its own
	// copy of the tools, so what a key sees here and there cannot differ.
	Endpoint   = "https://mcp.levelfour.ai/mcp"
	ServerName = "levelfour"
)

// A hosted tool can compute for tens of seconds before its response starts,
// which the REST client's ten-second header timeout would cut off.
const upstreamTimeout = 2 * time.Minute

// Connecting and listing answer quickly; only a tool call runs long. Bounding
// them separately reports a server that accepts and then stalls in seconds,
// where upstreamTimeout alone would leave `l4 mcp status` hanging for minutes.
const startupTimeout = 15 * time.Second

// ErrCredentialRefused marks a rejection of the key itself, as opposed to a
// network failure, so a caller can work out which of the two causes it was.
// It is raised by the transport on a 401 or 403, which the SDK then reports as
// a rejected request rather than a broken connection, so a key refused
// mid-session fails that call and leaves the next one free to succeed.
var ErrCredentialRefused = errors.New("the LevelFour MCP server refused this credential")

type Upstream struct {
	Endpoint string
	Key      string
	Version  string
}

// Surface counts what the hosted server offers the credential that asked.
type Surface struct {
	Tools     int
	Prompts   int
	Resources int
}

func (s Surface) String() string {
	return fmt.Sprintf("%d tools, %d prompts, %d resources", s.Tools, s.Prompts, s.Resources)
}

// Probe reports what the hosted server offers this credential, without serving.
func Probe(ctx context.Context, u Upstream) (Surface, error) {
	upstream, surface, err := start(ctx, u)
	if err != nil {
		return Surface{}, err
	}
	_ = upstream.Close()
	return surface, nil
}

// start connects and reads the surface under startupTimeout. The session
// outlives that deadline: the SDK detaches its connection from the context
// Connect was given, so later tool calls are held only to upstreamTimeout.
func start(ctx context.Context, u Upstream) (*sdk.ClientSession, Surface, error) {
	ctx, cancel := context.WithTimeout(ctx, startupTimeout)
	defer cancel()
	upstream, err := dial(ctx, u)
	if err != nil {
		return nil, Surface{}, err
	}
	surface, err := survey(ctx, upstream)
	if err != nil {
		_ = upstream.Close()
		return nil, Surface{}, err
	}
	return upstream, surface, nil
}

func dial(ctx context.Context, u Upstream) (*sdk.ClientSession, error) {
	endpoint, err := url.Parse(u.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid MCP endpoint: %w", err)
	}
	if err := api.ValidateBaseURL(u.Endpoint); err != nil {
		return nil, err
	}

	client := sdk.NewClient(&sdk.Implementation{Name: "levelfour-cli", Version: u.Version}, nil)
	session, err := client.Connect(ctx, &sdk.StreamableClientTransport{
		Endpoint:   u.Endpoint,
		HTTPClient: credentialedClient(endpoint, u),
		// The hosted server is stateless and answers the standalone stream's GET
		// with 405, so opening it would only log a failure on every start.
		DisableStandaloneSSE: true,
	}, nil)
	if errors.Is(err, ErrCredentialRefused) {
		return nil, fmt.Errorf("%w at %s", ErrCredentialRefused, u.Endpoint)
	}
	if err != nil {
		return nil, fmt.Errorf("connecting to the LevelFour MCP server at %s: %w", u.Endpoint, err)
	}
	if session.InitializeResult().Capabilities == nil {
		_ = session.Close()
		return nil, fmt.Errorf("the MCP server at %s advertised no capabilities", u.Endpoint)
	}
	return session, nil
}

// credentialedClient is the only HTTP client the key is handed to, so where it
// can be sent is decided here: this endpoint's origin, and no redirect beyond it.
func credentialedClient(endpoint *url.URL, u Upstream) *http.Client {
	transport := api.SecureTransport()
	transport.ResponseHeaderTimeout = upstreamTimeout
	return &http.Client{
		Timeout:       upstreamTimeout,
		Transport:     &bearerTransport{base: transport, key: u.Key, agent: "levelfour-cli/" + u.Version},
		CheckRedirect: sameOriginOnly(endpoint),
	}
}

// bearerTransport attaches the credential to every request. It never leaves the
// endpoint's origin because the client refuses redirects that would take it
// elsewhere, and a header set here would otherwise ride along on one.
type bearerTransport struct {
	base  http.RoundTripper
	key   string
	agent string
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.key)
	req.Header.Set("User-Agent", t.agent)
	resp, err := t.base.RoundTrip(req)
	if err == nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
		_ = resp.Body.Close()
		return nil, ErrCredentialRefused
	}
	return resp, err
}

// The scheme is part of the check because a same-host redirect to http would
// otherwise send the key in cleartext.
func sameOriginOnly(endpoint *url.URL) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, _ []*http.Request) error {
		if req.URL.Scheme != endpoint.Scheme || req.URL.Host != endpoint.Host {
			return fmt.Errorf("refusing to follow a redirect from %s://%s to %s://%s with the credential attached",
				endpoint.Scheme, endpoint.Host, req.URL.Scheme, req.URL.Host)
		}
		return nil
	}
}

// survey counts only what the server advertises: asking for a list it does not
// offer is a protocol error, not an empty answer.
func survey(ctx context.Context, upstream *sdk.ClientSession) (Surface, error) {
	caps := upstream.InitializeResult().Capabilities
	var s Surface
	var err error
	if s.Tools, err = countIf(caps.Tools != nil, upstream.Tools(ctx, nil)); err != nil {
		return Surface{}, fmt.Errorf("listing tools: %w", err)
	}
	if s.Prompts, err = countIf(caps.Prompts != nil, upstream.Prompts(ctx, nil)); err != nil {
		return Surface{}, fmt.Errorf("listing prompts: %w", err)
	}
	if s.Resources, err = countIf(caps.Resources != nil, upstream.Resources(ctx, nil)); err != nil {
		return Surface{}, fmt.Errorf("listing resources: %w", err)
	}
	return s, nil
}

func countIf[T any](advertised bool, items iter.Seq2[T, error]) (int, error) {
	if !advertised {
		return 0, nil
	}
	n := 0
	for _, err := range items {
		if err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}

// newBridgeServer answers the protocol's own lifecycle locally and relays every
// feature request upstream unchanged. Relaying rather than registering each
// hosted tool means the list is read fresh on every request, so a deploy shows
// up mid-session, and a schema the SDK would reject at registration cannot stop
// the server from starting.
func newBridgeServer(upstream *sdk.ClientSession, version string) *sdk.Server {
	hosted := upstream.InitializeResult()
	server := sdk.NewServer(&sdk.Implementation{
		Name:        ServerName,
		Title:       "LevelFour",
		Description: "Cloud cost visibility and savings recommendations.",
		Version:     version,
		WebsiteURL:  "https://levelfour.ai",
	}, &sdk.ServerOptions{
		Instructions: hosted.Instructions,
		Capabilities: relayedCapabilities(hosted.Capabilities),
	})
	server.AddReceivingMiddleware(relaying(upstream))
	return server
}

// Resource subscriptions are dropped because they need a standing stream from
// the hosted server, which this bridge does not hold open.
func relayedCapabilities(hosted *sdk.ServerCapabilities) *sdk.ServerCapabilities {
	caps := &sdk.ServerCapabilities{Tools: hosted.Tools, Prompts: hosted.Prompts}
	if hosted.Resources != nil {
		caps.Resources = &sdk.ResourceCapabilities{ListChanged: hosted.Resources.ListChanged}
	}
	return caps
}

type relay func(context.Context, *sdk.ClientSession, sdk.Request) (sdk.Result, error)

var relays = map[string]relay{
	"tools/list":               passThrough((*sdk.ClientSession).ListTools),
	"tools/call":               callTool,
	"prompts/list":             passThrough((*sdk.ClientSession).ListPrompts),
	"prompts/get":              passThrough((*sdk.ClientSession).GetPrompt),
	"resources/list":           passThrough((*sdk.ClientSession).ListResources),
	"resources/read":           passThrough((*sdk.ClientSession).ReadResource),
	"resources/templates/list": passThrough((*sdk.ClientSession).ListResourceTemplates),
}

// passThrough relays a request whose params the client session takes as they
// arrived. The params type is the one the server decoded for this method, so
// the assertion holds for every entry the map pairs it with.
func passThrough[P sdk.Params, R sdk.Result](send func(*sdk.ClientSession, context.Context, P) (R, error)) relay {
	return func(ctx context.Context, up *sdk.ClientSession, req sdk.Request) (sdk.Result, error) {
		return relayed(send(up, ctx, req.GetParams().(P)))
	}
}

func relaying(upstream *sdk.ClientSession) sdk.Middleware {
	return func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			if forward, ok := relays[method]; ok {
				return forward(ctx, upstream, req)
			}
			return next(ctx, method, req)
		}
	}
}

// relayed returns an untyped nil on failure, since a typed nil pointer inside
// the Result interface would not compare equal to nil downstream.
func relayed[R sdk.Result](result R, err error) (sdk.Result, error) {
	if err != nil {
		return nil, err
	}
	return result, nil
}

// callTool hands the hosted server's own protocol error back as one. Any other
// failure means the call never reached a tool, and a model shown that as a tool
// result can tell the user why the answer is missing.
func callTool(ctx context.Context, up *sdk.ClientSession, req sdk.Request) (sdk.Result, error) {
	in := req.(*sdk.CallToolRequest).Params
	params := &sdk.CallToolParams{
		Meta:           in.Meta,
		Name:           in.Name,
		InputResponses: in.InputResponses,
		RequestState:   in.RequestState,
	}
	// Left unset when absent so the SDK sends {}. A nil RawMessage inside the
	// interface is not a nil interface, and would go out as "arguments": null.
	if len(in.Arguments) > 0 {
		params.Arguments = in.Arguments
	}
	result, err := up.CallTool(ctx, params)
	switch {
	case err == nil || sentByHostedServer(err):
		return relayed(result, err)
	case errors.Is(err, ErrCredentialRefused):
		return errorResult(fmt.Sprintf("The LevelFour MCP server refused this credential while running %s: "+
			"the key was revoked, or MCP access ended for this organization. `l4 mcp status` says which.", in.Name)), nil
	default:
		return errorResult(fmt.Sprintf("The LevelFour MCP server could not be reached for %s: %v", in.Name, err)), nil
	}
}

// The SDK reports its own transport failures as JSON-RPC errors too, under
// codes it does not export, so the code is all that tells them apart from an
// error the hosted server sent.
const (
	codeClientClosing       = -32003
	codeServerClosing       = -32004
	codeRejectedByTransport = -32005
)

func sentByHostedServer(err error) bool {
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) {
		return false
	}
	switch rpcErr.Code {
	case codeClientClosing, codeServerClosing, codeRejectedByTransport:
		return false
	}
	return true
}

func errorResult(message string) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: message}},
	}
}
