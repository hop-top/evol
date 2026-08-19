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

// Verdict values recorded per candidate.
const (
	VerdictAccepted = "accepted"
	VerdictRejected = "rejected"
	VerdictFailed   = "failed"
)

// CandidateOutcome is one candidate's evaluated result within a
// generation, shaped for Corpus record.
type CandidateOutcome struct {
	ID        string      `json:"id"`
	Scores    []CaseScore `json:"scores"`
	Verdict   string      `json:"verdict"`
	Rationale string      `json:"rationale"`
}

// Result is the outcome of one engine run.
type Result struct {
	ArtifactRef     string  `json:"artifact_ref"`
	BaselineVersion string  `json:"baseline_version"`
	BaselineScore   float64 `json:"baseline_score"`
	Accepted        bool    `json:"accepted"`
	AcceptedID      string  `json:"accepted_id,omitempty"`
	NewVersion      string  `json:"new_version,omitempty"`
	BestScore       float64 `json:"best_score"`
	Generations     int     `json:"generations"`
	CandidatesTried int     `json:"candidates_tried"`
}
