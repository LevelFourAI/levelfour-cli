package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// The official Go SDK (github.com/modelcontextprotocol/go-sdk) is the library
// here for two reasons. It is the implementation the protocol maintainers ship
// with Google, so a spec revision lands in it rather than being chased in a
// fork, and it is the only Go library that lets a server hand over a raw JSON
// Schema for a tool. That second point decides it: these schemas have to be the
// hosted server's Pydantic output verbatim, and a library that infers a schema
// from a Go struct would quietly produce a near-miss instead.
//
// Server.AddTool is the non-generic entry point. mcp.AddTool[In, Out] is the
// friendlier one, but it derives the input schema from In, which is exactly the
// inference this package cannot use.

var readOnly = &sdk.ToolAnnotations{
	ReadOnlyHint:    true,
	DestructiveHint: boolPtr(false),
	IdempotentHint:  true,
	OpenWorldHint:   boolPtr(false),
}

func boolPtr(b bool) *bool { return &b }

// NewServer builds the local MCP server over an already-authenticated Fetcher.
func NewServer(f Fetcher, version string) *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{
		Name:        ServerName,
		Title:       "LevelFour",
		Description: "Cloud cost visibility and savings recommendations.",
		Version:     version,
		WebsiteURL:  "https://levelfour.ai",
	}, &sdk.ServerOptions{Instructions: Instructions})

	for _, t := range tools {
		server.AddTool(&sdk.Tool{
			Name:        t.name,
			Description: t.description,
			InputSchema: t.schema,
			Annotations: readOnly,
		}, t.handler(f))
	}
	for _, p := range prompts {
		server.AddPrompt(&sdk.Prompt{
			Name:        p.name,
			Description: p.description,
			Arguments:   []*sdk.PromptArgument{{Name: p.arg, Description: p.argDesc}},
		}, p.handler())
	}
	return server
}

func (t tool) handler(f Fetcher) sdk.ToolHandler {
	return func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		args := map[string]any{}
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return errorResult(fmt.Sprintf("Arguments for %s were not a JSON object: %v", t.name, err)), nil
			}
		}
		result, err := t.run(f, args)
		if err != nil {
			// The REST error text is the model's only record of what went wrong,
			// and on a local server it is the user's own machine talking to their
			// own tenant, so it goes through unedited.
			return errorResult(err.Error()), nil
		}
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return errorResult(fmt.Sprintf("Result for %s could not be encoded: %v", t.name, err)), nil
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(encoded)}}}, nil
	}
}

func (p promptDef) handler() sdk.PromptHandler {
	return func(_ context.Context, req *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
		return &sdk.GetPromptResult{
			Description: p.description,
			Messages: []*sdk.PromptMessage{{
				Role:    "user",
				Content: &sdk.TextContent{Text: p.render(req.Params.Arguments)},
			}},
		}, nil
	}
}

func errorResult(message string) *sdk.CallToolResult {
	return &sdk.CallToolResult{
		IsError: true,
		Content: []sdk.Content{&sdk.TextContent{Text: message}},
	}
}

// Serve runs the server over one newline-delimited JSON stream until the client
// disconnects or ctx is cancelled.
//
// notices is where the startup line goes. A stdio server's stderr is captured by
// the client into its own log (Claude Desktop writes mcp-server-<name>.log), so
// this is the one place a user can confirm which binary answered and how much of
// the surface it carries without going and reading the source.
func Serve(ctx context.Context, f Fetcher, version string, in io.ReadCloser, out io.WriteCloser, notices io.Writer) error {
	config := "the LevelFour API"
	fmt.Fprintf(notices, "levelfour mcp %s serving %d tools over stdio\n", version, len(tools))
	fmt.Fprintf(notices, "reading %s with the stored credential\n", config)

	err := NewServer(f, version).Run(ctx, &sdk.IOTransport{Reader: in, Writer: out})
	if isCleanShutdown(err) {
		return nil
	}
	return err
}

// errServerClosing is the JSON-RPC error the SDK reports when the peer hangs up.
// The SDK formats the underlying EOF with %v rather than %w, so the EOF itself
// is not recoverable from the chain and the code is what is left to match on.
// jsonrpc.Error compares by code.
var errServerClosing = &jsonrpc.Error{Code: -32004, Message: "server is closing"}

// isCleanShutdown reports whether the server stopped because the client hung up
// or the process was asked to stop. Both are normal endings for a stdio server:
// returning an error for either would make every ordinary exit non-zero, and the
// client logs that as a crashed server.
func isCleanShutdown(err error) bool {
	return err == nil ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, errServerClosing)
}

// ToolNames lists the tools this binary serves, in catalog order.
func ToolNames() []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.name)
	}
	return names
}

// Summary is the one-line surface description `l4 mcp status` prints. It counts
// what this binary registers, which is the read half of the hosted catalog. A
// user comparing this against the hosted server with a read-scoped key reads the
// same numbers; with a read-write key the hosted server shows two more.
func Summary() string {
	return fmt.Sprintf("%d tools, %d prompts", len(tools), len(prompts))
}
