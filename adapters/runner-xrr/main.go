// Command runner-xrr wraps any contract-conforming runner with xrr
// cassette record/replay, so repeated (candidate content, case input)
// pairs are served from disk instead of re-spending agent calls.
//
// Usage: runner-xrr <real-runner> [args...]
//
// The shim reads the reference runner contract's channels (stdin = case
// input, EVOL_CANDIDATE_REF, EVOL_PROVIDER) and keys the cassette on a
// synthetic identity: the real runner's basename, the sha256 of the
// candidate FILE CONTENT (not its temp path), and the provider URI.
// Mode comes from XRR_MODE / XRR_CASSETTE_DIR (xrr.SessionFromEnv).
package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	osexec "os/exec"
	"path/filepath"
	"time"

	xrr "hop.top/xrr"
	xrrexec "hop.top/xrr/adapters/exec"
)

// Exit codes beyond the runner contract's "non-zero = run failure":
// distinct values so operators can tell shim trouble from agent trouble.
const (
	exitConfig = 20 // shim misconfiguration or spawn/session failure
	exitMiss   = 21 // replay-mode cassette miss (record this pair first)
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// errf writes a diagnostic line; stderr write failures are unreportable.
func errf(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		errf(stderr, "runner-xrr: usage: runner-xrr <real-runner> [args...]\n")
		return exitConfig
	}

	input, err := io.ReadAll(stdin)
	if err != nil {
		errf(stderr, "runner-xrr: read stdin: %v\n", err)
		return exitConfig
	}

	candRef := os.Getenv("EVOL_CANDIDATE_REF")
	if candRef == "" {
		errf(stderr, "runner-xrr: EVOL_CANDIDATE_REF is required (runner contract)\n")
		return exitConfig
	}
	content, err := os.ReadFile(candRef) //nolint:gosec // G304: reading the operator-staged candidate is this shim's purpose
	if err != nil {
		errf(stderr, "runner-xrr: read candidate %q: %v\n", candRef, err)
		return exitConfig
	}
	candID := fmt.Sprintf("%x", sha256.Sum256(content))[:12]
	provider := os.Getenv("EVOL_PROVIDER")

	sess, err := xrr.SessionFromEnv()
	if err != nil {
		errf(stderr, "runner-xrr: %v\n", err)
		return exitConfig
	}
	if sess == nil {
		// XRR_MODE unset: faithful passthrough, no cassette involvement.
		return spawnStreaming(args, input, stdout, stderr)
	}

	// Synthetic fingerprint identity. The real argv carries absolute,
	// per-checkout paths and the staged candidate lives at a different
	// temp path every generation — neither may leak into the cassette
	// key. Candidate CONTENT is the identity.
	req := &xrrexec.Request{
		Argv: []string{
			filepath.Base(args[0]),
			"candidate:" + candID,
			"provider:" + provider,
		},
		Stdin: string(input),
	}

	resp, err := sess.Record(context.Background(), xrrexec.NewAdapter(), req,
		func() (xrr.Response, error) {
			return spawnCaptured(args, input, stderr)
		})
	if err != nil {
		if errors.Is(err, xrr.ErrCassetteMiss) {
			errf(stderr,
				"runner-xrr: cassette miss (candidate:%s provider:%s) — run record mode for this pair first\n",
				candID, provider)
			return exitMiss
		}
		// Live spawn failure in record mode, a replayed spawn-failure
		// recording, or session/save trouble.
		errf(stderr, "runner-xrr: %v\n", err)
		return exitConfig
	}

	switch r := resp.(type) {
	case *xrrexec.Response: // record mode: stderr already streamed live
		_, _ = io.WriteString(stdout, r.Stdout)
		return r.ExitCode
	case *xrr.RawResponse: // replay mode
		if s, ok := r.Payload["stdout"].(string); ok {
			_, _ = io.WriteString(stdout, s)
		}
		if s, ok := r.Payload["stderr"].(string); ok && s != "" {
			_, _ = io.WriteString(stderr, s)
		}
		switch c := r.Payload["exit_code"].(type) {
		case int:
			return c
		case float64:
			return int(c)
		}
		return 0
	default:
		errf(stderr, "runner-xrr: unexpected response type %T\n", resp)
		return exitConfig
	}
}

// spawnCaptured runs the real runner, capturing stdout for the cassette
// and teeing stderr live (diagnostics stay visible during record runs).
// A non-zero child exit is data (Response.ExitCode), not an error; only
// spawn-level failures return err.
func spawnCaptured(args []string, input []byte, liveStderr io.Writer) (xrr.Response, error) {
	var outBuf, errBuf bytes.Buffer
	cmd := osexec.Command(args[0], args[1:]...) //nolint:gosec // G204/G702: wrapping an operator-configured runner is this shim's purpose
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = &outBuf
	cmd.Stderr = io.MultiWriter(&errBuf, liveStderr)

	start := time.Now()
	runErr := cmd.Run()
	code := xrrexec.ExitCodeFromError(runErr)
	if code == -1 { // not a clean process exit: start failure etc.
		return nil, fmt.Errorf("spawn %q: %w", args[0], runErr)
	}
	return &xrrexec.Response{
		Stdout:     outBuf.String(),
		Stderr:     errBuf.String(),
		ExitCode:   code,
		DurationMs: time.Since(start).Milliseconds(),
	}, nil
}

// spawnStreaming runs the real runner with directly connected streams.
func spawnStreaming(args []string, input []byte, stdout, stderr io.Writer) int {
	cmd := osexec.Command(args[0], args[1:]...) //nolint:gosec // G204/G702: see spawnCaptured
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	code := xrrexec.ExitCodeFromError(err)
	if code == -1 {
		errf(stderr, "runner-xrr: spawn %q: %v\n", args[0], err)
		return exitConfig
	}
	return code
}
