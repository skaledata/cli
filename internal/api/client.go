package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/skaledata/cli/internal/config"
)

// Client wraps HTTP calls to the SkaleData API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
	authHeader string
}

// APIError represents an error response from the API.
type APIError struct {
	StatusCode int
	Detail     string `json:"detail"`
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Detail)
	}
	return fmt.Sprintf("API error %d", e.StatusCode)
}

// NewClient creates a new API client. It reads auth from config.
func NewClient() (*Client, error) {
	auth, err := config.GetAuthHeader()
	if err != nil {
		return nil, err
	}
	return &Client{
		BaseURL: config.APIURL(),
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		authHeader: auth,
	}, nil
}

// Get performs an authenticated GET request.
func (c *Client) Get(path string, result interface{}) error {
	return c.do("GET", path, nil, result)
}

// Post performs an authenticated POST request.
func (c *Client) Post(path string, body, result interface{}) error {
	return c.do("POST", path, body, result)
}

// Put performs an authenticated PUT request.
func (c *Client) Put(path string, body, result interface{}) error {
	return c.do("PUT", path, body, result)
}

// Delete performs an authenticated DELETE request.
func (c *Client) Delete(path string, result interface{}) error {
	return c.do("DELETE", path, nil, result)
}

func (c *Client) do(method, path string, body, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := c.BaseURL + path
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "skaledata-cli/0.1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		apiErr := &APIError{StatusCode: resp.StatusCode}
		_ = json.Unmarshal(respBody, apiErr)
		return apiErr
	}

	if result != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return nil
}
