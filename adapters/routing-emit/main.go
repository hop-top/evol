// Command routing-emit implements the draft "routing" port, action
// "emit": it turns provider-attributed evaluation evidence into a
// kit-llm pool configuration fragment.
//
// One JSON request on stdin, one JSON response on stdout. Non-zero exit
// only on adapter errors (malformed request, IO failure); stderr
// carries diagnostics.
//
// The emitted pool weights are normalized per-provider score means:
// the strongest provider gets weight 1.00, the rest scale against it.
// The pool file is a routing configuration — itself a tool-config
// artifact the loop can evolve.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	contractVersion = "1"
	portName        = "routing"
	poolFormat      = "kit-llm-pool"
)

type request struct {
	Evol        string     `json:"evol"`
	Port        string     `json:"port"`
	Action      string     `json:"action"`
	ArtifactRef string     `json:"artifact_ref"`
	Evidence    []evidence `json:"evidence"`
	Output      output     `json:"output"`
}

type evidence struct {
	Provider  string  `json:"provider"`
	MeanScore float64 `json:"mean_score"`
	N         int     `json:"n"`
}

type output struct {
	Path   string `json:"path"`
	Format string `json:"format"`
}

type entry struct {
	Alias   string  `json:"alias"`
	Scheme  string  `json:"scheme"`
	Model   string  `json:"model"`
	BaseURL string  `json:"base_url,omitempty"`
	Weight  float64 `json:"weight"`
}

type response struct {
	Evol    string  `json:"evol"`
	Port    string  `json:"port"`
	Action  string  `json:"action"`
	Written string  `json:"written"`
	Entries []entry `json:"entries"`
}

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

func run(stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	fail := func(err error) int {
		_, _ = fmt.Fprintf(stderr, "routing-emit: %v\n", err)
		return 1
	}

	raw, err := io.ReadAll(stdin)
	if err != nil {
		return fail(fmt.Errorf("read stdin: %w", err))
	}
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return fail(fmt.Errorf("decode request: %w", err))
	}
	if req.Evol != contractVersion {
		return fail(fmt.Errorf("unsupported contract version %q", req.Evol))
	}
	if req.Port != portName || req.Action != "emit" {
		return fail(fmt.Errorf("unsupported port/action %q/%q", req.Port, req.Action))
	}
	if len(req.Evidence) == 0 {
		return fail(errors.New("evidence is empty; nothing to emit"))
	}
	if req.Output.Format != "" && req.Output.Format != poolFormat {
		return fail(fmt.Errorf("unsupported output format %q (only %q)", req.Output.Format, poolFormat))
	}
	path, err := guardPath(req.Output.Path, getenv)
	if err != nil {
		return fail(err)
	}

	entries, err := buildEntries(req.Evidence)
	if err != nil {
		return fail(err)
	}
	doc := renderPool(req.ArtifactRef, entries)
	if err := atomicWrite(path, []byte(doc)); err != nil {
		return fail(err)
	}

	resp := response{Evol: contractVersion, Port: portName, Action: "emit",
		Written: path, Entries: entries}
	out, err := json.Marshal(resp)
	if err != nil {
		return fail(fmt.Errorf("encode response: %w", err))
	}
	_, _ = fmt.Fprintln(stdout, string(out))
	return 0
}

// guardPath refuses absolute paths and parent-dir escapes unless
// EVOL_ROUTING_ALLOW_ABS=1 — an emitted pool lands inside the working
// tree by default.
func guardPath(p string, getenv func(string) string) (string, error) {
	if p == "" {
		return "", errors.New("output.path is required")
	}
	if getenv("EVOL_ROUTING_ALLOW_ABS") == "1" {
		return p, nil
	}
	if filepath.IsAbs(p) {
		return "", fmt.Errorf("absolute output path %q refused (set EVOL_ROUTING_ALLOW_ABS=1 to allow)", p)
	}
	if !filepath.IsLocal(p) {
		return "", fmt.Errorf("output path %q escapes the working tree", p)
	}
	return p, nil
}

// buildEntries converts evidence to pool entries: sorted by mean
// descending (ties broken by provider string for determinism), weight =
// mean normalized against the best, rounded to two decimals.
func buildEntries(ev []evidence) ([]entry, error) {
	sorted := make([]evidence, len(ev))
	copy(sorted, ev)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].MeanScore != sorted[j].MeanScore {
			return sorted[i].MeanScore > sorted[j].MeanScore
		}
		return sorted[i].Provider < sorted[j].Provider
	})

	best := sorted[0].MeanScore
	if best <= 0 {
		return nil, fmt.Errorf("best mean score %v is not positive; refusing to emit weights", best)
	}

	entries := make([]entry, 0, len(sorted))
	seen := map[string]bool{}
	for _, e := range sorted {
		if e.Provider == "" {
			return nil, errors.New("evidence entry with empty provider")
		}
		scheme, model, base, err := parseProvider(e.Provider)
		if err != nil {
			return nil, err
		}
		alias := aliasFor(model)
		if seen[alias] {
			alias = alias + "-" + scheme
		}
		seen[alias] = true
		w := float64(int(e.MeanScore/best*100+0.5)) / 100
		entries = append(entries, entry{
			Alias: alias, Scheme: scheme, Model: model, BaseURL: base, Weight: w,
		})
	}
	return entries, nil
}

// parseProvider splits scheme://model[?params] by hand — model names
// carry colons (qwen3.6:35b) that defeat net/url host parsing — and
// keeps only base_url from the params: credentials (api_key and
// friends) never reach the emitted pool.
func parseProvider(uri string) (scheme, model, baseURL string, err error) {
	scheme, rest, ok := strings.Cut(uri, "://")
	if !ok || scheme == "" {
		return "", "", "", fmt.Errorf("provider URI %q is not scheme://model[?params]", uri)
	}
	model, query, _ := strings.Cut(rest, "?")
	if model == "" {
		return "", "", "", fmt.Errorf("provider URI %q carries no model", uri)
	}
	if query != "" {
		if vals, qerr := url.ParseQuery(query); qerr == nil {
			baseURL = vals.Get("base_url")
		}
	}
	return scheme, model, baseURL, nil
}

var aliasRe = regexp.MustCompile(`[^a-z0-9]+`)

func aliasFor(model string) string {
	a := aliasRe.ReplaceAllString(strings.ToLower(model), "-")
	return strings.Trim(a, "-") + "-evol"
}

// renderPool emits the kit-llm pool YAML fragment by hand — the shape
// is small and stable: pool: [{alias, scheme, model, base_url?, weight}].
func renderPool(artifactRef string, entries []entry) string {
	var b strings.Builder
	b.WriteString("# Pool fragment emitted from evaluation evidence")
	if artifactRef != "" {
		fmt.Fprintf(&b, " for %s", artifactRef)
	}
	b.WriteString(".\n")
	b.WriteString("# Weights are per-provider holdout score means normalized to the best.\n")
	b.WriteString("# This file is routing configuration — a tool-config artifact the loop\n")
	b.WriteString("# can itself evolve. Regenerate; do not hand-tune.\n")
	b.WriteString("pool:\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "  - alias: %s\n    scheme: %s\n    model: %q\n", e.Alias, e.Scheme, e.Model)
		if e.BaseURL != "" {
			fmt.Fprintf(&b, "    base_url: %q\n", e.BaseURL)
		}
		fmt.Fprintf(&b, "    weight: %.2f\n", e.Weight)
	}
	return b.String()
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	tmp, err := os.CreateTemp(dir, ".routing-emit-*")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}
