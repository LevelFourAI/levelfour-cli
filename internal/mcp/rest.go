package mcp

import "github.com/LevelFourAI/levelfour-cli/internal/api"

// restFetcher is the only path from a tool to the outside world. Every tool goes
// through the same authenticated client the rest of the CLI uses, so the local
// server can reach exactly what the stored credential can reach and nothing else.
type restFetcher struct {
	client *api.Client
}

// NewRESTFetcher adapts the CLI's API client to the Fetcher the tools call.
func NewRESTFetcher(client *api.Client) Fetcher {
	return restFetcher{client: client}
}

func (r restFetcher) Fetch(path string) (any, error) {
	raw, err := r.client.Get(path)
	if err != nil {
		return nil, err
	}
	// Routes answer inside a {success, data, timestamp} envelope. Anything that
	// does not is passed through rather than dropped.
	if data, ok := raw["data"]; ok {
		return data, nil
	}
	return raw, nil
}
