package mcp

import "github.com/LevelFourAI/levelfour-cli/internal/api"

// Every tool reaches the outside world through here, so the surface can read
// exactly what the stored credential can read and nothing else.
type restFetcher struct {
	client *api.Client
}

func NewRESTFetcher(client *api.Client) Fetcher {
	return restFetcher{client: client}
}

func (r restFetcher) Fetch(path string) (any, error) {
	raw, err := r.client.Get(path)
	if err != nil {
		return nil, err
	}
	// Routes answer inside a {success, data, timestamp} envelope.
	if data, ok := raw["data"]; ok {
		return data, nil
	}
	return raw, nil
}
