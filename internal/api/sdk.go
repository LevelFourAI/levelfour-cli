package api

import (
	"net/http"

	"github.com/LevelFourAI/levelfour-go/levelfour"
)

type SDKClient struct {
	sdk     *levelfour.Client
	raw     *RawClient
	BaseURL string
	version string
}

func NewSDKClient(baseURL, token, version string) (*SDKClient, error) {
	if err := ValidateBaseURL(baseURL); err != nil {
		return nil, err
	}

	sdk, err := levelfour.NewClient(token,
		levelfour.WithBaseURL(baseURL),
		levelfour.WithHTTPClient(sdkHTTPClient(version)),
	)
	if err != nil {
		return nil, err
	}

	raw, _ := NewRawClient(baseURL, token, version)

	return &SDKClient{
		sdk:     sdk,
		raw:     raw,
		BaseURL: baseURL,
		version: version,
	}, nil
}

// The SDK sends its own User-Agent, so without this its calls would not say
// they come from the CLI, the way the CLI's direct calls do.
type userAgentTransport struct {
	base  http.RoundTripper
	agent string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", t.agent)
	return t.base.RoundTrip(req)
}

func sdkHTTPClient(version string) *http.Client {
	client := SecureHTTPClient()
	client.Transport = &userAgentTransport{base: client.Transport, agent: UserAgent(version)}
	return client
}

func NewUnauthSDKClient(baseURL, version string) *SDKClient {
	return &SDKClient{
		raw:     NewUnauthRawClient(baseURL, version),
		BaseURL: baseURL,
		version: version,
	}
}

func (c *SDKClient) SDK() *levelfour.Client {
	return c.sdk
}

func (c *SDKClient) Raw() *RawClient {
	return c.raw
}
