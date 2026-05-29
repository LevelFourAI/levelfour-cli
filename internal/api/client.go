package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type Client struct {
	BaseURL    string
	apiKey     string
	httpClient *http.Client
	version    string
}

func NewClient(baseURL, apiKey, version string) (*Client, error) {
	if err := ValidateBaseURL(baseURL); err != nil {
		return nil, err
	}
	return &Client{
		BaseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: SecureHTTPClient(),
		version:    version,
	}, nil
}

func NewUnauthenticatedClient(baseURL, version string) *Client {
	return &Client{
		BaseURL:    baseURL,
		httpClient: SecureHTTPClient(),
		version:    version,
	}
}

func (c *Client) Get(path string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(context.Background(), "GET", c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) Post(path string, payload interface{}) (map[string]interface{}, error) {
	return c.doWithPayload("POST", path, payload)
}

func (c *Client) Delete(path string) (map[string]interface{}, error) {
	req, err := http.NewRequestWithContext(context.Background(), "DELETE", c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req)
}

func (c *Client) Patch(path string, payload interface{}) (map[string]interface{}, error) {
	return c.doWithPayload("PATCH", path, payload)
}

func (c *Client) Put(path string, payload interface{}) (map[string]interface{}, error) {
	return c.doWithPayload("PUT", path, payload)
}

func (c *Client) doWithPayload(method, path string, payload interface{}) (map[string]interface{}, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req)
}

func (c *Client) DoRaw(method, path string, body io.Reader) (*RawResponse, error) {
	rc := &RawClient{
		BaseURL:    c.BaseURL,
		apiKey:     c.apiKey,
		httpClient: c.httpClient,
		version:    c.version,
	}
	return rc.DoRaw(method, path, body)
}

func (c *Client) do(req *http.Request) (map[string]interface{}, error) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "LevelFour-CLI/"+c.version)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errResp map[string]interface{}
		if json.Unmarshal(respBody, &errResp) == nil {
			if errObj, ok := errResp["error"].(map[string]interface{}); ok {
				if msg, ok := errObj["message"].(string); ok {
					return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, msg)
				}
			}
		}
		return nil, fmt.Errorf("API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("invalid JSON response: %w", err)
	}
	return result, nil
}

func Decode[T any](raw map[string]interface{}, target *T) error {
	data, ok := raw["data"]
	if !ok {
		return fmt.Errorf("response missing 'data' field")
	}
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to re-marshal data: %w", err)
	}
	if err := json.Unmarshal(b, target); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}
	return nil
}

func ToFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}
