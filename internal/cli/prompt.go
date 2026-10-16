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
	"github.com/EstebanForge/elgit/internal/workflow"
)

// stdinIsTerminal reports whether the command's stdin is an interactive
// terminal. Destructive interactive flows refuse to run without one.
func stdinIsTerminal(cmd *cobra.Command) bool {
	f, ok := cmd.InOrStdin().(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

// askPrefer asks which side wins colliding hunks when a sync finds both
// the remote and the local branch moved. Without a terminal it returns
// PreferNone so scripts never stall: they get today's stop-on-conflict.
func askPrefer(cmd *cobra.Command, remote, branch string, remoteAhead, localAhead int) (workflow.Prefer, error) {
	if !stdinIsTerminal(cmd) {
		return workflow.PreferNone, nil
	}
	var choice string
	err := huh.NewSelect[string]().
		Title(fmt.Sprintf("%s/%s moved ahead %d; you have %d local", remote, branch, remoteAhead, localAhead)).
		Description("Which side wins if the integration conflicts?").
		Options(
			huh.NewOption("Prefer remote", string(workflow.PreferRemote)),
			huh.NewOption("Prefer local", string(workflow.PreferLocal)),
			huh.NewOption("Resolve manually", string(workflow.PreferNone)),
		).
		Value(&choice).
		WithTheme(huh.ThemeCharm()).WithKeyMap(picker.CancelKeymap()).
		Run()
	if errors.Is(err, huh.ErrUserAborted) {
		return workflow.PreferNone, picker.ErrAborted
	}
	if err != nil {
		return workflow.PreferNone, fmt.Errorf("strategy prompt: %w", err)
	}
	return workflow.Prefer(choice), nil
}

// promptCommitMessage asks for the commit summary (required) and the
// longer description (optional) on a terminal. Callers prefill
// *subject and *description before the call (amend prefills the last
// message). Without a terminal it returns an error pointing at -m, so
// scripts never stall: scripted calls pass -m.
func promptCommitMessage(cmd *cobra.Command, subject, description *string) error {
	if !stdinIsTerminal(cmd) {
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
