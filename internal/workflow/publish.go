package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Publish pushes a local branch to the remote and sets upstream tracking.
func (w *Workflow) Publish(ctx context.Context, name string) error {
	if err := w.guard(ctx); err != nil {
		return err
	}
	remote, err := w.Repo.Remote(ctx)
	if err != nil {
		return err
	}

	var branch string
	if name != "" {
		matched, found := w.Repo.FuzzyMatchBranch(ctx, remote, name)
		if !found {
			current, cerr := w.Repo.CurrentBranch(ctx)
			if cerr != nil {
				return cerr
			}
			w.printf("Branch %q not found; using current branch %s.", name, current)
			branch = current
		} else {
			branch = matched
		}
	} else {
		if branch, err = w.Repo.CurrentBranch(ctx); err != nil {
			return err
		}
	}

	if w.Repo.HasRemoteBranch(ctx, remote, branch) {
		return fmt.Errorf("branch %s is already published on %s", branch, remote)
	}

	w.printf("Publishing %s.", branch)
	refspec := w.Repo.BranchRef(branch) + ":" + w.Repo.BranchRef(branch)
	if _, err := w.Repo.Git.Mutate(ctx, "push", "-u", remote, refspec); err != nil && !w.Repo.Git.FakeOK(err) {
		return fmt.Errorf("publish failed: %w", err)
	}
	return nil
}

// Unpublish deletes a branch on the remote. The local branch stays.
func (w *Workflow) Unpublish(ctx context.Context, name string) error {
	if err := w.guard(ctx); err != nil {
		return err
	}
	remote, err := w.Repo.Remote(ctx)
	if err != nil {
		return err
	}
	if name == "" {
		w.listBranches(ctx, remote)
		return errors.New("specify a branch to unpublish")
	}

	branch, found := w.Repo.FuzzyMatchBranch(ctx, remote, name)
	if !found || !w.Repo.HasRemoteBranch(ctx, remote, branch) {
		return fmt.Errorf("branch %q is not published on %s", name, remote)
	}

	if current, err := w.Repo.CurrentBranch(ctx); err == nil && current == branch {
		w.printf("Note: %s is the current branch; only the remote copy is deleted.", branch)
	}

	w.printf("Unpublishing %s.", branch)
	if _, err := w.Repo.Git.Mutate(ctx, "push", remote, "--delete", w.Repo.BranchRef(branch)); err != nil && !w.Repo.Git.FakeOK(err) {
		// Refresh remote-tracking refs so the next listing is accurate.
		if _, perr := w.Repo.Git.Mutate(ctx, "fetch", remote, "--prune"); perr != nil {
			w.printf("git fetch %s --prune also failed; remote-tracking refs may be stale.", remote)
		}
		return fmt.Errorf("unpublish failed (if the remote branch was already deleted by someone else, the fetch above cleared it): %w", err)
	}
	return nil
}

// Undo removes the last commit from the current branch. With hard it also
// discards the working tree changes of that commit.
func (w *Workflow) Undo(ctx context.Context, hard bool) error {
	if err := w.guard(ctx); err != nil {
		return err
	}
	g := w.Repo.Git

	if !g.Ok(ctx, "rev-parse", "--verify", "--quiet", "HEAD^") {
		return errors.New("the repository only contains the root commit; nothing to undo")
	}

	parents, err := g.Query(ctx, "rev-list", "--parents", "-n", "1", "HEAD")
	if err != nil {
		return err
	}
	if len(strings.Fields(parents)) > 2 {
		w.printf("Warning: HEAD is a merge commit; this removes the entire merge.")
	}

	subject, err := g.Query(ctx, "log", "-1", "--format=%h %s")
	if err != nil {
		return err
	}

	argv := []string{"reset"}
	if hard {
		argv = append(argv, "--hard")
	}
	argv = append(argv, "HEAD^")

	if _, err := g.Mutate(ctx, argv...); err != nil && !g.FakeOK(err) {
		return fmt.Errorf("undo failed: %w", err)
	}
	w.printf("Removed %s", strings.TrimSpace(subject))
	return nil
}
