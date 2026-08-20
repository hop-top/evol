// Command casegen-llm implements the OPTIONAL Generator port action
// `synth` (spec/port-generator.md): generate new eval cases grounded in
// knowledge passages, over provider-agnostic LLM clients
// (hop.top/kit go/ai/llm).
//
// One JSON request on stdin, one JSON response on stdout. Non-zero exit
// means adapter error; stderr carries diagnostics. Cases returned here
// are unreviewed by definition — callers quarantine them via the Corpus
// `add-cases` action.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Request is the synth action request envelope.
type Request struct {
	Evol      string    `json:"evol"`
	Port      string    `json:"port"`
	Action    string    `json:"action"`
	Artifact  Artifact  `json:"artifact"`
	Knowledge []Passage `json:"knowledge"`
	Examples  []Example `json:"examples"`
	Count     int       `json:"count"`
}

// Artifact mirrors ArtifactStore load output.
type Artifact struct {
	Ref         string `json:"ref"`
	Kind        string `json:"kind"`
	Frontmatter string `json:"frontmatter"`
	Body        string `json:"body"`
	Version     string `json:"version"`
}

// Passage is one grounding excerpt.
type Passage struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

// Example is one existing case, passed as a style reference.
type Example struct {
	Input    string `json:"input"`
	Expected string `json:"expected,omitempty"`
}

// SynthCase is one generated case. No ids, splits, or provenance — the
// caller assigns those.
type SynthCase struct {
	Input     string `json:"input"`
	Expected  string `json:"expected,omitempty"`
	Rationale string `json:"rationale,omitempty"`
}

// Response is the synth action response envelope.
type Response struct {
	Evol   string      `json:"evol"`
	Port   string      `json:"port"`
	Action string      `json:"action"`
	Cases  []SynthCase `json:"cases"`
}

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

func diag(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// run drives one synth invocation. Exit codes: 0 success (possibly
// fewer cases than requested; zero = dry synthesis), 1 adapter error.
func run(stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		diag(stderr, "casegen-llm: read stdin: %v\n", err)
		return 1
	}

	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		diag(stderr, "casegen-llm: malformed request JSON: %v\n", err)
		return 1
	}
	if req.Evol != "1" || req.Port != "generator" || req.Action != "synth" {
		diag(stderr, "casegen-llm: unsupported envelope evol=%q port=%q action=%q\n",
			req.Evol, req.Port, req.Action)
		return 1
	}
	if req.Count <= 0 {
		diag(stderr, "%s\n", "casegen-llm: count must be > 0")
		return 1
	}
	if len(req.Knowledge) == 0 {
		// Grounded synthesis is the point: cases invented from the
		// artifact text alone are circular evals (the artifact grading
		// its own homework). Refuse loudly rather than degrade quietly.
		diag(stderr, "%s\n", "casegen-llm: refusing to synthesize without knowledge passages (circular-eval guard)")
		return 1
	}

	uri, note := resolveProviderURI(getenv)
	if note != "" {
		diag(stderr, "%s\n", note)
	}
	client, err := newKitClient(uri)
	if err != nil {
		diag(stderr, "casegen-llm: provider %q: %v\n", sanitizeURI(uri), err)
		return 1
	}

	text, err := client.complete(buildSystemPrompt(req.Count), buildUserPrompt(req))
	if err != nil {
		if isAuthError(err) {
			diag(stderr, "casegen-llm: auth error from API: %v\n", err)
			return 1
		}
		diag(stderr, "casegen-llm: synthesis call failed, dry result: %v\n", err)
		return emitCases(stdout, stderr, nil)
	}

	cases, err := parseCases(text)
	if err != nil {
		diag(stderr, "casegen-llm: unparseable model output, dry result: %v\n", err)
		return emitCases(stdout, stderr, nil)
	}
	if len(cases) > req.Count {
		cases = cases[:req.Count]
	}
	return emitCases(stdout, stderr, cases)
}

func emitCases(stdout, stderr io.Writer, cases []SynthCase) int {
	if cases == nil {
		cases = []SynthCase{}
	}
	resp := Response{Evol: "1", Port: "generator", Action: "synth", Cases: cases}
	if err := json.NewEncoder(stdout).Encode(resp); err != nil {
		diag(stderr, "casegen-llm: encode response: %v\n", err)
		return 1
	}
	return 0
}
