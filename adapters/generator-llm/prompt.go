package main

import (
	"fmt"
	"strings"
)

// strategy is one mutation approach. Names are recorded verbatim into
// tabu entries by the engine, so keep them stable across versions.
type strategy struct {
	name        string
	instruction string
}

var strategies = []strategy{
	{
		name: "tighten",
		instruction: "Remove redundancy. Cut repeated ideas, filler " +
			"phrases, and sections that restate other sections. Keep " +
			"every distinct behavior and constraint.",
	},
	{
		name: "restructure",
		instruction: "Reorder sections so they follow the reader's task " +
			"flow: when to use, then how, then edge cases. Do not add " +
			"or remove content beyond what reordering requires.",
	},
	{
		name: "add-example",
		instruction: "Add one concrete worked example that the scoring " +
			"feedback suggests is missing. Keep it minimal and " +
			"realistic; place it where a reader would look for it.",
	},
	{
		name: "sharpen-triggers",
		instruction: "Clarify when this artifact applies and when it " +
			"does not. Make trigger conditions explicit and testable; " +
			"remove vague qualifiers.",
	},
}

const (
	markFrontmatter = "===FRONTMATTER==="
	markBody        = "===BODY==="
	markRationale   = "===RATIONALE==="
)

// buildSystemPrompt explains the strategy and the hard constraints,
// and pins the exact output framing parseCandidate expects.
func buildSystemPrompt(strat strategy, artifact Artifact) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You revise a %s artifact used by AI agents. ", artifact.Kind)
	b.WriteString("Apply exactly one mutation strategy to produce one revised candidate.\n\n")
	fmt.Fprintf(&b, "Strategy %q: %s\n\n", strat.name, strat.instruction)
	b.WriteString("Hard constraints:\n")
	b.WriteString("- Preserve every field present in the frontmatter; you may improve field values, never drop fields.\n")
	fmt.Fprintf(&b, "- Keep total candidate size within +20%% of the baseline (%d characters).\n",
		len(artifact.Frontmatter)+len(artifact.Body))
	b.WriteString("- Return the COMPLETE revised artifact, not a diff.\n\n")
	b.WriteString("Output format — exactly these three fenced sections, nothing before or after:\n")
	fmt.Fprintf(&b, "%s\n<revised frontmatter>\n%s\n<revised body>\n%s\n<one-paragraph rationale: why this revision should score better>\n",
		markFrontmatter, markBody, markRationale)
	b.WriteString("\nCompact example of a valid reply:\n")
	fmt.Fprintf(&b, "%s\nname: example\ndescription: does a thing\n%s\n# Example\n\nBody text here.\n%s\nTighter wording should raise clarity scores.\n",
		markFrontmatter, markBody, markRationale)
	return b.String()
}

// buildUserPrompt carries the artifact, scoring history, knowledge
// passages, and the tabu list.
func buildUserPrompt(req Request) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Artifact %s (kind %s, version %s).\n\n",
		req.Artifact.Ref, req.Artifact.Kind, req.Artifact.Version)
	fmt.Fprintf(&b, "Current frontmatter:\n%s\n\nCurrent body:\n%s\n",
		req.Artifact.Frontmatter, req.Artifact.Body)

	if len(req.Scores) > 0 {
		b.WriteString("\nRecent scores (higher is better):\n")
		for _, s := range req.Scores {
			fmt.Fprintf(&b, "- version %s: %.2f", s.Version, s.Score)
			if s.Feedback != "" {
				fmt.Fprintf(&b, " — %s", s.Feedback)
			}
			b.WriteString("\n")
		}
	}

	if len(req.Knowledge) > 0 {
		b.WriteString("\nRelevant knowledge:\n")
		for _, p := range req.Knowledge {
			fmt.Fprintf(&b, "- [%s] %s\n", p.Source, p.Text)
		}
	}

	if len(req.Tabu) > 0 {
		b.WriteString("\nTabu list — these approaches already lost; do NOT re-propose them or close variants:\n")
		for _, t := range req.Tabu {
			fmt.Fprintf(&b, "- strategy %q: %s (%s)\n", t.Strategy, t.Rationale, t.Verdict)
		}
	}

	return b.String()
}

// parseCandidate extracts frontmatter, body, and rationale from the
// fenced structure requested in the system prompt. It tolerates
// leading/trailing chatter around the fenced block but requires the
// three markers in order.
func parseCandidate(text string) (Candidate, error) {
	text = stripFence(text)
	fmIdx := strings.Index(text, markFrontmatter)
	bodyIdx := strings.Index(text, markBody)
	ratIdx := strings.Index(text, markRationale)
	if fmIdx < 0 || bodyIdx < 0 || ratIdx < 0 {
		return Candidate{}, fmt.Errorf("missing markers (frontmatter %d, body %d, rationale %d)",
			fmIdx, bodyIdx, ratIdx)
	}
	if fmIdx >= bodyIdx || bodyIdx >= ratIdx {
		return Candidate{}, fmt.Errorf("markers out of order (frontmatter %d, body %d, rationale %d)",
			fmIdx, bodyIdx, ratIdx)
	}

	fm := strings.TrimSpace(text[fmIdx+len(markFrontmatter) : bodyIdx])
	body := strings.TrimSpace(text[bodyIdx+len(markBody) : ratIdx])
	rat := strings.TrimSpace(text[ratIdx+len(markRationale):])

	rat = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(rat), "```"))

	if fm == "" || body == "" {
		return Candidate{}, fmt.Errorf("empty section (frontmatter %d chars, body %d chars)",
			len(fm), len(body))
	}

	return Candidate{Frontmatter: fm, Body: body, Rationale: rat}, nil
}

// stripFence removes ONE wrapping code-fence layer (``` or ```lang) when
// the whole reply is fenced — a common small-model habit. Markers stay
// required; this only unwraps, never loosens.
func stripFence(text string) string {
	t := strings.TrimSpace(text)
	if !strings.HasPrefix(t, "```") {
		return text
	}
	nl := strings.IndexByte(t, '\n')
	if nl < 0 {
		return text
	}
	rest := t[nl+1:]
	if end := strings.LastIndex(rest, "```"); end >= 0 {
		rest = rest[:end]
	}
	return rest
}
