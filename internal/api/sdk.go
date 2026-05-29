package api

import (
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
		levelfour.WithHTTPClient(SecureHTTPClient()),
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
