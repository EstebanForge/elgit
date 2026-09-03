package workflow

import (
	"context"
	"errors"

	"github.com/EstebanForge/elgit/internal/gitx"
)

// mergeLogLimit caps the commit list in the merge execution header.
const mergeLogLimit = 5

// MergeRequest names the merge candidate: Ref is fully qualified
// (refs/heads/x or refs/remotes/<remote>/x) so it can never be read as a
// git option; Display is the branch name for messages.
type MergeRequest struct {
	Ref     string
	Display string
}

// MergePreflight validates the repository is merge-ready without naming
// a candidate: no in-progress operation, on a branch, with a HEAD, and a
// clean tree. The CLI runs it before the interactive picker and the
// network; Merge runs it again as the authoritative gate.
func (w *Workflow) MergePreflight(ctx context.Context) error {
	if err := w.guard(ctx); err != nil {
		return err
	}
	dirty, err := w.Repo.IsDirty(ctx)
	if err != nil {
		return err
	}
	if dirty {
		return errors.New("working tree has uncommitted changes; commit them (elgit commit) or stash them before merging")
	}
	return nil
}

// Merge merges another branch into the current one: compare first, merge
// second, push never (sync owns the remote). The working tree must be
// clean: unlike sync there is no stash flow here, so a conflicted stash
// restore can never land on top of a committed merge.
func (w *Workflow) Merge(ctx context.Context, req MergeRequest) error {
	if err := w.MergePreflight(ctx); err != nil {
		return err
	}
	current, err := w.Repo.CurrentBranch(ctx)
	if err != nil {
		return err
	}

	// ahead is the left side of the range: passing the candidate first
	// counts commits it has that HEAD lacks.
	ahead, _, err := w.Repo.AheadBehind(ctx, req.Ref, "HEAD")
	if err != nil {
		return err
	}
	if ahead == 0 {
		w.printf("Already up to date.")
		return nil
	}

	noun := "commits"
	if ahead == 1 {
		noun = "commit"
	}
	w.printf("Merging %d %s from %s into %s:", ahead, noun, req.Display, current)
	lines, err := w.Repo.LogOneline(ctx, "HEAD", req.Ref, mergeLogLimit)
	if err != nil {
		return err
	}
	for _, line := range lines {
		w.printf("  %s", line)
	}
	if ahead > mergeLogLimit {
		w.printf("  ... and %d more", ahead-mergeLogLimit)
	}

	if _, err := w.Repo.Git.Mutate(ctx, "merge", req.Ref); err != nil {
		if errors.Is(err, gitx.ErrFakeMutation) {
			w.printf("(fake) would merge %s into %s.", req.Display, current)
			return nil
		}
		var gErr *gitx.Error
		if errors.As(err, &gErr) {
			// The repository stays conflicted on purpose; elgit never
			// aborts a merge on its own.
			return &ConflictError{Verb: "merge", Ref: req.Ref, Err: gErr}
		}
		return err
	}
	w.printf("Merged %s into %s. Run elgit sync to push.", req.Display, current)
	return nil
}
