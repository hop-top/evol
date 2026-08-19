// Command generator-llm implements the evol Generator port with LLM
// mutation strategies over the Anthropic Messages API.
//
// One JSON request on stdin, one JSON response on stdout. Non-zero exit
// means adapter error; stderr carries diagnostics. See
// spec/port-generator.md for the contract.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// Request is the propose action request envelope.
type Request struct {
	Evol      string      `json:"evol"`
	Port      string      `json:"port"`
	Action    string      `json:"action"`
	Artifact  Artifact    `json:"artifact"`
	Scores    []Score     `json:"scores"`
	Tabu      []TabuEntry `json:"tabu"`
	Knowledge []Passage   `json:"knowledge,omitempty"`
	Budget    Budget      `json:"budget"`
}

// Artifact mirrors ArtifactStore load output.
type Artifact struct {
	Ref         string `json:"ref"`
	Kind        string `json:"kind"`
	Frontmatter string `json:"frontmatter"`
	Body        string `json:"body"`
	Version     string `json:"version"`
}

// Score is one prior scoring summary.
type Score struct {
	Version  string  `json:"version"`
	Score    float64 `json:"score"`
	Feedback string  `json:"feedback,omitempty"`
}

// TabuEntry is a past reject the generator must not re-propose.
type TabuEntry struct {
	Strategy  string `json:"strategy"`
	Rationale string `json:"rationale"`
	Verdict   string `json:"verdict"`
}

// Passage is optional knowledge context.
type Passage struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

// Budget bounds the proposal count.
type Budget struct {
	MaxCandidates int `json:"max_candidates"`
}

// Response is the propose action response envelope.
type Response struct {
	Evol       string      `json:"evol"`
	Port       string      `json:"port"`
	Action     string      `json:"action"`
	Candidates []Candidate `json:"candidates"`
}

// Candidate is one proposed revision (complete artifact, not a diff).
type Candidate struct {
	ID          string `json:"id"`
	Frontmatter string `json:"frontmatter"`
	Body        string `json:"body"`
	Rationale   string `json:"rationale"`
	Strategy    string `json:"strategy"`
}

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr, envConfig()))
}

// diag writes a best-effort diagnostic line; a failed stderr write is
// deliberately ignored (nothing actionable remains at that point).
func diag(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// run drives one propose invocation. Exit codes: 0 success (including
// zero candidates = dry generation), 1 adapter error (bad request,
// missing configuration, auth failure).
func run(stdin io.Reader, stdout, stderr io.Writer, cfg config) int {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		diag(stderr, "generator-llm: read stdin: %v\n", err)
		return 1
	}

	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		diag(stderr, "generator-llm: malformed request JSON: %v\n", err)
		return 1
	}
	if req.Evol != "1" || req.Port != "generator" || req.Action != "propose" {
		diag(stderr, "generator-llm: unsupported envelope evol=%q port=%q action=%q\n",
			req.Evol, req.Port, req.Action)
		return 1
	}
	if cfg.apiKey == "" {
		diag(stderr, "%s\n", "generator-llm: ANTHROPIC_API_KEY is not set")
		return 1
	}
	if req.Budget.MaxCandidates <= 0 {
		diag(stderr, "%s\n", "generator-llm: budget.max_candidates must be > 0")
		return 1
	}

	client := newAnthropicClient(cfg)
	candidates := make([]Candidate, 0, req.Budget.MaxCandidates)
	for i := 0; i < req.Budget.MaxCandidates; i++ {
		strat := strategies[i%len(strategies)]
		text, err := client.complete(buildSystemPrompt(strat, req.Artifact),
			buildUserPrompt(req))
		if err != nil {
			if isAuthError(err) {
				diag(stderr, "generator-llm: auth error from API: %v\n", err)
				return 1
			}
			diag(stderr, "generator-llm: candidate %d (%s) dropped: %v\n",
				i+1, strat.name, err)
			continue
		}
		cand, err := parseCandidate(text)
		if err != nil {
			diag(stderr, "generator-llm: candidate %d (%s) unparseable, dropped: %v\n",
				i+1, strat.name, err)
			continue
		}
		cand.ID = fmt.Sprintf("cand-%02d", len(candidates)+1)
		cand.Strategy = strat.name
		candidates = append(candidates, cand)
	}

	resp := Response{Evol: "1", Port: "generator", Action: "propose", Candidates: candidates}
	enc := json.NewEncoder(stdout)
	if err := enc.Encode(resp); err != nil {
		diag(stderr, "generator-llm: encode response: %v\n", err)
		return 1
	}
	return 0
}
