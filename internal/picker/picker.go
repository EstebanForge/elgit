// Package picker selects an item from a list interactively. On a terminal
// it uses charmbracelet/huh, the same select stack and keybindings as
// wicket-cli-tools: arrows to move, type to filter, enter to select, ESC
// or Ctrl-C to cancel. Without a terminal (pipes, scripts, tests) it
// falls back to a numbered prompt. Both modes share one contract: the
// chosen Name, or ErrAborted when the user backs out.
package picker

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// Item is one selectable row: Name is the value, Detail is the aligned
// hint shown next to it, for example "(remote only)".
type Item struct {
	Name   string
	Detail string
}

// ErrAborted reports a deliberate cancellation: nothing was selected.
// Mirrors wicket-cli-tools' ui.ErrCancelled over huh.ErrUserAborted.
var ErrAborted = errors.New("selection aborted")

// Pick shows items under title and returns the selected Name.
func Pick(out io.Writer, in io.Reader, title string, items []Item) (string, error) {
	if len(items) == 0 {
		return "", ErrAborted
	}
	if f, ok := in.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return pickSelect(title, items)
	}
	return pickNumbered(out, in, title, items)
}

// pickSelect runs the huh select prompt. huh renders to the terminal
// itself; out is unused here and kept for symmetry with pickNumbered.
func pickSelect(title string, items []Item) (string, error) {
	width := nameWidth(items)
	options := make([]huh.Option[string], 0, len(items))
	for _, it := range items {
		label := it.Name + strings.Repeat(" ", width-utf8.RuneCountInString(it.Name)+1) + it.Detail
		options = append(options, huh.NewOption(label, it.Name))
	}

	var selected string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(title).
				Options(options...).
				Filtering(true).
				Height(15).
				Value(&selected),
		),
	).
		WithTheme(huh.ThemeCharm()).
		WithShowHelp(false).
		WithKeyMap(cancelKeymap()).
		Run()
	if err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrAborted
		}
		return "", err
	}
	return selected, nil
}

// cancelKeymap binds quit to both Ctrl-C and ESC, matching user
// expectations (and wicket-cli-tools): ESC aborts the prompt instead of
// only clearing the filter. Backspace still clears the filter.
func cancelKeymap() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"))
	return km
}

// sayln prints one advisory line; a closed output stream must not fail
// the prompt.
func sayln(out io.Writer, a ...any) {
	_, _ = fmt.Fprintln(out, a...) //nolint:errcheck // advisory output; see doc comment
}

// sayf prints formatted advisory output. Same policy as sayln.
func sayf(out io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(out, format, a...) //nolint:errcheck // advisory output; see sayln
}

// pickNumbered lists items and reads a row number. 0, EOF, or an invalid
// answer aborts. Works over pipes, so scripts and tests can drive it.
func pickNumbered(out io.Writer, in io.Reader, title string, items []Item) (string, error) {
	sayln(out, title)
	width := nameWidth(items)
	for i, it := range items {
		pad := strings.Repeat(" ", width-utf8.RuneCountInString(it.Name)+1)
		sayf(out, " %2d) %s%s%s\n", i+1, it.Name, pad, it.Detail)
	}
	sayf(out, "Select [1-%d, 0 cancels]: ", len(items))

	line, err := bufio.NewReader(in).ReadString('\n')
	if strings.TrimSpace(line) == "" && err != nil {
		return "", ErrAborted
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(line))
	if convErr != nil || n < 0 || n > len(items) {
		return "", ErrAborted
	}
	if n == 0 {
		return "", ErrAborted
	}
	return items[n-1].Name, nil
}

func nameWidth(items []Item) int {
	width := 0
	for _, it := range items {
		if n := utf8.RuneCountInString(it.Name); n > width {
			width = n
		}
	}
	return width
}
