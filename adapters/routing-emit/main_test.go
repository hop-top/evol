package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func call(t *testing.T, req string, env map[string]string) (int, string, string) {
	t.Helper()
	getenv := func(k string) string { return env[k] }
	var out, errb bytes.Buffer
	code := run(strings.NewReader(req), &out, &errb, getenv)
	return code, out.String(), errb.String()
}

func reqFor(t *testing.T, path string, ev []map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"evol": "1", "port": "routing", "action": "emit",
		"artifact_ref": "commit-messages/SKILL.md",
		"evidence":     ev,
		"output":       map[string]any{"path": path, "format": "kit-llm-pool"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestEmitSortsNormalizesAndWrites(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "pool.yaml")
	req := reqFor(t, out, []map[string]any{
		{"provider": "ollama://qwen3.6:35b?base_url=http://127.0.0.1:11600", "mean_score": 0.62, "n": 3},
		{"provider": "claude://haiku", "mean_score": 0.82, "n": 6},
	})
	code, stdout, stderr := call(t, req, map[string]string{"EVOL_ROUTING_ALLOW_ABS": "1"})
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, stderr)
	}

	var resp response
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Written != out || len(resp.Entries) != 2 {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Entries[0].Model != "haiku" || resp.Entries[0].Weight != 1.00 {
		t.Errorf("best entry = %+v, want haiku weight 1.00", resp.Entries[0])
	}
	if resp.Entries[1].Weight != 0.76 { // 0.62/0.82 = 0.756 -> 0.76
		t.Errorf("second weight = %v, want 0.76", resp.Entries[1].Weight)
	}

	data, err := os.ReadFile(out) //nolint:gosec // test temp path
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	for _, want := range []string{
		"pool:", "alias: haiku-evol", "scheme: claude",
		`model: "qwen3.6:35b"`, `base_url: "http://127.0.0.1:11600"`, "weight: 1.00",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("pool yaml missing %q:\n%s", want, doc)
		}
	}
	if strings.Contains(doc, "api_key") {
		t.Error("credentials leaked into pool yaml")
	}
}

func TestAPIKeyStripped(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "pool.yaml")
	req := reqFor(t, out, []map[string]any{
		{"provider": "anthropic://claude-sonnet-5?api_key=sk-secret&base_url=http://x", "mean_score": 0.5, "n": 1},
	})
	code, stdout, _ := call(t, req, map[string]string{"EVOL_ROUTING_ALLOW_ABS": "1"})
	if code != 0 {
		t.Fatal("expected success")
	}
	data, _ := os.ReadFile(out) //nolint:gosec // test temp path
	if strings.Contains(string(data), "sk-secret") || strings.Contains(stdout, "sk-secret") {
		t.Error("api_key leaked")
	}
	if !strings.Contains(string(data), `base_url: "http://x"`) {
		t.Error("base_url dropped")
	}
}

func TestPathGuard(t *testing.T) {
	req := reqFor(t, "/tmp/abs-pool.yaml", []map[string]any{
		{"provider": "claude://haiku", "mean_score": 0.5, "n": 1},
	})
	if code, _, stderr := call(t, req, nil); code == 0 || !strings.Contains(stderr, "refused") {
		t.Errorf("absolute path accepted without override (code %d, stderr %q)", code, stderr)
	}
	req = reqFor(t, "../escape.yaml", []map[string]any{
		{"provider": "claude://haiku", "mean_score": 0.5, "n": 1},
	})
	if code, _, _ := call(t, req, nil); code == 0 {
		t.Error("traversal path accepted")
	}
}

func TestRelativePathAllowed(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	req := reqFor(t, "sub/pool.yaml", []map[string]any{
		{"provider": "claude://haiku", "mean_score": 0.5, "n": 1},
	})
	if code, _, stderr := call(t, req, nil); code != 0 {
		t.Fatalf("relative path refused: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "sub", "pool.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterErrors(t *testing.T) {
	cases := map[string]string{
		"bad json":       `{`,
		"wrong version":  `{"evol":"9","port":"routing","action":"emit"}`,
		"wrong port":     `{"evol":"1","port":"corpus","action":"emit"}`,
		"wrong action":   `{"evol":"1","port":"routing","action":"route"}`,
		"empty evidence": `{"evol":"1","port":"routing","action":"emit","evidence":[],"output":{"path":"p.yaml"}}`,
		"no path":        `{"evol":"1","port":"routing","action":"emit","evidence":[{"provider":"claude://h","mean_score":0.5}],"output":{}}`,
		"bad format":     `{"evol":"1","port":"routing","action":"emit","evidence":[{"provider":"claude://h","mean_score":0.5}],"output":{"path":"p.yaml","format":"toml"}}`,
		"no model":       `{"evol":"1","port":"routing","action":"emit","evidence":[{"provider":"claude://","mean_score":0.5}],"output":{"path":"p.yaml"}}`,
		"zero best":      `{"evol":"1","port":"routing","action":"emit","evidence":[{"provider":"claude://h","mean_score":0}],"output":{"path":"p.yaml"}}`,
	}
	for name, req := range cases {
		if code, _, _ := call(t, req, nil); code == 0 {
			t.Errorf("%s: expected non-zero exit", name)
		}
	}
}

func TestAliasCollisionDisambiguates(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "pool.yaml")
	req := reqFor(t, out, []map[string]any{
		{"provider": "ollama://llama3.2", "mean_score": 0.6, "n": 1},
		{"provider": "groq://llama3.2", "mean_score": 0.5, "n": 1},
	})
	code, stdout, stderr := call(t, req, map[string]string{"EVOL_ROUTING_ALLOW_ABS": "1"})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var resp response
	_ = json.Unmarshal([]byte(stdout), &resp)
	if resp.Entries[0].Alias == resp.Entries[1].Alias {
		t.Errorf("alias collision: %+v", resp.Entries)
	}
}
