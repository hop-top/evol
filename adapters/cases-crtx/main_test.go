package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runTool(t *testing.T, stdin string, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errb)
	return out.String(), errb.String(), code
}

func decodeCases(t *testing.T, out string) []caseLine {
	t.Helper()
	var cases []caseLine
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var c caseLine
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("bad output line %q: %v", line, err)
		}
		cases = append(cases, c)
	}
	return cases
}

// mkEnvelope builds a compact synthetic envelope JSON string.
func mkEnvelope(id string, turns ...map[string]any) string {
	env := map[string]any{
		"crtx_version": "0.1",
		"id":           id,
		"created_at":   "2026-05-28T14:00:00Z",
		"updated_at":   "2026-05-28T14:00:01Z",
		"source":       map[string]any{"kind": "stem", "version": "0.1.0"},
		"turns":        turns,
	}
	b, err := json.Marshal(env)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func textTurn(id, role, text string) map[string]any {
	return map[string]any{
		"id": id, "role": role, "created_at": "2026-05-28T14:00:00Z",
		"content": []map[string]any{{"type": "text", "text": text}},
	}
}

func TestPairingAndShape(t *testing.T) {
	in := mkEnvelope("ENV1",
		textTurn("t1", "user", "write a commit subject for a retry fix"),
		textTurn("t2", "assistant", "fix(fetch): add retry with backoff"),
		textTurn("t3", "user", "and one for docs"),
		textTurn("t4", "assistant", "docs: clarify install steps"),
	)
	out, stderr, code := runTool(t, in)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	cases := decodeCases(t, out)
	if len(cases) != 2 {
		t.Fatalf("want 2 cases, got %d", len(cases))
	}
	c := cases[0]
	if c.Input != "write a commit subject for a retry fix" ||
		c.ExpectedOutput != "fix(fetch): add retry with backoff" {
		t.Fatalf("bad pairing: %+v", c)
	}
	if c.Split != "train" || c.Provenance != "mined" || c.Source != "crtx:ENV1" {
		t.Fatalf("bad fixed fields: %+v", c)
	}
	if !strings.HasPrefix(c.ID, "crtx-") || len(c.ID) != len("crtx-")+12 {
		t.Fatalf("bad id: %q", c.ID)
	}
	// deterministic ids
	out2, _, _ := runTool(t, in)
	if out != out2 {
		t.Fatal("output not deterministic")
	}
}

func TestInterveningToolTurns(t *testing.T) {
	toolCallTurn := map[string]any{
		"id": "t2", "role": "assistant", "created_at": "2026-05-28T14:00:00Z",
		"content": []map[string]any{{"type": "tool_call", "call_id": "c1", "name": "search", "input": map[string]any{}}},
	}
	toolResultTurn := map[string]any{
		"id": "t3", "role": "tool", "created_at": "2026-05-28T14:00:00Z",
		"content": []map[string]any{{"type": "tool_result", "call_id": "c1", "output": "hits"}},
	}
	in := mkEnvelope("ENV2",
		textTurn("t1", "user", "look this up and summarize"),
		toolCallTurn,
		toolResultTurn,
		textTurn("t4", "assistant", "summary: it works"),
	)
	cases := decodeCases(t, mustOut(t, in))
	if len(cases) != 1 || cases[0].ExpectedOutput != "summary: it works" {
		t.Fatalf("tool-interleaved pairing failed: %+v", cases)
	}
}

func mustOut(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	out, stderr, code := runTool(t, stdin, args...)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, stderr)
	}
	return out
}

func TestScrubbingAllClasses(t *testing.T) {
	// secret = the exact sensitive token that must vanish from output
	secrets := map[string]string{ //nolint:gosec // synthetic fixtures, not credentials
		"aws-key":      "AKIAABCDEFGHIJKLMNOP",
		"github-token": "ghp_abcdefghijklmnopqrstuvwxyz0123456789",
		"github-pat":   "github_pat_22CHARSOFPATHERE1234567890ABCDEFG",
		"jwt":          "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.SflKxwRJSMeKKF2QT4fwpM",
		"openai-key":   "sk-abcdefghij1234567890KLMNOP",
		"slack-token":  "xoxb-1234567890-abcdef",
		"bearer":       "Bearer abcdefghijklmnop123456",
		"pem":          "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----",
	}
	i := 0
	for class, secret := range secrets {
		i++
		in := mkEnvelope(fmt.Sprintf("E%d", i),
			textTurn("t1", "user", "input with "+secret+" here"),
			textTurn("t2", "assistant", "reply with "+secret+" too"),
		)
		out, stderr, code := runTool(t, in)
		if code != 0 {
			t.Fatalf("[%s] exit %d", class, code)
		}
		cases := decodeCases(t, out)
		want := "<redacted:" + class + ">"
		if !strings.Contains(cases[0].Input, want) || !strings.Contains(cases[0].ExpectedOutput, want) {
			t.Errorf("[%s] not redacted in both fields: %+v", class, cases[0])
		}
		if strings.Contains(out, secret) {
			t.Errorf("[%s] raw secret leaked into output", class)
		}
		if !strings.Contains(stderr, "redacted "+class) {
			t.Errorf("[%s] stderr missing redaction count: %s", class, stderr)
		}
	}
}

func TestGrepFilter(t *testing.T) {
	in := mkEnvelope("ENV3",
		textTurn("t1", "user", "write a COMMIT subject"),
		textTurn("t2", "assistant", "feat: x"),
		textTurn("t3", "user", "bake a cake"),
		textTurn("t4", "assistant", "preheat the oven"),
	)
	cases := decodeCases(t, mustOut(t, in, "--grep", "commit"))
	if len(cases) != 1 || !strings.Contains(cases[0].Input, "COMMIT") {
		t.Fatalf("grep filter failed: %+v", cases)
	}
}

func TestDedupAcrossEnvelopes(t *testing.T) {
	e1 := mkEnvelope("A", textTurn("t1", "user", "same question"), textTurn("t2", "assistant", "answer one"))
	e2 := mkEnvelope("B", textTurn("t1", "user", "same question"), textTurn("t2", "assistant", "answer two"))
	out, stderr, code := runTool(t, e1+"\n"+e2)
	if code != 0 {
		t.Fatal(code)
	}
	cases := decodeCases(t, out)
	if len(cases) != 1 {
		t.Fatalf("dedup failed: %d cases", len(cases))
	}
	if !strings.Contains(stderr, "dedup 1") {
		t.Fatalf("stderr missing dedup count: %s", stderr)
	}
}

func TestMalformedLinesSkipped(t *testing.T) {
	good := mkEnvelope("G", textTurn("t1", "user", "q"), textTurn("t2", "assistant", "a"))
	in := "{not json\n" + good + "\n{\"also\": \"not an envelope\"}\n"
	out, stderr, code := runTool(t, in)
	if code != 0 {
		t.Fatal(code)
	}
	if n := len(decodeCases(t, out)); n != 1 {
		t.Fatalf("want 1 case, got %d", n)
	}
	if !strings.Contains(stderr, "malformed lines 2") {
		t.Fatalf("stderr missing malformed count: %s", stderr)
	}
}

func TestVersionAndRoleRejection(t *testing.T) {
	bad := strings.Replace(
		mkEnvelope("V2", textTurn("t1", "user", "q"), textTurn("t2", "assistant", "a")),
		`"crtx_version":"0.1"`, `"crtx_version":"0.2"`, 1)
	weird := mkEnvelope("WR", textTurn("t1", "robot", "q"))
	out, stderr, code := runTool(t, bad+"\n"+weird)
	if code != 0 {
		t.Fatal(code)
	}
	if strings.TrimSpace(out) != "" {
		t.Fatalf("rejected envelopes emitted cases: %q", out)
	}
	if !strings.Contains(stderr, "rejected envelopes 2") {
		t.Fatalf("stderr missing rejection count: %s", stderr)
	}
}

func TestNoExpectedToggle(t *testing.T) {
	in := mkEnvelope("NE", textTurn("t1", "user", "q"), textTurn("t2", "assistant", "a"))
	out := mustOut(t, in, "--no-expected")
	if strings.Contains(out, "expected_output") {
		t.Fatalf("expected_output present with --no-expected: %s", out)
	}
	// pairing still required: user turn without assistant reply emits nothing
	lone := mkEnvelope("L", textTurn("t1", "user", "unanswered"))
	if strings.TrimSpace(mustOut(t, lone, "--no-expected")) != "" {
		t.Fatal("unanswered user turn should not emit a case")
	}
}

func TestLimit(t *testing.T) {
	in := mkEnvelope("LIM",
		textTurn("t1", "user", "q one"), textTurn("t2", "assistant", "a one"),
		textTurn("t3", "user", "q two"), textTurn("t4", "assistant", "a two"),
		textTurn("t5", "user", "q three"), textTurn("t6", "assistant", "a three"),
	)
	if n := len(decodeCases(t, mustOut(t, in, "--limit", "2"))); n != 2 {
		t.Fatalf("limit ignored: %d", n)
	}
}

func TestSingleEnvelopeFileAndFixture(t *testing.T) {
	dir := t.TempDir()
	pretty := "{\n  \"crtx_version\": \"0.1\",\n  \"id\": \"PRETTY\",\n  \"created_at\": \"2026-05-28T14:00:00Z\",\n  \"updated_at\": \"2026-05-28T14:00:01Z\",\n  \"source\": {\"kind\": \"stem\", \"version\": \"0.1.0\"},\n  \"turns\": [\n    {\"id\": \"t1\", \"role\": \"user\", \"created_at\": \"2026-05-28T14:00:00Z\", \"content\": [{\"type\": \"text\", \"text\": \"hello\"}]},\n    {\"id\": \"t2\", \"role\": \"assistant\", \"created_at\": \"2026-05-28T14:00:01Z\", \"content\": [{\"type\": \"text\", \"text\": \"hi\"}]}\n  ]\n}\n"
	path := filepath.Join(dir, "single.json")
	if err := os.WriteFile(path, []byte(pretty), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := decodeCases(t, mustOut(t, "", path))
	if len(cases) != 1 || cases[0].Source != "crtx:PRETTY" {
		t.Fatalf("single-envelope file parse failed: %+v", cases)
	}

	// committed fixture round-trip
	fixture := "testdata/envelopes.jsonl"
	cases = decodeCases(t, mustOut(t, "", fixture))
	if len(cases) < 2 {
		t.Fatalf("fixture should yield >=2 cases, got %d", len(cases))
	}
}
