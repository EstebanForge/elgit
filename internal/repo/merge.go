package repo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// AheadBehindAll counts, in one for-each-ref call, how far every branch
// is ahead of and behind HEAD: ahead is commits the branch has that HEAD
// lacks. Keys are full refnames (refs/heads/x, refs/remotes/<remote>/x).
// The ahead-behind atom needs git 2.29 or newer; callers check the
// version first and degrade to no counts on older git.
func (r *Repo) AheadBehindAll(ctx context.Context, remote string) (map[string][2]int, error) {
	patterns := []string{"refs/heads"}
	if remote != "" {
		patterns = append(patterns, "refs/remotes/"+remote)
	}
	argv := append([]string{"for-each-ref", "--format=%(refname)%09%(ahead-behind:HEAD)"}, patterns...)
	out, err := r.Git.Query(ctx, argv...)
	if err != nil {
		return nil, fmt.Errorf("for-each-ref: %w", err)
	}

	counts := make(map[string][2]int)
	for _, line := range strings.Split(out, "\n") {
		ref, ab, _ := strings.Cut(strings.TrimSpace(line), "\t")
		if ref == "" || ab == "" {
			continue
		}
		fields := strings.Fields(ab)
		if len(fields) != 2 {
			continue
		}
		ahead, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		behind, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		counts[ref] = [2]int{ahead, behind}
	}
	return counts, nil
}

// LogOneline lists the commits in from..to, one "%h %s" line each,
// newest first, capped at limit lines.
func (r *Repo) LogOneline(ctx context.Context, from, to string, limit int) ([]string, error) {
	out, err := r.Git.Query(ctx, "log", "--format=%h %s", "--max-count="+strconv.Itoa(limit), from+".."+to)
	if err != nil {
		return nil, fmt.Errorf("log: %w", err)
	}
	lines := make([]string, 0, limit)
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}
