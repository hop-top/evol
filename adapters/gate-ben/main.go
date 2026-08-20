// Command gate-ben implements a draft evol Gate port over the ben
// benchmark runner. ben has no baseline/threshold/fail-on-regression
// concept of its own (it exits 0 regardless of outcome), so this
// adapter runs the suite, fetches the pinned baseline run, and computes
// the pass/fail verdict from the two result payloads.
//
// The Gate port is not part of spec/ yet (Tier 2 — extracted from
// working implementations, not designed up front). The contract
// implemented here is a draft; see README.md in this directory.
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
	"sort"
	"strings"
	"time"
)

const (
	contractVersion = "1"
	portName        = "gate"
	actionName      = "check"

	defaultTimeout = 300 * time.Second
)

// request is the draft Gate check request.
type request struct {
	Evol         string   `json:"evol"`
	Port         string   `json:"port"`
	Action       string   `json:"action"`
	CandidateRef string   `json:"candidate_ref"`
	Baseline     *ref     `json:"baseline"`
	Suite        string   `json:"suite"`
	Metrics      []metric `json:"metrics"`
}

type ref struct {
	RunID string `json:"run_id"`
}

type metric struct {
	Name           string  `json:"name"`
	Direction      string  `json:"direction"` // "min" (lower is better) | "max" (higher is better)
	ThresholdDelta float64 `json:"threshold_delta"`
}

// response is the draft Gate check response.
type response struct {
	Evol   string  `json:"evol"`
	Port   string  `json:"port"`
	Action string  `json:"action"`
	Pass   bool    `json:"pass"`
	Deltas []delta `json:"deltas"`
	RunID  string  `json:"run_id"`
	Reason string  `json:"reason"`
}

type delta struct {
	Metric    string  `json:"metric"`
	Baseline  float64 `json:"baseline"`
	Candidate float64 `json:"candidate"`
	Delta     float64 `json:"delta"`
}

// benRun is the subset of ben's run/show JSON this adapter reads.
type benRun struct {
	RunID      string         `json:"run_id"`
	Suite      string         `json:"suite"`
	Candidates []benCandidate `json:"candidates"`
}

type benCandidate struct {
	Name    string                 `json:"name"`
	Metrics map[string]json.Number `json:"metrics"`
}

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

func run(stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	req, err := decodeRequest(stdin)
	if err != nil {
		errf(stderr, "gate-ben: %v\n", err)
		return 1
	}

	timeout := defaultTimeout
	if v := getenv("EVOL_BEN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			errf(stderr, "gate-ben: invalid EVOL_BEN_TIMEOUT %q: %v\n", v, err)
			return 1
		}
		timeout = d
	}

	benBin := getenv("EVOL_BEN_BIN")
	if benBin == "" {
		benBin = "ben"
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	candRun, err := benJSON(ctx, stderr, benBin, "run", req.Suite, "--format", "json")
	if err != nil {
		errf(stderr, "gate-ben: candidate run: %v\n", err)
		return 1
	}
	candRow, err := pickCandidate(candRun, req.CandidateRef)
	if err != nil {
		errf(stderr, "gate-ben: candidate run %s: %v\n", candRun.RunID, err)
		return 1
	}

	resp := response{
		Evol:   contractVersion,
		Port:   portName,
		Action: actionName,
		Deltas: []delta{},
		RunID:  candRun.RunID,
	}

	if req.Baseline == nil || req.Baseline.RunID == "" {
		resp.Pass = true
		resp.Reason = "no baseline (first run)"
		return emit(stdout, stderr, resp)
	}

	baseRun, err := benJSON(ctx, stderr, benBin, "show", req.Baseline.RunID, "--format", "json")
	if err != nil {
		errf(stderr, "gate-ben: baseline show %s: %v\n", req.Baseline.RunID, err)
		return 1
	}
	baseRow, err := pickCandidate(baseRun, req.CandidateRef)
	if err != nil {
		errf(stderr, "gate-ben: baseline run %s: %v\n", req.Baseline.RunID, err)
		return 1
	}

	resp.Pass = true
	var reasons []string
	for _, m := range req.Metrics {
		cv, cok := metricValue(candRow, m.Name)
		bv, bok := metricValue(baseRow, m.Name)
		if !cok || !bok {
			// A metric absent from either run must fail the gate:
			// silently skipping it is how regressions hide.
			resp.Pass = false
			reasons = append(reasons, fmt.Sprintf("metric %q missing (baseline present: %t, candidate present: %t)", m.Name, bok, cok))
			continue
		}
		d := cv - bv
		resp.Deltas = append(resp.Deltas, delta{Metric: m.Name, Baseline: bv, Candidate: cv, Delta: d})
		switch m.Direction {
		case "min": // lower is better; regression when candidate exceeds baseline by more than the threshold
			if d > m.ThresholdDelta {
				resp.Pass = false
				reasons = append(reasons, fmt.Sprintf("%s regressed: %v -> %v (delta %+v > %v)", m.Name, bv, cv, d, m.ThresholdDelta))
			}
		case "max": // higher is better; regression when candidate falls below baseline by more than the threshold
			if -d > m.ThresholdDelta {
				resp.Pass = false
				reasons = append(reasons, fmt.Sprintf("%s regressed: %v -> %v (delta %+v < -%v)", m.Name, bv, cv, d, m.ThresholdDelta))
			}
		}
	}
	if resp.Pass {
		resp.Reason = fmt.Sprintf("all %d metrics within thresholds vs baseline %s", len(req.Metrics), req.Baseline.RunID)
	} else {
		resp.Reason = strings.Join(reasons, "; ")
	}
	return emit(stdout, stderr, resp)
}

func decodeRequest(r io.Reader) (*request, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var req request
	if err := dec.Decode(&req); err != nil {
		return nil, fmt.Errorf("decode request: %w", err)
	}
	if req.Evol != contractVersion {
		return nil, fmt.Errorf("unsupported contract version %q (want %q)", req.Evol, contractVersion)
	}
	if req.Port != portName || req.Action != actionName {
		return nil, fmt.Errorf("unsupported port/action %q/%q (want %q/%q)", req.Port, req.Action, portName, actionName)
	}
	if req.Suite == "" {
		return nil, errors.New("suite is required")
	}
	if len(req.Metrics) == 0 {
		return nil, errors.New("metrics is required: a gate with nothing to check is a misconfiguration")
	}
	for _, m := range req.Metrics {
		if m.Name == "" {
			return nil, errors.New("metrics[].name is required")
		}
		if m.Direction != "min" && m.Direction != "max" {
			return nil, fmt.Errorf("metrics[%q].direction must be \"min\" or \"max\", got %q", m.Name, m.Direction)
		}
		if m.ThresholdDelta < 0 {
			return nil, fmt.Errorf("metrics[%q].threshold_delta must be >= 0", m.Name)
		}
	}
	return &req, nil
}

// benJSON invokes ben and parses its JSON output, unwrapping the _meta
// envelope that ben's read verbs (list/show) add around payloads.
func benJSON(ctx context.Context, stderr io.Writer, bin string, args ...string) (*benRun, error) {
	var stdout, errBuf bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...) // #nosec G204 -- binary and args are operator-configured by design; this adapter exists to shell out to ben
	cmd.Stdout = &stdout
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		if errBuf.Len() > 0 {
			errf(stderr, "ben stderr: %s\n", strings.TrimSpace(errBuf.String()))
		}
		return nil, fmt.Errorf("ben %s: %w", strings.Join(args, " "), err)
	}
	return parseBenRun(stdout.Bytes())
}

func parseBenRun(raw []byte) (*benRun, error) {
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("parse ben output: %w", err)
	}
	delete(generic, "_meta")
	// Some verbs nest the payload under "data"; unwrap when the outer
	// object has no run fields of its own.
	if data, ok := generic["data"]; ok {
		if _, hasRun := generic["run_id"]; !hasRun {
			var inner map[string]json.RawMessage
			if err := json.Unmarshal(data, &inner); err == nil {
				generic = inner
			}
		}
	}
	rewrapped, err := json.Marshal(generic)
	if err != nil {
		return nil, fmt.Errorf("rewrap ben output: %w", err)
	}
	var run benRun
	if err := json.Unmarshal(rewrapped, &run); err != nil {
		return nil, fmt.Errorf("parse ben run: %w", err)
	}
	if run.RunID == "" {
		return nil, errors.New("ben output has no run_id")
	}
	return &run, nil
}

// pickCandidate resolves which ben candidate row the gate scores:
// exact name match on candidate_ref, else the sole candidate when the
// run has exactly one. Anything else is ambiguous configuration.
func pickCandidate(run *benRun, candidateRef string) (*benCandidate, error) {
	for i := range run.Candidates {
		if run.Candidates[i].Name == candidateRef {
			return &run.Candidates[i], nil
		}
	}
	if len(run.Candidates) == 1 {
		return &run.Candidates[0], nil
	}
	names := make([]string, 0, len(run.Candidates))
	for _, c := range run.Candidates {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return nil, fmt.Errorf("candidate %q not found and run has %d candidates (%s)", candidateRef, len(run.Candidates), strings.Join(names, ", "))
}

func metricValue(c *benCandidate, name string) (float64, bool) {
	n, ok := c.Metrics[name]
	if !ok {
		return 0, false
	}
	v, err := n.Float64()
	if err != nil {
		return 0, false
	}
	return v, true
}

func emit(stdout, stderr io.Writer, resp response) int {
	enc := json.NewEncoder(stdout)
	if err := enc.Encode(resp); err != nil {
		errf(stderr, "gate-ben: encode response: %v\n", err)
		return 1
	}
	return 0
}

// errf writes a diagnostic line, deliberately ignoring the write error:
// stderr diagnostics must never mask the real failure being reported.
func errf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}
