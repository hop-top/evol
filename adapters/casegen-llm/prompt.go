package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// buildSystemPrompt pins the task and a strict output contract: a JSON
// array only, so small local models parse reliably.
func buildSystemPrompt(count int) string {
	return fmt.Sprintf(`You design evaluation cases for an agent skill.

Generate up to %d NEW eval cases. Each case is a realistic task input a
user might give, plus (when objectively determinable) the expected
behavior of a correct answer.

Hard rules:
- Ground every case in the provided knowledge passages: each case must
  exercise at least one stated rule or fact from them. Do not invent
  cases from the skill text alone.
- Cases must differ meaningfully from the provided examples and from
  each other.
- Reply with ONLY a JSON array, no prose, no code fences. Each element:
  {"input": "...", "expected": "...", "rationale": "which knowledge rule this exercises"}
- "expected" may be omitted when no single correct answer exists.`, count)
}

func buildUserPrompt(req Request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Skill under evaluation (%s):\n%s\n", req.Artifact.Ref, req.Artifact.Body)

	b.WriteString("\nKnowledge passages (ground truth to exercise):\n")
	for i, p := range req.Knowledge {
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, p.Source, p.Text)
	}

	if len(req.Examples) > 0 {
		b.WriteString("\nExisting cases (style reference — do NOT duplicate):\n")
		for _, e := range req.Examples {
			fmt.Fprintf(&b, "- input: %s\n", e.Input)
			if e.Expected != "" {
				fmt.Fprintf(&b, "  expected: %s\n", e.Expected)
			}
		}
	}

	fmt.Fprintf(&b, "\nGenerate up to %d cases as a JSON array now.", req.Count)
	return b.String()
}

// parseCases extracts the JSON array from model output, tolerating
// surrounding chatter and one code-fence layer.
func parseCases(text string) ([]SynthCase, error) {
	trimmed := stripFence(strings.TrimSpace(text))
	start := strings.Index(trimmed, "[")
	end := strings.LastIndex(trimmed, "]")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON array found in output")
	}

	var raw []SynthCase
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &raw); err != nil {
		return nil, err
	}
	cases := make([]SynthCase, 0, len(raw))
	for _, c := range raw {
		if strings.TrimSpace(c.Input) == "" {
			continue // dropped: an input-less case is unusable
		}
		cases = append(cases, c)
	}
	return cases, nil
}

// stripFence removes one wrapping ``` fence layer (with or without a
// language tag), mirroring the mutation generator's tolerance.
func stripFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	rest := s[3:]
	if i := strings.Index(rest, "\n"); i >= 0 {
		rest = rest[i+1:]
	}
	if j := strings.LastIndex(rest, "```"); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimSpace(rest)
}
