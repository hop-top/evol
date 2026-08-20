package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// binPath is the compiled adapter under test. Tests exec the real
// binary so exit codes are asserted honestly (go run masks them).
var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kb-ctxt-bin")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(dir, "kb-ctxt")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", binPath, ".") //nolint:gosec // binPath is a test-owned temp path
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// writeFake writes an executable shell script standing in for ctxt.
func writeFake(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-ctxt")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script), 0o755); err != nil { //nolint:gosec // test fixture must be executable
		t.Fatal(err)
	}
	return path
}

type result struct {
	exitCode int
	stdout   string
	stderr   string
}

func invoke(t *testing.T, fakeBin, request string, extraEnv ...string) result {
	t.Helper()
	cmd := exec.Command(binPath)
	cmd.Stdin = strings.NewReader(request)
	cmd.Env = append(os.Environ(), "EVOL_CTXT_BIN="+fakeBin)
	cmd.Env = append(cmd.Env, extraEnv...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		code = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	return result{exitCode: code, stdout: stdout.String(), stderr: stderr.String()}
}

func decode(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("response not JSON: %v\nraw: %s", err, raw)
	}
	return m
}

const healthyStatus = `if [ "$1" = "status" ]; then echo '{"health":"healthy"}'; exit 0; fi`

const findFixture = `{"objects":[` +
	`{"id":"obj-1","text_content":"Passage one text.","raw_content":"","metadata":{"rrf_score":0.9}},` +
	`{"id":"obj-2","text_content":"","raw_content":"Raw fallback text.","metadata":{"rrf_score":0.4}},` +
	`{"id":"obj-3","text_content":"","raw_content":"","metadata":{"rrf_score":0.1}}]}`

func TestSearchHappyPath(t *testing.T) {
	fake := writeFake(t, healthyStatus+`
if [ "$1" = "find" ]; then echo '`+findFixture+`'; exit 0; fi
exit 64`)
	res := invoke(t, fake, `{"evol":"1","port":"knowledgebase","action":"search","query":"anything","limit":3}`)
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	m := decode(t, res.stdout)
	if m["unavailable"] == true {
		t.Fatal("unexpected unavailable")
	}
	passages := m["passages"].([]any)
	if len(passages) != 2 {
		t.Fatalf("want 2 passages (empty-content object dropped), got %d", len(passages))
	}
	first := passages[0].(map[string]any)
	if first["text"] != "Passage one text." || first["source"] != "obj-1" || first["score"].(float64) != 0.9 {
		t.Fatalf("bad first passage: %v", first)
	}
	second := passages[1].(map[string]any)
	if second["text"] != "Raw fallback text." {
		t.Fatalf("raw_content fallback not applied: %v", second)
	}
}

func TestBriefComposeHappyPath(t *testing.T) {
	fake := writeFake(t, healthyStatus+`
if [ "$1" = "compose" ]; then echo 'Composed brief text.'; exit 0; fi
exit 64`)
	res := invoke(t, fake, `{"evol":"1","port":"knowledgebase","action":"brief","topic":"commit-style"}`)
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	m := decode(t, res.stdout)
	if m["text"] != "Composed brief text." {
		t.Fatalf("bad brief text: %v", m["text"])
	}
}

func TestBriefFallsBackToSearchJoin(t *testing.T) {
	fake := writeFake(t, healthyStatus+`
if [ "$1" = "compose" ]; then exit 3; fi
if [ "$1" = "find" ]; then echo '`+findFixture+`'; exit 0; fi
exit 64`)
	res := invoke(t, fake, `{"evol":"1","port":"knowledgebase","action":"brief","topic":"anything"}`)
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	m := decode(t, res.stdout)
	text := m["text"].(string)
	if !strings.Contains(text, "Passage one text.") || !strings.Contains(text, "obj-1") {
		t.Fatalf("fallback join missing passages: %q", text)
	}
}

func TestAppendHappyPath(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "invoked")
	fake := writeFake(t, healthyStatus+`
if [ "$1" = "analyze" ]; then echo "$@" > `+marker+`; exit 0; fi
exit 64`)
	res := invoke(t, fake, `{"evol":"1","port":"knowledgebase","action":"append","text":"note body","tags":["evolution","x"]}`)
	if res.exitCode != 0 {
		t.Fatalf("exit %d, stderr: %s", res.exitCode, res.stderr)
	}
	m := decode(t, res.stdout)
	if m["unavailable"] == true {
		t.Fatal("unexpected unavailable")
	}
	recorded, err := os.ReadFile(marker) //nolint:gosec // marker is a test-owned temp path
	if err != nil {
		t.Fatalf("analyze never invoked: %v", err)
	}
	if !strings.Contains(string(recorded), "note body") || !strings.Contains(string(recorded), "evolution,x") {
		t.Fatalf("analyze args wrong: %s", recorded)
	}
}

func TestDaemonDownIsUnavailableNotError(t *testing.T) {
	fake := writeFake(t, `exit 1`)
	res := invoke(t, fake, `{"evol":"1","port":"knowledgebase","action":"search","query":"anything"}`)
	if res.exitCode != 0 {
		t.Fatalf("daemon-down must exit 0, got %d", res.exitCode)
	}
	m := decode(t, res.stdout)
	if m["unavailable"] != true {
		t.Fatalf("want unavailable:true, got: %s", res.stdout)
	}
}

func TestMissingBinaryIsUnavailable(t *testing.T) {
	res := invoke(t, filepath.Join(t.TempDir(), "nonexistent"), `{"evol":"1","port":"knowledgebase","action":"brief","topic":"x"}`)
	if res.exitCode != 0 {
		t.Fatalf("missing binary must exit 0, got %d", res.exitCode)
	}
	if decode(t, res.stdout)["unavailable"] != true {
		t.Fatalf("want unavailable:true, got: %s", res.stdout)
	}
}

func TestTimeoutIsUnavailable(t *testing.T) {
	fake := writeFake(t, `sleep 2; exit 0`)
	res := invoke(t, fake, `{"evol":"1","port":"knowledgebase","action":"search","query":"x"}`,
		"EVOL_CTXT_PROBE_TIMEOUT_MS=200", "EVOL_CTXT_TIMEOUT_MS=200")
	if res.exitCode != 0 {
		t.Fatalf("timeout must exit 0, got %d", res.exitCode)
	}
	if decode(t, res.stdout)["unavailable"] != true {
		t.Fatalf("want unavailable:true, got: %s", res.stdout)
	}
}

func TestMalformedRequestFaults(t *testing.T) {
	fake := writeFake(t, healthyStatus)
	for name, req := range map[string]string{
		"not json":      `{"evol":`,
		"wrong version": `{"evol":"9","port":"knowledgebase","action":"search","query":"x"}`,
		"wrong port":    `{"evol":"1","port":"generator","action":"search","query":"x"}`,
		"bad action":    `{"evol":"1","port":"knowledgebase","action":"summon"}`,
		"missing query": `{"evol":"1","port":"knowledgebase","action":"search"}`,
		"missing topic": `{"evol":"1","port":"knowledgebase","action":"brief"}`,
		"missing text":  `{"evol":"1","port":"knowledgebase","action":"append"}`,
	} {
		res := invoke(t, fake, req)
		if res.exitCode == 0 {
			t.Errorf("%s: want non-zero exit, got 0 (stdout: %s)", name, res.stdout)
		}
		if res.stderr == "" {
			t.Errorf("%s: want stderr diagnostics", name)
		}
	}
}

func TestUnparseableFindOutputDegrades(t *testing.T) {
	fake := writeFake(t, healthyStatus+`
if [ "$1" = "find" ]; then echo 'not json at all'; exit 0; fi
exit 64`)
	res := invoke(t, fake, `{"evol":"1","port":"knowledgebase","action":"search","query":"x"}`)
	if res.exitCode != 0 {
		t.Fatalf("unparseable output must degrade with exit 0, got %d", res.exitCode)
	}
	if decode(t, res.stdout)["unavailable"] != true {
		t.Fatalf("want unavailable:true, got: %s", res.stdout)
	}
}
