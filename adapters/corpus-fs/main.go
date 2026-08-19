// Command corpus-fs implements the Corpus port (spec/port-corpus.md)
// over a plain directory of JSONL files.
//
// Layout: $EVOL_CORPUS_ROOT/<sha256(artifact_ref)[:12]>/
//
//	ref.txt            — the artifact_ref, for humans
//	cases.jsonl        — eval cases (seeded externally)
//	generations.jsonl  — one line per recorded candidate
//	corrections.jsonl  — human corrections promoted to cases
//
// Wire protocol: one JSON request on stdin, one JSON response on stdout.
// Non-zero exit = adapter error (stderr carries diagnostics). Reads on
// artifacts never recorded return empty results, not errors.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	contractVersion = "1"
	portName        = "corpus"

	casesFile       = "cases.jsonl"
	generationsFile = "generations.jsonl"
	correctionsFile = "corrections.jsonl"
	refFile         = "ref.txt"

	maxLineBytes = 1 << 20 // 1 MiB per JSONL line
)

type envelope struct {
	Evol   string `json:"evol"`
	Port   string `json:"port"`
	Action string `json:"action"`
}

type caseEntry struct {
	ID       string `json:"id"`
	Input    string `json:"input"`
	Expected string `json:"expected"`
	Split    string `json:"split,omitempty"`
	Source   string `json:"source,omitempty"`
}

type scoreEntry struct {
	CaseID string  `json:"case_id,omitempty"`
	Score  float64 `json:"score"`
	Reason string  `json:"reason,omitempty"`
}

type generationInfo struct {
	ArtifactRef     string `json:"artifact_ref"`
	BaselineVersion string `json:"baseline_version,omitempty"`
	Number          int    `json:"number"`
}

type candidateRecord struct {
	ID        string       `json:"id"`
	Scores    []scoreEntry `json:"scores,omitempty"`
	Verdict   string       `json:"verdict"`
	Rationale string       `json:"rationale,omitempty"`
	Strategy  string       `json:"strategy,omitempty"`
	TS        string       `json:"ts,omitempty"`
	// Fixtures round-trips verbatim (optional {cassette_dir} on
	// promoted candidates; see spec/port-corpus.md).
	Fixtures json.RawMessage `json:"fixtures,omitempty"`
}

// generationLine is one appended line in generations.jsonl: the
// generation info embedded per candidate so lines stand alone.
type generationLine struct {
	Generation generationInfo `json:"generation"`
	candidateRecord
}

type tabuEntry struct {
	Strategy  string `json:"strategy"`
	Rationale string `json:"rationale"`
	Verdict   string `json:"verdict"`
}

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "corpus-fs: %v\n", err)
		os.Exit(1)
	}
}

func run(stdin interface{ Read([]byte) (int, error) }, stdout interface{ Write([]byte) (int, error) }) error {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(stdin); err != nil {
		return fmt.Errorf("read request: %w", err)
	}
	raw := buf.Bytes()

	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("decode request: %w", err)
	}
	if env.Evol != contractVersion {
		return fmt.Errorf("unsupported contract version %q (want %q)", env.Evol, contractVersion)
	}
	if env.Port != portName {
		return fmt.Errorf("unsupported port %q (want %q)", env.Port, portName)
	}

	root := os.Getenv("EVOL_CORPUS_ROOT")
	if root == "" {
		return errors.New("EVOL_CORPUS_ROOT is not set")
	}

	var (
		resp any
		err  error
	)
	switch env.Action {
	case "cases":
		resp, err = handleCases(root, raw)
	case "record":
		resp, err = handleRecord(root, raw)
	case "tabu":
		resp, err = handleTabu(root, raw)
	case "corrections":
		resp, err = handleCorrections(root, raw)
	default:
		return fmt.Errorf("unsupported action %q", env.Action)
	}
	if err != nil {
		return err
	}

	out, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	out = append(out, '\n')
	if _, werr := stdout.Write(out); werr != nil {
		return fmt.Errorf("write response: %w", werr)
	}
	return nil
}

func artifactDir(root, artifactRef string) (string, error) {
	if artifactRef == "" {
		return "", errors.New("artifact_ref is required")
	}
	sum := sha256.Sum256([]byte(artifactRef))
	return filepath.Join(root, hex.EncodeToString(sum[:])[:12]), nil
}

// readJSONL decodes every parseable line of path into T, skipping
// malformed lines with a stderr warning. A missing file yields nil.
func readJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path) //nolint:gosec // paths derive from EVOL_CORPUS_ROOT by design
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only handle

	var out []T
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	line := 0
	for sc.Scan() {
		line++
		text := bytes.TrimSpace(sc.Bytes())
		if len(text) == 0 {
			continue
		}
		var item T
		if err := json.Unmarshal(text, &item); err != nil {
			fmt.Fprintf(os.Stderr, "corpus-fs: %s:%d: skipping malformed line: %v\n", path, line, err)
			continue
		}
		out = append(out, item)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return out, nil
}

// appendJSONL marshals items one-per-line and appends them with a single
// O_APPEND write.
func appendJSONL[T any](path string, items []T) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	var buf bytes.Buffer
	for _, item := range items {
		line, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("encode line: %w", err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640) //nolint:gosec // paths derive from EVOL_CORPUS_ROOT by design
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close() //nolint:errcheck,gosec // write error takes precedence
		return fmt.Errorf("append %s: %w", path, err)
	}
	return f.Close()
}

func handleCases(root string, raw []byte) (any, error) {
	var req struct {
		ArtifactRef string `json:"artifact_ref"`
		Split       string `json:"split,omitempty"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode cases request: %w", err)
	}
	dir, err := artifactDir(root, req.ArtifactRef)
	if err != nil {
		return nil, err
	}
	all, err := readJSONL[caseEntry](filepath.Join(dir, casesFile))
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	cases := make([]caseEntry, 0, len(all))
	for _, c := range all {
		if req.Split != "" && c.Split != req.Split {
			continue
		}
		sum := sha256.Sum256([]byte(c.Input + "\x00" + c.Expected))
		key := hex.EncodeToString(sum[:])
		if seen[key] {
			continue
		}
		seen[key] = true
		cases = append(cases, c)
	}

	return struct {
		envelope
		Cases []caseEntry `json:"cases"`
	}{envelope{contractVersion, portName, "cases"}, cases}, nil
}

func handleRecord(root string, raw []byte) (any, error) {
	var req struct {
		Generation generationInfo    `json:"generation"`
		Candidates []candidateRecord `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode record request: %w", err)
	}
	dir, err := artifactDir(root, req.Generation.ArtifactRef)
	if err != nil {
		return nil, err
	}

	lines := make([]generationLine, 0, len(req.Candidates))
	for _, c := range req.Candidates {
		lines = append(lines, generationLine{Generation: req.Generation, candidateRecord: c})
	}
	if len(lines) > 0 {
		if err := appendJSONL(filepath.Join(dir, generationsFile), lines); err != nil {
			return nil, err
		}
	} else if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}

	// Human breadcrumb; best-effort, never fails the record.
	refPath := filepath.Join(dir, refFile)
	if _, err := os.Stat(refPath); errors.Is(err, fs.ErrNotExist) {
		_ = os.WriteFile(refPath, []byte(req.Generation.ArtifactRef+"\n"), 0o600)
	}

	return envelope{contractVersion, portName, "record"}, nil
}

func handleTabu(root string, raw []byte) (any, error) {
	var req struct {
		ArtifactRef string `json:"artifact_ref"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode tabu request: %w", err)
	}
	dir, err := artifactDir(root, req.ArtifactRef)
	if err != nil {
		return nil, err
	}
	lines, err := readJSONL[generationLine](filepath.Join(dir, generationsFile))
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	entries := make([]tabuEntry, 0, len(lines))
	for _, l := range lines {
		if l.Verdict == "accepted" {
			continue
		}
		key := l.Strategy + "\x00" + l.Rationale
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, tabuEntry{Strategy: l.Strategy, Rationale: l.Rationale, Verdict: l.Verdict})
	}

	return struct {
		envelope
		Entries []tabuEntry `json:"entries"`
	}{envelope{contractVersion, portName, "tabu"}, entries}, nil
}

func handleCorrections(root string, raw []byte) (any, error) {
	var req struct {
		ArtifactRef string `json:"artifact_ref"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode corrections request: %w", err)
	}
	dir, err := artifactDir(root, req.ArtifactRef)
	if err != nil {
		return nil, err
	}
	cases, err := readJSONL[caseEntry](filepath.Join(dir, correctionsFile))
	if err != nil {
		return nil, err
	}
	for i := range cases {
		if cases[i].Source == "" {
			cases[i].Source = "correction"
		}
	}
	if cases == nil {
		cases = []caseEntry{}
	}

	return struct {
		envelope
		Cases []caseEntry `json:"cases"`
	}{envelope{contractVersion, portName, "corrections"}, cases}, nil
}
