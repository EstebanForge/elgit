package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/EstebanForge/elgit/internal/gitx"
)

// aliasCommands are the elgit commands installed as git aliases. "sw" is
// the interactive switcher; a "switch" alias is never installed: every
// supported git (2.24+) ships a native git switch that must not be shadowed.
var aliasCommands = []string{"sw", "sync", "publish", "unpublish", "undo", "branches"}

// legacyAliases are removed on uninstall even though elgit never installs
// them: they date back to the Python legit tool, and "switch" clears any
// alias shadowing the native git command.
var legacyAliases = []string{"graft", "harvest", "sprout", "resync", "settings", "install", "uninstall", "switch"}

// runAliasInstall writes git aliases of the form
//
//	alias.<name> = !<absolute elgit path> <command>
//
// through git config argv calls: no shell, no string escaping problems.
func runAliasInstall(cmd *cobra.Command, g *gitx.Runner) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()

	exe, err := programPath()
	if err != nil {
		return fmt.Errorf("cannot locate the elgit executable: %w", err)
	}

	sayln(out, "The following git aliases will be installed:")
	for _, name := range aliasCommands {
		sayf(out, "  git %-12s %s %s\n", name, exe, name)
	}

	if !g.Fake {
		sayf(out, "\nInstall these aliases? [y/N] ")
		answer, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n') //nolint:errcheck // EOF answers "no"
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			sayln(out, "Aliases not installed.")
			return nil
		}
	}

	for _, name := range aliasCommands {
		if _, err := g.Mutate(ctx, "config", "--global", "--replace-all", "alias."+name, "!"+shellQuote(exe)+" "+name); err != nil && !g.FakeOK(err) {
			return fmt.Errorf("installing alias %s: %w", name, err)
		}
	}
	if !g.Fake {
		sayln(out, "Aliases installed.")
	}
	return nil
}

// runAliasUninstall removes elgit and legacy legit aliases from the global
// git config. A missing alias is not an error.
func runAliasUninstall(cmd *cobra.Command, g *gitx.Runner) error {
	ctx := cmd.Context()
	out := cmd.OutOrStdout()
	names := append([]string{}, aliasCommands...)
	names = append(names, legacyAliases...)

	sayln(out, "Removing git aliases:")
	for _, name := range names {
		sayf(out, "  git %s\n", name)
		if _, err := g.Mutate(ctx, "config", "--global", "--unset-all", "alias."+name); err != nil {
			var gErr *gitx.Error
			if errors.As(err, &gErr) && gErr.ExitCode == 5 {
				continue // alias not set; nothing to remove
			}
			if !g.FakeOK(err) {
				return fmt.Errorf("removing alias %s: %w", name, err)
			}
		}
	}
	if !g.Fake {
		sayln(out, "Aliases removed.")
	}
	return nil
}

// atLeastGit reports whether the git binary is at least wantMajor.wantMinor.
func atLeastGit(ctx context.Context, g *gitx.Runner, wantMajor, wantMinor int) bool {
	out, err := g.Query(ctx, "version")
	if err != nil {
		return false
	}
	var major, minor int
	if _, err := fmt.Sscanf(strings.TrimSpace(out), "git version %d.%d", &major, &minor); err != nil {
		return false
	}
	return major > wantMajor || (major == wantMajor && minor >= wantMinor)
}

// programPath returns the absolute path of the running executable with
// symlinks resolved, so aliases survive PATH changes.
func programPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	return filepath.Abs(exe)
}

// shellQuote wraps s in single quotes for git's "!" aliases, which git
// executes through a shell. A path with spaces or metacharacters stays a
// single word; embedded quotes are escaped POSIX-style.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
