package engine

// Wire types shared with the port contracts (spec/, evol: "1").
// Field names are snake_case on the wire.

// Artifact is the document under evolution, as served by the
// ArtifactStore port.
type Artifact struct {
	Ref         string `json:"ref"`
	Kind        string `json:"kind"`
	Frontmatter string `json:"frontmatter"`
	Body        string `json:"body"`
	Version     string `json:"version"`
}

// Case is one eval case from the Corpus port.
type Case struct {
	ID       string `json:"id"`
	Input    string `json:"input"`
	Expected string `json:"expected"`
	Split    string `json:"split"`
	Source   string `json:"source"`
}

// ScoreSummary is a scoring history entry passed to the Generator.
type ScoreSummary struct {
	Version  string  `json:"version"`
	Score    float64 `json:"score"`
	Feedback string  `json:"feedback,omitempty"`
}

// TabuEntry is a distilled past reject from the Corpus port.
type TabuEntry struct {
	Strategy  string `json:"strategy"`
	Rationale string `json:"rationale"`
	Verdict   string `json:"verdict"`
}

// Passage is a knowledge excerpt from the KnowledgeBase port.
type Passage struct {
	Text   string  `json:"text"`
	Source string  `json:"source"`
	Score  float64 `json:"score,omitempty"`
}

// Candidate is one proposed revision from the Generator port.
type Candidate struct {
	ID          string `json:"id"`
	Frontmatter string `json:"frontmatter"`
	Body        string `json:"body"`
	Rationale   string `json:"rationale"`
	Strategy    string `json:"strategy"`
}

// ToolCall is one observed tool invocation inside a run.
type ToolCall struct {
	Tool   string `json:"tool"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

// Transcript is the Executor port's account of one run.
type Transcript struct {
	Output     string     `json:"output"`
	ToolCalls  []ToolCall `json:"tool_calls"`
	DurationMS int64      `json:"duration_ms"`
	ExitCode   *int       `json:"exit_code,omitempty"`
}

// CaseScore is one scored case, recorded to the Corpus.
type CaseScore struct {
	CaseID string  `json:"case_id,omitempty"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason,omitempty"`
}

// Verdict values recorded per candidate. VerdictEvidence marks
// provider-sweep rows: scored under a non-primary provider (or the
// baseline under any provider), recorded for routing evidence and never
// gated on.
const (
	VerdictAccepted = "accepted"
	VerdictRejected = "rejected"
	VerdictFailed   = "failed"
	VerdictEvidence = "evidence"
)

// Fixtures pins the recorded environment behind a promoted run so it
// can serve as a regression fixture (optional; see spec/port-corpus.md).
type Fixtures struct {
	CassetteDir string `json:"cassette_dir"`
}

// CandidateOutcome is one candidate's evaluated result within a
// generation, shaped for Corpus record.
type CandidateOutcome struct {
	ID     string      `json:"id"`
	Scores []CaseScore `json:"scores"`
	// Strategy is the generator strategy that produced the candidate;
	// recorded so tabu entries keep their strategy dimension.
	Strategy  string `json:"strategy,omitempty"`
	Verdict   string `json:"verdict"`
	Rationale string `json:"rationale"`
	// Provider is the executor provider URI these scores were produced
	// under; set whenever one is configured (sweep rows always carry it).
	Provider string    `json:"provider,omitempty"`
	Fixtures *Fixtures `json:"fixtures,omitempty"`
	// RecordedAt is stamped by the engine at record time (RFC3339, UTC).
	// Selection reads it back as the artifact's last-evolution clock;
	// nothing fingerprints or replays it, so determinism is unaffected.
	RecordedAt string `json:"recorded_at,omitempty"`
}

// Result is the outcome of one engine run.
type Result struct {
	ArtifactRef     string  `json:"artifact_ref"`
	BaselineVersion string  `json:"baseline_version"`
	BaselineScore   float64 `json:"baseline_score"`
	Accepted        bool    `json:"accepted"`
	AcceptedID      string  `json:"accepted_id,omitempty"`
	NewVersion      string  `json:"new_version,omitempty"`
	// GitCommit is the promotion commit SHA when the artifact store runs
	// git-native versioning; empty otherwise.
	GitCommit       string  `json:"git_commit,omitempty"`
	BestScore       float64 `json:"best_score"`
	Generations     int     `json:"generations"`
	CandidatesTried int     `json:"candidates_tried"`
	// SigP is the accepted candidate's paired-bootstrap p-value, when
	// significance testing ran (nil when disabled by the pair floor).
	SigP *float64 `json:"sig_p,omitempty"`
}
