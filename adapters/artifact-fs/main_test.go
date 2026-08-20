package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runWith(t *testing.T, root, input string) (int, string, string) {
	t.Helper()
	var out, errBuf strings.Builder
	getenv := func(key string) string {
		if key == "EVOL_ARTIFACT_ROOT" {
			return root
		}
		return ""
	}
	code := run(strings.NewReader(input), &out, &errBuf, getenv)
	return code, out.String(), errBuf.String()
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func decode(t *testing.T, raw string) response {
	t.Helper()
	var resp response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v\nraw: %s", err, raw)
	}
	return resp
}

func TestLoad(t *testing.T) {
	withFM := "---\nname: commit-style\ndescription: how to commit\n---\n## When to use\nAlways.\n"
	withoutFM := "## Bare\nNo frontmatter here.\n"

	tests := []struct {
		name            string
		ref             string
		content         string
		wantKind        string
		wantFrontmatter string
		wantBody        string
	}{
		{
			name:            "skill with frontmatter",
			ref:             "skills/commit-style/SKILL.md",
			content:         withFM,
			wantKind:        "skill",
			wantFrontmatter: "name: commit-style\ndescription: how to commit\n",
			wantBody:        "## When to use\nAlways.\n",
		},
		{
			name:            "prompt without frontmatter",
			ref:             "prompts/reviewer.md",
			content:         withoutFM,
			wantKind:        "prompt",
			wantFrontmatter: "",
			wantBody:        withoutFM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mustWriteFile(t, filepath.Join(root, filepath.FromSlash(tt.ref)), tt.content)

			req := `{"evol":"1","port":"artifactstore","action":"load","ref":"` + tt.ref + `"}`
			code, out, stderr := runWith(t, root, req)
			if code != 0 {
				t.Fatalf("exit %d, stderr: %s", code, stderr)
			}
			resp := decode(t, out)
			if resp.Artifact == nil {
				t.Fatal("no artifact in response")
			}
			a := resp.Artifact
			if a.Ref != tt.ref || a.Kind != tt.wantKind {
				t.Errorf("ref/kind = %q/%q, want %q/%q", a.Ref, a.Kind, tt.ref, tt.wantKind)
			}
			if a.Frontmatter != tt.wantFrontmatter {
				t.Errorf("frontmatter = %q, want %q", a.Frontmatter, tt.wantFrontmatter)
			}
			if a.Body != tt.wantBody {
				t.Errorf("body = %q, want %q", a.Body, tt.wantBody)
			}
			if len(a.Version) != versionLen {
				t.Errorf("version = %q, want %d hex chars", a.Version, versionLen)
			}
		})
	}
}

func TestWriteThenLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	fm := "name: fresh\ndescription: new skill\n"
	body := "## Steps\n1. do it\n"

	wreq, _ := json.Marshal(map[string]string{
		"evol": "1", "port": "artifactstore", "action": "write",
		"ref": "skills/fresh/SKILL.md", "frontmatter": fm, "body": body,
		"message": "initial version",
	})
	code, out, stderr := runWith(t, root, string(wreq))
	if code != 0 {
		t.Fatalf("write exit %d, stderr: %s", code, stderr)
	}
	wresp := decode(t, out)
	if len(wresp.Version) != versionLen {
		t.Fatalf("write version = %q", wresp.Version)
	}
	if !strings.Contains(stderr, "initial version") {
		t.Errorf("message not echoed to stderr diagnostics: %q", stderr)
	}

	code, out, stderr = runWith(t, root,
		`{"evol":"1","port":"artifactstore","action":"load","ref":"skills/fresh/SKILL.md"}`)
	if code != 0 {
		t.Fatalf("load exit %d, stderr: %s", code, stderr)
	}
	lresp := decode(t, out)
	if lresp.Artifact.Frontmatter != fm || lresp.Artifact.Body != body {
		t.Errorf("round-trip mismatch: fm=%q body=%q", lresp.Artifact.Frontmatter, lresp.Artifact.Body)
	}
	if lresp.Artifact.Version != wresp.Version {
		t.Errorf("load version %q != write version %q", lresp.Artifact.Version, wresp.Version)
	}
	// No temp files left behind.
	entries, err := os.ReadDir(filepath.Join(root, "skills", "fresh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".artifact-fs-") {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
}

func TestWriteRejectsEscapes(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "..", "escaped.md")

	for _, ref := range []string{"../escaped.md", "skills/../../escaped.md", "/etc/passwd"} {
		req, _ := json.Marshal(map[string]string{
			"evol": "1", "port": "artifactstore", "action": "write",
			"ref": ref, "frontmatter": "", "body": "nope",
		})
		code, _, _ := runWith(t, root, string(req))
		if code == 0 {
			t.Errorf("ref %q: want non-zero exit", ref)
		}
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Errorf("escape file was created: %v", err)
	}
}

func TestList(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "skills/a/SKILL.md"), "a")
	mustWriteFile(t, filepath.Join(root, "skills/b/SKILL.md"), "b")
	mustWriteFile(t, filepath.Join(root, "prompts/p.md"), "p")
	mustWriteFile(t, filepath.Join(root, "commands/c.md"), "c")
	mustWriteFile(t, filepath.Join(root, "tool-configs/t.yaml"), "t")
	mustWriteFile(t, filepath.Join(root, "notes/ignore.md"), "x")
	mustWriteFile(t, filepath.Join(root, ".git/objects/junk.md"), "x")

	tests := []struct {
		kind string
		want []string
	}{
		{"", []string{"commands/c.md", "prompts/p.md", "skills/a/SKILL.md", "skills/b/SKILL.md", "tool-configs/t.yaml"}},
		{"skill", []string{"skills/a/SKILL.md", "skills/b/SKILL.md"}},
		{"prompt", []string{"prompts/p.md"}},
		{"tool-config", []string{"tool-configs/t.yaml"}},
	}
	for _, tt := range tests {
		req := `{"evol":"1","port":"artifactstore","action":"list"`
		if tt.kind != "" {
			req += `,"kind":"` + tt.kind + `"`
		}
		req += `}`
		code, out, stderr := runWith(t, root, req)
		if code != 0 {
			t.Fatalf("kind %q: exit %d, stderr: %s", tt.kind, code, stderr)
		}
		resp := decode(t, out)
		if strings.Join(resp.Refs, ",") != strings.Join(tt.want, ",") {
			t.Errorf("kind %q: refs = %v, want %v", tt.kind, resp.Refs, tt.want)
		}
	}

	code, _, _ := runWith(t, root, `{"evol":"1","port":"artifactstore","action":"list","kind":"bogus"}`)
	if code == 0 {
		t.Error("bogus kind: want non-zero exit")
	}
}

func TestAdapterErrors(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name  string
		root  string
		input string
	}{
		{"malformed json", root, `{"evol":`},
		{"wrong contract version", root, `{"evol":"2","port":"artifactstore","action":"list"}`},
		{"wrong port", root, `{"evol":"1","port":"corpus","action":"list"}`},
		{"unknown action", root, `{"evol":"1","port":"artifactstore","action":"drop"}`},
		{"missing root", "", `{"evol":"1","port":"artifactstore","action":"list"}`},
		{"load missing file", root, `{"evol":"1","port":"artifactstore","action":"load","ref":"skills/nope/SKILL.md"}`},
		{"load empty ref", root, `{"evol":"1","port":"artifactstore","action":"load","ref":""}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, out, stderr := runWith(t, tt.root, tt.input)
			if code == 0 {
				t.Fatalf("want non-zero exit, stdout: %s", out)
			}
			if stderr == "" {
				t.Error("want diagnostics on stderr")
			}
		})
	}
}

func TestSplitAssembleProperties(t *testing.T) {
	cases := []struct{ fm, body string }{
		{"", "plain body\n"},
		{"name: x\n", "body\n"},
		{"name: x\nmulti: line\n", "## H\n\ntext with --- inside\n"},
	}
	for _, c := range cases {
		doc := assemble(c.fm, c.body)
		fm, body := splitFrontmatter(doc)
		if fm != c.fm || body != c.body {
			t.Errorf("assemble/split not inverse: fm %q->%q body %q->%q", c.fm, fm, c.body, body)
		}
	}
}
