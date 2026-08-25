package workflow

import (
	"context"
	"fmt"
)

// Switch checks out a branch, stashing local changes on the way out and
// restoring the stash that belongs to the destination branch.
func (w *Workflow) Switch(ctx context.Context, target string) error {
	if err := w.guard(ctx); err != nil {
		return err
	}
	remote := w.remoteName(ctx)
	current, err := w.Repo.CurrentBranch(ctx)
	if err != nil {
		return err
	}

	matched, found := w.Repo.FuzzyMatchBranch(ctx, remote, target)
	if !found {
		w.listBranches(ctx, remote)
		return fmt.Errorf("branch %q does not exist; pick one from the list above", target)
	}
	if matched == current {
		w.printf("Already on %s.", matched)
		return nil
	}

	led, err := loadLedger(ctx, w)
	if err != nil {
		return err
	}
	stashed, hadStash, err := w.stashAway(ctx, led, current, purposeSwitch)
	if err != nil {
		return err
	}

	w.printf("Switching to %s.", matched)
	// --end-of-options stops option parsing: a branch named like an option
	// ("-f", "--mirror") is checked out as a branch, never executed as a flag.
	if _, err := w.Repo.Git.Mutate(ctx, "checkout", "--end-of-options", matched); err != nil && !w.Repo.Git.FakeOK(err) {
		if hadStash {
			return fmt.Errorf("checkout failed; your changes are safe in stash@{0} (%s): %w",
				shortOID(stashed.StashOID), err)
		}
		return err
	}
	return w.restore(ctx, led, matched, purposeSwitch)
}
