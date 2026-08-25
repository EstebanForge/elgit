// Package safety guards mutating workflows: it refuses to operate while git
// is mid-rebase or mid-merge, serializes concurrent elgit runs per
// repository, and tracks stashes by commit OID so a restore never has to
// guess which stash entry belongs to which branch.
//
// The lock and the ledger live in the repository's common git dir (not the
// per-worktree dir) because the stash stack they protect is shared by all
// worktrees of a repository.
package safety

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/EstebanForge/elgit/internal/gitx"
)

// stateMarkers are git state files and directories; the presence of one
// means an operation is in progress.
var stateMarkers = []struct{ name, kind string }{
	{"rebase-merge", "rebase"},
	{"rebase-apply", "rebase or am"},
	{"MERGE_HEAD", "merge"},
	{"CHERRY_PICK_HEAD", "cherry-pick"},
	{"REVERT_HEAD", "revert"},
	{"BISECT_LOG", "bisect"},
}

// InProgressOp returns the kind of operation git has in progress, or ""
// when the repository is clean of in-progress state.
func InProgressOp(ctx context.Context, g *gitx.Runner) (string, error) {
	for _, m := range stateMarkers {
		out, err := g.Query(ctx, "rev-parse", "--git-path", m.name)
		if err != nil {
			return "", fmt.Errorf("rev-parse --git-path %s: %w", m.name, err)
		}
		path := resolvePath(g.Dir, strings.TrimSpace(out))
		if path == "" {
			continue
		}
		if _, statErr := os.Stat(path); statErr == nil {
			return m.kind, nil
		}
	}
	return "", nil
}

// Lock is an advisory lock preventing concurrent elgit runs in one
// repository (all worktrees included). Release is ownership-checked: a
// lock that was taken over as stale is never deleted by its old owner.
type Lock struct {
	path string
}

// Acquire takes the repository lock. Creation is atomic: the pid is
// written to a temporary file that is hard-linked into place, so no other
// process can ever observe a half-written lock. A stale lock (dead pid or
// older than lockMaxAge) is claimed by renaming it away; when two
// processes race for the same stale lock, the rename succeeds for exactly
// one of them.
func Acquire(ctx context.Context, g *gitx.Runner) (*Lock, error) {
	path, err := commonPath(ctx, g, "elgit.lock")
	if err != nil {
		return nil, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".elgit-lock-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() //nolint:errcheck // temp cleanup; nothing to do on failure
	if _, err := fmt.Fprintf(tmp, "%d\n", os.Getpid()); err != nil {
		_ = tmp.Close() //nolint:errcheck // the Fprintf error already aborts; Close adds nothing
		return nil, fmt.Errorf("writing lock pid: %w", err)
	}
	closeErr := tmp.Close()
	if closeErr != nil {
		return nil, closeErr
	}

	for attempt := 0; attempt < 50; attempt++ {
		if err := os.Link(tmpName, path); err == nil {
			return &Lock{path: path}, nil
		} else if !errors.Is(err, os.ErrExist) {
			return nil, err
		}

		// A lock exists. Refuse live ones.
		if pid, live := liveLockHolder(path); live {
			return nil, fmt.Errorf("another elgit process (pid %d) is working in this repository", pid)
		}

		// Claim the stale lock by renaming it away. A racing process that
		// also judged it stale loses the rename and retries the link.
		claim := fmt.Sprintf("%s.old.%d", path, os.Getpid())
		if err := os.Rename(path, claim); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // stolen by another racer; retry
			}
			return nil, err
		}
		// If a live lock was swapped in between the check and the rename,
		// put it back untouched and report the holder.
		if pid, live := liveLockHolder(claim); live {
			if rerr := os.Rename(claim, path); rerr != nil {
				return nil, fmt.Errorf("failed to restore a live lock claimed by mistake: %w", rerr)
			}
			return nil, fmt.Errorf("another elgit process (pid %d) is working in this repository", pid)
		}
		_ = os.Remove(claim) //nolint:errcheck // best-effort cleanup; a leftover claim file is harmless
	}
	return nil, errors.New("could not acquire the repository lock after repeated races; try again")
}

// Release removes the lock if this process still owns it.
func (l *Lock) Release() error {
	if l == nil || l.path == "" {
		return nil
	}
	if pid := readPid(l.path); pid != os.Getpid() {
		return nil // taken over as stale or already released
	}
	return os.Remove(l.path)
}

// lockMaxAge bounds how long an abandoned lock may block a run. Long-lived
// live runs hold their lock; only ancient leftovers are taken over.
const lockMaxAge = 6 * time.Hour

// liveLockHolder reports whether the lock file at path is held by a live
// process, and which pid holds it. A file without a readable, live pid or
// one older than lockMaxAge counts as stale.
func liveLockHolder(path string) (int, bool) {
	pid := readPid(path)
	if !processAlive(pid) {
		return pid, false
	}
	info, err := os.Stat(path)
	if err == nil && time.Since(info.ModTime()) > lockMaxAge {
		return pid, false
	}
	return pid, pid > 0
}

func readPid(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(data))) //nolint:errcheck // unreadable content means no holder; 0 is the intended fallback
	return pid
}

// Entry records one stash elgit created and intends to restore.
type Entry struct {
	StashOID string    `json:"stash_oid"`
	Branch   string    `json:"branch"`
	Purpose  string    `json:"purpose"` // "switching" or "syncing"
	Created  time.Time `json:"created"`
}

// Ledger is the persistent record of elgit-created stashes, stored under
// the common git dir. It survives crashes: an entry whose stash still
// exists is reported to the user instead of being silently dropped.
type Ledger struct {
	path    string
	Entries []Entry
}

// LoadLedger reads the ledger. It creates nothing; a missing ledger is
// empty. Reads are safe in fake mode.
func LoadLedger(ctx context.Context, g *gitx.Runner) (*Ledger, error) {
	path, err := commonPath(ctx, g, filepath.Join("elgit", "ledger.json"))
	if err != nil {
		return nil, err
	}

	l := &Ledger{path: path}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return l, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(data, &l.Entries); err != nil {
		return nil, fmt.Errorf("ledger %s is corrupt (%v); remove it after checking git stash list", path, err)
	}
	return l, nil
}

// Record appends an entry and persists the ledger atomically.
func (l *Ledger) Record(e Entry) error {
	l.Entries = append(l.Entries, e)
	return l.save()
}

// Find returns the newest entry matching branch and purpose.
func (l *Ledger) Find(branch, purpose string) (Entry, bool) {
	for i := len(l.Entries) - 1; i >= 0; i-- {
		if l.Entries[i].Branch == branch && l.Entries[i].Purpose == purpose {
			return l.Entries[i], true
		}
	}
	return Entry{}, false
}

// Remove drops the entry with the given stash OID.
func (l *Ledger) Remove(oid string) error {
	kept := l.Entries[:0]
	for _, e := range l.Entries {
		if e.StashOID != oid {
			kept = append(kept, e)
		}
	}
	l.Entries = kept
	return l.save()
}

// WarnPending prints entries for the given branch whose stashes may still
// be waiting. Entries parked on other branches are normal (a stash travels
// with its branch and restores when you return to it), so they stay silent.
func (l *Ledger) WarnPending(out io.Writer, branch string) {
	for _, e := range l.Entries {
		if e.Branch != branch {
			continue
		}
		_, _ = fmt.Fprintf(out, "unrestored elgit stash from %s: branch %q, stash %s (git stash list)\n", //nolint:errcheck // warning output; delivery is best-effort
			e.Created.Format(time.RFC3339), e.Branch, shortOID(e.StashOID))
	}
}

func (l *Ledger) save() error {
	data, err := json.MarshalIndent(l.Entries, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

// NewStashOID returns the commit OID of the most recent stash entry. Call
// right after creating a stash, before anything else can touch the stack.
func NewStashOID(ctx context.Context, g *gitx.Runner) (string, error) {
	out, err := g.Query(ctx, "rev-parse", "--verify", "--quiet", "stash@{0}")
	if err != nil {
		return "", errors.New("stash was created but its commit cannot be resolved; inspect: git stash list")
	}
	return strings.TrimSpace(out), nil
}

// StashIndexByOID locates a stash commit on the current stack and returns
// its index. Replaces legacy legit's string-grep of stash subjects.
func StashIndexByOID(ctx context.Context, g *gitx.Runner, oid string) (int, bool) {
	out, err := g.Query(ctx, "stash", "list", "--format=%H")
	if err != nil {
		return -1, false
	}
	for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if strings.TrimSpace(line) == oid {
			return i, true
		}
	}
	return -1, false
}

// DropStashByOID removes the stash entry holding the given commit OID. The
// index is re-resolved immediately before the drop and verified after it:
// if anything else touched the stack underneath, the entry is kept and the
// caller is told instead of dropping the wrong stash.
func DropStashByOID(ctx context.Context, g *gitx.Runner, oid string) error {
	idx, ok := StashIndexByOID(ctx, g, oid)
	if !ok {
		return nil // already gone
	}
	if _, err := g.Mutate(ctx, "stash", "drop", fmt.Sprintf("stash@{%d}", idx)); err != nil {
		var gErr *gitx.Error
		if errors.As(err, &gErr) {
			return fmt.Errorf("stash %s applied but not dropped; remove it manually (git stash drop): %w", shortOID(oid), gErr)
		}
		return err
	}
	if _, still := StashIndexByOID(ctx, g, oid); still {
		return fmt.Errorf("stash %s applied but the drop may have hit a different entry; check git stash list", shortOID(oid))
	}
	return nil
}

func shortOID(oid string) string {
	if len(oid) > 7 {
		return oid[:7]
	}
	return oid
}

// commonPath returns a path inside the repository's common git dir. The
// common dir is shared by all linked worktrees, which is where the lock
// and ledger must live: the stash stack is shared, so its guards must be
// shared too.
func commonPath(ctx context.Context, g *gitx.Runner, name string) (string, error) {
	out, err := g.Query(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", fmt.Errorf("rev-parse --git-common-dir: %w", err)
	}
	base := strings.TrimSpace(out)
	if base == "" {
		base = ".git"
	}
	if !filepath.IsAbs(base) {
		base = resolvePath(g.Dir, base)
	}
	return filepath.Join(base, name), nil
}

// resolvePath joins relative git-path output with the runner directory.
// --git-path and --git-common-dir print paths relative to the working tree
// root, or absolute for linked worktrees.
func resolvePath(dir, p string) string {
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	if dir == "" {
		return p
	}
	return filepath.Join(dir, p)
}
