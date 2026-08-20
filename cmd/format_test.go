package cmd

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestFormatUnknownRefused: an unknown --format must refuse loudly on
// every verb — exit code 3, message naming the accepted values — before
// any config or port work happens (hence the nonexistent config path).
func TestFormatUnknownRefused(t *testing.T) {
	verbs := [][]string{
		{"targets"},
		{"run"},
		{"runs", "list"},
		{"runs", "show", "r1"},
		{"rollback", "--artifact", "a/SKILL.md"},
		{"cases", "list"},
		{"cases", "synth"},
		{"cases", "promote"},
		{"cases", "correct"},
		{"routing", "emit"},
	}
	for _, verb := range verbs {
		t.Run(strings.Join(verb, "_"), func(t *testing.T) {
			args := append(append([]string{}, verb...),
				"--config", "nonexistent.yaml", "--format", "xml")
			out, errOut, err := execRoot(t, args...)
			if err == nil {
				t.Fatalf("want refusal for --format xml; stdout:\n%s", out)
			}
			if ExitCode(err) != exitConfigError {
				t.Errorf("exit = %d, want %d", ExitCode(err), exitConfigError)
			}
			if !strings.Contains(errOut, `unknown output format "xml"`) {
				t.Errorf("stderr missing refusal:\n%s", errOut)
			}
			if !strings.Contains(errOut, "text, json, yaml") {
				t.Errorf("stderr missing accepted values:\n%s", errOut)
			}
			if strings.Contains(out, "REF") {
				t.Errorf("human table leaked to stdout despite refusal:\n%s", out)
			}
		})
	}
}

// TestFormatYAMLTargets: --format yaml must render real YAML carrying
// the same key names as the JSON contract, not the human table.
func TestFormatYAMLTargets(t *testing.T) {
	t.Setenv("EVOL_TEST_DIR", t.TempDir())
	t.Setenv("EVOL_FAKE_HISTORY", "mixed")

	out, _, err := execRoot(t,
		"targets", "--config", writeFakeConfig(t), "--format", "yaml")
	if err != nil {
		t.Fatalf("targets: %v", err)
	}
	if strings.Contains(out, "REF") {
		t.Fatalf("--format yaml rendered the human table:\n%s", out)
	}
	var rows []map[string]any
	if err := yaml.Unmarshal([]byte(out), &rows); err != nil {
		t.Fatalf("targets output is not YAML: %v\n%s", err, out)
	}
	if len(rows) != 2 || rows[0]["ref"] != "skills/fake" {
		t.Fatalf("rows = %+v", rows)
	}
	// Keys must match the JSON contract (json tags), not smashed
	// lowercase Go field names.
	if _, ok := rows[0]["last_best_score"]; !ok {
		t.Errorf("row missing json-contract key last_best_score: %+v", rows[0])
	}
	if _, ok := rows[0]["lastbest"]; ok {
		t.Errorf("row carries smashed field name lastbest: %+v", rows[0])
	}
}

// TestFormatYAMLRunDryRun: yaml flows through emit for verbs that call
// it unconditionally too.
func TestFormatYAMLRunDryRun(t *testing.T) {
	out, _, err := execRoot(t,
		"run", "--config", writeConfig(t), "--dry-run", "--format", "yaml")
	if err != nil {
		t.Fatalf("run --dry-run: %v", err)
	}
	var plan map[string]any
	if err := yaml.Unmarshal([]byte(out), &plan); err != nil {
		t.Fatalf("dry-run output is not YAML: %v\n%s", err, out)
	}
	if plan["dry_run"] != true || plan["artifact"] != "skills/fake" {
		t.Fatalf("plan = %+v", plan)
	}
}
