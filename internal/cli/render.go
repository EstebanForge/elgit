package cli

import (
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
