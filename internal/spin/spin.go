// Package spin shows an animated indicator with a message while one
// blocking call runs, so a slow remote fetch never looks like a hang.
// Without a terminal it degrades to a single plain line: piped output
// and CI logs stay readable.
package spin

import (
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/term"
)

// dotFrames are the braille spinner frames, one per tick.
var dotFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Run shows msg with an animated spinner until fn returns, then returns
// fn's result verbatim. fn runs exactly once. The caller owns the error
// messaging; Run only animates.
func Run(out io.Writer, msg string, fn func() error) error {
	f, terminal := out.(*os.File)
	if !terminal || !term.IsTerminal(int(f.Fd())) {
		// Plain line: a log reader sees the intent, no escape codes.
		fmt.Fprintln(out, msg) //nolint:errcheck // advisory output; see picker.sayln
		return fn()
	}

	// The work runs once, in its own goroutine; the loop only paints.
	// Ctrl-C kills the process as usual, so no signal handling here.
	done := make(chan error, 1)
	go func() { done <- fn() }()

	paint := func(frame string) {
		fmt.Fprintf(f, "\r%s %s", frame, msg) //nolint:errcheck // advisory output
	}
	paint(dotFrames[0])

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for i := 0; ; i = (i + 1) % len(dotFrames) {
		select {
		case err := <-done:
			fmt.Fprint(f, "\r\x1b[K") //nolint:errcheck // clear the painted line
			return err
		case <-ticker.C:
			paint(dotFrames[i])
		}
	}
}
