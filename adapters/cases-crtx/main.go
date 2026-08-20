// Command cases-crtx mines eval cases from crtx conversation envelopes.
//
// One normalized format in (crtx v0.1 envelopes, JSONL or single-object
// files), cases.jsonl out: {id, input, expected_output?, split, provenance,
// source}. Replaces per-CLI session parsers with a single converter over
// the spec'd envelope shape.
//
// Parsing is spec-direct with the standard library; swapping to the stem
// SDK once it is publicly importable is a TODO (see README).
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// ---- crtx v0.1 envelope shapes (spec-crtx specs/v0.1/envelope.md) ----

type envelope struct {
	CrtxVersion string `json:"crtx_version"`
	ID          string `json:"id"`
	Turns       []turn `json:"turns"`
}

type turn struct {
	ID      string        `json:"id"`
	Role    string        `json:"role"`
	Content []contentPart `json:"content"`
}

type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// knownRoles per spec §4; unknown roles reject the whole envelope.
var knownRoles = map[string]bool{
	"user": true, "assistant": true, "tool": true,
	"system": true, "developer": true,
}

// ---- output shape ----

type caseLine struct {
	ID             string `json:"id"`
	Input          string `json:"input"`
	ExpectedOutput string `json:"expected_output,omitempty"`
	Split          string `json:"split"`
	Provenance     string `json:"provenance"`
	Source         string `json:"source"`
}

// ---- secret scrubbing (applied to input and expected_output) ----

type scrubRule struct {
	name string
	re   *regexp.Regexp
}

var scrubRules = []scrubRule{
	{"pem", regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*(?:PRIVATE |SECRET )?KEY-----.*?-----END [A-Z0-9 ]*(?:PRIVATE |SECRET )?KEY-----`)},
	{"aws-key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"github-token", regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}\b`)},
	{"github-pat", regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{22,}\b`)},
	{"jwt", regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`)},
	{"openai-key", regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`)},
	{"slack-token", regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`)},
	{"bearer", regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]{16,}`)},
}

// diag writes a diagnostic line to stderr; diagnostics are best-effort.
func diag(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

func scrub(s string, counts map[string]int) string {
	for _, r := range scrubRules {
		s = r.re.ReplaceAllStringFunc(s, func(string) string {
			counts[r.name]++
			return "<redacted:" + r.name + ">"
		})
	}
	return s
}

// ---- mining ----

type options struct {
	grep     *regexp.Regexp
	limit    int
	expected bool
}

type stats struct {
	malformed   int
	rejected    int // envelopes rejected (version/role)
	redactions  map[string]int
	emitted     int
	dedupSkips  int
	grepSkips   int
	pairsFound  int
	filesParsed int
}

func newStats() *stats { return &stats{redactions: map[string]int{}} }

// pairTexts extracts (userText, assistantText) pairs from an envelope's
// turns in canonical array order: each user turn with text pairs with the
// next assistant turn carrying text.
func pairTexts(env envelope) [][2]string {
	var pairs [][2]string
	for i := 0; i < len(env.Turns); i++ {
		if env.Turns[i].Role != "user" {
			continue
		}
		userText := turnText(env.Turns[i])
		if userText == "" {
			continue
		}
		for j := i + 1; j < len(env.Turns); j++ {
			if env.Turns[j].Role == "user" {
				break // next exchange started; this user turn has no reply
			}
			if env.Turns[j].Role == "assistant" {
				if at := turnText(env.Turns[j]); at != "" {
					pairs = append(pairs, [2]string{userText, at})
					break
				}
			}
		}
	}
	return pairs
}

// turnText concatenates the text parts of a turn. thinking / tool parts
// are ignored: cases carry conversational content only.
func turnText(t turn) string {
	var parts []string
	for _, c := range t.Content {
		if c.Type == "text" && strings.TrimSpace(c.Text) != "" {
			parts = append(parts, strings.TrimSpace(c.Text))
		}
	}
	return strings.Join(parts, "\n\n")
}

func validEnvelope(env envelope, st *stats, stderr io.Writer) bool {
	if env.CrtxVersion != "0.1" {
		st.rejected++
		diag(stderr, "cases-crtx: rejected envelope %s: unsupported crtx_version %q\n", env.ID, env.CrtxVersion)
		return false
	}
	for _, t := range env.Turns {
		if !knownRoles[t.Role] {
			st.rejected++
			diag(stderr, "cases-crtx: rejected envelope %s: unknown role %q\n", env.ID, t.Role)
			return false
		}
	}
	return true
}

// parseInput accepts either one pretty-printed envelope object or JSONL
// (one envelope per line).
func parseInput(data []byte, st *stats, stderr io.Writer) []envelope {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil
	}
	// whole-input single object first
	var single envelope
	if err := json.Unmarshal(trimmed, &single); err == nil && single.ID != "" {
		return []envelope{single}
	}
	var envs []envelope
	sc := bufio.NewScanner(bytes.NewReader(trimmed))
	sc.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var env envelope
		if err := json.Unmarshal(line, &env); err != nil || env.ID == "" {
			st.malformed++
			continue
		}
		envs = append(envs, env)
	}
	if err := sc.Err(); err != nil {
		diag(stderr, "cases-crtx: scan error: %v\n", err)
	}
	return envs
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("cases-crtx", flag.ContinueOnError)
	fs.SetOutput(stderr)
	grepPat := fs.String("grep", "", "case-insensitive regex; only user turns matching are mined")
	limit := fs.Int("limit", 0, "max cases to emit (0 = all)")
	expected := fs.Bool("expected", true, "include the assistant reply as expected_output")
	noExpected := fs.Bool("no-expected", false, "omit expected_output (overrides --expected)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	opts := options{limit: *limit, expected: *expected && !*noExpected}
	if *grepPat != "" {
		re, err := regexp.Compile("(?i)" + *grepPat)
		if err != nil {
			diag(stderr, "cases-crtx: bad --grep regex: %v\n", err)
			return 2
		}
		opts.grep = re
	}

	st := newStats()
	var envs []envelope
	if fs.NArg() == 0 {
		data, err := io.ReadAll(stdin)
		if err != nil {
			diag(stderr, "cases-crtx: read stdin: %v\n", err)
			return 1
		}
		envs = parseInput(data, st, stderr)
	} else {
		for _, path := range fs.Args() {
			data, err := os.ReadFile(path) //nolint:gosec // operator-supplied input path is this tool's purpose
			if err != nil {
				diag(stderr, "cases-crtx: read %s: %v\n", path, err)
				return 1
			}
			st.filesParsed++
			envs = append(envs, parseInput(data, st, stderr)...)
		}
	}

	seen := map[string]bool{}
	out := bufio.NewWriter(stdout)
	defer out.Flush()
	enc := json.NewEncoder(out)

	for _, env := range envs {
		if !validEnvelope(env, st, stderr) {
			continue
		}
		for _, p := range pairTexts(env) {
			st.pairsFound++
			input, expectedOut := p[0], p[1]
			if opts.grep != nil && !opts.grep.MatchString(input) {
				st.grepSkips++
				continue
			}
			input = scrub(input, st.redactions)
			expectedOut = scrub(expectedOut, st.redactions)
			sum := sha256.Sum256([]byte(input))
			hash := hex.EncodeToString(sum[:])[:12]
			if seen[hash] {
				st.dedupSkips++
				continue
			}
			seen[hash] = true
			c := caseLine{
				ID:         "crtx-" + hash,
				Input:      input,
				Split:      "train",
				Provenance: "mined",
				Source:     "crtx:" + env.ID,
			}
			if opts.expected {
				c.ExpectedOutput = expectedOut
			}
			if err := enc.Encode(c); err != nil {
				diag(stderr, "cases-crtx: encode: %v\n", err)
				return 1
			}
			st.emitted++
			if opts.limit > 0 && st.emitted >= opts.limit {
				report(st, stderr)
				return 0
			}
		}
	}
	report(st, stderr)
	return 0
}

func report(st *stats, stderr io.Writer) {
	total := 0
	for _, n := range st.redactions {
		total += n
	}
	fmt.Fprintf(stderr,
		"cases-crtx: emitted %d case(s) from %d pair(s); dedup %d, grep-filtered %d, redactions %d, malformed lines %d, rejected envelopes %d\n",
		st.emitted, st.pairsFound, st.dedupSkips, st.grepSkips, total, st.malformed, st.rejected)
	if total > 0 {
		for _, r := range scrubRules {
			if n := st.redactions[r.name]; n > 0 {
				diag(stderr, "cases-crtx:   redacted %s ×%d\n", r.name, n)
			}
		}
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
