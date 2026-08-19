package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	defaultBaseURL   = "https://api.anthropic.com"
	defaultModel     = "claude-sonnet-5"
	anthropicVersion = "2023-06-01"
	maxTokens        = 4096
	callTimeout      = 60 * time.Second
)

type config struct {
	apiKey  string
	model   string
	baseURL string
}

func envConfig() config {
	cfg := config{
		apiKey:  os.Getenv("ANTHROPIC_API_KEY"),
		model:   os.Getenv("EVOL_GENERATOR_MODEL"),
		baseURL: os.Getenv("ANTHROPIC_BASE_URL"),
	}
	if cfg.model == "" {
		cfg.model = defaultModel
	}
	if cfg.baseURL == "" {
		cfg.baseURL = defaultBaseURL
	}
	return cfg
}

type anthropicClient struct {
	cfg  config
	http *http.Client
}

func newAnthropicClient(cfg config) *anthropicClient {
	return &anthropicClient{cfg: cfg, http: &http.Client{Timeout: callTimeout}}
}

type messagesRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Content []contentBlock `json:"content"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// authError marks HTTP 401/403 responses so run() can distinguish
// misconfiguration (adapter error, non-zero exit) from per-candidate
// failures (dropped candidate).
type authError struct{ status int }

func (e *authError) Error() string {
	return fmt.Sprintf("API returned HTTP %d (check ANTHROPIC_API_KEY)", e.status)
}

func isAuthError(err error) bool {
	var ae *authError
	return errors.As(err, &ae)
}

// complete performs one Messages API call and returns the first text
// block of the response. One call per candidate; no retries in v0.
func (c *anthropicClient) complete(system, user string) (string, error) {
	body, err := json.Marshal(messagesRequest{
		Model:     c.cfg.model,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  []message{{Role: "user", Content: user}},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.cfg.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", c.cfg.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("call API: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-side close; nothing actionable

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", &authError{status: resp.StatusCode}
	}
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("API returned HTTP %d: %s", resp.StatusCode, snippet)
	}

	var mr messagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&mr); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	for _, b := range mr.Content {
		if b.Type == "text" {
			return b.Text, nil
		}
	}
	return "", errors.New("response contains no text block")
}
