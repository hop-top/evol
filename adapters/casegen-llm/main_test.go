package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const goodArrayOutput = `[
  {"input": "Commit message for bumping a lockfile-only dependency",
   "expected": "build: prefix; body optional",
   "rationale": "exercises the dependency-bump type rule"},
  {"input": "Commit message renaming a module, breaking imports",
   "expected": "! before colon and BREAKING CHANGE trailer",
   "rationale": "exercises the breaking-change rule"}
]`

func anthropicFixture(text string) string {
	blob, _ := json.Marshal(map[string]any{
		"id": "msg_test", "type": "message", "role": "assistant",
		"model":       "test-model",
		"content":     []map[string]any{{"type": "text", "text": text}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	return string(blob)
}

func newServer(t *testing.T, respond func(i int) (int, string)) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var mu sync.Mutex
	calls := &[]map[string]any{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		mu.Lock()
		idx := len(*calls)
		*calls = append(*calls, body)
		mu.Unlock()
		status, resp := respond(idx)
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(ts.Close)
	return ts, calls
}

func synthRequest(count int, knowledge []Passage) Request {
	return Request{
		Evol: "1", Port: "generator", Action: "synth",
		Artifact: Artifact{
			Ref: "skills/commit-style", Kind: "skill",
			Body: "## Guidelines\nFollow house style.",
		},
		Knowledge: knowledge,
		Examples: []Example{{
			Input:    "Commit message for a fix touching two packages",
			Expected: "type(scope) prefix; imperative subject",
		}},
		Count: count,
	}
}

func groundedKnowledge() []Passage {
	return []Passage{
		{Text: "Dependency bumps use the build type.", Source: "notes/style"},
		{Text: "Breaking changes need ! and a BREAKING CHANGE trailer.", Source: "notes/style"},
	}
}

func runAdapter(t *testing.T, req any, env map[string]string) (int, Response, string) {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
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

func clearKeyEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("LLM_API_KEY", "")
}

func uriFor(baseURL string) string {
	return "anthropic://test-model?base_url=" + baseURL + "&api_key=test-key"
}

func TestSynthHappyPathGroundedRequestShape(t *testing.T) {
	clearKeyEnv(t)
	ts, calls := newServer(t, func(int) (int, string) {
		return 200, anthropicFixture(goodArrayOutput)
	})

	code, resp, stderr := runAdapter(t, synthRequest(3, groundedKnowledge()),
		map[string]string{"EVOL_CASEGEN_PROVIDER": uriFor(ts.URL)})
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	if len(resp.Cases) != 2 {
		t.Fatalf("cases = %d, want 2", len(resp.Cases))
	}
	if resp.Cases[0].Input == "" || resp.Cases[0].Rationale == "" {
		t.Errorf("case fields not parsed: %+v", resp.Cases[0])
	}
	if len(*calls) != 1 {
		t.Fatalf("api calls = %d, want 1 (all cases in one call)", len(*calls))
	}
	// The user prompt must carry the grounding passages and examples.
	body := (*calls)[0]
	msgs, _ := json.Marshal(body["messages"])
	for _, must := range []string{"Dependency bumps", "BREAKING CHANGE", "fix touching two packages"} {
		if !strings.Contains(string(msgs), must) {
			t.Errorf("user prompt missing %q", must)
		}
	}
}

func TestSynthCapsAtCount(t *testing.T) {
	clearKeyEnv(t)
	ts, _ := newServer(t, func(int) (int, string) {
		return 200, anthropicFixture(goodArrayOutput)
	})
	code, resp, _ := runAdapter(t, synthRequest(1, groundedKnowledge()),
		map[string]string{"EVOL_CASEGEN_PROVIDER": uriFor(ts.URL)})
	if code != 0 || len(resp.Cases) != 1 {
		t.Fatalf("exit %d cases %d, want 0/1", code, len(resp.Cases))
	}
}

func TestSynthRefusesWithoutKnowledge(t *testing.T) {
	clearKeyEnv(t)
	code, _, stderr := runAdapter(t, synthRequest(3, nil),
		map[string]string{"EVOL_CASEGEN_PROVIDER": "anthropic://m?api_key=k"})
	if code != 1 || !strings.Contains(stderr, "circular-eval guard") {
		t.Fatalf("exit %d stderr %q, want refusal with circular-eval guard", code, stderr)
	}
}

func TestSynthFenceWrappedOutputParses(t *testing.T) {
	clearKeyEnv(t)
	ts, _ := newServer(t, func(int) (int, string) {
		return 200, anthropicFixture("```json\n" + goodArrayOutput + "\n```")
	})
	code, resp, _ := runAdapter(t, synthRequest(3, groundedKnowledge()),
		map[string]string{"EVOL_CASEGEN_PROVIDER": uriFor(ts.URL)})
	if code != 0 || len(resp.Cases) != 2 {
		t.Fatalf("exit %d cases %d, want 0/2", code, len(resp.Cases))
	}
}

func TestSynthUnparseableOutputIsDryNotError(t *testing.T) {
	clearKeyEnv(t)
	ts, _ := newServer(t, func(int) (int, string) {
		return 200, anthropicFixture("I refuse to answer in JSON today.")
	})
	code, resp, stderr := runAdapter(t, synthRequest(2, groundedKnowledge()),
		map[string]string{"EVOL_CASEGEN_PROVIDER": uriFor(ts.URL)})
	if code != 0 {
		t.Fatalf("exit %d, want 0 (dry synthesis), stderr %s", code, stderr)
	}
	if len(resp.Cases) != 0 {
		t.Fatalf("cases = %d, want 0", len(resp.Cases))
	}
	if !strings.Contains(stderr, "unparseable") {
		t.Errorf("stderr lacks diagnostic: %q", stderr)
	}
}

func TestSynthInputlessCasesDropped(t *testing.T) {
	clearKeyEnv(t)
	ts, _ := newServer(t, func(int) (int, string) {
		return 200, anthropicFixture(`[{"expected":"no input"},{"input":"real one"}]`)
	})
	code, resp, _ := runAdapter(t, synthRequest(3, groundedKnowledge()),
		map[string]string{"EVOL_CASEGEN_PROVIDER": uriFor(ts.URL)})
	if code != 0 || len(resp.Cases) != 1 || resp.Cases[0].Input != "real one" {
		t.Fatalf("exit %d cases %+v, want the single input-bearing case", code, resp.Cases)
	}
}

func TestSynthBadEnvelopeRejected(t *testing.T) {
	req := synthRequest(1, groundedKnowledge())
	req.Action = "propose"
	code, _, stderr := runAdapter(t, req, nil)
	if code != 1 || !strings.Contains(stderr, "unsupported envelope") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestSynthCountRejected(t *testing.T) {
	code, _, stderr := runAdapter(t, synthRequest(0, groundedKnowledge()), nil)
	if code != 1 || !strings.Contains(stderr, "count must be > 0") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
}

func TestSynthProviderFallsBackToGeneratorEnv(t *testing.T) {
	clearKeyEnv(t)
	ts, calls := newServer(t, func(int) (int, string) {
		return 200, anthropicFixture(goodArrayOutput)
	})
	code, _, stderr := runAdapter(t, synthRequest(1, groundedKnowledge()),
		map[string]string{"EVOL_GENERATOR_PROVIDER": uriFor(ts.URL)})
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, stderr)
	}
	if len(*calls) != 1 {
		t.Fatalf("fallback env not honored; calls = %d", len(*calls))
	}
}
