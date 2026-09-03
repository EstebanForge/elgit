// Package cli wires the elgit command tree: user-facing flags, locking,
// and output rendering on top of the workflow layer.
package cli

import (
	"context"
	"errors"

	"github.com/spf13/cobra"

	"github.com/EstebanForge/elgit/internal/gitx"
	"github.com/EstebanForge/elgit/internal/repo"
	"github.com/EstebanForge/elgit/internal/safety"
	"github.com/EstebanForge/elgit/internal/workflow"
)

// Version is reported by --version.
var Version = "0.2.0"

// NewRootCmd builds the full command tree.
func NewRootCmd() *cobra.Command {
	var fake, verbose, install, uninstall bool

	root := &cobra.Command{
		Use:   "elgit",
		Short: "Git for humans, safely.",
		Long: "elgit wraps everyday git workflows: switch branches without losing\n" +
			"uncommitted work, create commits, merge other branches, sync with\n" +
			"the remote, publish and unpublish branches, undo the last commit,\n" +
			"list branches, glance at the repository state.",
		Version:      Version,
		SilenceUsage: true,
	}

	root.PersistentFlags().BoolVar(&fake, "fake", false, "show mutating git commands without running them")
	root.PersistentFlags().BoolVar(&verbose, "verbose", false, "echo every git command before it runs")
	root.Flags().BoolVar(&install, "install", false, "install elgit subcommands as git aliases")
	root.Flags().BoolVar(&uninstall, "uninstall", false, "remove elgit and legacy legit git aliases")

	// --end-of-options (checkout hardening) requires git 2.24; stash push
	// requires 2.13. Gate on the newer of the two.
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if !atLeastGit(cmd.Context(), &gitx.Runner{}, 2, 24) {
			return errors.New("elgit requires git 2.24 or newer on PATH")
		}
		return nil
	}

	runner := func() *gitx.Runner {
		return &gitx.Runner{Fake: fake, Verbose: verbose}
	}

	root.RunE = func(cmd *cobra.Command, _ []string) error {
		switch {
		case install:
			return runAliasInstall(cmd, runner())
		case uninstall:
			return runAliasUninstall(cmd, runner())
		default:
			return cmd.Help()
		}
	}

	root.AddCommand(
		newSwitchCmd(runner),
		newSyncCmd(runner),
		newCommitCmd(runner),
		newMergeCmd(runner),
		newPublishCmd(runner),
		newUnpublishCmd(runner),
		newUndoCmd(runner),
		newBranchesCmd(runner),
		newStatusCmd(runner),
		newConfigCmd(runner),
	)
	return root
}

// withLock guards a mutating command with the repository lock and warns
// about unrestored stashes from previous runs. In fake mode nothing is
// written: no lock file, no ledger update.
func withLock(cmd *cobra.Command, g *gitx.Runner, fn func(ctx context.Context, w *workflow.Workflow) error) error {
	ctx := cmd.Context()
	r := repo.Open(g)
	if !r.IsInsideWorkTree(ctx) {
		return errors.New("not a git repository")
	}

	if !g.Fake {
		lock, err := safety.Acquire(ctx, g)
		if err != nil {
			return err
		}
		defer func() { _ = lock.Release() }() //nolint:errcheck // best-effort release; a stale lock is taken over later
	}

	led, err := safety.LoadLedger(ctx, g)
	if err != nil {
		return err
	}
	if branch, berr := r.CurrentBranch(ctx); berr == nil {
		led.WarnPending(cmd.ErrOrStderr(), branch)
	}

	w := workflow.New(r, cmd.OutOrStdout())
	err = fn(ctx, w)
	printRemedies(cmd, err)
	return err
}

// printRemedies prints recovery hints for known workflow failures.
func printRemedies(cmd *cobra.Command, err error) {
	var conflict *workflow.ConflictError
	if errors.As(err, &conflict) {
		sayln(cmd.ErrOrStderr(), conflict.Remedies())
		return
	}
	var restore *workflow.RestoreConflictError
	if errors.As(err, &restore) {
		sayln(cmd.ErrOrStderr(), restore.Remedies())
	}
}
