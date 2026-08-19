// Command ctxt-kb adapts the evol KnowledgeBase port onto the ctxt CLI.
//
// One JSON request on stdin, one JSON response on stdout. The knowledge
// daemon being down is data, not an error: any action may answer
// {"unavailable": true} with exit 0. Non-zero exit is reserved for
// adapter faults (malformed request, unknown action).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	contractVersion = "1"
	portName        = "knowledgebase"

	defaultLimit          = 5
	maxRequestBytes       = 10 << 20
	maxPassageRunes       = 700
	defaultCallTimeoutMS  = 15000
	defaultProbeTimeoutMS = 5000
)

type request struct {
	Evol   string   `json:"evol"`
	Port   string   `json:"port"`
	Action string   `json:"action"`
	Query  string   `json:"query,omitempty"`
	Limit  int      `json:"limit,omitempty"`
	Topic  string   `json:"topic,omitempty"`
	Text   string   `json:"text,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

type passage struct {
	Text   string  `json:"text"`
	Source string  `json:"source"`
	Score  float64 `json:"score"`
}

type response struct {
	Evol        string    `json:"evol"`
	Port        string    `json:"port"`
	Action      string    `json:"action"`
	Unavailable bool      `json:"unavailable,omitempty"`
	Passages    []passage `json:"passages,omitempty"`
	Text        string    `json:"text,omitempty"`
}

// findResult mirrors the subset of `ctxt find --format json` output the
// adapter consumes. Fields beyond these are ignored.
type findResult struct {
	Objects []struct {
		ID          string `json:"id"`
		TextContent string `json:"text_content"`
		RawContent  string `json:"raw_content"`
		Metadata    struct {
			RRFScore float64 `json:"rrf_score"`
		} `json:"metadata"`
	} `json:"objects"`
}

type adapter struct {
	bin          string
	callTimeout  time.Duration
	probeTimeout time.Duration
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "ctxt-kb: %v\n", err)
		os.Exit(1)
	}
}

func run(in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(in, maxRequestBytes))
	if err != nil {
		return fmt.Errorf("read request: %w", err)
	}

	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return fmt.Errorf("malformed request: %w", err)
	}
	if req.Evol != contractVersion {
		return fmt.Errorf("unsupported contract version %q", req.Evol)
	}
	if req.Port != portName {
		return fmt.Errorf("wrong port %q, this adapter serves %q", req.Port, portName)
	}

	a := adapter{
		bin:          envOr("EVOL_CTXT_BIN", "ctxt"),
		callTimeout:  msEnv("EVOL_CTXT_TIMEOUT_MS", defaultCallTimeoutMS),
		probeTimeout: msEnv("EVOL_CTXT_PROBE_TIMEOUT_MS", defaultProbeTimeoutMS),
	}

	resp := response{Evol: contractVersion, Port: portName, Action: req.Action}

	switch req.Action {
	case "search", "brief", "append":
	default:
		return fmt.Errorf("unknown action %q", req.Action)
	}

	if req.Action == "search" && req.Query == "" {
		return fmt.Errorf("search: query is required")
	}
	if req.Action == "brief" && req.Topic == "" {
		return fmt.Errorf("brief: topic is required")
	}
	if req.Action == "append" && req.Text == "" {
		return fmt.Errorf("append: text is required")
	}

	if !a.available() {
		resp.Unavailable = true
		return emit(out, resp)
	}

	switch req.Action {
	case "search":
		passages, ok := a.search(req.Query, req.Limit)
		if !ok {
			resp.Unavailable = true
			return emit(out, resp)
		}
		resp.Passages = passages
	case "brief":
		text, ok := a.brief(req.Topic)
		if !ok {
			resp.Unavailable = true
			return emit(out, resp)
		}
		resp.Text = text
	case "append":
		if !a.append(req.Text, req.Tags) {
			resp.Unavailable = true
			return emit(out, resp)
		}
	}

	return emit(out, resp)
}

// available probes daemon health. Exit 0 covers healthy and degraded
// (still serving); anything else — unreachable daemon, missing binary,
// timeout — reads as unavailable.
func (a adapter) available() bool {
	_, err := a.exec(a.probeTimeout, "status", "--format", "json")
	return err == nil
}

func (a adapter) search(query string, limit int) ([]passage, bool) {
	if limit <= 0 {
		limit = defaultLimit
	}
	stdout, err := a.exec(a.callTimeout,
		"find", query, "--format", "json", "--limit", strconv.Itoa(limit), "--no-hints", "--quiet")
	if err != nil {
		diag("search: %v", err)
		return nil, false
	}

	var res findResult
	if err := json.Unmarshal(stdout, &res); err != nil {
		// Unexpected output shape from a live daemon: degrade rather
		// than fault — the engine treats it like a missing knowledge
		// base and proceeds.
		diag("search: unparseable find output: %v", err)
		return nil, false
	}

	passages := make([]passage, 0, len(res.Objects))
	for _, obj := range res.Objects {
		text := obj.TextContent
		if text == "" {
			text = obj.RawContent
		}
		if text == "" {
			continue
		}
		passages = append(passages, passage{
			Text:   truncate(text, maxPassageRunes),
			Source: obj.ID,
			Score:  obj.Metadata.RRFScore,
		})
	}
	return passages, true
}

// brief prefers a composed brief; when composition fails (e.g. the
// topic maps to no tag) it degrades to a joined view of top search
// passages so the engine still gets grounding text.
func (a adapter) brief(topic string) (string, bool) {
	stdout, err := a.exec(a.callTimeout,
		"compose", "brief", "--tagged", topic, "--quiet", "--no-hints")
	if err == nil && len(bytes.TrimSpace(stdout)) > 0 {
		return string(bytes.TrimSpace(stdout)), true
	}
	diag("brief: compose failed (%v), falling back to search join", err)

	passages, ok := a.search(topic, defaultLimit)
	if !ok {
		return "", false
	}
	var b strings.Builder
	for _, p := range passages {
		fmt.Fprintf(&b, "- %s (%s)\n", p.Text, p.Source)
	}
	return strings.TrimSpace(b.String()), true
}

func (a adapter) append(text string, tags []string) bool {
	args := []string{"analyze", text, "--quiet", "--no-hints"}
	if len(tags) > 0 {
		args = append(args, "--hint", strings.Join(tags, ","))
	}
	if _, err := a.exec(a.callTimeout, args...); err != nil {
		diag("append: %v (note dropped)", err)
		return false
	}
	return true
}

func (a adapter) exec(timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, a.bin, args...) //nolint:gosec // bin comes from EVOL_CTXT_BIN; shelling to ctxt is this adapter's purpose
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("%s %s: timeout after %s", a.bin, args[0], timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w (stderr: %s)", a.bin, args[0], err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func emit(out io.Writer, resp response) error {
	enc := json.NewEncoder(out)
	return enc.Encode(resp)
}

func truncate(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxRunes]) + "…"
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func msEnv(key string, fallback int) time.Duration {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return time.Duration(fallback) * time.Millisecond
}

func diag(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ctxt-kb: "+format+"\n", args...)
}
