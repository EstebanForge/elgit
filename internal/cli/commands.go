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

func newCommitCmd(runner func() *gitx.Runner) *cobra.Command {
	var msg, desc string
	var all, amend bool
	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Stage all modified tracked files and create one commit",
		Long: "Stages every modified tracked file and creates one commit.\n" +
			"Untracked files are included only with --all; a message is asked\n" +
			"for interactively when -m is absent on a terminal.\n" +
			"Commit never pushes: run elgit sync to integrate and push, or\n" +
			"elgit pub to publish a new branch.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withLock(cmd, runner(), func(ctx context.Context, w *workflow.Workflow) error {
				branch, err := w.Repo.CurrentBranch(ctx)
				if err != nil {
					return err
				}
				if strings.TrimSpace(msg) == "" {
					// Gate before prompting: a terminal user must not fill in
					// the form only to hear there is nothing to commit, and a
					// clean tree must exit 0 without a message, not error.
					ok, err := w.CommitGate(ctx, workflow.CommitRequest{All: all, Amend: amend})
					if err != nil || !ok {
						return err
					}
					var subject, body string
					if amend {
						s, b, err := lastCommitMessage(ctx, w)
						if err != nil {
							return err
						}
						subject, body = s, b
					}
					if err := promptCommitMessage(cmd, &subject, &body); err != nil {
						if errors.Is(err, picker.ErrAborted) {
							return nil // user backed out: nothing written
						}
						return err
					}
					msg, desc = strings.TrimSpace(subject), body
				}
				res, err := w.Commit(ctx, workflow.CommitRequest{
					Subject:     msg,
					Description: desc,
					All:         all,
					Amend:       amend,
				})
				if err != nil {
					return err
				}
				if res.Committed {
					switch {
					case res.SHA != "" && res.Amended:
						sayf(cmd.OutOrStdout(), "Amended %s: %s\n", branch, res.SHA)
					case res.SHA != "":
						sayf(cmd.OutOrStdout(), "Committed to %s: %s\n", branch, res.SHA)
					case res.Amended:
						sayf(cmd.OutOrStdout(), "Would amend %s (fake).\n", branch)
					default:
						sayf(cmd.OutOrStdout(), "Would commit to %s (fake).\n", branch)
					}
					if res.LeftDirty > 0 {
						sayf(cmd.OutOrStdout(), "Note: hooks left %d tracked file(s) modified; review before staging again.\n", res.LeftDirty)
					}
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVarP(&msg, "message", "m", "", "commit summary (first line)")
	cmd.Flags().StringVarP(&desc, "description", "d", "", "optional longer description")
	cmd.Flags().BoolVar(&all, "all", false, "also stage untracked files")
	cmd.Flags().BoolVar(&amend, "amend", false, "amend the last commit instead of adding one")
	return cmd
}

// lastCommitMessage returns the current message split into subject and
// body so the amend prompt can prefill it.
func lastCommitMessage(ctx context.Context, w *workflow.Workflow) (string, string, error) {
	out, err := w.Repo.Git.Query(ctx, "log", "-1", "--pretty=%B")
	if err != nil {
		return "", "", err
	}
	subject, body, _ := strings.Cut(strings.TrimSpace(out), "\n")
	return subject, strings.TrimSpace(body), nil
}

func newStatusCmd(runner func() *gitx.Runner) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Aliases: []string{"st"},
		Short:   "Show branch, upstream distance, and working tree summary",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			r := repo.Open(runner())
			if !r.IsInsideWorkTree(ctx) {
				return errors.New("not a git repository")
			}
			view, err := gatherStatus(ctx, r)
			if err != nil {
				return err
			}
			renderStatus(cmd.OutOrStdout(), view)
			return nil
		},
	}
}

func newMergeCmd(runner func() *gitx.Runner) *cobra.Command {
	return &cobra.Command{
		Use:   "merge [branch]",
		Short: "Merge another branch into the current branch",
		Long: "Shows how far the candidate branch is ahead of the current one,\n" +
			"then merges it with a plain git merge: your merge.ff config decides\n" +
			"fast-forward versus merge commit. Never pushes: run elgit sync\n" +
			"afterwards. The working tree must be clean.\n" +
			"Without an argument the branch is picked from a filterable list\n" +
			"with ahead/behind counts; remote-only branches (marked) are merged\n" +
			"from their remote-tracking ref.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return withLock(cmd, runner(), func(ctx context.Context, w *workflow.Workflow) error {
				r := w.Repo
				// Preflight before anything interactive or remote: a dirty
				// tree or mid-operation repo must stop the run before the
				// picker prompts or the network is touched.
				if err := w.MergePreflight(ctx); err != nil {
					return err
				}
				remote, rerr := r.Remote(ctx)
				if rerr != nil {
					remote = "" // local-only repo: local merges still work
				}

				if name == "" {
					picked, perr := mergePick(ctx, cmd, w, remote)
					if perr != nil {
						return perr
					}
					if picked == "" {
						return nil // user aborted the picker
					}
					name = picked
				}

				ref, display, isRemote, err := resolveMergeRef(ctx, r, remote, name)
				if err != nil {
					return err
				}

				current, err := r.CurrentBranch(ctx)
				if err != nil {
					return err
				}
				if display == current {
					return fmt.Errorf("branch %q is already checked out; there is nothing to merge into itself", display)
				}

				// A remote-only candidate is merged from its tracking ref;
				// without a fresh fetch the counts would lie. Offline, fail
				// instead of guessing. The refspec uses the resolved display
				// name: a prefix like "fea" is not a remote ref.
				if isRemote && remote != "" && !r.Git.Fake {
					spec := "refs/heads/" + display + ":refs/remotes/" + remote + "/" + display
					if _, ferr := r.Git.Mutate(ctx, "fetch", remote, spec); ferr != nil {
						return fmt.Errorf("cannot fetch %s from %s; a remote-only branch needs the network: %w", display, remote, ferr)
					}
				}
				return w.Merge(ctx, workflow.MergeRequest{Ref: ref, Display: display})
			})
		},
	}
}

// mergePick opens the branch picker with ahead/behind counts and returns
// the chosen branch name, "" when the user aborts. Counts need git 2.29+;
// older git lists branches without them.
func mergePick(ctx context.Context, cmd *cobra.Command, w *workflow.Workflow, remote string) (string, error) {
	r := w.Repo
	if remote != "" && !r.Git.Fake {
		if _, err := r.Git.Mutate(ctx, "fetch", remote, "--prune"); err != nil {
			sayln(cmd.ErrOrStderr(), "fetch failed; showing last-known remote branches")
		}
	}
	branches, err := r.Branches(ctx, remote)
	if err != nil {
		return "", err
	}
	current, err := r.CurrentBranch(ctx)
	if err != nil {
		return "", err
	}

	var counts map[string][2]int
	if atLeastGit(ctx, r.Git, 2, 29) {
		if counts, err = r.AheadBehindAll(ctx, remote); err != nil {
			return "", err
		}
	}

	items := make([]picker.Item, 0, len(branches))
	for _, b := range branches {
		if b.IsCurrent || b.Name == current {
			continue
		}
		detail := b.Detail()
		if counts != nil {
			refname := r.BranchRef(b.Name)
			if !b.IsLocal {
				refname = r.RemoteRef(remote, b.Name)
			}
			if ab, ok := counts[refname]; ok {
				detail = fmt.Sprintf("%s, %d ahead, %d behind", detail, ab[0], ab[1])
			}
		}
		items = append(items, picker.Item{Name: b.Name, Detail: detail})
	}
	if len(items) == 0 {
		sayln(cmd.OutOrStdout(), "No other branches to merge.")
		return "", nil
	}
	return picker.Pick(cmd.OutOrStdout(), cmd.InOrStdin(),
		"Merge into "+current+" (type to filter or use arrows)", items)
}

// resolveMergeRef maps a branch name to a fully qualified ref: a local
// branch wins, then the remote-tracking ref. A unique prefix resolves
// like elgit sw. isRemote marks candidates merged from the tracking ref.
func resolveMergeRef(ctx context.Context, r *repo.Repo, remote, name string) (ref, display string, isRemote bool, err error) {
	if ref, disp, isRemote, ok := resolveExactMergeRef(ctx, r, remote, name); ok {
		return ref, disp, isRemote, nil
	}
	matched, found := r.FuzzyMatchBranch(ctx, remote, name)
	if !found {
		return "", "", false, fmt.Errorf("branch %q does not exist", name)
	}
	if ref, disp, isRemote, ok := resolveExactMergeRef(ctx, r, remote, matched); ok {
		return ref, disp, isRemote, nil
	}
	return "", "", false, fmt.Errorf("branch %q does not exist", name)
}

func resolveExactMergeRef(ctx context.Context, r *repo.Repo, remote, name string) (ref, display string, isRemote bool, ok bool) {
	if r.Git.Ok(ctx, "rev-parse", "--verify", "--quiet", r.BranchRef(name)) {
		return r.BranchRef(name), name, false, true
	}
	if remote != "" && r.HasRemoteBranch(ctx, remote, name) {
		return r.RemoteRef(remote, name), name, true, true
	}
	return "", "", false, false
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
