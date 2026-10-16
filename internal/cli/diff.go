package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/EstebanForge/elgit/internal/gitx"
	"github.com/EstebanForge/elgit/internal/repo"
)

func newDiffCmd(runner func() *gitx.Runner) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:     "diff",
		Aliases: []string{"df"},
		Short:   "List uncommitted changes and show their diff",
		Long: "Lists every uncommitted change (staged, unstaged, untracked) with\n" +
			"per-file +/- counts, then streams the full diff against HEAD.\n" +
			"On a terminal the diff pages through your own git pager config,\n" +
			"so delta and core.pager apply untouched; piped output stays\n" +
			"plain. Read-only: the index is never written. Untracked file\n" +
			"contents join the diff only with --all.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDiff(cmd, runner(), all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "also show untracked file contents")
	return cmd
}

func runDiff(cmd *cobra.Command, g *gitx.Runner, all bool) error {
	ctx := cmd.Context()
	r := repo.Open(g)
	if !r.IsInsideWorkTree(ctx) {
		return errors.New("not a git repository")
	}
	out := cmd.OutOrStdout()
	changes, err := r.Changes(ctx)
	if err != nil {
		return err
	}
	if branch, berr := r.CurrentBranch(ctx); berr == nil {
		sayf(out, "On branch %s\n", branch)
	} else if head := r.ShortHEAD(ctx); head != "" {
		sayf(out, "HEAD detached at %s\n", head)
	}
	if len(changes) == 0 {
		sayln(out, "Working tree clean")
		return nil
	}
	renderChangeList(out, changes, colorEnabled(out))

	base := "HEAD"
	if !r.HasCommits(ctx) {
		// Unborn HEAD: diff against the empty tree so staged additions and
		// unstaged edits both show (--cached would drop the worktree half).
		base, err = r.EmptyTreeOID(ctx)
		if err != nil {
			return err
		}
	}
	sayln(out, "")
	if err := streamDiff(ctx, g, out, "--no-optional-locks", "diff", base); err != nil {
		return err
	}

	untracked := untrackedChanges(changes)
	if len(untracked) == 0 {
		return nil
	}
	if !all {
		sayf(out, "\n%s\n", maybeFaint(colorEnabled(out), "Untracked content hidden; use elgit diff --all"))
		return nil
	}
	files, err := r.UntrackedFiles(ctx)
	if err != nil {
		return err
	}
	for _, f := range files {
		sayln(out, "")
		// --no-pager keeps each per-file block from spawning its own pager
		// session after the main diff's pager has already exited.
		if err := streamDiff(ctx, g, out, "--no-pager", "--no-optional-locks", "diff", "--no-index", "--", os.DevNull, f); err != nil {
			return err
		}
	}
	return nil
}

// streamDiff lets one diff flow to w untouched. git diff --no-index
// exits 1 to report differences; that is the expected result for an
// untracked file, and only when stderr is empty — a real error exits 1
// with a message.
func streamDiff(ctx context.Context, g *gitx.Runner, w io.Writer, argv ...string) error {
	err := g.Stream(ctx, w, argv...)
	var gErr *gitx.Error
	if errors.As(err, &gErr) && gErr.ExitCode == 1 && gErr.Stderr == "" && slices.Contains(argv, "--no-index") {
		return nil
	}
	return err
}

// untrackedChanges keeps the untracked entries of the change list.
func untrackedChanges(changes []repo.FileChange) []repo.FileChange {
	var untracked []repo.FileChange
	for _, c := range changes {
		if c.Code == "??" {
			untracked = append(untracked, c)
		}
	}
	return untracked
}

// renderChangeList prints one line per change: porcelain letters, path,
// then diff size. Paths pad to a shared column so counts stay readable
// in a scan.
func renderChangeList(out io.Writer, changes []repo.FileChange, colored bool) {
	width := 0
	for _, c := range changes {
		if n := utf8.RuneCountInString(c.Path); n > width {
			width = n
		}
	}
	for _, c := range changes {
		pad := strings.Repeat(" ", width-utf8.RuneCountInString(c.Path)+2)
		sayf(out, "%s %s%s%s\n", c.Code, c.Path, pad, changeCounts(c, colored))
	}
}

func changeCounts(c repo.FileChange, colored bool) string {
	text := ""
	switch {
	case c.Code == "??":
		text = "untracked"
	case c.Binary:
		text = "binary"
	case c.Counted && (c.Added > 0 || c.Deleted > 0):
		text = fmt.Sprintf("+%d -%d", c.Added, c.Deleted)
	}
	if !colored {
		return text
	}
	if c.Code == "??" || c.Binary {
		return ansiFaint + text + ansiReset
	}
	return colorCounts(text)
}

// colorCounts paints each +N and -N token separately so a -N token
// never inherits the + color.
func colorCounts(text string) string {
	var b strings.Builder
	for _, part := range strings.Split(text, " ") {
		if strings.HasPrefix(part, "+") {
			b.WriteString(ansiGreen + part + ansiReset + " ")
			continue
		}
		if strings.HasPrefix(part, "-") {
			b.WriteString(ansiRed + part + ansiReset + " ")
			continue
		}
		b.WriteString(part + " ")
	}
	return strings.TrimRight(b.String(), " ")
}

func maybeFaint(colored bool, text string) string {
	if !colored {
		return text
	}
	return ansiFaint + text + ansiReset
}
