// Command audit-tlc implements the Audit port (spec/port-audit.md) by
// delegating to the family task tracker's external-run audit surface:
//
//	tlc audit record --tool <t> --stdin
//	tlc audit list   --tool <t> [--subject s] [--limit N] --format json
//	tlc audit show   <run-id> [--tool <t>] --format json
//
// This makes the tracker the ledger's home: loop runs sit next to task
// and flow history, project-scoped the same way (tlc resolves the
// project from the working directory; set EVOL_TLC_CHDIR to pass an
// explicit `-C <dir>`).
//
// Wire protocol: one JSON request on stdin, one JSON response on
// stdout. Non-zero exit = adapter error (stderr carries diagnostics).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const (
	contractVersion = "1"
	portName        = "audit"
	defaultTimeout  = 30 * time.Second
)

type envelope struct {
	Evol   string `json:"evol"`
	Port   string `json:"port"`
	Action string `json:"action"`
}

func main() {
	if err := run(os.Stdin, os.Stdout, os.Stderr, os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "audit-tlc: %v\n", err)
		os.Exit(1)
	}
}

type tlcRunner struct {
	bin     string
	chdir   string
	timeout time.Duration
	stderr  io.Writer
}

func newRunner(getenv func(string) string, stderr io.Writer) tlcRunner {
	r := tlcRunner{bin: "tlc", timeout: defaultTimeout, stderr: stderr}
	if v := getenv("EVOL_TLC_BIN"); v != "" {
		r.bin = v
	}
	r.chdir = getenv("EVOL_TLC_CHDIR")
	if v := getenv("EVOL_TLC_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			r.timeout = d
		}
	}
	return r
}

// argv builds the tlc invocation: global -C first when configured, then
// the audit subcommand arguments.
func (r tlcRunner) argv(args ...string) []string {
	out := make([]string, 0, len(args)+2)
	if r.chdir != "" {
		out = append(out, "-C", r.chdir)
	}
	return append(out, args...)
}

func (r tlcRunner) exec(stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	full := r.argv(args...)
	cmd := exec.CommandContext(ctx, r.bin, full...) // #nosec G204 -- operator-configured tracker binary is the adapter's purpose
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = r.stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("tlc timed out after %s", r.timeout)
		}
		return nil, fmt.Errorf("tlc %v: %w", full, err)
	}
	return out.Bytes(), nil
}

func run(stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) error {
	raw, err := io.ReadAll(io.LimitReader(stdin, 8<<20))
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("parse request: %w", err)
	}
	if env.Evol != contractVersion {
		return fmt.Errorf("unsupported contract version %q", env.Evol)
	}
	if env.Port != portName {
		return fmt.Errorf("wrong port %q", env.Port)
	}

	tlc := newRunner(getenv, stderr)

	switch env.Action {
	case "record":
		return doRecord(raw, tlc, stdout)
	case "list":
		return doList(raw, tlc, stdout)
	case "show":
		return doShow(raw, tlc, stdout)
	default:
		return fmt.Errorf("unsupported action %q", env.Action)
	}
}

func reply(stdout io.Writer, action string, extra map[string]any) error {
	payload := map[string]any{
		"evol": contractVersion, "port": portName, "action": action,
	}
	for k, v := range extra {
		payload[k] = v
	}
	return json.NewEncoder(stdout).Encode(payload)
}

func doRecord(raw []byte, tlc tlcRunner, stdout io.Writer) error {
	var req struct {
		Run map[string]any `json:"run"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("parse record request: %w", err)
	}
	runID, _ := req.Run["run_id"].(string)
	tool, _ := req.Run["tool"].(string)
	if runID == "" || tool == "" {
		return errors.New("record: run.tool and run.run_id are required")
	}
	payload, err := json.Marshal(req.Run)
	if err != nil {
		return err
	}
	if _, err := tlc.exec(payload, "audit", "record", "--tool", tool, "--stdin"); err != nil {
		return err
	}
	return reply(stdout, "record", nil)
}

// parseRuns tolerates both a bare JSON array and a {"runs": [...]}
// wrapper — the tracker side of this contract is young, and additive
// envelope drift must not break the consumer.
func parseRuns(out []byte) ([]map[string]any, error) {
	trimmed := bytes.TrimSpace(out)
	var arr []map[string]any
	if err := json.Unmarshal(trimmed, &arr); err == nil {
		return arr, nil
	}
	var wrapped struct {
		Runs []map[string]any `json:"runs"`
	}
	if err := json.Unmarshal(trimmed, &wrapped); err == nil && wrapped.Runs != nil {
		return wrapped.Runs, nil
	}
	return nil, fmt.Errorf("unparseable tlc list output: %.120s", trimmed)
}

func doList(raw []byte, tlc tlcRunner, stdout io.Writer) error {
	var req struct {
		Tool    string `json:"tool"`
		Subject string `json:"subject"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("parse list request: %w", err)
	}
	args := []string{"audit", "list", "--format", "json"}
	if req.Tool != "" {
		args = append(args, "--tool", req.Tool)
	}
	if req.Subject != "" {
		args = append(args, "--subject", req.Subject)
	}
	if req.Limit > 0 {
		args = append(args, "--limit", strconv.Itoa(req.Limit))
	}
	out, err := tlc.exec(nil, args...)
	if err != nil {
		return err
	}
	runs, err := parseRuns(out)
	if err != nil {
		return err
	}
	if runs == nil {
		runs = []map[string]any{}
	}
	return reply(stdout, "list", map[string]any{"runs": runs})
}

func doShow(raw []byte, tlc tlcRunner, stdout io.Writer) error {
	var req struct {
		Tool  string `json:"tool"`
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("parse show request: %w", err)
	}
	if req.RunID == "" {
		return errors.New("show: run_id is required")
	}
	args := []string{"audit", "show", req.RunID, "--format", "json"}
	if req.Tool != "" {
		args = append(args, "--tool", req.Tool)
	}
	out, err := tlc.exec(nil, args...)
	if err != nil {
		return err
	}
	trimmed := bytes.TrimSpace(out)
	var obj map[string]any
	if err := json.Unmarshal(trimmed, &obj); err != nil {
		return fmt.Errorf("unparseable tlc show output: %.120s", trimmed)
	}
	// Tolerate a {"run": {...}} wrapper the same way list tolerates one.
	if inner, ok := obj["run"].(map[string]any); ok {
		obj = inner
	}
	return reply(stdout, "show", map[string]any{"run": obj})
}
