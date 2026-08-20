package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hop.top/evol/internal/engine"
)

// fakeStore answers versions + restore, recording each request.
func fakeStore(t *testing.T, dir string) (bin, reqLog string) {
	t.Helper()
	reqLog = filepath.Join(dir, "store-reqs.jsonl")
	bin = filepath.Join(dir, "fake-store")
	script := fmt.Sprintf(`#!/bin/sh
req=$(cat)
printf '%%s\n' "$req" >> %q
case "$req" in
*'"action":"versions"'*)
  printf '{"evol":"1","port":"artifactstore","action":"versions","versions":[{"version":"vNEW","git_commit":"cccc2222"},{"version":"vOLD","git_commit":"aaaa1111"}]}\n' ;;
*'"action":"restore"'*)
  printf '{"evol":"1","port":"artifactstore","action":"restore","version":"vOLD","git_commit":"dddd3333"}\n' ;;
*) exit 1 ;;
esac
`, reqLog)
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil { //nolint:gosec // test fixture executable
		t.Fatal(err)
	}
	return bin, reqLog
}

func rollbackConfig(t *testing.T, dir, storeBin string) string {
	t.Helper()
	cfg := filepath.Join(dir, "evol.yaml")
	content := fmt.Sprintf("ports:\n  artifactstore:\n    cmd: [%q]\n", storeBin)
	if err := os.WriteFile(cfg, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRollbackDefaultsToPreviousVersion(t *testing.T) {
	dir := t.TempDir()
	bin, reqLog := fakeStore(t, dir)
	cfg := rollbackConfig(t, dir, bin)

	out, errOut, err := execRoot(t, "rollback",
		"--config", cfg, "--artifact", "a/SKILL.md", "--format", "json")
	if err != nil {
		t.Fatalf("rollback: %v (stderr %s)", err, errOut)
	}
	var res rollbackResult
	if jErr := json.Unmarshal([]byte(out), &res); jErr != nil {
		t.Fatalf("output not JSON: %v\n%s", jErr, out)
	}
	if res.RestoredTo != "vOLD" || res.GitCommit != "dddd3333" {
		t.Fatalf("unexpected result: %+v", res)
	}

	raw, _ := os.ReadFile(reqLog) //nolint:gosec // test temp path
	if !strings.Contains(string(raw), `"version":"vOLD"`) {
		t.Fatalf("restore request did not target the previous version:\n%s", raw)
	}
}

func TestRollbackExplicitTarget(t *testing.T) {
	dir := t.TempDir()
	bin, reqLog := fakeStore(t, dir)
	cfg := rollbackConfig(t, dir, bin)

	_, errOut, err := execRoot(t, "rollback",
		"--config", cfg, "--artifact", "a/SKILL.md", "--to", "aaaa1111")
	if err != nil {
		t.Fatalf("rollback --to: %v (stderr %s)", err, errOut)
	}
	raw, _ := os.ReadFile(reqLog) //nolint:gosec // test temp path
	if !strings.Contains(string(raw), `"version":"aaaa1111"`) {
		t.Fatalf("restore request did not carry the explicit target:\n%s", raw)
	}
}

func TestRollbackNoHistoryAdapterError(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-store-nohist")
	script := "#!/bin/sh\necho 'fs-artifact: versions requires git-native mode' >&2\nexit 1\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil { //nolint:gosec // test fixture executable
		t.Fatal(err)
	}
	cfg := rollbackConfig(t, dir, bin)

	_, errOut, err := execRoot(t, "rollback", "--config", cfg, "--artifact", "a/SKILL.md")
	if err == nil || ExitCode(err) != exitConfigError {
		t.Fatalf("want exit %d, got err=%v", exitConfigError, err)
	}
	if !strings.Contains(errOut, "git-native mode") {
		t.Fatalf("stderr lacks adapter guidance: %s", errOut)
	}
}

func TestPromotionHookRunsAndFailureWarns(t *testing.T) {
	dir := t.TempDir()
	capture := filepath.Join(dir, "hook-env.txt")
	hook := filepath.Join(dir, "hook.sh")
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s|%%s|%%s\\n' \"$EVOL_PROMOTED_REF\" \"$EVOL_PROMOTED_VERSION\" \"$EVOL_PROMOTED_GIT_COMMIT\" > %q\n", capture)
	if err := os.WriteFile(hook, []byte(script), 0o700); err != nil { //nolint:gosec // test fixture executable
		t.Fatal(err)
	}

	cfg := &engine.Config{}
	cfg.Promotion.Hook = []string{hook}
	res := &engine.Result{
		Accepted: true, ArtifactRef: "a/SKILL.md",
		NewVersion: "v2", GitCommit: "abc123",
	}
	resetFlags(rootCmd)
	rootCmd.SetArgs(nil)
	runPromotionHook(rollbackCmd, cfg, res)

	raw, err := os.ReadFile(capture) //nolint:gosec // test temp path
	if err != nil {
		t.Fatalf("hook did not run: %v", err)
	}
	if got := strings.TrimSpace(string(raw)); got != "a/SKILL.md|v2|abc123" {
		t.Fatalf("hook env %q", got)
	}

	// Failing hook: warning only, no panic, promotion result untouched.
	cfg.Promotion.Hook = []string{filepath.Join(dir, "does-not-exist")}
	runPromotionHook(rollbackCmd, cfg, res)

	// Unaccepted result: hook must not run.
	if err := os.Remove(capture); err != nil {
		t.Fatal(err)
	}
	cfg.Promotion.Hook = []string{hook}
	runPromotionHook(rollbackCmd, cfg, &engine.Result{Accepted: false})
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatal("hook ran for an unaccepted result")
	}
}
