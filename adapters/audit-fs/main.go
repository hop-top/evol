// Command audit-fs implements the Audit port (spec/port-audit.md) over
// a single JSONL ledger file.
//
// Layout: $EVOL_AUDIT_ROOT/runs.jsonl — one run record per line.
// record upserts by (tool, run_id) with an atomic rewrite; list serves
// newest first; show refuses unknown run ids.
//
// Wire protocol: one JSON request on stdin, one JSON response on
// stdout. Non-zero exit = adapter error (stderr carries diagnostics).
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

const (
	contractVersion = "1"
	portName        = "audit"
	ledgerFile      = "runs.jsonl"
	maxLineBytes    = 1 << 20
)

type envelope struct {
	Evol   string `json:"evol"`
	Port   string `json:"port"`
	Action string `json:"action"`
}

// Step is one phase of a recorded run.
type Step struct {
	Seq    int    `json:"seq"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// Run is one ledger entry, as specced.
type Run struct {
	Tool       string         `json:"tool"`
	RunID      string         `json:"run_id"`
	Subject    string         `json:"subject"`
	StartedAt  string         `json:"started_at"`
	FinishedAt string         `json:"finished_at"`
	Outcome    string         `json:"outcome"`
	Metrics    map[string]any `json:"metrics,omitempty"`
	Steps      []Step         `json:"steps,omitempty"`
}

func main() {
	if err := run(os.Stdin, os.Stdout, os.Getenv); err != nil {
		fmt.Fprintf(os.Stderr, "audit-fs: %v\n", err)
		os.Exit(1)
	}
}

func run(stdin io.Reader, stdout io.Writer, getenv func(string) string) error {
	root := getenv("EVOL_AUDIT_ROOT")
	if root == "" {
		return errors.New("EVOL_AUDIT_ROOT is not set")
	}

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

	switch env.Action {
	case "record":
		return doRecord(raw, root, stdout)
	case "list":
		return doList(raw, root, stdout)
	case "show":
		return doShow(raw, root, stdout)
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
	enc := json.NewEncoder(stdout)
	return enc.Encode(payload)
}

func readLedger(root string) ([]Run, error) {
	path := filepath.Join(root, ledgerFile)
	f, err := os.Open(path) // #nosec G304 -- operator-configured ledger root is the adapter's purpose
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var runs []Run
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	line := 0
	for sc.Scan() {
		line++
		text := bytes.TrimSpace(sc.Bytes())
		if len(text) == 0 {
			continue
		}
		var r Run
		if err := json.Unmarshal(text, &r); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", ledgerFile, line, err)
		}
		runs = append(runs, r)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return runs, nil
}

func writeLedger(root string, runs []Run) error {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(root, ledgerFile+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	enc := json.NewEncoder(tmp)
	for _, r := range runs {
		if err := enc.Encode(r); err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
			return err
		}
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, filepath.Join(root, ledgerFile))
}

func doRecord(raw []byte, root string, stdout io.Writer) error {
	var req struct {
		Run Run `json:"run"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("parse record request: %w", err)
	}
	if req.Run.RunID == "" {
		return errors.New("record: run.run_id is required")
	}
	if req.Run.Tool == "" {
		return errors.New("record: run.tool is required")
	}

	runs, err := readLedger(root)
	if err != nil {
		return err
	}
	replaced := false
	for i := range runs {
		if runs[i].Tool == req.Run.Tool && runs[i].RunID == req.Run.RunID {
			runs[i] = req.Run
			replaced = true
			break
		}
	}
	if !replaced {
		runs = append(runs, req.Run)
	}
	if err := writeLedger(root, runs); err != nil {
		return err
	}
	return reply(stdout, "record", nil)
}

// newestFirst orders by started_at descending, ties broken by run_id
// ascending for determinism.
func newestFirst(runs []Run) {
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].StartedAt != runs[j].StartedAt {
			return runs[i].StartedAt > runs[j].StartedAt
		}
		return runs[i].RunID < runs[j].RunID
	})
}

func doList(raw []byte, root string, stdout io.Writer) error {
	var req struct {
		Tool    string `json:"tool"`
		Subject string `json:"subject"`
		Limit   int    `json:"limit"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("parse list request: %w", err)
	}

	runs, err := readLedger(root)
	if err != nil {
		return err
	}
	filtered := make([]Run, 0, len(runs))
	for _, r := range runs {
		if req.Tool != "" && r.Tool != req.Tool {
			continue
		}
		if req.Subject != "" && r.Subject != req.Subject {
			continue
		}
		filtered = append(filtered, r)
	}
	newestFirst(filtered)
	if req.Limit > 0 && len(filtered) > req.Limit {
		filtered = filtered[:req.Limit]
	}
	return reply(stdout, "list", map[string]any{"runs": filtered})
}

func doShow(raw []byte, root string, stdout io.Writer) error {
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

	runs, err := readLedger(root)
	if err != nil {
		return err
	}
	for _, r := range runs {
		if r.RunID != req.RunID {
			continue
		}
		if req.Tool != "" && r.Tool != req.Tool {
			continue
		}
		return reply(stdout, "show", map[string]any{"run": r})
	}
	return fmt.Errorf("show: run %q not found", req.RunID)
}
