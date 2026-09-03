package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/EstebanForge/elgit/internal/picker"
)

// promptCommitMessage asks for the commit summary (required) and the
// longer description (optional) on a terminal. Callers prefill
// *subject and *description before the call (amend prefills the last
// message). Without a terminal it returns an error pointing at -m, so
// scripts never stall: scripted calls pass -m.
func promptCommitMessage(cmd *cobra.Command, subject, description *string) error {
	f, ok := cmd.InOrStdin().(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return errors.New("no message given: pass -m or run elgit commit from a terminal")
	}
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Summary").
				Description("Required, one line").
				Value(subject).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return errors.New("the summary is required")
					}
					return nil
				}),
			huh.NewInput().
				Title("Description").
				Description("Optional, longer body").
				Value(description),
		),
	).WithTheme(huh.ThemeCharm()).WithShowHelp(false).WithKeyMap(picker.CancelKeymap()).Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return picker.ErrAborted
	}
	if err != nil {
		return fmt.Errorf("message prompt: %w", err)
	}
	return nil
}
