package repo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// StatusCounts summarizes `git status --porcelain`. A line can count in
// both Staged and Modified: "MM" means the file has staged changes and
// further unstaged edits on top.
type StatusCounts struct {
	Staged    int
	Modified  int
	Untracked int
}

// Total reports whether anything at all is pending.
func (c StatusCounts) Total() int { return c.Staged + c.Modified + c.Untracked }

// StatusCounts classifies the working tree and index without touching it.
// --no-optional-locks keeps the status call from refreshing and locking
// the index.
func (r *Repo) StatusCounts(ctx context.Context) (StatusCounts, error) {
	out, err := r.Git.Query(ctx, "--no-optional-locks", "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return StatusCounts{}, fmt.Errorf("status: %w", err)
	}
	var counts StatusCounts
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		if strings.HasPrefix(line, "??") {
			counts.Untracked++
			continue
		}
		if line[0] != ' ' {
			counts.Staged++
		}
		if line[1] != ' ' {
			counts.Modified++
		}
	}
	return counts, nil
}

// AheadBehind counts commits unique to each side of a symmetric range:
// ahead is reachable from left only, behind from right only. Both refs
// must be resolvable (HEAD, a branch, or a fully qualified ref).
func (r *Repo) AheadBehind(ctx context.Context, left, right string) (ahead, behind int, err error) {
	out, err := r.Git.Query(ctx, "rev-list", "--left-right", "--count", left+"..."+right)
	if err != nil {
		return 0, 0, fmt.Errorf("rev-list: %w", err)
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("rev-list: unexpected output %q", out)
	}
	ahead, err = strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("rev-list: unexpected output %q", out)
	}
	behind, err = strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("rev-list: unexpected output %q", out)
	}
	return ahead, behind, nil
}

// Upstream returns the upstream ref of the current branch, or "" when no
// upstream is configured. Reading upstream state must not fail the status
// glance, so query errors degrade to "".
func (r *Repo) Upstream(ctx context.Context) string {
	out, err := r.Git.Query(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// HasCommits reports whether HEAD resolves at all. False means the
// repository has no commits yet.
func (r *Repo) HasCommits(ctx context.Context) bool {
	return r.Git.Ok(ctx, "rev-parse", "--verify", "--quiet", "HEAD")
}

// ShortHEAD returns the abbreviated HEAD commit hash, or "" when HEAD
// cannot resolve.
func (r *Repo) ShortHEAD(ctx context.Context) string {
	out, err := r.Git.Query(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
