package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeTLC writes a shell script that records argv + stdin to files and
// prints a canned stdout.
func fakeTLC(t *testing.T, stdoutPayload string) (bin, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake")
	}
	dir = t.TempDir()
	bin = filepath.Join(dir, "tlc")
	script := `#!/bin/sh
printf '%s\n' "$@" > "` + dir + `/argv.txt"
cat > "` + dir + `/stdin.txt"
printf '%s' '` + stdoutPayload + `'
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil { // #nosec G306 -- executable test fake
		t.Fatal(err)
	}
	return bin, dir
}

func callWith(t *testing.T, bin, chdir, request string) (map[string]any, error) {
	t.Helper()
	var out, errBuf bytes.Buffer
	getenv := func(k string) string {
		switch k {
		case "EVOL_TLC_BIN":
			return bin
		case "EVOL_TLC_CHDIR":
			return chdir
		}
		return ""
	}
	err := run(strings.NewReader(request), &out, &errBuf, getenv)
	if err != nil {
		return nil, err
	}
	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("bad response %q: %v", out.String(), err)
	}
	return resp, nil
}

func argvOf(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "argv.txt")) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestRecordArgvAndStdin(t *testing.T) {
	bin, dir := fakeTLC(t, `{}`)
	req := `{"evol":"1","port":"audit","action":"record",` +
		`"run":{"tool":"evol","run_id":"r1","subject":"s","started_at":"t","finished_at":"t","outcome":"promoted"}}`
	if _, err := callWith(t, bin, "", req); err != nil {
		t.Fatalf("record: %v", err)
	}
	want := []string{"audit", "record", "--tool", "evol", "--stdin"}
	got := argvOf(t, dir)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("argv: got %v want %v", got, want)
	}
	stdin, _ := os.ReadFile(filepath.Join(dir, "stdin.txt")) // #nosec G304 -- test temp dir
	var payload map[string]any
	if err := json.Unmarshal(stdin, &payload); err != nil {
		t.Fatalf("stdin not JSON: %v", err)
	}
	if payload["run_id"] != "r1" || payload["tool"] != "evol" {
		t.Fatalf("stdin payload: %v", payload)
	}
}

func TestChdirPrependsGlobalFlag(t *testing.T) {
	bin, dir := fakeTLC(t, `[]`)
	req := `{"evol":"1","port":"audit","action":"list","tool":"evol"}`
	if _, err := callWith(t, bin, "/some/project", req); err != nil {
		t.Fatalf("list: %v", err)
	}
	got := argvOf(t, dir)
	if got[0] != "-C" || got[1] != "/some/project" {
		t.Fatalf("chdir not first: %v", got)
	}
}

func TestListArgvAndBareArrayParse(t *testing.T) {
	bin, dir := fakeTLC(t, `[{"run_id":"r2"},{"run_id":"r1"}]`)
	req := `{"evol":"1","port":"audit","action":"list","tool":"evol","subject":"s","limit":5}`
	resp, err := callWith(t, bin, "", req)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := "audit list --format json --tool evol --subject s --limit 5"
	if got := strings.Join(argvOf(t, dir), " "); got != want {
		t.Fatalf("argv: got %q want %q", got, want)
	}
	runs := resp["runs"].([]any)
	if len(runs) != 2 || runs[0].(map[string]any)["run_id"] != "r2" {
		t.Fatalf("parsed runs: %v", runs)
	}
}

func TestListWrappedObjectParse(t *testing.T) {
	bin, _ := fakeTLC(t, `{"runs":[{"run_id":"r9"}]}`)
	resp, err := callWith(t, bin, "", `{"evol":"1","port":"audit","action":"list"}`)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	runs := resp["runs"].([]any)
	if len(runs) != 1 || runs[0].(map[string]any)["run_id"] != "r9" {
		t.Fatalf("wrapped parse: %v", runs)
	}
}

func TestShowArgvAndUnwrap(t *testing.T) {
	bin, dir := fakeTLC(t, `{"run":{"run_id":"r1","outcome":"promoted"}}`)
	resp, err := callWith(t, bin, "", `{"evol":"1","port":"audit","action":"show","run_id":"r1","tool":"evol"}`)
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	want := "audit show r1 --format json --tool evol"
	if got := strings.Join(argvOf(t, dir), " "); got != want {
		t.Fatalf("argv: got %q want %q", got, want)
	}
	runObj := resp["run"].(map[string]any)
	if runObj["outcome"] != "promoted" {
		t.Fatalf("unwrap: %v", runObj)
	}
}

func TestTLCFailureIsAdapterError(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tlc")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 7\n"), 0o700); err != nil { // #nosec G306 -- executable test fake
		t.Fatal(err)
	}
	if _, err := callWith(t, bin, "", `{"evol":"1","port":"audit","action":"list"}`); err == nil {
		t.Fatal("want error when tlc exits non-zero")
	}
}

func TestBadRequests(t *testing.T) {
	bin, _ := fakeTLC(t, `[]`)
	for _, req := range []string{
		`nope`,
		`{"evol":"2","port":"audit","action":"list"}`,
		`{"evol":"1","port":"corpus","action":"list"}`,
		`{"evol":"1","port":"audit","action":"bogus"}`,
		`{"evol":"1","port":"audit","action":"record","run":{}}`,
		`{"evol":"1","port":"audit","action":"show"}`,
	} {
		if _, err := callWith(t, bin, "", req); err == nil {
			t.Errorf("want error for %q", req)
		}
	}
}
