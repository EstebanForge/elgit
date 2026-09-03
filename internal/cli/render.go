package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/EstebanForge/elgit/internal/repo"
)

const (
	ansiReset = "\x1b[0m"
	ansiRed   = "\x1b[31;1m"
	ansiGreen = "\x1b[32;1m"
	ansiFaint = "\x1b[2m"
)

// renderBranches prints the branch table: marker, name, publication state.
// Padding is computed from the plain name length so colored output stays
// aligned.
func renderBranches(out io.Writer, branches []repo.Branch, colored bool) {
	if len(branches) == 0 {
		sayln(out, "No branches match")
		return
	}
	width := 0
	for _, b := range branches {
		if n := utf8.RuneCountInString(b.Name); n > width {
			width = n
		}
	}
	for _, b := range branches {
		marker, name := " ", b.Name
		state := b.Detail()
		if b.IsCurrent {
			marker = "*"
			if colored {
				name = ansiGreen + name + ansiReset
				marker = ansiRed + marker + ansiReset
			}
		}
		if colored {
			state = ansiFaint + state + ansiReset
		}
		// Pad by rune count so non-ASCII names keep the columns aligned.
		pad := strings.Repeat(" ", width-utf8.RuneCountInString(b.Name)+1)
		sayf(out, "%s %s%s%s\n", marker, name, pad, state)
	}
}

// statusView is the gathered state behind one status render. Every
// field degrades gracefully: status is a glance, not a validation gate.
type statusView struct {
	Branch    string // abbreviated hash when detached, branch name otherwise
	Detached  bool
	NoCommits bool
	Upstream  string // "" when no upstream is configured
	Ahead     int
	Behind    int
	Counts    repo.StatusCounts
}

// gatherStatus collects the state for renderStatus. It fails only when
// the repository answers untruthfully (status or rev-list errors): a
// glance that prints a wrong "clean" is worse than an error. Missing
// upstream and detached HEAD degrade to their own lines.
func gatherStatus(ctx context.Context, r *repo.Repo) (statusView, error) {
	var v statusView
	v.NoCommits = !r.HasCommits(ctx)
	branch, err := r.CurrentBranch(ctx)
	if err != nil {
		v.Detached = !v.NoCommits
		v.Branch = r.ShortHEAD(ctx)
	} else {
		v.Branch = branch
	}
	if !v.Detached && !v.NoCommits {
		v.Upstream = r.Upstream(ctx)
		if v.Upstream != "" {
			v.Ahead, v.Behind, err = r.AheadBehind(ctx, "HEAD", v.Upstream)
			if err != nil {
				return v, err
			}
		}
	}
	v.Counts, err = r.StatusCounts(ctx)
	if err != nil {
		return v, err
	}
	return v, nil
}

// renderStatus prints the status glance: branch, upstream distance, and
// the working tree summary. One line per concern, no table. Counts print
// in every state: an unborn repository can still hold staged files.
func renderStatus(out io.Writer, v statusView) {
	switch {
	case v.NoCommits:
		sayf(out, "On branch %s\n", v.Branch)
		sayln(out, "No commits yet")
	case v.Detached:
		sayf(out, "HEAD detached at %s\n", v.Branch)
	default:
		sayf(out, "On branch %s\n", v.Branch)
	}

	switch {
	case v.Detached || v.NoCommits:
		// No branch to track yet.
	case v.Upstream == "":
		sayln(out, "No upstream; use elgit pub to publish")
	case v.Ahead == 0 && v.Behind == 0:
		sayf(out, "Tracking %s: up to date\n", v.Upstream)
	default:
		sayf(out, "Tracking %s: %d ahead, %d behind\n", v.Upstream, v.Ahead, v.Behind)
	}

	var parts []string
	if v.Counts.Staged > 0 {
		parts = append(parts, fmt.Sprintf("%d staged", v.Counts.Staged))
	}
	if v.Counts.Modified > 0 {
		parts = append(parts, fmt.Sprintf("%d modified", v.Counts.Modified))
	}
	if v.Counts.Untracked > 0 {
		parts = append(parts, fmt.Sprintf("%d untracked", v.Counts.Untracked))
	}
	if len(parts) == 0 {
		sayln(out, "Working tree clean")
		return
	}
	sayln(out, strings.Join(parts, ", "))
}

// colorEnabled reports whether out is an interactive terminal that accepts
// color. NO_COLOR disables colors everywhere.
func colorEnabled(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
