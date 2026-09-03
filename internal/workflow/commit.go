package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/EstebanForge/elgit/internal/safety"
)

// CommitRequest is one commit flow: what to stage and the message parts.
type CommitRequest struct {
	Subject     string // first line, required
	Description string // optional body
	All         bool   // also stage untracked files
	Amend       bool   // fold into the last commit instead of adding one
}

// CommitResult reports what the flow did.
type CommitResult struct {
	Committed bool   // true when a commit or amend actually ran (fake included)
	SHA       string // abbreviated HEAD after the commit, "" in fake mode
	Amended   bool
	LeftDirty int // tracked files hooks left modified; untracked never counts
}

// CommitGate reports whether the request would produce a commit. It
// prints and reports false for the empty cases, so callers can gate
// before asking the user for a message. Amend always proceeds: rewording
// a commit on a clean tree is legitimate.
func (w *Workflow) CommitGate(ctx context.Context, req CommitRequest) (bool, error) {
	if req.Amend {
		return true, nil
	}
	counts, err := w.Repo.StatusCounts(ctx)
	if err != nil {
		return false, err
	}
	switch {
	case counts.Total() == 0:
		w.printf("Nothing to commit.")
		return false, nil
	case counts.Staged == 0 && counts.Modified == 0 && counts.Untracked > 0 && !req.All:
		w.printf("No tracked changes to commit; pass --all to include untracked files.")
		return false, nil
	}
	return true, nil
}

// Commit stages and creates exactly one commit. Tracked modifications are
// staged always; untracked files only with All. Commit never pushes: sync
// and publish own the remote.
//
// The unborn-HEAD case is allowed on purpose: a first commit needs it.
// That is why this flow cannot reuse guard(), which refuses repositories
// without commits. The in-progress check still applies.
func (w *Workflow) Commit(ctx context.Context, req CommitRequest) (CommitResult, error) {
	var res CommitResult

	kind, err := safety.InProgressOp(ctx, w.Repo.Git)
	if err != nil {
		return res, err
	}
	if kind != "" {
		return res, fmt.Errorf("a %s is in progress; finish or abort it before committing", kind)
	}
	// Refuse detached HEAD: elgit flows are branch-oriented, and a commit
	// on a detached HEAD is easy to lose. The branch name itself is not
	// needed here; the caller reports it.
	if _, err := w.Repo.CurrentBranch(ctx); err != nil {
		return res, err
	}

	proceed, err := w.CommitGate(ctx, req)
	if err != nil || !proceed {
		return res, err
	}

	stage := []string{"add", "-u"}
	if req.All {
		stage = []string{"add", "-A"}
	}
	if _, err := w.Repo.Git.Mutate(ctx, stage...); err != nil && !w.Repo.Git.FakeOK(err) {
		return res, err
	}

	// Re-check after staging: hooks, background tools, or an unborn HEAD
	// (where add -u stages nothing) can leave the index empty.
	if !w.Repo.Git.Fake && !req.Amend {
		after, err := w.Repo.StatusCounts(ctx)
		if err != nil {
			return res, err
		}
		if after.Staged == 0 {
			w.printf("Nothing to commit.")
			return res, nil
		}
	}

	commit := []string{"commit", "-m", req.Subject}
	if req.Description != "" {
		commit = append(commit, "-m", req.Description)
	}
	if req.Amend {
		commit = append(commit, "--amend")
	}
	if _, err := w.Repo.Git.Mutate(ctx, commit...); err != nil && !w.Repo.Git.FakeOK(err) {
		return res, err
	}
	res.Committed = true
	res.Amended = req.Amend

	if w.Repo.Git.Fake {
		res.SHA = "" // nothing happened; a stale HEAD hash would mislead
		return res, nil
	}

	sha, err := w.Repo.Git.Query(ctx, "rev-parse", "--short", "HEAD")
	if err != nil {
		return res, err
	}
	res.SHA = strings.TrimSpace(sha)

	// Pre-commit formatters often edit files after staging; their edits
	// stay unstaged. Report them instead of losing them silently. Only
	// tracked modifications count: untracked files predate the commit.
	// A query failure must not mask the commit that already landed.
	if after, err := w.Repo.StatusCounts(ctx); err == nil {
		res.LeftDirty = after.Modified
	}
	return res, nil
}
