// Package workflow implements the elgit commands on top of repo queries.
// Every mutation goes through the gitx runner, stashes are tracked in the
// safety ledger by commit OID, and a merge or rebase conflict stops the
// flow: elgit never aborts an in-progress operation automatically.
package workflow

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/EstebanForge/elgit/internal/gitx"
	"github.com/EstebanForge/elgit/internal/repo"
	"github.com/EstebanForge/elgit/internal/safety"
)

const (
	purposeSwitch = "switching"
	purposeSync   = "syncing"
)

// Workflow runs elgit command flows against one repository.
type Workflow struct {
	Repo *repo.Repo
	Out  io.Writer
}

// New builds a workflow writing progress to out.
func New(r *repo.Repo, out io.Writer) *Workflow {
	return &Workflow{Repo: r, Out: out}
}

func (w *Workflow) printf(format string, a ...any) {
	// Best-effort progress output; a closed stdout must not abort a workflow.
	_, _ = fmt.Fprintf(w.Out, format+"\n", a...) //nolint:errcheck // deliberate discard, see comment
}

// guard refuses mutating work while git is mid-operation, off a branch,
// or before the first commit exists.
func (w *Workflow) guard(ctx context.Context) error {
	kind, err := safety.InProgressOp(ctx, w.Repo.Git)
	if err != nil {
		return err
	}
	if kind != "" {
		return fmt.Errorf("a %s is in progress; finish or abort it before running elgit", kind)
	}
	if _, err := w.Repo.CurrentBranch(ctx); err != nil {
		return err
	}
	if !w.Repo.Git.Ok(ctx, "rev-parse", "--verify", "--quiet", "HEAD") {
		return errors.New("the repository has no commits yet")
	}
	return nil
}

// remoteName returns the configured remote, or "" when none exists.
func (w *Workflow) remoteName(ctx context.Context) string {
	remote, err := w.Repo.Remote(ctx)
	if err != nil {
		return ""
	}
	return remote
}

// stashAway stashes a dirty tree and records the stash in the ledger.
// Reports the entry and whether a stash was created. In fake mode the
// stash is echoed only and nothing is recorded.
//
// The stash top OID is snapshotted before the push: if an external actor
// cleans the tree between the dirty check and the push, git exits 0
// having stashed nothing, and resolving stash@{0} would record whatever
// stash was already on top. An unchanged top means nothing was stashed.
func (w *Workflow) stashAway(ctx context.Context, led *safety.Ledger, branch, purpose string) (safety.Entry, bool, error) {
	dirty, err := w.Repo.IsDirty(ctx)
	if err != nil {
		return safety.Entry{}, false, err
	}
	if !dirty {
		return safety.Entry{}, false, nil
	}

	prevTop := stashTopOID(ctx, w.Repo.Git)

	w.printf("Saving local changes.")
	if _, err := w.Repo.Git.Mutate(ctx, "stash", "push", "--include-untracked", "-m",
		fmt.Sprintf("elgit: stashing before %s %s", purpose, branch)); err != nil && !w.Repo.Git.FakeOK(err) {
		return safety.Entry{}, false, err
	}
	if w.Repo.Git.Fake {
		return safety.Entry{}, false, nil
	}

	oid, err := safety.NewStashOID(ctx, w.Repo.Git)
	if err != nil {
		if prevTop == "" {
			// The stack was empty and stays empty: the push had nothing to
			// stash. Not an error, just nothing to record.
			return safety.Entry{}, false, nil
		}
		return safety.Entry{}, false, err
	}
	if oid == prevTop {
		// Push no-op'd (tree cleaned underneath us); do not hijack the
		// previous stash entry.
		return safety.Entry{}, false, nil
	}

	entry := safety.Entry{StashOID: oid, Branch: branch, Purpose: purpose, Created: time.Now()}
	if err := led.Record(entry); err != nil {
		return safety.Entry{}, false, err
	}
	return entry, true, nil
}

// stashTopOID returns the commit OID of the current stash top, or "" when
// the stack is empty or cannot be read.
func stashTopOID(ctx context.Context, g *gitx.Runner) string {
	out, err := g.Query(ctx, "rev-parse", "--verify", "--quiet", "stash@{0}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// restore applies the ledger stash recorded for branch and purpose. The
// stash is applied by commit OID, so concurrent changes to the stash stack
// cannot redirect the restore onto a different entry; the entry is dropped
// only after a post-drop OID verification.
func (w *Workflow) restore(ctx context.Context, led *safety.Ledger, branch, purpose string) error {
	entry, ok := led.Find(branch, purpose)
	if !ok {
		return nil
	}

	if _, found := safety.StashIndexByOID(ctx, w.Repo.Git, entry.StashOID); !found {
		// The stash was applied or dropped outside elgit; the record is stale.
		w.printf("Note: recorded stash %s is no longer on the stack; assuming it was handled.", shortOID(entry.StashOID))
		if w.Repo.Git.Fake {
			return nil
		}
		return led.Remove(entry.StashOID)
	}

	w.printf("Restoring local changes.")
	if _, err := w.Repo.Git.Mutate(ctx, "stash", "apply", entry.StashOID); err != nil {
		if errors.Is(err, gitx.ErrFakeMutation) {
			return nil
		}
		var gErr *gitx.Error
		if errors.As(err, &gErr) {
			// git keeps the stash entry when apply conflicts; the conflicted
			// changes are in the working tree, nothing is lost.
			idx, _ := safety.StashIndexByOID(ctx, w.Repo.Git, entry.StashOID)
			return &RestoreConflictError{Index: idx, OID: entry.StashOID, Err: gErr}
		}
		return err
	}

	if w.Repo.Git.Fake {
		return nil
	}
	if err := safety.DropStashByOID(ctx, w.Repo.Git, entry.StashOID); err != nil {
		return err
	}
	return led.Remove(entry.StashOID)
}

func (w *Workflow) listBranches(ctx context.Context, remote string) {
	branches, err := w.Repo.Branches(ctx, remote)
	if err != nil {
		return
	}
	w.printf("Available branches:")
	for _, b := range branches {
		w.printf("  %s %s", b.Name, b.Detail())
	}
}

// ConflictError reports a failed merge or rebase. The repository is left
// in its conflicted state on purpose; elgit never aborts automatically.
type ConflictError struct {
	Verb string // "merge" or "rebase"
	Ref  string
	Err  error
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("git %s %s failed; the repository is left in conflict for you to resolve", e.Verb, e.Ref)
}

// Unwrap exposes the underlying git error.
func (e *ConflictError) Unwrap() error { return e.Err }

// Remedies prints the exact recovery commands.
func (e *ConflictError) Remedies() string {
	return fmt.Sprintf("resolve the conflicts, then: git %s --continue (or give up with: git %s --abort), and re-run elgit", e.Verb, e.Verb)
}

// RestoreConflictError reports a stash apply that hit conflicts. The
// conflicted changes are in the working tree and the stash entry is kept.
type RestoreConflictError struct {
	Index int // -1 when the entry could not be located
	OID   string
	Err   error
}

func (e *RestoreConflictError) Error() string {
	return fmt.Sprintf("conflict while restoring stash %s; the stash entry was kept and the changes are in your working tree", shortOID(e.OID))
}

// Unwrap exposes the underlying git error.
func (e *RestoreConflictError) Unwrap() error { return e.Err }

// Remedies prints the exact recovery commands.
func (e *RestoreConflictError) Remedies() string {
	if e.Index >= 0 {
		return fmt.Sprintf("resolve the conflicts in your working tree, then drop the applied stash entry: git stash drop stash@{%d}", e.Index)
	}
	return "resolve the conflicts in your working tree, then drop the applied stash entry (find it with: git stash list)"
}

func shortOID(oid string) string {
	if len(oid) > 7 {
		return oid[:7]
	}
	return oid
}
