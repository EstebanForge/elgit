package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/EstebanForge/elgit/internal/gitx"
	"github.com/EstebanForge/elgit/internal/repo"
)

func newConfigCmd(runner func() *gitx.Runner) *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show effective elgit settings and their origin",
		Long: "elgit reads its settings from the git config hierarchy (all files and\n" +
			"includes), [legit] section. This command shows every relevant key with\n" +
			"the file it comes from. Settings are changed with git config itself.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			g := runner()
			out := cmd.OutOrStdout()

			sayln(out, "elgit settings (git config, [legit] section):")
			for _, key := range []string{"legit.remote", "legit.remoteFallback", "legit.smartMerge", "pull.rebase", "pull.ff"} {
				origin, err := g.Query(ctx, "config", "--show-origin", "--get", key)
				if err != nil || strings.TrimSpace(origin) == "" {
					sayf(out, "  %-22s (unset)\n", key)
					continue
				}
				file, value, _ := strings.Cut(strings.TrimSpace(origin), "\t")
				sayf(out, "  %-22s %-8s %s\n", key, value, file)
			}

			r := repo.Open(g)
			if remote, err := r.Remote(ctx); err == nil {
				sayf(out, "\nresolved remote: %s\n", remote)
			} else {
				sayf(out, "\nresolved remote: error: %v\n", err)
			}
			sayln(out, "\nchange a setting with: git config [--global] <key> <value>")
			return nil
		},
	}
}
