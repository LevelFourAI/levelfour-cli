package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type RawClient struct {
	BaseURL    string
	apiKey     string
	httpClient *http.Client
	version    string
}

func NewRawClient(baseURL, apiKey, version string) (*RawClient, error) {
	if err := ValidateBaseURL(baseURL); err != nil {
		return nil, err
	}
	return &RawClient{
		BaseURL:    baseURL,
		apiKey:     apiKey,
		httpClient: SecureHTTPClient(),
		version:    version,
	}, nil
}

func NewUnauthRawClient(baseURL, version string) *RawClient {
	return &RawClient{
		BaseURL:    baseURL,
		httpClient: SecureHTTPClient(),
		version:    version,
	}
}

type RawResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func (c *RawClient) DoRaw(method, path string, body io.Reader) (*RawResponse, error) {
	req, err := http.NewRequestWithContext(context.Background(), method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}

	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "LevelFour-CLI/"+c.version)

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return &RawResponse{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       respBody,
	}, nil
}

func (r *RawResponse) DecodeError() error {
	var errResp map[string]interface{}
	if json.Unmarshal(r.Body, &errResp) == nil {
		if errObj, ok := errResp["error"].(map[string]interface{}); ok {
			if msg, ok := errObj["message"].(string); ok {
				return fmt.Errorf("API error (%d): %s", r.StatusCode, msg)
			}
		}
	}
	return fmt.Errorf("API error (%d): %s", r.StatusCode, strings.TrimSpace(string(r.Body)))
}

func ValidateBaseURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}
	if u.Scheme == "http" && u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1" {
		return fmt.Errorf("insecure HTTP not allowed for non-localhost URLs: use HTTPS")
	}
	return nil
}

func SecureHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   10,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: transport,
	}
}

func BuildQueryString(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	parts := make([]string, 0, len(params))
	for k, v := range params {
		if v != "" {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}

func (c *RawClient) AnalyzeIaC(ctx context.Context, req *AnalyzePrRequest) (*AnalyzePrResponse, error) {
	body, _ := json.Marshal(req)

	raw, err := c.DoRaw("POST", "/api/v1/iac-analysis/analyze", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	if raw.StatusCode >= 400 {
		return nil, raw.DecodeError()
	}

	var envelope struct {
		Data *AnalyzePrResponse `json:"data"`
	}
	if err := json.Unmarshal(raw.Body, &envelope); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("API returned empty response data")
	}
	return envelope.Data, nil
}

func (c *RawClient) CreateDeviceCode(ctx context.Context) (*DeviceCodeResponse, error) {
	raw, err := c.DoRaw("POST", "/api/v1/auth/device", nil)
	if err != nil {
		return nil, err
	}
	if raw.StatusCode >= 400 {
		return nil, raw.DecodeError()
	}

	var resp DeviceCodeResponse
	if err := json.Unmarshal(raw.Body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	if resp.Data == nil || resp.Data.DeviceCode == "" {
		return nil, fmt.Errorf("invalid device code response")
	}
	return &resp, nil
}

func (c *RawClient) PollDeviceCode(ctx context.Context, deviceCode string) (*PollResponse, error) {
	raw, err := c.DoRaw("GET", "/api/v1/auth/device/"+url.PathEscape(deviceCode), nil)
	if err != nil {
		return nil, err
	}
	if raw.StatusCode >= 400 {
		return nil, raw.DecodeError()
	}

	var resp PollResponse
	if err := json.Unmarshal(raw.Body, &resp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}
	return &resp, nil
}

func (c *RawClient) VerifyAuth(ctx context.Context) error {
	raw, err := c.DoRaw("GET", "/api/v1/auth/device/verify", nil)
	if err != nil {
		return err
	}
	if raw.StatusCode >= 400 {
		return raw.DecodeError()
	}
	return nil
}

func BuildQueryStringMulti(params map[string][]string) string {
	if len(params) == 0 {
		return ""
	}
	parts := make([]string, 0)
	for k, vals := range params {
		for _, v := range vals {
			if v != "" {
				parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "?" + strings.Join(parts, "&")
}
