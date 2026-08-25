package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/EstebanForge/elgit/internal/gitx"
	"github.com/EstebanForge/elgit/internal/safety"
)

func loadLedger(ctx context.Context, w *Workflow) (*safety.Ledger, error) {
	return safety.LoadLedger(ctx, w.Repo.Git)
}

// syncState carries what a sync run stashed, so failures can restore it or
// point the user at it precisely.
type syncState struct {
	led      *safety.Ledger
	branch   string
	stashed  safety.Entry
	hadStash bool
	// parked is a stash created by the external-branch switch that brought
	// us here; it belongs to the original branch and must not be forgotten.
	parked    safety.Entry
	hasParked bool
}

// Sync synchronizes a branch with its remote: stash, fetch, integrate
// (merge or rebase), push, restore. With a target branch it switches
// there first and returns to the original branch afterwards.
func (w *Workflow) Sync(ctx context.Context, target string) error {
	if err := w.guard(ctx); err != nil {
		return err
	}
	remote, err := w.Repo.Remote(ctx)
	if err != nil {
		return err
	}
	current, err := w.Repo.CurrentBranch(ctx)
	if err != nil {
		return err
	}

	branch := current
	external := false
	if target != "" {
		matched, found := w.Repo.FuzzyMatchBranch(ctx, remote, target)
		if !found {
			return fmt.Errorf("branch %q does not exist", target)
		}
		if matched != current {
			branch = matched
			external = true
		}
	}

	if !w.Repo.HasRemoteBranch(ctx, remote, branch) {
		return fmt.Errorf("branch %s is not published; publish it first: elgit publish %s", branch, branch)
	}

	led, err := loadLedger(ctx, w)
	if err != nil {
		return err
	}

	state := &syncState{led: led, branch: branch}
	if external {
		if err := w.Switch(ctx, branch); err != nil {
			return err
		}
		// Reload: Switch recorded its own stash in the same ledger file.
		if led, err = loadLedger(ctx, w); err != nil {
			return err
		}
		state.led = led
		state.parked, state.hasParked = led.Find(current, purposeSwitch)
	}

	stashed, hadStash, err := w.stashAway(ctx, led, branch, purposeSync)
	if err != nil {
		return err
	}
	state.stashed, state.hadStash = stashed, hadStash

	w.printf("Fetching %s.", remote)
	if _, err := w.Repo.Git.Mutate(ctx, "fetch", remote); err != nil && !w.Repo.Git.FakeOK(err) {
		return w.syncFail(ctx, state, true, err)
	}

	remoteRef := w.Repo.RemoteRef(remote, branch)
	if _, err := w.integrate(ctx, remoteRef); err != nil {
		var conflict *ConflictError
		if errors.As(err, &conflict) {
			// The tree is conflicted; restoring the stash on top would
			// tangle two states. Keep the stash, point at it, stop.
			return w.syncFail(ctx, state, false, err)
		}
		return w.syncFail(ctx, state, true, err)
	}

	if err := w.maybePush(ctx, remote, branch, remoteRef); err != nil {
		return w.syncFail(ctx, state, true, err)
	}

	if err := w.restore(ctx, led, branch, purposeSync); err != nil {
		return err
	}

	if external {
		if err := w.Switch(ctx, current); err != nil {
			return err
		}
	}
	return nil
}

// syncFail reports a failed sync. With tryRestore the tree is still clean,
// so the stashed work is restored first and the user ends where they
// started. Without it (conflict) the stash is kept off the conflicted tree
// and the error names exactly where it lives. A stash parked by an
// external-branch switch is always named so it cannot be forgotten.
func (w *Workflow) syncFail(ctx context.Context, s *syncState, tryRestore bool, err error) error {
	switch {
	case tryRestore:
		if rerr := w.restore(ctx, s.led, s.branch, purposeSync); rerr != nil {
			err = fmt.Errorf("%w; the stash could not be restored automatically: %v", err, rerr)
		}
	case s.hadStash:
		err = fmt.Errorf("sync failed; stashed changes are safe (stash commit %s, see git stash list); fix the cause and re-run: %w",
			shortOID(s.stashed.StashOID), err)
	}
	if s.hasParked {
		err = fmt.Errorf("%w; a stash parked for branch %q is waiting (%s, see git stash list)",
			err, s.parked.Branch, shortOID(s.parked.StashOID))
	}
	return err
}

// integrate brings remoteRef into HEAD when HEAD is behind. Verb choice
// keeps the legacy legit heuristic: merge when the local side of the range
// contains merge commits, else rebase. legit.smartMerge=false defers to
// pull.rebase and pull.ff instead. remoteRef must be a fully qualified ref
// (refs/remotes/<remote>/<branch>) so branch names can never be read as
// git options.
func (w *Workflow) integrate(ctx context.Context, remoteRef string) (bool, error) {
	g := w.Repo.Git

	if g.Ok(ctx, "merge-base", "--is-ancestor", remoteRef, "HEAD") {
		return false, nil // already up to date; skip the merge entirely
	}

	merges, err := g.Query(ctx, "log", "--merges", "--format=%H", remoteRef+"..HEAD")
	if err != nil {
		return false, err
	}

	verb := "merge"
	switch {
	case w.Repo.ConfigBool(ctx, "legit.smartMerge", true):
		if strings.TrimSpace(merges) == "" {
			verb = "rebase"
		}
	case w.pullRebaseWanted(ctx):
		verb = "rebase"
	}

	w.printf("Integrating %s (%s).", remoteRef, verb)
	argv := []string{verb}
	if verb == "merge" && w.Repo.ConfigString(ctx, "pull.ff") == "only" {
		argv = append(argv, "--ff-only")
	}
	argv = append(argv, remoteRef)

	if _, err := g.Mutate(ctx, argv...); err != nil {
		if errors.Is(err, gitx.ErrFakeMutation) {
			return true, nil
		}
		var gErr *gitx.Error
		if errors.As(err, &gErr) {
			return true, &ConflictError{Verb: verb, Ref: remoteRef, Err: gErr}
		}
		return true, err
	}
	return true, nil
}

// pullRebaseWanted honors pull.rebase values that mean "rebase", including
// the word forms merges and interactive that a boolean-only read misses.
func (w *Workflow) pullRebaseWanted(ctx context.Context) bool {
	switch strings.ToLower(w.Repo.ConfigString(ctx, "pull.rebase")) {
	case "true", "1", "yes", "on", "merges", "interactive", "preserve":
		return true
	}
	return false
}

// maybePush pushes only when local commits are missing on the remote,
// saving the network round-trip when there is nothing to send. The refspec
// is fully qualified so branch names can never be read as git options.
// elgit never force-pushes.
func (w *Workflow) maybePush(ctx context.Context, remote, branch, remoteRef string) error {
	g := w.Repo.Git

	out, err := g.Query(ctx, "rev-list", "--count", remoteRef+"..HEAD")
	if err != nil {
		return fmt.Errorf("cannot count unpushed commits: %w", err)
	}
	if strings.TrimSpace(out) == "0" {
		return nil
	}

	w.printf("Pushing commits to %s.", remote)
	refspec := w.Repo.BranchRef(branch) + ":" + w.Repo.BranchRef(branch)
	if _, err := g.Mutate(ctx, "push", remote, refspec); err != nil {
		if errors.Is(err, gitx.ErrFakeMutation) {
			return nil
		}
		return fmt.Errorf("push rejected (elgit never force-pushes); fetch and re-run elgit sync: %w", err)
	}
	return nil
}
