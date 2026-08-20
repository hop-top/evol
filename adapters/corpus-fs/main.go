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
	"sort"
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
	// Quarantined cases (synthetic/mined intake) are excluded from
	// `cases` responses until promoted; see spec/port-corpus.md.
	Quarantined bool `json:"quarantined,omitempty"`
}

// contentKey is the dedup identity of a case: its input + expected.
func contentKey(c caseEntry) string {
	sum := sha256.Sum256([]byte(c.Input + "\x00" + c.Expected))
	return hex.EncodeToString(sum[:])
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
	// RecordedAt is the engine's wall-clock stamp at record time
	// (RFC3339); read back by `history` as the last-evolution clock.
	RecordedAt string `json:"recorded_at,omitempty"`
	// Provider is the executor provider URI the scores were produced
	// under (model-dimension sweep rows always carry one).
	Provider string `json:"provider,omitempty"`
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
	case "add-cases":
		resp, err = handleAddCases(root, raw)
	case "add-corrections":
		resp, err = handleAddCorrections(root, raw)
	case "promote-cases":
		resp, err = handlePromoteCases(root, raw)
	case "history":
		resp, err = handleHistory(root, raw)
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
		ArtifactRef        string `json:"artifact_ref"`
		Split              string `json:"split,omitempty"`
		IncludeQuarantined bool   `json:"include_quarantined,omitempty"`
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
		if c.Quarantined && !req.IncludeQuarantined {
			continue
		}
		if req.Split != "" && c.Split != req.Split {
			continue
		}
		key := contentKey(c)
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

func handleAddCases(root string, raw []byte) (any, error) {
	var req struct {
		ArtifactRef string      `json:"artifact_ref"`
		Cases       []caseEntry `json:"cases"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode add-cases request: %w", err)
	}
	dir, err := artifactDir(root, req.ArtifactRef)
	if err != nil {
		return nil, err
	}
	existing, err := readJSONL[caseEntry](filepath.Join(dir, casesFile))
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(existing))
	usedIDs := make(map[string]bool, len(existing))
	for _, c := range existing {
		known[contentKey(c)] = true
		usedIDs[c.ID] = true
	}

	added := make([]caseEntry, 0, len(req.Cases))
	ids := make([]string, 0, len(req.Cases))
	duplicates := 0
	for _, c := range req.Cases {
		if c.Input == "" {
			return nil, errors.New("add-cases: case input is required")
		}
		key := contentKey(c)
		if known[key] {
			duplicates++
			continue
		}
		known[key] = true
		if c.ID == "" {
			c.ID = "syn-" + key[:12]
		}
		if usedIDs[c.ID] {
			duplicates++
			continue
		}
		usedIDs[c.ID] = true
		added = append(added, c)
		ids = append(ids, c.ID)
	}
	if len(added) > 0 {
		if err := appendJSONL(filepath.Join(dir, casesFile), added); err != nil {
			return nil, err
		}
	}

	return struct {
		envelope
		Added      int      `json:"added"`
		Duplicates int      `json:"duplicates"`
		IDs        []string `json:"ids"`
	}{envelope{contractVersion, portName, "add-cases"}, len(added), duplicates, ids}, nil
}

func handlePromoteCases(root string, raw []byte) (any, error) {
	var req struct {
		ArtifactRef string   `json:"artifact_ref"`
		IDs         []string `json:"ids"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode promote-cases request: %w", err)
	}
	if len(req.IDs) == 0 {
		return nil, errors.New("promote-cases: ids is required")
	}
	dir, err := artifactDir(root, req.ArtifactRef)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, casesFile)
	all, err := readJSONL[caseEntry](path)
	if err != nil {
		return nil, err
	}

	want := make(map[string]bool, len(req.IDs))
	for _, id := range req.IDs {
		want[id] = true
	}
	promoted := 0
	for i := range all {
		if want[all[i].ID] {
			delete(want, all[i].ID)
			if all[i].Quarantined {
				all[i].Quarantined = false
				promoted++
			}
		}
	}
	missing := make([]string, 0, len(want))
	for id := range want {
		missing = append(missing, id)
	}
	sort.Strings(missing)

	if promoted > 0 {
		// Atomic rewrite: temp file + rename.
		var buf bytes.Buffer
		for _, c := range all {
			line, merr := json.Marshal(c)
			if merr != nil {
				return nil, fmt.Errorf("encode case: %w", merr)
			}
			buf.Write(line)
			buf.WriteByte('\n')
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, buf.Bytes(), 0o640); err != nil { //nolint:gosec // corpus root by design
			return nil, fmt.Errorf("write %s: %w", tmp, err)
		}
		if err := os.Rename(tmp, path); err != nil {
			return nil, fmt.Errorf("rename %s: %w", tmp, err)
		}
	}

	return struct {
		envelope
		Promoted int      `json:"promoted"`
		Missing  []string `json:"missing"`
	}{envelope{contractVersion, portName, "promote-cases"}, promoted, missing}, nil
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
		// Accepted rows are not tabu; evidence rows (provider sweep,
		// baseline sweeps) are observations, not rejections.
		if l.Verdict == "accepted" || l.Verdict == "evidence" {
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

// historyEntry is one generation's summary: the best candidate mean in
// that generation and that candidate's verdict.
type historyEntry struct {
	Generation int     `json:"generation"`
	BestScore  float64 `json:"best_score"`
	Verdict    string  `json:"verdict"`
	// RecordedAt is the latest recorded_at among the generation's
	// non-evidence rows; empty for rows recorded before stamps existed.
	RecordedAt string `json:"recorded_at,omitempty"`
}

func handleHistory(root string, raw []byte) (any, error) {
	var req struct {
		ArtifactRef string `json:"artifact_ref"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode history request: %w", err)
	}
	dir, err := artifactDir(root, req.ArtifactRef)
	if err != nil {
		return nil, err
	}
	lines, err := readJSONL[generationLine](filepath.Join(dir, generationsFile))
	if err != nil {
		return nil, err
	}

	best := make(map[int]historyEntry)
	order := make([]int, 0)
	for _, l := range lines {
		// Evidence rows (provider sweeps) are observations under other
		// providers — mixing them into per-generation bests would let a
		// stronger secondary model masquerade as loop progress.
		if l.Verdict == "evidence" {
			continue
		}
		gen := l.Generation.Number
		var sum float64
		for _, s := range l.Scores {
			sum += s.Score
		}
		mean := 0.0
		if len(l.Scores) > 0 {
			mean = sum / float64(len(l.Scores))
		}
		cur, ok := best[gen]
		if !ok {
			order = append(order, gen)
			cur = historyEntry{Generation: gen, BestScore: mean, Verdict: l.Verdict, RecordedAt: l.RecordedAt}
		} else {
			if mean > cur.BestScore {
				cur.BestScore = mean
				cur.Verdict = l.Verdict
			}
			// Latest stamp wins regardless of which candidate scored best:
			// the question history answers is "when did this generation
			// happen", not "when did its best row land".
			if l.RecordedAt > cur.RecordedAt {
				cur.RecordedAt = l.RecordedAt
			}
		}
		best[gen] = cur
	}
	sort.Ints(order)
	generations := make([]historyEntry, 0, len(order))
	for _, gen := range order {
		generations = append(generations, best[gen])
	}

	return struct {
		envelope
		Generations []historyEntry `json:"generations"`
	}{envelope{contractVersion, portName, "history"}, generations}, nil
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

// handleAddCorrections records human-authored corrections. Corrections
// are never quarantined — a human wrote them; they join the eval pool
// at the next eval-set build via the `corrections` action. Dedup is by
// id and by content against the existing corrections store.
func handleAddCorrections(root string, raw []byte) (any, error) {
	var req struct {
		ArtifactRef string      `json:"artifact_ref"`
		Corrections []caseEntry `json:"corrections"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode add-corrections request: %w", err)
	}
	dir, err := artifactDir(root, req.ArtifactRef)
	if err != nil {
		return nil, err
	}
	existing, err := readJSONL[caseEntry](filepath.Join(dir, correctionsFile))
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(existing))
	usedIDs := make(map[string]bool, len(existing))
	for _, c := range existing {
		known[contentKey(c)] = true
		usedIDs[c.ID] = true
	}

	added := make([]caseEntry, 0, len(req.Corrections))
	ids := make([]string, 0, len(req.Corrections))
	duplicates := 0
	for _, c := range req.Corrections {
		if c.ID == "" {
			return nil, errors.New("add-corrections: correction id is required")
		}
		if c.Input == "" {
			return nil, errors.New("add-corrections: correction input is required")
		}
		c.Source = "correction"
		c.Quarantined = false
		key := contentKey(c)
		if known[key] || usedIDs[c.ID] {
			duplicates++
			continue
		}
		known[key] = true
		usedIDs[c.ID] = true
		added = append(added, c)
		ids = append(ids, c.ID)
	}
	if len(added) > 0 {
		if err := appendJSONL(filepath.Join(dir, correctionsFile), added); err != nil {
			return nil, err
		}
	}

	return struct {
		envelope
		Added      int      `json:"added"`
		Duplicates int      `json:"duplicates"`
		IDs        []string `json:"ids"`
	}{envelope{contractVersion, portName, "add-corrections"}, len(added), duplicates, ids}, nil
}
