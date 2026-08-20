package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scrubGitEnv keeps scratch-repo git operations hermetic: inherited
// GIT_DIR/GIT_WORK_TREE/etc. from hook contexts would otherwise leak
// operations into the OUTER repository.
func scrubGitEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_COMMON_DIR"} {
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
}

func gitScratch(t *testing.T) string {
	t.Helper()
	scrubGitEnv(t)
	root := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.invalid"},
		{"config", "commit.gpgsign", "false"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...) // #nosec G204 -- fixed binary, test-authored args
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return root
}

func runGitMode(t *testing.T, root, input string) (int, string, string) {
	t.Helper()
	var out, errBuf strings.Builder
	getenv := func(key string) string {
		switch key {
		case "EVOL_ARTIFACT_ROOT":
			return root
		case "EVOL_ARTIFACT_GIT":
			return "1"
		}
		return ""
	}
	code := run(strings.NewReader(input), &out, &errBuf, getenv)
	return code, out.String(), errBuf.String()
}

func writeReq(ref, body string) string {
	req := map[string]any{
		"evol": "1", "port": "artifactstore", "action": "write",
		"ref": ref, "frontmatter": "name: x\n", "body": body,
	}
	raw, _ := json.Marshal(req)
	return string(raw)
}

func gitLogSubjects(t *testing.T, root string) []string {
	t.Helper()
	out, err := gitOut(root, "log", "--format=%s")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(out, "\n")
}

func TestGitModeWriteCommits(t *testing.T) {
	root := gitScratch(t)
	code, out, errOut := runGitMode(t, root, writeReq("skills/a/SKILL.md", "v1 body\n"))
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut)
	}
	resp := decode(t, out)
	if resp.Version == "" || resp.GitCommit == "" {
		t.Fatalf("want version and git_commit, got %+v", resp)
	}
	subjects := gitLogSubjects(t, root)
	want := fmt.Sprintf("evol: promote skills/a/SKILL.md %s", resp.Version)
	if subjects[0] != want {
		t.Fatalf("commit subject %q, want %q", subjects[0], want)
	}
}

func TestGitModeRefusesUnrelatedStaged(t *testing.T) {
	root := gitScratch(t)
	mustWriteFile(t, filepath.Join(root, "unrelated.txt"), "staged\n")
	if _, err := gitOut(root, "add", "--", "unrelated.txt"); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runGitMode(t, root, writeReq("skills/a/SKILL.md", "body\n"))
	if code == 0 {
		t.Fatal("want failure with unrelated staged changes")
	}
	if !strings.Contains(errOut, "unrelated staged changes") {
		t.Fatalf("stderr %q lacks the staged-changes diagnostic", errOut)
	}
	// And the file must not have been swallowed into a commit.
	if out, _ := gitOut(root, "log", "--oneline"); out != "" {
		t.Fatalf("unexpected commits: %s", out)
	}
}

func TestNoGitPassthrough(t *testing.T) {
	root := gitScratch(t) // a git repo, but EVOL_ARTIFACT_GIT unset
	code, out, errOut := runWith(t, root, writeReq("skills/a/SKILL.md", "body\n"))
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut)
	}
	resp := decode(t, out)
	if resp.GitCommit != "" {
		t.Fatalf("git_commit set without EVOL_ARTIFACT_GIT: %+v", resp)
	}
	if out, _ := gitOut(root, "log", "--oneline"); out != "" {
		t.Fatalf("unexpected commits: %s", out)
	}
}

func TestVersionsAndRestoreRoundTrip(t *testing.T) {
	root := gitScratch(t)
	ref := "skills/a/SKILL.md"

	// Two promotions.
	code, out1, errOut := runGitMode(t, root, writeReq(ref, "v1 body\n"))
	if code != 0 {
		t.Fatalf("write v1: exit %d %s", code, errOut)
	}
	v1 := decode(t, out1)
	code, out2, errOut := runGitMode(t, root, writeReq(ref, "v2 body\n"))
	if code != 0 {
		t.Fatalf("write v2: exit %d %s", code, errOut)
	}
	v2 := decode(t, out2)

	// versions: newest first, both entries, versions match writes.
	code, out, errOut := runGitMode(t, root,
		`{"evol":"1","port":"artifactstore","action":"versions","ref":"`+ref+`"}`)
	if code != 0 {
		t.Fatalf("versions: exit %d %s", code, errOut)
	}
	vresp := decode(t, out)
	if len(vresp.Versions) != 2 {
		t.Fatalf("want 2 versions, got %+v", vresp.Versions)
	}
	if vresp.Versions[0].Version != v2.Version || vresp.Versions[1].Version != v1.Version {
		t.Fatalf("version order wrong: %+v (writes %s then %s)", vresp.Versions, v1.Version, v2.Version)
	}

	// restore to v1 by content-hash version.
	code, out, errOut = runGitMode(t, root,
		`{"evol":"1","port":"artifactstore","action":"restore","ref":"`+ref+`","version":"`+v1.Version+`"}`)
	if code != 0 {
		t.Fatalf("restore: exit %d %s", code, errOut)
	}
	rresp := decode(t, out)
	if rresp.Version != v1.Version || rresp.GitCommit == "" {
		t.Fatalf("restore response %+v, want version %s + a commit", rresp, v1.Version)
	}
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(ref))) // #nosec G304 -- t.TempDir path
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "v1 body") {
		t.Fatalf("restored content wrong: %q", content)
	}
	subjects := gitLogSubjects(t, root)
	if !strings.HasPrefix(subjects[0], "evol: rollback "+ref) {
		t.Fatalf("head commit %q is not a rollback commit", subjects[0])
	}
	if len(subjects) != 3 {
		t.Fatalf("want promote, promote, rollback = 3 commits, got %d: %v", len(subjects), subjects)
	}
}

func TestRestoreBySHAPrefix(t *testing.T) {
	root := gitScratch(t)
	ref := "skills/a/SKILL.md"
	_, out1, _ := runGitMode(t, root, writeReq(ref, "v1 body\n"))
	v1 := decode(t, out1)
	if code, _, errOut := runGitMode(t, root, writeReq(ref, "v2 body\n")); code != 0 {
		t.Fatalf("write v2: %s", errOut)
	}
	code, out, errOut := runGitMode(t, root,
		`{"evol":"1","port":"artifactstore","action":"restore","ref":"`+ref+`","version":"`+v1.GitCommit[:10]+`"}`)
	if code != 0 {
		t.Fatalf("restore by sha prefix: exit %d %s", code, errOut)
	}
	if decode(t, out).Version != v1.Version {
		t.Fatalf("restore by sha prefix landed on the wrong version")
	}
}

func TestVersionsWithoutGitModeErrors(t *testing.T) {
	root := t.TempDir() // no git repo at all
	scrubGitEnv(t)
	code, _, errOut := runWith(t, root,
		`{"evol":"1","port":"artifactstore","action":"versions","ref":"skills/a/SKILL.md"}`)
	if code == 0 {
		t.Fatal("want adapter error without git mode")
	}
	if !strings.Contains(errOut, "git-native mode") {
		t.Fatalf("stderr %q lacks the git-mode guidance", errOut)
	}
}
