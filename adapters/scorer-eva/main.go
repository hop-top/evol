// Command scorer-eva scores a candidate transcript by piping it through
// the eva CLI's standalone contract mode.
//
// Draft port: "scorer" is not yet part of spec/ (Tier-2, to be extracted
// from the working implementation). The request/response shapes here are
// the reference the extraction will start from.
//
// Wire protocol: one JSON request on stdin, one JSON response on stdout.
// Non-zero exit = adapter error (stderr carries diagnostics).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	contractVersion = "1"
	portName        = "scorer"
	actionScore     = "score"

	defaultTimeout = 60 * time.Second
	maxReasonLen   = 1000
)

type scoreRequest struct {
	Evol   string `json:"evol"`
	Port   string `json:"port"`
	Action string `json:"action"`
	Case   struct {
		ID             string `json:"id"`
		Input          string `json:"input"`
		ExpectedOutput string `json:"expected_output,omitempty"`
	} `json:"case"`
	Transcript struct {
		Output string `json:"output"`
	} `json:"transcript"`
}

type scoreResponse struct {
	Evol   string `json:"evol"`
	Port   string `json:"port"`
	Action string `json:"action"`
	Score  struct {
		Value  float64 `json:"value"`
		Reason string  `json:"reason"`
	} `json:"score"`
}

// evaReport mirrors eva's standalone contract-mode JSON report.
type evaReport struct {
	Contract   string         `json:"contract"`
	Passed     bool           `json:"passed"`
	DurationMs int64          `json:"duration_ms"`
	Evaluators []evaEvaluator `json:"evaluators"`
	Skipped    []string       `json:"skipped"`
}

type evaEvaluator struct {
	Name     string          `json:"name"`
	Mode     string          `json:"mode"`
	MinScore float64         `json:"min_score"`
	Score    json.RawMessage `json:"score"`
	Passed   bool            `json:"passed"`
	Reason   string          `json:"reason"`
}

// scoreValue extracts the numeric score, accepting either a bare number
// or an object carrying a "value" field.
func (e evaEvaluator) scoreValue() (float64, bool) {
	if len(e.Score) == 0 || string(e.Score) == "null" {
		return 0, false
	}
	var f float64
	if err := json.Unmarshal(e.Score, &f); err == nil {
		return f, true
	}
	var obj struct {
		Value float64 `json:"value"`
	}
	if err := json.Unmarshal(e.Score, &obj); err == nil {
		return obj.Value, true
	}
	return 0, false
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "scorer-eva: %v\n", err)
		os.Exit(1)
	}
}

func run(stdin interface{ Read([]byte) (int, error) }, stdout interface{ Write([]byte) (int, error) }) error {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(stdin); err != nil {
		return fmt.Errorf("read request: %w", err)
	}

	var req scoreRequest
	if err := json.Unmarshal(buf.Bytes(), &req); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if req.Evol != contractVersion {
		return fmt.Errorf("unsupported contract version %q (want %q)", req.Evol, contractVersion)
	}
	if req.Port != portName || req.Action != actionScore {
		return fmt.Errorf("unsupported port/action %q/%q (want %s/%s)", req.Port, req.Action, portName, actionScore)
	}

	contract := os.Getenv("EVOL_EVA_CONTRACT")
	if contract == "" {
		return errors.New("EVOL_EVA_CONTRACT is not set")
	}
	if _, err := os.Stat(contract); err != nil { //nolint:gosec // contract path is operator-supplied config by design
		return fmt.Errorf("contract file: %w", err)
	}

	report, err := invokeEva(contract, req.Transcript.Output)
	if err != nil {
		return err
	}

	value, reason := summarize(report)

	resp := scoreResponse{Evol: contractVersion, Port: portName, Action: actionScore}
	resp.Score.Value = value
	resp.Score.Reason = reason
	out, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	out = append(out, '\n')
	if _, err := stdout.Write(out); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}

// invokeEva runs eva standalone contract mode over the transcript output.
// Exit 0 = pass (report on stdout), exit 1 = evaluation failure (report on
// stderr), exit 2 = bad input (adapter error). The report is looked for on
// the exit-code-indicated stream first, then the other, since the split is
// an eva quirk rather than a contract.
func invokeEva(contract, transcript string) (*evaReport, error) {
	evaBin := os.Getenv("EVOL_EVA_BIN")
	if evaBin == "" {
		evaBin = "eva"
	}

	timeout := defaultTimeout
	if t := os.Getenv("EVOL_EVA_TIMEOUT"); t != "" {
		d, err := time.ParseDuration(t)
		if err != nil {
			return nil, fmt.Errorf("EVOL_EVA_TIMEOUT: %w", err)
		}
		timeout = d
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	//nolint:gosec // executing the configured eva binary is this adapter's purpose
	cmd := exec.CommandContext(ctx, evaBin, "run", "--contract", contract, "--input", "-", "--format", "json")
	cmd.Stdin = strings.NewReader(transcript)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("eva timed out after %s", timeout)
	}

	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("run eva: %w", err)
		}
	}

	switch exitCode {
	case 0:
		return parseReport(stdout.Bytes(), stderr.Bytes())
	case 1:
		return parseReport(stderr.Bytes(), stdout.Bytes())
	default:
		return nil, fmt.Errorf("eva exited %d: %s", exitCode, strings.TrimSpace(stderr.String()))
	}
}

func parseReport(primary, secondary []byte) (*evaReport, error) {
	var report evaReport
	if err := json.Unmarshal(bytes.TrimSpace(primary), &report); err == nil {
		return &report, nil
	}
	if err := json.Unmarshal(bytes.TrimSpace(secondary), &report); err == nil {
		return &report, nil
	}
	return nil, errors.New("eva produced no parseable JSON report on either stream")
}

// summarize folds an eva report into one score: mean of evaluator scores,
// falling back to passed→1.0/0.0 when no evaluator carries a score.
// Reasons are joined failing-first.
func summarize(report *evaReport) (float64, string) {
	var sum float64
	var counted int
	entries := make([]evaEvaluator, len(report.Evaluators))
	copy(entries, report.Evaluators)
	sort.SliceStable(entries, func(i, j int) bool {
		return !entries[i].Passed && entries[j].Passed
	})

	var reasons []string
	for _, ev := range entries {
		if v, ok := ev.scoreValue(); ok {
			sum += v
			counted++
		}
		status := "passed"
		if !ev.Passed {
			status = "failed"
		}
		reason := ev.Reason
		if reason == "" {
			reason = status
		}
		reasons = append(reasons, fmt.Sprintf("%s: %s", ev.Name, reason))
	}

	var value float64
	switch {
	case counted > 0:
		value = sum / float64(counted)
	case report.Passed:
		value = 1.0
	default:
		value = 0.0
	}
	if value < 0 {
		value = 0
	}
	if value > 1 {
		value = 1
	}

	reason := strings.Join(reasons, "; ")
	if len(report.Skipped) > 0 {
		reason = strings.TrimSuffix(reason+"; skipped: "+strings.Join(report.Skipped, ", "), "; ")
	}
	if reason == "" {
		if report.Passed {
			reason = "contract passed"
		} else {
			reason = "contract failed"
		}
	}
	if len(reason) > maxReasonLen {
		reason = reason[:maxReasonLen]
	}
	return value, reason
}
