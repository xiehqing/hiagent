package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	"github.com/xiehqing/hiagent/internal/proto"
)

const defaultBasePath = "/v1"

// Option customizes a Client.
type Option func(*Client)

// Client provides a small HTTP SDK for the Crush server API.
type Client struct {
	baseURL    *url.URL
	basePath   string
	httpClient *http.Client
	headers    http.Header
}

// Error represents an HTTP error returned by the Crush server.
type Error struct {
	StatusCode int
	Message    string
	Body       string
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return fmt.Sprintf("crush sdk request failed: status=%d message=%s", e.StatusCode, e.Message)
	}
	if e.Body != "" {
		return fmt.Sprintf("crush sdk request failed: status=%d body=%s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("crush sdk request failed: status=%d", e.StatusCode)
}

// WithHTTPClient overrides the underlying HTTP client.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// WithHeader adds a default header sent with every request.
func WithHeader(key, value string) Option {
	return func(c *Client) {
		c.headers.Add(key, value)
	}
}

// WithBasePath overrides the API base path. The default is /v1.
func WithBasePath(basePath string) Option {
	return func(c *Client) {
		if basePath == "" {
			return
		}
		c.basePath = normalizeBasePath(basePath)
	}
}

// NewClient creates a new HTTP SDK client for a Crush server.
func NewClient(baseURL string, opts ...Option) (*Client, error) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("base url must include scheme and host")
	}

	client := &Client{
		baseURL:    parsed,
		basePath:   defaultBasePath,
		httpClient: http.DefaultClient,
		headers:    make(http.Header),
	}
	for _, opt := range opts {
		opt(client)
	}
	return client, nil
}

// Do issues an HTTP request to the Crush API and optionally decodes the JSON response.
func (c *Client) Do(ctx context.Context, method, endpoint string, reqBody, respBody any) error {
	req, err := c.NewRequest(ctx, method, endpoint, reqBody)
	if err != nil {
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	return decodeResponse(resp, respBody)
}

// NewRequest builds an HTTP request against the configured Crush API base URL.
func (c *Client) NewRequest(ctx context.Context, method, endpoint string, reqBody any) (*http.Request, error) {
	reqReader, contentType, err := encodeBody(reqBody)
	if err != nil {
		return nil, err
	}

	reqURL := *c.baseURL
	reqURL.Path = joinURLPath(c.baseURL.Path, c.basePath, endpoint)

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), reqReader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	for key, values := range c.headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if respExpected(method) {
		req.Header.Set("Accept", "application/json")
	}

	return req, nil
}

// Health checks whether the server is reachable and healthy.
func (c *Client) Health(ctx context.Context) error {
	return c.Do(ctx, http.MethodGet, "/health", nil, nil)
}

// VersionInfo returns server version metadata.
func (c *Client) VersionInfo(ctx context.Context) (*proto.VersionInfo, error) {
	var info proto.VersionInfo
	if err := c.Do(ctx, http.MethodGet, "/version", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// ListWorkspaces returns all workspaces from the Crush server.
func (c *Client) ListWorkspaces(ctx context.Context) ([]proto.Workspace, error) {
	var workspaces []proto.Workspace
	if err := c.Do(ctx, http.MethodGet, "/workspaces", nil, &workspaces); err != nil {
		return nil, err
	}
	return workspaces, nil
}

// CreateWorkspace creates a workspace on the Crush server.
func (c *Client) CreateWorkspace(ctx context.Context, workspace proto.Workspace) (*proto.Workspace, error) {
	var created proto.Workspace
	if err := c.Do(ctx, http.MethodPost, "/workspaces", workspace, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

// GetWorkspace loads a workspace by ID.
func (c *Client) GetWorkspace(ctx context.Context, id string) (*proto.Workspace, error) {
	var workspace proto.Workspace
	if err := c.Do(ctx, http.MethodGet, "/workspaces/"+id, nil, &workspace); err != nil {
		return nil, err
	}
	return &workspace, nil
}

// DeleteWorkspace removes a workspace by ID.
func (c *Client) DeleteWorkspace(ctx context.Context, id string) error {
	return c.Do(ctx, http.MethodDelete, "/workspaces/"+id, nil, nil)
}

func encodeBody(body any) (io.Reader, string, error) {
	if body == nil {
		return nil, "", nil
	}

	switch v := body.(type) {
	case io.Reader:
		return v, "application/octet-stream", nil
	case []byte:
		return bytes.NewReader(v), "application/octet-stream", nil
	case string:
		return strings.NewReader(v), "text/plain; charset=utf-8", nil
	default:
		payload, err := json.Marshal(body)
		if err != nil {
			return nil, "", fmt.Errorf("marshal request body: %w", err)
		}
		return bytes.NewReader(payload), "application/json", nil
	}
}

func decodeResponse(resp *http.Response, out any) error {
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return buildError(resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func buildError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	bodyText := strings.TrimSpace(string(body))

	var apiErr proto.Error
	if len(body) > 0 && json.Unmarshal(body, &apiErr) == nil && apiErr.Message != "" {
		return &Error{
			StatusCode: resp.StatusCode,
			Message:    apiErr.Message,
			Body:       bodyText,
		}
	}

	return &Error{
		StatusCode: resp.StatusCode,
		Body:       bodyText,
	}
}

func joinURLPath(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		cleaned = append(cleaned, part)
	}
	if len(cleaned) == 0 {
		return "/"
	}
	joined := path.Join(cleaned...)
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	return joined
}

func normalizeBasePath(basePath string) string {
	if basePath == "" {
		return defaultBasePath
	}
	if !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}
	return strings.TrimRight(basePath, "/")
}

func respExpected(method string) bool {
	switch method {
	case http.MethodHead:
		return false
	default:
		return true
	}
}
