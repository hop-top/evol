// Command fs-artifact implements the evol ArtifactStore port over a
// filesystem directory. One JSON request on stdin, one JSON response
// on stdout; non-zero exit with stderr diagnostics on adapter errors.
//
// The artifact root comes from EVOL_ARTIFACT_ROOT. Refs are
// root-relative paths. See README.md for kind conventions.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	contractVersion = "1"
	portName        = "artifactstore"
	versionLen      = 8
)

type request struct {
	Evol   string `json:"evol"`
	Port   string `json:"port"`
	Action string `json:"action"`

	// load + write
	Ref string `json:"ref"`

	// write
	Frontmatter string `json:"frontmatter"`
	Body        string `json:"body"`
	Message     string `json:"message"`

	// restore
	Version string `json:"version"`

	// list
	Kind string `json:"kind"`
}

type artifact struct {
	Ref         string `json:"ref"`
	Kind        string `json:"kind"`
	Frontmatter string `json:"frontmatter"`
	Body        string `json:"body"`
	Version     string `json:"version"`
}

type response struct {
	Evol   string `json:"evol"`
	Port   string `json:"port"`
	Action string `json:"action"`

	Artifact *artifact `json:"artifact,omitempty"`
	Version  string    `json:"version,omitempty"`
	Refs     []string  `json:"refs,omitempty"`

	// git-native mode only
	GitCommit string         `json:"git_commit,omitempty"`
	Versions  []versionEntry `json:"versions,omitempty"`
}

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr, os.Getenv))
}

func run(stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	fail := func(format string, args ...any) int {
		_, _ = fmt.Fprintf(stderr, "fs-artifact: "+format+"\n", args...)
		return 1
	}

	root := getenv("EVOL_ARTIFACT_ROOT")
	if root == "" {
		return fail("EVOL_ARTIFACT_ROOT is not set")
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fail("EVOL_ARTIFACT_ROOT %q is not a directory", root)
	}

	raw, err := io.ReadAll(stdin)
	if err != nil {
		return fail("reading stdin: %v", err)
	}
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return fail("malformed request JSON: %v", err)
	}
	if req.Evol != contractVersion {
		return fail("unsupported contract version %q (want %q)", req.Evol, contractVersion)
	}
	if req.Port != portName {
		return fail("wrong port %q (this adapter serves %q)", req.Port, portName)
	}

	resp := response{Evol: contractVersion, Port: portName, Action: req.Action}

	switch req.Action {
	case "load":
		art, err := load(root, req.Ref)
		if err != nil {
			return fail("load %q: %v", req.Ref, err)
		}
		resp.Artifact = art
	case "write":
		if req.Message != "" {
			_, _ = fmt.Fprintf(stderr, "fs-artifact: write %q: %s\n", req.Ref, req.Message)
		}
		gitMode := gitVersioning(root, getenv)
		if gitMode {
			// Refuse before touching the tree: committing on top of
			// unrelated staged work would swallow it.
			if err := refuseUnrelatedStaged(root); err != nil {
				return fail("write %q (git): %v", req.Ref, err)
			}
		}
		version, err := write(root, req.Ref, req.Frontmatter, req.Body)
		if err != nil {
			return fail("write %q: %v", req.Ref, err)
		}
		resp.Version = version
		if gitMode {
			msg := fmt.Sprintf("evol: promote %s %s", req.Ref, version)
			sha, err := gitCommitRef(root, req.Ref, msg)
			if err != nil {
				return fail("write %q (git): %v", req.Ref, err)
			}
			resp.GitCommit = sha
		}
	case "versions":
		if _, err := resolve(root, req.Ref); err != nil {
			return fail("versions %q: %v", req.Ref, err)
		}
		if !gitVersioning(root, getenv) {
			return fail("versions %q: requires git-native mode (EVOL_ARTIFACT_GIT=1 and an artifact root inside a git work tree)", req.Ref)
		}
		entries, err := gitVersions(root, req.Ref)
		if err != nil {
			return fail("versions %q: %v", req.Ref, err)
		}
		resp.Versions = entries
	case "restore":
		if _, err := resolve(root, req.Ref); err != nil {
			return fail("restore %q: %v", req.Ref, err)
		}
		if !gitVersioning(root, getenv) {
			return fail("restore %q: requires git-native mode (EVOL_ARTIFACT_GIT=1 and an artifact root inside a git work tree)", req.Ref)
		}
		if req.Version == "" {
			return fail("restore %q: version is required", req.Ref)
		}
		version, sha, err := gitRestore(root, req.Ref, req.Version)
		if err != nil {
			return fail("restore %q: %v", req.Ref, err)
		}
		resp.Version = version
		resp.GitCommit = sha
	case "list":
		refs, err := list(root, req.Kind)
		if err != nil {
			return fail("list: %v", err)
		}
		resp.Refs = refs
	default:
		return fail("unknown action %q", req.Action)
	}

	enc := json.NewEncoder(stdout)
	if err := enc.Encode(resp); err != nil {
		return fail("encoding response: %v", err)
	}
	return 0
}

// resolve joins ref onto root, rejecting absolute refs and any path
// that escapes the root.
func resolve(root, ref string) (string, error) {
	if ref == "" {
		return "", fmt.Errorf("empty ref")
	}
	if filepath.IsAbs(ref) {
		return "", fmt.Errorf("absolute refs are not allowed")
	}
	if !filepath.IsLocal(ref) {
		return "", fmt.Errorf("ref escapes artifact root")
	}
	return filepath.Join(root, filepath.FromSlash(ref)), nil
}

func load(root, ref string) (*artifact, error) {
	path, err := resolve(root, ref)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path is validated against root by resolve
	if err != nil {
		return nil, err
	}
	fm, body := splitFrontmatter(string(raw))
	return &artifact{
		Ref:         ref,
		Kind:        kindOf(ref),
		Frontmatter: fm,
		Body:        body,
		Version:     versionOf(raw),
	}, nil
}

func write(root, ref, frontmatter, body string) (string, error) {
	path, err := resolve(root, ref)
	if err != nil {
		return "", err
	}
	content := assemble(frontmatter, body)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".fs-artifact-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return versionOf([]byte(content)), nil
}

func list(root, kind string) ([]string, error) {
	if kind != "" && !validKind(kind) {
		return nil, fmt.Errorf("unknown kind %q", kind)
	}
	refs := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if strings.HasPrefix(name, ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		ref := filepath.ToSlash(rel)
		k := kindOf(ref)
		if k == "" {
			return nil
		}
		if kind != "" && k != kind {
			return nil
		}
		refs = append(refs, ref)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(refs)
	return refs, nil
}

// kindOf derives the artifact kind from ref conventions:
// any SKILL.md is a skill; prompts/, commands/, tool-configs/ prefixes
// map to their kinds. Everything else is not an artifact ("").
func kindOf(ref string) string {
	base := ref
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		base = ref[i+1:]
	}
	if base == "SKILL.md" {
		return "skill"
	}
	top := ref
	if i := strings.Index(ref, "/"); i >= 0 {
		top = ref[:i]
	}
	ext := strings.ToLower(filepath.Ext(base))
	switch top {
	case "prompts":
		if ext == ".md" {
			return "prompt"
		}
	case "commands":
		if ext == ".md" {
			return "command"
		}
	case "tool-configs":
		switch ext {
		case ".md", ".yaml", ".yml", ".json", ".toml":
			return "tool-config"
		}
	}
	return ""
}

func validKind(kind string) bool {
	switch kind {
	case "skill", "prompt", "command", "tool-config":
		return true
	}
	return false
}

// splitFrontmatter splits a document into its raw frontmatter block
// (without the --- fences) and body. Documents without a leading
// frontmatter fence return an empty frontmatter and the full content
// as body.
func splitFrontmatter(content string) (frontmatter, body string) {
	const fence = "---\n"
	if !strings.HasPrefix(content, fence) {
		return "", content
	}
	rest := content[len(fence):]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", content
	}
	fm := rest[:idx+1] // keep trailing newline of the last fm line
	after := rest[idx+1:]
	// after starts with "---"; drop the fence line.
	if nl := strings.Index(after, "\n"); nl >= 0 {
		body = after[nl+1:]
	} else {
		body = ""
	}
	return fm, body
}

// assemble reverses splitFrontmatter.
func assemble(frontmatter, body string) string {
	if frontmatter == "" {
		return body
	}
	if !strings.HasSuffix(frontmatter, "\n") {
		frontmatter += "\n"
	}
	return "---\n" + frontmatter + "---\n" + body
}

func versionOf(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])[:versionLen]
}
