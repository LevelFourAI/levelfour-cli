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

// Server.AddTool is used rather than the generic mcp.AddTool[In, Out] because
// the latter derives the input schema from a Go struct. These schemas have to
// match the hosted server's output exactly, and inference produces a near-miss.

var readOnly = &sdk.ToolAnnotations{
	ReadOnlyHint:    true,
	DestructiveHint: boolPtr(false),
	IdempotentHint:  true,
	OpenWorldHint:   boolPtr(false),
}

func boolPtr(b bool) *bool { return &b }

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

// Session is one stdio serving session. Notices goes to stderr, which the client
// captures into its own log, and is where a user confirms which binary answered.
type Session struct {
	Fetcher Fetcher
	Version string
	In      io.ReadCloser
	Out     io.WriteCloser
	Notices io.Writer
}

func Serve(ctx context.Context, s Session) error {
	s.announce()
	err := NewServer(s.Fetcher, s.Version).Run(ctx, &sdk.IOTransport{Reader: s.In, Writer: s.Out})
	if isCleanShutdown(err) {
		return nil
	}
	return err
}

func (s Session) announce() {
	fmt.Fprintf(s.Notices, "levelfour mcp %s serving %d tools over stdio\n", s.Version, len(tools))
	fmt.Fprintln(s.Notices, "reading the LevelFour API with the stored credential")
}

// The SDK formats the underlying EOF with %v rather than %w, so the code is all
// that is left to match on.
var errServerClosing = &jsonrpc.Error{Code: -32004, Message: "server is closing"}

// A client hanging up is a normal ending for a stdio server. Returning an error
// would make every ordinary exit non-zero, which clients log as a crash.
func isCleanShutdown(err error) bool {
	return err == nil ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, errServerClosing)
}

func ToolNames() []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.name)
	}
	return names
}

// Counts what this binary registers, which is the read half of the hosted
// catalog. A read-write key sees more on the hosted server.
func Summary() string {
	return fmt.Sprintf("%d tools, %d prompts", len(tools), len(prompts))
}
