package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"hop.top/kit/go/ai/llm"
	"hop.top/kit/go/ai/llm/anthropic"
	llmerrors "hop.top/kit/go/ai/llm/errors"
	"hop.top/kit/go/ai/llm/ollama"
	"hop.top/kit/go/ai/llm/openai"
	"hop.top/kit/go/ai/llm/routellm"
)

const (
	defaultProviderURI = "anthropic://claude-sonnet-5"
	maxTokens          = 4096
	callTimeout        = 60 * time.Second
)

// factories maps URI schemes to provider constructors. An explicit map
// instead of the package-level registry keeps the supported set equal to
// the documented set and avoids double-registration panics in tests.
var factories = map[string]llm.Factory{
	"anthropic":  anthropic.New,
	"ollama":     ollama.New,
	"routellm":   routellm.New,
	"openai":     openai.New,
	"openrouter": openai.New,
	"xai":        openai.New,
	"groq":       openai.New,
	"together":   openai.New,
	"fireworks":  openai.New,
	"deepseek":   openai.New,
	"mistral":    openai.New,
}

func supportedSchemes() string {
	names := make([]string, 0, len(factories))
	for s := range factories {
		names = append(names, s)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

type kitClient struct {
	client   *llm.Client
	provider string // sanitized URI, safe to echo into responses
}

// newKitClient resolves a provider URI into a ready client.
//
// Configuration is deliberately explicit: the URI (host, params) plus
// the provider-specific API-key env var (via [llm.SecretFor], which
// also honors the universal LLM_API_KEY). The llm.yaml config file is
// intentionally NOT consulted — an adapter invocation should be fully
// described by its environment.
func newKitClient(uri string) (*kitClient, error) {
	parsed, err := llm.ParseURI(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid provider URI: %w", err)
	}
	factory, ok := factories[parsed.Scheme]
	if !ok {
		return nil, fmt.Errorf("unsupported provider scheme %q (supported: %s)",
			parsed.Scheme, supportedSchemes())
	}

	cfg := llm.ResolvedConfig{
		URI:      parsed,
		Provider: llm.ProviderConfig{Model: parsed.Model},
	}
	if parsed.Host != "" {
		cfg.Provider.BaseURL = "http://" + parsed.Host
	}
	if parsed.Params != nil {
		cfg.Provider.Params = parsed.Params
		if v, ok := parsed.Params["api_key"]; ok {
			cfg.Provider.APIKey = v
		}
		if v, ok := parsed.Params["base_url"]; ok {
			cfg.Provider.BaseURL = v
		}
	}
	if cfg.Provider.APIKey == "" {
		// Best-effort: provider-specific env var, then LLM_API_KEY.
		// Not-found is fine — key-less providers (ollama) never need it
		// and key-requiring factories reject below with a pointed hint.
		if key, kerr := llm.SecretFor(context.Background(), nil, uri); kerr == nil {
			cfg.Provider.APIKey = key
		}
	}

	provider, err := factory(cfg)
	if err != nil {
		return nil, fmt.Errorf("%w (hint: set %s or pass ?api_key= in the URI)",
			err, llm.EnvKeyFor(uri))
	}
	return &kitClient{
		client:   llm.NewClient(provider),
		provider: sanitizeURI(uri),
	}, nil
}

// complete performs one completion call: system + user message, one
// candidate per call, no retries beyond what the provider SDK does.
func (c *kitClient) complete(system, user string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()

	resp, err := c.client.Complete(ctx, llm.Request{
		MaxTokens: maxTokens,
		Messages: []llm.Message{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	})
	if err != nil {
		return "", err
	}
	if resp.Content == "" {
		return "", errors.New("response contains no text content")
	}
	return resp.Content, nil
}

func (c *kitClient) providerLabel() string { return c.provider }

// isAuthError reports whether err is an authentication/authorization
// failure — misconfiguration, so run() exits non-zero instead of
// dropping the candidate.
func isAuthError(err error) bool {
	var ae *llmerrors.ErrAuth
	return errors.As(err, &ae)
}

// sanitizeURI strips the api_key query param so the URI is safe to
// echo into responses and diagnostics.
func sanitizeURI(raw string) string {
	i := strings.Index(raw, "?")
	if i < 0 {
		return raw
	}
	base, query := raw[:i], raw[i+1:]
	kept := make([]string, 0, 4)
	for _, kv := range strings.Split(query, "&") {
		if kv == "" || strings.HasPrefix(kv, "api_key=") {
			continue
		}
		kept = append(kept, kv)
	}
	if len(kept) == 0 {
		return base
	}
	return base + "?" + strings.Join(kept, "&")
}

// resolveProviderURI applies the env contract: EVOL_GENERATOR_PROVIDER
// wins; EVOL_GENERATOR_MODEL is a deprecated fallback mapped to the
// anthropic scheme; otherwise the default. The returned note, when
// non-empty, is a deprecation diagnostic for stderr.
func resolveProviderURI(getenv func(string) string) (uri, note string) {
	if v := getenv("EVOL_GENERATOR_PROVIDER"); v != "" {
		return v, ""
	}
	if m := getenv("EVOL_GENERATOR_MODEL"); m != "" {
		return "anthropic://" + m,
			"generator-llm: EVOL_GENERATOR_MODEL is deprecated; use EVOL_GENERATOR_PROVIDER=anthropic://" + m
	}
	return defaultProviderURI, ""
}

// envGetenv is the production getenv.
func envGetenv(key string) string { return os.Getenv(key) }
