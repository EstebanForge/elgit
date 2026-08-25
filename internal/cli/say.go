package cli

import (
	"fmt"
	"io"
)

// sayln prints one advisory line. A closed or broken output stream must not
// abort a command, so the write error is discarded here, once, on purpose.
func sayln(out io.Writer, a ...any) {
	_, _ = fmt.Fprintln(out, a...) //nolint:errcheck // advisory output; see doc comment
}

// sayf prints formatted advisory output. Same policy as sayln.
func sayf(out io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(out, format, a...) //nolint:errcheck // advisory output; see sayln doc comment
}
