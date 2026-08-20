package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// loadConfig must preserve the case of ports.<name>.env keys: they are
// environment variable names, and a loader that lowercases map keys
// (viper does) silently breaks every adapter that reads them.
func TestLoadConfigPreservesEnvKeyCase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "evol.yaml")
	yaml := `artifact: skills/x
ports:
  corpus:
    cmd: [corpus-fs]
    env:
      EVOL_CORPUS_ROOT: .evol/corpus
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	got, ok := cfg.Ports.Corpus.Env["EVOL_CORPUS_ROOT"]
	if !ok {
		t.Fatalf("env key EVOL_CORPUS_ROOT missing; keys = %v (case mangled?)", cfg.Ports.Corpus.Env)
	}
	if got != ".evol/corpus" {
		t.Errorf("EVOL_CORPUS_ROOT = %q, want %q", got, ".evol/corpus")
	}
}
