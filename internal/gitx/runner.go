// Package gitx executes git commands as subprocesses.
//
// Every git interaction in elgit goes through Runner. Commands run with
// argv arrays only: no shell, no string interpolation into a command line.
// GIT_TERMINAL_PROMPT=0 disables interactive credential prompts so a
// missing credential fails fast instead of hanging the workflow.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// Runner executes git commands in one repository.
type Runner struct {
	// Git is the git binary. Empty means "git".
	Git string
	// Dir is the repository working tree. Empty means the current directory.
	Dir string
	// Fake suppresses mutating commands: they are echoed, not executed.
	// Read-only queries still execute, so dry-runs see real repository state.
	Fake bool
	// Verbose echoes every command line before execution.
	Verbose bool
	// Echo receives verbose and fake output. Defaults to os.Stderr.
	Echo io.Writer
}

// Error is a failed git command. It carries the full argv, the process
// exit code, and stderr, so callers can report exactly what went wrong.
type Error struct {
	Argv     []string
	ExitCode int
	Stderr   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("git %s failed (exit %d): %s",
		strings.Join(e.Argv, " "), e.ExitCode, strings.TrimSpace(e.Stderr))
}

// ErrFakeMutation is returned by Mutate in fake mode instead of a real
// result, wrapped so callers can detect simulated runs when needed.
var ErrFakeMutation = errors.New("mutation suppressed: fake mode")

// Query runs a read-only command and returns stdout.
// Queries execute even in fake mode; reading repository state is safe.
func (r *Runner) Query(ctx context.Context, argv ...string) (string, error) {
	return r.run(ctx, argv)
}

// Mutate runs a state-changing command and returns stdout.
// In fake mode the command is echoed and ErrFakeMutation is returned
// wrapped in a fake-success sentinel so normal flows continue.
func (r *Runner) Mutate(ctx context.Context, argv ...string) (string, error) {
	if r.Fake {
		r.echo("FAKE", argv)
		return "", ErrFakeMutation
	}
	return r.run(ctx, argv)
}

// FakeOK reports whether a Mutate call would proceed.
// True means the command ran (or would run) normally.
func (r *Runner) FakeOK(err error) bool {
	return err == nil || errors.Is(err, ErrFakeMutation)
}

// Ok probes a command and reports whether it exited zero.
// Use for cheap condition checks such as rev-parse or merge-base.
func (r *Runner) Ok(ctx context.Context, argv ...string) bool {
	_, err := r.run(ctx, argv)
	return err == nil
}

// run executes one git command.
func (r *Runner) run(ctx context.Context, argv []string) (string, error) {
	if len(argv) == 0 {
		return "", errors.New("gitx: empty argv")
	}
	binary := r.Git
	if binary == "" {
		binary = "git"
	}

	if r.Verbose {
		r.echo(">>>", argv)
	}

	cmd := exec.CommandContext(ctx, binary, argv...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			gErr := &Error{
				Argv:     argv,
				ExitCode: exitErr.ExitCode(),
				Stderr:   stderr.String(),
			}
			return stdout.String(), gErr
		}
		return "", fmt.Errorf("gitx: run %v: %w", argv, err)
	}
	return stdout.String(), nil
}

func (r *Runner) echo(prefix string, argv []string) {
	w := r.Echo
	if w == nil {
		w = os.Stderr
	}
	_, _ = fmt.Fprintf(w, "%s git %s\n", prefix, strings.Join(argv, " ")) //nolint:errcheck // echo output; a closed stream must not abort the command
}
