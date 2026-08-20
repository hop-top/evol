package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// --- fixtures -------------------------------------------------------------

const goodModelOutput = "===FRONTMATTER===\n" +
	"name: commit-style\ndescription: tightened\n" +
	"===BODY===\n## When to use\nAlways.\n" +
	"===RATIONALE===\nRemoved two redundant sections.\n"

// anthropicFixture is a minimal Messages API response the official SDK
// can parse.
func anthropicFixture(text string) string {
	blob, _ := json.Marshal(map[string]any{
		"id":          "msg_test",
		"type":        "message",
		"role":        "assistant",
		"model":       "test-model",
		"content":     []map[string]any{{"type": "text", "text": text}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	return string(blob)
}

// capturedCall is one recorded API request.
type capturedCall struct {
	header http.Header
	body   map[string]any
}

// newServer returns an httptest server whose per-call behavior comes
// from respond(callIndex) -> (status, responseBody).
func newServer(t *testing.T, respond func(i int) (int, string)) (*httptest.Server, *[]capturedCall) {
	t.Helper()
	var mu sync.Mutex
	calls := &[]capturedCall{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		idx := len(*calls)
		*calls = append(*calls, capturedCall{header: r.Header.Clone(), body: body})
		mu.Unlock()
		status, resp := respond(idx)
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(ts.Close)
	return ts, calls
}

// blockText flattens Anthropic content blocks (string or []blocks) to text.
func blockText(v any) string {
	switch tv := v.(type) {
	case string:
		return tv
	case []any:
		var b strings.Builder
		for _, blk := range tv {
			if m, ok := blk.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					b.WriteString(s)
				}
			}
		}
		return b.String()
	}
	return ""
}

func systemText(body map[string]any) string { return blockText(body["system"]) }

func userText(body map[string]any) string {
	msgs, _ := body["messages"].([]any)
	var b strings.Builder
	for _, m := range msgs {
		mm, ok := m.(map[string]any)
		if !ok || mm["role"] != "user" {
			continue
		}
		b.WriteString(blockText(mm["content"]))
	}
	return b.String()
}

// --- request helpers ------------------------------------------------------

func proposeRequest(budget int) Request {
	return Request{
		Evol: "1", Port: "generator", Action: "propose",
		Artifact: Artifact{
			Ref: "skills/commit-style", Kind: "skill",
			Frontmatter: "name: commit-style", Body: "## When to use\nSometimes.",
			Version: "b1946ac9",
		},
		Scores: []Score{{Version: "b1946ac9", Score: 0.71, Feedback: "misses scoped-package examples"}},
		Tabu: []TabuEntry{{
			Strategy: "reorder", Rationale: "moved examples first",
			Verdict: "rejected: holdout regression",
		}},
		Knowledge: []Passage{{Text: "Conventional Commits requires a type prefix", Source: "notes/cc"}},
		Budget:    Budget{MaxCandidates: budget},
	}
}

func runAdapter(t *testing.T, req any, env map[string]string) (int, Response, string) {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run(bytes.NewReader(raw), &stdout, &stderr,
		func(k string) string { return env[k] })
	var resp Response
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v (stdout=%q)", err, stdout.String())
		}
	}
	return code, resp, stderr.String()
}

// clearKeyEnv blanks real key env vars so machine config can't leak in.
func clearKeyEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("LLM_API_KEY", "")
}

func anthropicURI(baseURL string) string {
	return "anthropic://test-model?base_url=" + baseURL + "&api_key=test-key"
}

// --- tests ----------------------------------------------------------------

func TestHappyPathRequestShapeAndProvider(t *testing.T) {
	ts, calls := newServer(t, func(int) (int, string) {
		return 200, anthropicFixture(goodModelOutput)
	})
	code, resp, stderr := runAdapter(t, proposeRequest(1),
		map[string]string{"EVOL_GENERATOR_PROVIDER": anthropicURI(ts.URL)})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 API call, got %d", len(*calls))
	}
	call := (*calls)[0]
	if got := call.header.Get("x-api-key"); got != "test-key" {
		t.Errorf("x-api-key = %q", got)
	}
	if got := call.body["model"]; got != "test-model" {
		t.Errorf("model = %v", got)
	}
	if got, _ := call.body["max_tokens"].(float64); int(got) != maxTokens {
		t.Errorf("max_tokens = %v", call.body["max_tokens"])
	}
	sys := systemText(call.body)
	for _, want := range []string{`"tighten"`, markFrontmatter, markBody, markRationale} {
		if !strings.Contains(sys, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
	user := userText(call.body)
	for _, want := range []string{
		"## When to use", "misses scoped-package examples",
		"moved examples first", "Conventional Commits requires a type prefix",
	} {
		if !strings.Contains(user, want) {
			t.Errorf("user prompt missing %q", want)
		}
	}
	if len(resp.Candidates) != 1 {
		t.Fatalf("candidates = %d", len(resp.Candidates))
	}
	c := resp.Candidates[0]
	if c.ID != "cand-01" || c.Strategy != "tighten" {
		t.Errorf("candidate id/strategy = %q/%q", c.ID, c.Strategy)
	}
	if c.Rationale != "Removed two redundant sections." {
		t.Errorf("rationale = %q", c.Rationale)
	}
	wantProvider := "anthropic://test-model?base_url=" + ts.URL
	if c.Provider != wantProvider {
		t.Errorf("provider = %q, want %q (api_key must be stripped)", c.Provider, wantProvider)
	}
}

func TestRoundRobinStrategies(t *testing.T) {
	ts, calls := newServer(t, func(int) (int, string) {
		return 200, anthropicFixture(goodModelOutput)
	})
	code, resp, stderr := runAdapter(t, proposeRequest(4),
		map[string]string{"EVOL_GENERATOR_PROVIDER": anthropicURI(ts.URL)})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	want := []string{"tighten", "restructure", "add-example", "sharpen-triggers"}
	if len(*calls) != len(want) {
		t.Fatalf("calls = %d", len(*calls))
	}
	for i, name := range want {
		if sys := systemText((*calls)[i].body); !strings.Contains(sys, fmt.Sprintf("%q", name)) {
			t.Errorf("call %d: system prompt missing strategy %q", i, name)
		}
		if resp.Candidates[i].Strategy != name {
			t.Errorf("candidate %d strategy = %q, want %q", i, resp.Candidates[i].Strategy, name)
		}
	}
}

func TestUnparseableOutputDroppedIsDryGeneration(t *testing.T) {
	ts, _ := newServer(t, func(int) (int, string) {
		return 200, anthropicFixture("no markers here at all")
	})
	code, resp, stderr := runAdapter(t, proposeRequest(2),
		map[string]string{"EVOL_GENERATOR_PROVIDER": anthropicURI(ts.URL)})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if len(resp.Candidates) != 0 {
		t.Fatalf("candidates = %d, want 0", len(resp.Candidates))
	}
	if !strings.Contains(stderr, "unparseable") {
		t.Errorf("stderr missing drop diagnostic: %s", stderr)
	}
}

func TestBadRequestDropsCandidateDenseIDs(t *testing.T) {
	ts, _ := newServer(t, func(i int) (int, string) {
		if i == 0 {
			return 400, `{"type":"error","error":{"type":"invalid_request_error","message":"boom"}}`
		}
		return 200, anthropicFixture(goodModelOutput)
	})
	code, resp, stderr := runAdapter(t, proposeRequest(2),
		map[string]string{"EVOL_GENERATOR_PROVIDER": anthropicURI(ts.URL)})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if len(resp.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(resp.Candidates))
	}
	if resp.Candidates[0].ID != "cand-01" {
		t.Errorf("dense id = %q, want cand-01", resp.Candidates[0].ID)
	}
	// second strategy survived, so the id is dense over survivors
	if resp.Candidates[0].Strategy != "restructure" {
		t.Errorf("strategy = %q, want restructure", resp.Candidates[0].Strategy)
	}
}

func TestAuthErrorIsAdapterError(t *testing.T) {
	ts, _ := newServer(t, func(int) (int, string) {
		return 401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`
	})
	code, _, stderr := runAdapter(t, proposeRequest(2),
		map[string]string{"EVOL_GENERATOR_PROVIDER": anthropicURI(ts.URL)})
	if code != 1 {
		t.Fatalf("exit=%d, want 1 (stderr=%s)", code, stderr)
	}
	if !strings.Contains(stderr, "auth") {
		t.Errorf("stderr missing auth diagnostic: %s", stderr)
	}
}

func TestMissingKeyIsAdapterError(t *testing.T) {
	clearKeyEnv(t)
	code, _, stderr := runAdapter(t, proposeRequest(1),
		map[string]string{"EVOL_GENERATOR_PROVIDER": "anthropic://test-model"})
	if code != 1 {
		t.Fatalf("exit=%d, want 1", code)
	}
	if !strings.Contains(stderr, "ANTHROPIC_API_KEY") {
		t.Errorf("stderr missing key hint: %s", stderr)
	}
}

func TestBudgetRejected(t *testing.T) {
	code, _, stderr := runAdapter(t, proposeRequest(0), nil)
	if code != 1 || !strings.Contains(stderr, "max_candidates") {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
}

func TestEnvelopeRejected(t *testing.T) {
	req := proposeRequest(1)
	req.Port = "scorer"
	code, _, stderr := runAdapter(t, req, nil)
	if code != 1 || !strings.Contains(stderr, "unsupported envelope") {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
}

func TestMalformedJSONRejected(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(strings.NewReader("{nope"), &stdout, &stderr,
		func(string) string { return "" })
	if code != 1 || !strings.Contains(stderr.String(), "malformed request JSON") {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
}

func TestUnsupportedSchemeRejected(t *testing.T) {
	code, _, stderr := runAdapter(t, proposeRequest(1),
		map[string]string{"EVOL_GENERATOR_PROVIDER": "bogus://model"})
	if code != 1 || !strings.Contains(stderr, "unsupported provider scheme") {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "ollama") {
		t.Errorf("stderr should list supported schemes: %s", stderr)
	}
}

func TestOllamaURIConstructsWithoutKey(t *testing.T) {
	clearKeyEnv(t)
	c, err := newKitClient("ollama://localhost:11500/qwen3")
	if err != nil {
		t.Fatalf("ollama client: %v", err)
	}
	if got := c.providerLabel(); got != "ollama://localhost:11500/qwen3" {
		t.Errorf("provider label = %q", got)
	}
	if _, err := newKitClient("ollama://"); err == nil {
		t.Error("ollama without model should fail construction")
	}
}

func TestDeprecatedModelEnvMapsToAnthropic(t *testing.T) {
	clearKeyEnv(t)
	code, _, stderr := runAdapter(t, proposeRequest(1),
		map[string]string{"EVOL_GENERATOR_MODEL": "claude-sonnet-5"})
	if code != 1 {
		t.Fatalf("exit=%d, want 1 (no key present)", code)
	}
	if !strings.Contains(stderr, "deprecated") {
		t.Errorf("stderr missing deprecation note: %s", stderr)
	}
	if !strings.Contains(stderr, "anthropic://claude-sonnet-5") {
		t.Errorf("stderr should show the mapped URI: %s", stderr)
	}
}

func TestSanitizeURI(t *testing.T) {
	cases := map[string]string{
		"anthropic://m":                          "anthropic://m",
		"anthropic://m?api_key=s":                "anthropic://m",
		"anthropic://m?api_key=s&base_url=http://x": "anthropic://m?base_url=http://x",
		"ollama://h:1/m?base_url=http://x":       "ollama://h:1/m?base_url=http://x",
	}
	for in, want := range cases {
		if got := sanitizeURI(in); got != want {
			t.Errorf("sanitizeURI(%q) = %q, want %q", in, got, want)
		}
	}
}
