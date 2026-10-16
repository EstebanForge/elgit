package repo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// FileChange is one entry of the uncommitted-changes listing. Counts come
// from `git diff --numstat` vs HEAD and cover staged and unstaged work
// together; untracked files have no diff row, so Counted stays false and
// the caller decides whether to expand their content.
type FileChange struct {
	Code    string // porcelain XY letters, verbatim: " M", "MM", "A ", "R ", "??"
	Path    string
	Added   int
	Deleted int
	Binary  bool // numstat reported "-" for this file
	Counted bool // a numstat row exists for this path
}

// Changes lists every uncommitted change: staged, unstaged, and untracked,
// in `git status` order. The status and numstat calls run under
// --no-optional-locks so a read-only glance never refreshes the index.
// On an unborn HEAD the diff base is the empty tree, so staged additions
// and unstaged edits both count.
func (r *Repo) Changes(ctx context.Context) ([]FileChange, error) {
	out, err := r.Git.Query(ctx, "--no-optional-locks", "status", "--porcelain", "-z", "--untracked-files=normal")
	if err != nil {
		return nil, fmt.Errorf("status: %w", err)
	}
	changes := parsePorcelainZ(out)

	base := "HEAD"
	if !r.HasCommits(ctx) {
		base, err = r.EmptyTreeOID(ctx)
		if err != nil {
			return nil, err
		}
	}
	numstat, err := r.Git.Query(ctx, "--no-optional-locks", "diff", base, "--numstat", "-z")
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	applyNumstat(changes, numstat)
	return changes, nil
}

// parsePorcelainZ parses `git status --porcelain -z`. Entries are
// NUL-separated; a rename or copy is followed by a second NUL field
// holding the original path, which is consumed and dropped so the new
// path stays the identity of the entry.
func parsePorcelainZ(out string) []FileChange {
	fields := strings.Split(out, "\x00")
	changes := make([]FileChange, 0, len(fields))
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 { // "XY " plus at least one path byte
			continue
		}
		c := FileChange{Code: f[:2], Path: f[3:]}
		changes = append(changes, c)
		if f[0] == 'R' || f[0] == 'C' {
			i++ // skip the original-path field
		}
	}
	return changes
}

// applyNumstat merges `git diff --numstat -z` counts into the status
// entries by path. A rename or copy under -z is the record
// "<added>\t<deleted>\t" followed by NUL-separated old and new paths; the
// new path is the identity, matching parsePorcelainZ.
func applyNumstat(changes []FileChange, out string) {
	counts := make(map[string][2]string)
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		added, rest, ok := strings.Cut(fields[i], "\t")
		if !ok {
			continue
		}
		deleted, path, ok := strings.Cut(rest, "\t")
		if !ok {
			continue
		}
		if path == "" {
			if i+2 >= len(fields) {
				continue
			}
			path = fields[i+2]
			i += 2
		}
		counts[path] = [2]string{added, deleted}
	}
	for i := range changes {
		n, ok := counts[changes[i].Path]
		if !ok {
			continue
		}
		changes[i].Counted = true
		if n[0] == "-" || n[1] == "-" {
			changes[i].Binary = true
			continue
		}
		added, aerr := strconv.Atoi(n[0])
		deleted, derr := strconv.Atoi(n[1])
		if aerr != nil || derr != nil {
			continue // malformed row: show the file without counts
		}
		changes[i].Added, changes[i].Deleted = added, deleted
	}
}

// EmptyTreeOID returns the object ID of the empty tree, computed by git
// so it stays correct on any hash algorithm. It is the diff base for an
// unborn HEAD, where --cached would drop unstaged worktree edits.
func (r *Repo) EmptyTreeOID(ctx context.Context) (string, error) {
	out, err := r.Git.Query(ctx, "hash-object", "-t", "tree", "/dev/null")
	if err != nil {
		return "", fmt.Errorf("empty tree: %w", err)
	}
	return strings.TrimSpace(out), nil
}
