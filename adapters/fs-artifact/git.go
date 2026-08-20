package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// Git-native versioning (opt-in). When EVOL_ARTIFACT_GIT=1 and the
// artifact root sits inside a git work tree, every write additionally
// commits the written ref, and the optional `versions` / `restore`
// actions serve the ref's git history. Without the env (or without a
// work tree) behavior is unchanged and history actions error cleanly.

// gitVersioning reports whether git-native mode is active for root.
func gitVersioning(root string, getenv func(string) string) bool {
	return getenv("EVOL_ARTIFACT_GIT") == "1" && inWorkTree(root)
}

func inWorkTree(root string) bool {
	out, err := gitOut(root, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// gitOut runs git -C root with args and returns trimmed stdout.
// Errors carry git's stderr for diagnosis.
func gitOut(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...) // #nosec G204 -- fixed binary, adapter-built args
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}

// refuseUnrelatedStaged errors when the index already holds staged
// changes — committing on top of them would swallow unrelated work.
func refuseUnrelatedStaged(root string) error {
	out, err := gitOut(root, "diff", "--cached", "--name-only")
	if err != nil {
		return err
	}
	if strings.TrimSpace(out) != "" {
		return fmt.Errorf("unrelated staged changes present (%s); commit or unstage them first",
			strings.Join(strings.Split(strings.TrimSpace(out), "\n"), ", "))
	}
	return nil
}

// gitCommitRef stages ref and commits it with message, returning the
// commit SHA. A write that changed nothing resolves to the current
// HEAD without creating an empty commit.
func gitCommitRef(root, ref, message string) (string, error) {
	if _, err := gitOut(root, "add", "--", ref); err != nil {
		return "", err
	}
	// No staged diff for the ref → content unchanged; HEAD already
	// represents this state.
	if _, err := gitOut(root, "diff", "--cached", "--quiet", "--", ref); err == nil {
		return gitOut(root, "rev-parse", "HEAD")
	}
	if _, err := gitOut(root, "commit", "-m", message, "--", ref); err != nil {
		return "", err
	}
	return gitOut(root, "rev-parse", "HEAD")
}

type versionEntry struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit,omitempty"`
}

// gitVersions lists the ref's history newest-first: content-hash
// version + the commit that produced it.
func gitVersions(root, ref string) ([]versionEntry, error) {
	out, err := gitOut(root, "log", "--format=%H", "--", ref)
	if err != nil {
		return nil, err
	}
	shas := strings.Fields(out)
	if len(shas) == 0 {
		return nil, fmt.Errorf("no git history for %q", ref)
	}
	entries := make([]versionEntry, 0, len(shas))
	for _, sha := range shas {
		blob, err := gitBlob(root, sha, ref)
		if err != nil {
			return nil, err
		}
		entries = append(entries, versionEntry{Version: versionOf(blob), GitCommit: sha})
	}
	return entries, nil
}

// gitBlob returns the exact bytes of ref at sha.
func gitBlob(root, sha, ref string) ([]byte, error) {
	cmd := exec.Command("git", "-C", root, "cat-file", "blob", sha+":"+ref) // #nosec G204 -- fixed binary, adapter-built args
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		return nil, fmt.Errorf("git cat-file %s:%s: %s", sha, ref, msg)
	}
	return []byte(stdout.String()), nil
}

// gitRestore checks out ref at the entry matching want (content-hash
// version or commit-SHA prefix), commits the rollback, and returns the
// restored content hash + rollback commit.
func gitRestore(root, ref, want string) (string, string, error) {
	entries, err := gitVersions(root, ref)
	if err != nil {
		return "", "", err
	}
	var target *versionEntry
	for i := range entries {
		if entries[i].Version == want || strings.HasPrefix(entries[i].GitCommit, want) {
			target = &entries[i]
			break
		}
	}
	if target == nil {
		return "", "", fmt.Errorf("no version %q in the history of %q", want, ref)
	}
	if err := refuseUnrelatedStaged(root); err != nil {
		return "", "", err
	}
	if _, err := gitOut(root, "checkout", target.GitCommit, "--", ref); err != nil {
		return "", "", err
	}
	// Content may already equal the target (rollback to HEAD state).
	if _, err := gitOut(root, "diff", "--cached", "--quiet", "--", ref); err == nil {
		head, herr := gitOut(root, "rev-parse", "HEAD")
		if herr != nil {
			return "", "", herr
		}
		return target.Version, head, nil
	}
	msg := fmt.Sprintf("evol: rollback %s to %s", ref, target.Version)
	if _, err := gitOut(root, "commit", "-m", msg, "--", ref); err != nil {
		return "", "", err
	}
	head, err := gitOut(root, "rev-parse", "HEAD")
	if err != nil {
		return "", "", err
	}
	return target.Version, head, nil
}
