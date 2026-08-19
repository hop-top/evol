// Command executor-apx implements the evol Executor port (spec/port-executor.md)
// as a layered subprocess runner: plain subprocess by default, optional xrr
// cassette env plumbing, optional aps profile wrapping. See README.md.
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	contractVersion = "1"
	portName        = "executor"
	actionRun       = "run"

	defaultTimeout = 120 * time.Second
)

type request struct {
	Evol         string `json:"evol"`
	Port         string `json:"port"`
	Action       string `json:"action"`
	CandidateRef string `json:"candidate_ref"`
	Case         struct {
		ID    string `json:"id"`
		Input string `json:"input"`
	} `json:"case"`
	Env struct {
		Mode string `json:"mode"`
	} `json:"env"`
}

type toolCall struct {
	Tool   string `json:"tool"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

type transcript struct {
	Output     string     `json:"output"`
	ToolCalls  []toolCall `json:"tool_calls"`
	DurationMS int64      `json:"duration_ms"`
	ExitCode   *int       `json:"exit_code,omitempty"`
}

type response struct {
	Evol       string      `json:"evol"`
	Port       string      `json:"port"`
	Action     string      `json:"action"`
	Transcript *transcript `json:"transcript,omitempty"`
	Error      string      `json:"error,omitempty"`
}

// env abstracts process environment for tests.
type env func(string) string

func main() {
	os.Exit(realMain(os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

func realMain(stdin io.Reader, stdout, stderr io.Writer, getenv env) int {
	req, err := decodeRequest(stdin)
	if err != nil {
		warnf(stderr, "executor-apx: %v\n", err)
		return 1
	}

	argv, err := commandTemplate(getenv)
	if err != nil {
		warnf(stderr, "executor-apx: %v\n", err)
		return 1
	}
	argv = substitute(argv, req)

	childEnv, xrrErr := xrrEnv(getenv, req)
	if xrrErr != nil {
		warnf(stderr, "executor-apx: %v\n", xrrErr)
		return 1
	}

	if profile := getenv("EVOL_APS_PROFILE"); profile != "" {
		argv = apsWrap(getenv, profile, childEnv, argv)
	}

	timeout, err := execTimeout(getenv)
	if err != nil {
		warnf(stderr, "executor-apx: %v\n", err)
		return 1
	}

	tr, runErr, adapterErr := runChild(argv, childEnv, req.Case.Input, timeout, getenv)
	if adapterErr != nil {
		warnf(stderr, "executor-apx: %v\n", adapterErr)
		return 1
	}

	resp := response{Evol: contractVersion, Port: portName, Action: actionRun, Transcript: tr, Error: runErr}
	enc := json.NewEncoder(stdout)
	if err := enc.Encode(resp); err != nil {
		warnf(stderr, "executor-apx: encode response: %v\n", err)
		return 1
	}
	return 0
}

func decodeRequest(stdin io.Reader) (*request, error) {
	raw, err := io.ReadAll(io.LimitReader(stdin, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("read stdin: %w", err)
	}
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("parse request: %w", err)
	}
	if req.Evol != contractVersion {
		return nil, fmt.Errorf("unsupported contract version %q (want %q)", req.Evol, contractVersion)
	}
	if req.Port != portName || req.Action != actionRun {
		return nil, fmt.Errorf("unsupported port/action %q/%q (want %s/%s)", req.Port, req.Action, portName, actionRun)
	}
	switch req.Env.Mode {
	case "", "live", "replay", "record":
	default:
		return nil, fmt.Errorf("unsupported env.mode %q (want replay|record|live)", req.Env.Mode)
	}
	return &req, nil
}

func commandTemplate(getenv env) ([]string, error) {
	raw := getenv("EVOL_EXEC_CMD")
	if raw == "" {
		return nil, errors.New("EVOL_EXEC_CMD is not set (JSON argv array required)")
	}
	var argv []string
	if err := json.Unmarshal([]byte(raw), &argv); err != nil {
		return nil, fmt.Errorf("EVOL_EXEC_CMD is not a JSON argv array: %w", err)
	}
	if len(argv) == 0 {
		return nil, errors.New("EVOL_EXEC_CMD is an empty argv array")
	}
	return argv, nil
}

func substitute(argv []string, req *request) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		a = strings.ReplaceAll(a, "{input}", req.Case.Input)
		a = strings.ReplaceAll(a, "{candidate_ref}", req.CandidateRef)
		out[i] = a
	}
	return out
}

// xrrEnv computes the extra child environment for the xrr layer.
// The layer is enabled by EVOL_XRR_MODE; the request's env.mode, when set,
// selects the effective mode (the engine varies record/replay per run —
// record for baselines, replay for candidates). Mode "live" disables
// injection for that run. Returns adapter errors for inconsistent config
// or for frozen-mode requests when the layer is disabled.
func xrrEnv(getenv env, req *request) ([]string, error) {
	layerMode := getenv("EVOL_XRR_MODE")
	mode := layerMode
	if req.Env.Mode != "" {
		mode = req.Env.Mode
	}
	if layerMode == "" {
		if mode == "replay" || mode == "record" {
			return nil, fmt.Errorf("env.mode %q requested but xrr layer is disabled (set EVOL_XRR_MODE)", mode)
		}
		return nil, nil
	}
	if mode == "live" || mode == "" {
		return nil, nil
	}

	dir := getenv("EVOL_XRR_CASSETTE_DIR")
	if dir == "" {
		root := getenv("EVOL_XRR_CASSETTE_ROOT")
		if root == "" {
			return nil, errors.New("EVOL_XRR_MODE set but neither EVOL_XRR_CASSETTE_DIR nor EVOL_XRR_CASSETTE_ROOT given")
		}
		sum := sha256.Sum256([]byte(req.CandidateRef))
		dir = filepath.Join(root, hex.EncodeToString(sum[:])[:12])
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create cassette dir: %w", err)
	}
	return []string{"XRR_MODE=" + mode, "XRR_CASSETTE_DIR=" + dir}, nil
}

// apsWrap rewrites argv to run under an aps profile. Env pairs must be
// passed as --env flags BEFORE the -- separator; after it, everything goes
// verbatim to the child.
func apsWrap(getenv env, profile string, childEnv, argv []string) []string {
	apsBin := getenv("EVOL_APS_BIN")
	if apsBin == "" {
		apsBin = "aps"
	}
	wrapped := []string{apsBin, "run", profile}
	for _, kv := range childEnv {
		wrapped = append(wrapped, "--env", kv)
	}
	wrapped = append(wrapped, "--")
	return append(wrapped, argv...)
}

func execTimeout(getenv env) (time.Duration, error) {
	raw := getenv("EVOL_EXEC_TIMEOUT")
	if raw == "" {
		return defaultTimeout, nil
	}
	if d, err := time.ParseDuration(raw); err == nil {
		return d, nil
	}
	if secs, err := strconv.Atoi(raw); err == nil {
		return time.Duration(secs) * time.Second, nil
	}
	return 0, fmt.Errorf("EVOL_EXEC_TIMEOUT %q: not a duration or integer seconds", raw)
}

// runChild executes argv and shapes the two failure planes:
// adapter failures (cannot start) abort with err; run failures (timeout,
// signal) return a transcript plus a run-level error string; a non-zero
// child exit is plain data in transcript.exit_code.
func runChild(argv, extraEnv []string, caseInput string, timeout time.Duration, getenv env) (*transcript, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	//nolint:gosec // G204: argv comes from operator config (EVOL_EXEC_CMD); executing it is this adapter's purpose.
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdin = strings.NewReader(caseInput)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Kill the whole process group on deadline (grandchildren inherit the
	// stdout pipe and would otherwise block Wait past the timeout), with
	// WaitDelay as the backstop for anything that survives.
	setProcGroup(cmd)
	cmd.WaitDelay = time.Second

	start := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(start).Milliseconds()

	output := stdout.String()
	if getenv("EVOL_APS_PROFILE") != "" {
		output = stripAPSEnvelope(output)
	}

	tr := &transcript{Output: output, ToolCalls: []toolCall{}, DurationMS: elapsed}

	if ctx.Err() == context.DeadlineExceeded {
		return tr, fmt.Sprintf("timeout after %s", timeout), nil
	}
	if runErr == nil {
		code := 0
		tr.ExitCode = &code
		return tr, "", nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		code := exitErr.ExitCode()
		if code >= 0 {
			// Non-zero exit is data for scoring, not a run failure.
			tr.ExitCode = &code
			return tr, "", nil
		}
		// Killed by signal without a deadline: run failure.
		return tr, fmt.Sprintf("child terminated by signal: %v", exitErr), nil
	}
	// Could not start at all: adapter failure.
	return nil, "", fmt.Errorf("start %q: %w (stderr: %s)", argv[0], runErr, tail(stderr.String(), 300))
}

// stripAPSEnvelope removes aps progress lines from captured stdout: either
// bracket-prefixed text lines ("[exec] ...", "[exit] ...") or JSONL objects
// whose event/type marks exec/exit. Everything else passes through intact.
func stripAPSEnvelope(s string) string {
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, line := range lines {
		if isAPSEnvelopeLine(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func isAPSEnvelopeLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "[exec]") || strings.HasPrefix(trimmed, "[exit]") {
		return true
	}
	if !strings.HasPrefix(trimmed, "{") {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
		return false
	}
	for _, key := range []string{"event", "type"} {
		if v, ok := obj[key].(string); ok && (v == "exec" || v == "exit") {
			return true
		}
	}
	_, hasExec := obj["exec"]
	_, hasExit := obj["exit"]
	return hasExec || hasExit
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// warnf writes a diagnostic line, ignoring write errors — stderr is
// best-effort by definition here.
func warnf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
