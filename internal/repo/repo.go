// Package repo answers state questions about one git repository: current
// branch, dirtiness, branch listings, remotes, config values. Every query
// is a single git invocation; no object-graph walking.
package repo

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/EstebanForge/elgit/internal/gitx"
)

// Branch is one entry of the branch listing.
type Branch struct {
	Name        string
	IsLocal     bool // a local branch exists
	IsPublished bool // a remote counterpart exists
	IsCurrent   bool
}

// Detail names the publication state for display.
func (b Branch) Detail() string {
	switch {
	case !b.IsLocal:
		return "(remote only)"
	case b.IsPublished:
		return "(published)"
	default:
		return "(unpublished)"
	}
}

// Repo reads repository state through a gitx.Runner.
type Repo struct {
	Git *gitx.Runner
}

// Open wraps a runner with state queries.
func Open(g *gitx.Runner) *Repo { return &Repo{Git: g} }

// IsInsideWorkTree reports whether the working directory belongs to a repository.
func (r *Repo) IsInsideWorkTree(ctx context.Context) bool {
	return r.Git.Ok(ctx, "rev-parse", "--is-inside-work-tree")
}

// CurrentBranch returns the checked-out branch name. It fails on detached
// HEAD and on repositories without commits.
func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	out, err := r.Git.Query(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", errors.New("no branch checked out (detached HEAD or empty repository)")
	}
	return strings.TrimSpace(out), nil
}

// IsDirty reports uncommitted or untracked changes. --no-optional-locks
// keeps the status call from refreshing and locking the index.
func (r *Repo) IsDirty(ctx context.Context) (bool, error) {
	out, err := r.Git.Query(ctx, "--no-optional-locks", "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return false, fmt.Errorf("status: %w", err)
	}
	return strings.TrimSpace(out) != "", nil
}

// Remotes lists configured remote names.
func (r *Repo) Remotes(ctx context.Context) ([]string, error) {
	out, err := r.Git.Query(ctx, "remote")
	if err != nil {
		return nil, fmt.Errorf("remote: %w", err)
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// Remote resolves the remote elgit works against. The [legit] config keys
// keep legacy legit compatibility: legit.remote selects a remote by name;
// when that name does not exist, legit.remoteFallback=true falls back to
// the first remote, otherwise it is an error. Without legit.remote the
// first remote wins.
func (r *Repo) Remote(ctx context.Context) (string, error) {
	remotes, err := r.Remotes(ctx)
	if err != nil {
		return "", err
	}
	if len(remotes) == 0 {
		return "", errors.New("no git remotes configured; add one with: git remote add <name> <url>")
	}
	for _, name := range remotes {
		if strings.HasPrefix(name, "-") {
			return "", fmt.Errorf("remote %q has an unsafe name (starts with '-'); rename it before using elgit", name)
		}
	}
	configured := r.ConfigString(ctx, "legit.remote")
	if configured != "" {
		if slices.Contains(remotes, configured) {
			return configured, nil
		}
		if r.ConfigBool(ctx, "legit.remoteFallback", false) {
			return remotes[0], nil
		}
		return "", fmt.Errorf("legit.remote = %q matches no remote (%s); fix it or set legit.remoteFallback=true",
			configured, strings.Join(remotes, ", "))
	}
	return remotes[0], nil
}

// ConfigString returns a config value or "" when unset. Values come from
// the full git config hierarchy, includes included.
func (r *Repo) ConfigString(ctx context.Context, key string) string {
	out, err := r.Git.Query(ctx, "config", "--get", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// ConfigBool reads a boolean config value with a default.
func (r *Repo) ConfigBool(ctx context.Context, key string, def bool) bool {
	switch strings.ToLower(r.ConfigString(ctx, key)) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	}
	return def
}

// Branches lists local and remote-tracking branches of one remote. A branch
// appears once: published when a remote counterpart exists. remote may be
// empty to list local branches only.
func (r *Repo) Branches(ctx context.Context, remote string) ([]Branch, error) {
	patterns := []string{"refs/heads"}
	if remote != "" {
		patterns = append(patterns, "refs/remotes/"+remote)
	}
	argv := append([]string{"for-each-ref", "--format=%(refname)%09%(HEAD)"}, patterns...)
	out, err := r.Git.Query(ctx, argv...)
	if err != nil {
		return nil, fmt.Errorf("for-each-ref: %w", err)
	}

	remotePrefix := "refs/remotes/" + remote + "/"
	byName := make(map[string]*Branch)
	add := func(name string) *Branch {
		b := byName[name]
		if b == nil {
			b = &Branch{Name: name}
			byName[name] = b
		}
		return b
	}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		ref, marker, _ := strings.Cut(line, "\t")
		switch {
		case strings.HasPrefix(ref, "refs/heads/"):
			b := add(strings.TrimPrefix(ref, "refs/heads/"))
			b.IsLocal = true
			b.IsCurrent = marker == "*"
		case remote != "" && strings.HasPrefix(ref, remotePrefix):
			name := strings.TrimPrefix(ref, remotePrefix)
			if name == "HEAD" {
				continue // symbolic default branch of the remote
			}
			add(name).IsPublished = true
		}
	}

	branches := make([]Branch, 0, len(byName))
	for _, b := range byName {
		branches = append(branches, *b)
	}
	sort.Slice(branches, func(i, j int) bool { return branches[i].Name < branches[j].Name })
	return branches, nil
}

// FuzzyMatchBranch resolves a branch name: exact match first, then a unique
// prefix match. Returns the resolved name and whether it was found.
func (r *Repo) FuzzyMatchBranch(ctx context.Context, remote, name string) (string, bool) {
	if name == "" {
		return "", false
	}
	branches, err := r.Branches(ctx, remote)
	if err != nil {
		return name, false
	}
	var candidates []string
	for _, b := range branches {
		switch {
		case b.Name == name:
			return b.Name, true
		case strings.HasPrefix(b.Name, name):
			candidates = append(candidates, b.Name)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return name, false
}

// BranchRef returns the fully qualified local branch ref. Full refs can
// never be mistaken for git options, whatever the branch name contains.
func (r *Repo) BranchRef(branch string) string { return "refs/heads/" + branch }

// RemoteRef returns the fully qualified remote-tracking ref.
func (r *Repo) RemoteRef(remote, branch string) string {
	return "refs/remotes/" + remote + "/" + branch
}

// HasRemoteBranch reports whether the remote-tracking ref exists.
func (r *Repo) HasRemoteBranch(ctx context.Context, remote, branch string) bool {
	return r.Git.Ok(ctx, "rev-parse", "--verify", "--quiet", r.RemoteRef(remote, branch))
}
