package cli

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/EstebanForge/elgit/internal/gitx"
	"github.com/EstebanForge/elgit/internal/picker"
	"github.com/EstebanForge/elgit/internal/repo"
	"github.com/EstebanForge/elgit/internal/workflow"
)

func newSwitchCmd(runner func() *gitx.Runner) *cobra.Command {
	return &cobra.Command{
		Use:     "sw [branch]",
		Aliases: []string{"switch"},
		Short:   "Switch branches, stashing and restoring local changes",
		Long: "With a branch name: switch there directly (unique prefix match allowed).\n" +
			"Without: pick a branch from a filterable list; remote-only branches are\n" +
			"offered too (marked) and are created locally with tracking on switch.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withLock(cmd, runner(), func(ctx context.Context, w *workflow.Workflow) error {
				name := ""
				if len(args) == 1 {
					name = args[0]
				}
				if name == "" {
					return switchInteractive(ctx, cmd, w)
				}
				return w.Switch(ctx, name)
			})
		},
	}
}

// switchInteractive resolves a switch target from the branch list: huh
// picker on a terminal, numbered prompt otherwise. Remote-only branches
// are offered with a marker; switching creates the local branch (git's
// checkout sets up tracking). A fetch --prune refreshes the remote view
// first; offline it degrades to the last-known refs.
func switchInteractive(ctx context.Context, cmd *cobra.Command, w *workflow.Workflow) error {
	r := w.Repo
	remote, err := r.Remote(ctx)
	if err != nil {
		remote = ""
	}

	if remote != "" && !r.Git.Fake {
		if _, ferr := r.Git.Mutate(ctx, "fetch", remote, "--prune"); ferr != nil {
			sayln(cmd.ErrOrStderr(), "fetch failed; showing last-known remote branches")
		}
	}

	branches, err := r.Branches(ctx, remote)
	if err != nil {
		return err
	}
	current, err := r.CurrentBranch(ctx)
	if err != nil {
		return err
	}

	items := make([]picker.Item, 0, len(branches))
	for _, b := range branches {
		if b.Name == current {
			continue
		}
		items = append(items, picker.Item{Name: b.Name, Detail: b.Detail()})
	}
	if len(items) == 0 {
		sayln(cmd.OutOrStdout(), "No other branches available.")
		return nil
	}

	selected, err := picker.Pick(cmd.OutOrStdout(), cmd.InOrStdin(),
		"Switch to branch (type to filter or use arrows)", items)
	if errors.Is(err, picker.ErrAborted) {
		return nil
	}
	if err != nil {
		return err
	}
	return w.Switch(ctx, selected)
}

func newSyncCmd(runner func() *gitx.Runner) *cobra.Command {
	return &cobra.Command{
		Use:     "sync [branch]",
		Aliases: []string{"sy"},
		Short:   "Stash, fetch, merge or rebase, push, restore",
		Long: "Synchronizes a branch with its remote: stashes uncommitted work, fetches,\n" +
			"integrates remote commits (rebase for linear history, merge when the local\n" +
			"side has merge commits), pushes, and restores the stash.\n" +
			"With a branch argument it switches there first and returns afterwards.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			branch := ""
			if len(args) == 1 {
				branch = args[0]
			}
			return withLock(cmd, runner(), func(ctx context.Context, w *workflow.Workflow) error {
				return w.Sync(ctx, branch)
			})
		},
	}
}

func newPublishCmd(runner func() *gitx.Runner) *cobra.Command {
	return &cobra.Command{
		Use:     "publish [branch]",
		Aliases: []string{"pub"},
		Short:   "Push a branch to the remote and set upstream",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			branch := ""
			if len(args) == 1 {
				branch = args[0]
			}
			return withLock(cmd, runner(), func(ctx context.Context, w *workflow.Workflow) error {
				return w.Publish(ctx, branch)
			})
		},
	}
}

func newUnpublishCmd(runner func() *gitx.Runner) *cobra.Command {
	return &cobra.Command{
		Use:     "unpublish <branch>",
		Aliases: []string{"unpub"},
		Short:   "Delete a branch on the remote",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withLock(cmd, runner(), func(ctx context.Context, w *workflow.Workflow) error {
				return w.Unpublish(ctx, args[0])
			})
		},
	}
}

func newUndoCmd(runner func() *gitx.Runner) *cobra.Command {
	var hard bool
	cmd := &cobra.Command{
		Use:   "undo",
		Short: "Remove the last commit from the branch",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withLock(cmd, runner(), func(ctx context.Context, w *workflow.Workflow) error {
				return w.Undo(ctx, hard)
			})
		},
	}
	cmd.Flags().BoolVar(&hard, "hard", false, "also discard the working tree changes of that commit")
	return cmd
}

// matchPattern implements fnmatch-style matching where * and ? also cross
// "/", matching legacy legit (Python fnmatch) behavior: "feature/*" hits
// "feature/a/b" too. The pattern compiles once per call site, not once
// per branch.
func matchPattern(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

func newBranchesCmd(runner func() *gitx.Runner) *cobra.Command {
	return &cobra.Command{
		Use:   "branches [pattern]",
		Short: "List local and remote branches",
		Long: "Lists branches with their publication state. A branch shows as published\n" +
			"when its remote counterpart exists. An optional fnmatch pattern filters\n" +
			"the list, for example: elgit branches 'feature/*'",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			r := repo.Open(runner())
			if !r.IsInsideWorkTree(ctx) {
				return errors.New("not a git repository")
			}

			remote, err := r.Remote(ctx)
			if err != nil {
				remote = "" // no remote configured: list local branches only
			}
			branches, err := r.Branches(ctx, remote)
			if err != nil {
				return err
			}

			pattern := "*"
			if len(args) == 1 {
				pattern = args[0]
			}
			re, perr := matchPattern(pattern)
			if perr != nil {
				return fmt.Errorf("invalid pattern %q: %w", pattern, perr)
			}
			matched := make([]repo.Branch, 0, len(branches))
			for _, b := range branches {
				if re.MatchString(b.Name) {
					matched = append(matched, b)
				}
			}

			out := cmd.OutOrStdout()
			renderBranches(out, matched, colorEnabled(out))
			return nil
		},
	}
}
