package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sampleRequest(maxCandidates int) Request {
	return Request{
		Evol:   "1",
		Port:   "generator",
		Action: "propose",
		Artifact: Artifact{
			Ref:         "skills/commit-style",
			Kind:        "skill",
			Frontmatter: "name: commit-style\ndescription: how to write commits",
			Body:        "## When to use\nAlways.\n",
			Version:     "b1946ac9",
		},
		Scores: []Score{{Version: "b1946ac9", Score: 0.71, Feedback: "misses scoped-package examples"}},
		Tabu: []TabuEntry{{
			Strategy:  "reorder",
			Rationale: "moved examples first",
			Verdict:   "rejected: holdout regression",
		}},
		Knowledge: []Passage{{Text: "Conventional Commits requires a type prefix.", Source: "notes/cc"}},
		Budget:    Budget{MaxCandidates: maxCandidates},
	}
}

func wellFormedModelText() string {
	return fmt.Sprintf("%s\nname: commit-style\ndescription: how to write commits\n%s\n## When to use\nAlways.\n## Examples\nfeat(scope): add thing\n%s\nAdds the scoped-package example the feedback asked for.",
		markFrontmatter, markBody, markRationale)
}

func modelResponse(text string) string {
	resp := messagesResponse{Content: []contentBlock{{Type: "text", Text: text}}}
	raw, _ := json.Marshal(resp)
	return string(raw)
}

func runAdapter(t *testing.T, req Request, cfg config) (int, Response, string) {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run(bytes.NewReader(raw), &stdout, &stderr, cfg)
	var resp Response
	if stdout.Len() > 0 {
		if err := json.Unmarshal(stdout.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v (stdout %q)", err, stdout.String())
		}
	}
	return code, resp, stderr.String()
}

func TestProposeRequestShapeAndParse(t *testing.T) {
	var captured []messagesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %q, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "test-key" {
			t.Errorf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Error("anthropic-version header missing")
		}
		var mr messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&mr); err != nil {
			t.Errorf("decode API request: %v", err)
		}
		captured = append(captured, mr)
		_, _ = fmt.Fprint(w, modelResponse(wellFormedModelText()))
	}))
	defer srv.Close()

	cfg := config{apiKey: "test-key", model: "test-model", baseURL: srv.URL}
	code, resp, stderr := runAdapter(t, sampleRequest(1), cfg)

	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if len(captured) != 1 {
		t.Fatalf("API calls = %d, want 1", len(captured))
	}
	mr := captured[0]
	if mr.Model != "test-model" {
		t.Errorf("model = %q", mr.Model)
	}
	if mr.MaxTokens <= 0 {
		t.Errorf("max_tokens = %d, want > 0", mr.MaxTokens)
	}
	if !strings.Contains(mr.System, `"tighten"`) {
		t.Errorf("system prompt missing strategy name: %q", mr.System)
	}
	if len(mr.Messages) != 1 || mr.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v, want single user message", mr.Messages)
	}
	user := mr.Messages[0].Content
	for _, want := range []string{
		"## When to use",        // artifact body
		"moved examples first",  // tabu rationale
		"do NOT re-propose",     // tabu instruction
		"Conventional Commits",  // knowledge passage
		"misses scoped-package", // score feedback
	} {
		if !strings.Contains(user, want) {
			t.Errorf("user prompt missing %q", want)
		}
	}

	if len(resp.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1 (stderr: %s)", len(resp.Candidates), stderr)
	}
	c := resp.Candidates[0]
	if c.ID != "cand-01" || c.Strategy != "tighten" {
		t.Errorf("candidate id/strategy = %q/%q", c.ID, c.Strategy)
	}
	if !strings.Contains(c.Body, "## Examples") {
		t.Errorf("candidate body not parsed: %q", c.Body)
	}
	if c.Frontmatter == "" || c.Rationale == "" {
		t.Errorf("candidate missing sections: %+v", c)
	}
	if resp.Evol != "1" || resp.Port != "generator" || resp.Action != "propose" {
		t.Errorf("response envelope = %+v", resp)
	}
}

func TestProposeRoundRobinStrategies(t *testing.T) {
	var systems []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var mr messagesRequest
		_ = json.NewDecoder(r.Body).Decode(&mr)
		systems = append(systems, mr.System)
		_, _ = fmt.Fprint(w, modelResponse(wellFormedModelText()))
	}))
	defer srv.Close()

	cfg := config{apiKey: "k", model: "m", baseURL: srv.URL}
	code, resp, stderr := runAdapter(t, sampleRequest(3), cfg)

	if code != 0 {
		t.Fatalf("exit = %d (stderr: %s)", code, stderr)
	}
	if len(resp.Candidates) != 3 {
		t.Fatalf("candidates = %d, want 3", len(resp.Candidates))
	}
	wantOrder := []string{"tighten", "restructure", "add-example"}
	for i, want := range wantOrder {
		if resp.Candidates[i].Strategy != want {
			t.Errorf("candidate %d strategy = %q, want %q", i, resp.Candidates[i].Strategy, want)
		}
		if !strings.Contains(systems[i], fmt.Sprintf("%q", want)) {
			t.Errorf("call %d system prompt missing %q", i, want)
		}
	}
}

func TestProposeMalformedModelOutputDropped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, modelResponse("no markers here at all"))
	}))
	defer srv.Close()

	cfg := config{apiKey: "k", model: "m", baseURL: srv.URL}
	code, resp, stderr := runAdapter(t, sampleRequest(2), cfg)

	if code != 0 {
		t.Fatalf("exit = %d, want 0 (dry generation)", code)
	}
	if len(resp.Candidates) != 0 {
		t.Fatalf("candidates = %d, want 0", len(resp.Candidates))
	}
	if !strings.Contains(stderr, "unparseable") {
		t.Errorf("stderr missing drop diagnostic: %q", stderr)
	}
}

func TestProposeMissingAPIKey(t *testing.T) {
	cfg := config{apiKey: "", model: "m", baseURL: "http://localhost:0"}
	code, _, stderr := runAdapter(t, sampleRequest(1), cfg)
	if code == 0 {
		t.Fatal("exit = 0, want non-zero for missing API key")
	}
	if !strings.Contains(stderr, "ANTHROPIC_API_KEY") {
		t.Errorf("stderr = %q, want mention of ANTHROPIC_API_KEY", stderr)
	}
}

func TestProposeAuthErrorIsAdapterError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()

	cfg := config{apiKey: "bad", model: "m", baseURL: srv.URL}
	code, _, stderr := runAdapter(t, sampleRequest(2), cfg)
	if code == 0 {
		t.Fatal("exit = 0, want non-zero for auth failure")
	}
	if !strings.Contains(stderr, "auth error") {
		t.Errorf("stderr = %q, want auth diagnostic", stderr)
	}
}

func TestProposeTransportErrorDropsCandidate(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprint(w, modelResponse(wellFormedModelText()))
	}))
	defer srv.Close()

	cfg := config{apiKey: "k", model: "m", baseURL: srv.URL}
	code, resp, stderr := runAdapter(t, sampleRequest(2), cfg)

	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if len(resp.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1 (first call dropped)", len(resp.Candidates))
	}
	if resp.Candidates[0].ID != "cand-01" {
		t.Errorf("surviving candidate id = %q, want cand-01 (ids are dense)", resp.Candidates[0].ID)
	}
	if !strings.Contains(stderr, "dropped") {
		t.Errorf("stderr missing drop diagnostic: %q", stderr)
	}
}

func TestBadEnvelopeRejected(t *testing.T) {
	req := sampleRequest(1)
	req.Port = "corpus"
	cfg := config{apiKey: "k", model: "m", baseURL: "http://localhost:0"}
	code, _, stderr := runAdapter(t, req, cfg)
	if code == 0 {
		t.Fatal("exit = 0, want non-zero for wrong port")
	}
	if !strings.Contains(stderr, "unsupported envelope") {
		t.Errorf("stderr = %q", stderr)
	}
}

func TestMalformedRequestJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run(strings.NewReader("{not json"), &stdout, &stderr,
		config{apiKey: "k", model: "m", baseURL: "http://localhost:0"})
	if code == 0 {
		t.Fatal("exit = 0, want non-zero for malformed JSON")
	}
	if !strings.Contains(stderr.String(), "malformed request") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestParseCandidateMarkerOrder(t *testing.T) {
	out := fmt.Sprintf("%s\nrat\n%s\nfm\n%s\nbody", markRationale, markFrontmatter, markBody)
	if _, err := parseCandidate(out); err == nil {
		t.Fatal("out-of-order markers accepted")
	}
}
