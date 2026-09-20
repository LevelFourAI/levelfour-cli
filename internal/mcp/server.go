package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Session is one stdio serving session. Notices goes to stderr, which the client
// captures into its own log, and is where a user confirms which binary answered.
type Session struct {
	Upstream Upstream
	In       io.ReadCloser
	Out      io.WriteCloser
	Notices  io.Writer
}

func Serve(ctx context.Context, s Session) error {
	upstream, surface, err := start(ctx, s.Upstream)
	if err != nil {
		return err
	}
	defer func() { _ = upstream.Close() }()
	s.announce(surface)

	err = newBridgeServer(upstream, s.Upstream.Version).Run(ctx, &sdk.IOTransport{Reader: s.In, Writer: s.Out})
	if isCleanShutdown(err) {
		return nil
	}
	return err
}

func (s Session) announce(surface Surface) {
	fmt.Fprintf(s.Notices, "levelfour mcp %s serving %s over stdio\n", s.Upstream.Version, surface)
	fmt.Fprintf(s.Notices, "relaying %s with the stored credential\n", s.Upstream.Endpoint)
}

// The SDK formats the underlying EOF with %v rather than %w, so the code is all
// that is left to match on.
var errServerClosing = &jsonrpc.Error{Code: codeServerClosing, Message: "server is closing"}

// A client hanging up is a normal ending for a stdio server. Returning an error
// would make every ordinary exit non-zero, which clients log as a crash.
func isCleanShutdown(err error) bool {
	return err == nil ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, errServerClosing)
}
