package workflow

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EstebanForge/elgit/internal/gitx"
	"github.com/EstebanForge/elgit/internal/repo"
	"github.com/EstebanForge/elgit/internal/safety"
)

// setup creates a work repo with a bare remote: main published with a.txt,
// feature local and unpublished.
func setup(t *testing.T) (*Workflow, *bytes.Buffer, string, string) {
	t.Helper()
	base := t.TempDir()
	work := filepath.Join(base, "work")
	bare := filepath.Join(base, "remote.git")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	g := &gitx.Runner{Dir: work}
	ctx := context.Background()
	for _, argv := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.com"},
	} {
		if _, err := g.Query(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"add", "."},
		{"commit", "-m", "init"},
		{"init", "--bare", "-b", "main", bare},
		{"remote", "add", "origin", bare},
		{"push", "-u", "origin", "main"},
		{"branch", "feature"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	var buf bytes.Buffer
	w := New(repo.Open(g), &buf)
	return w, &buf, base, work
}

func runGit(t *testing.T, dir string, argv ...string) string {
	t.Helper()
	g := &gitx.Runner{Dir: dir}
	out, err := g.Mutate(context.Background(), argv...)
	if err != nil {
		t.Fatalf("git %v (in %s): %v", argv, dir, err)
	}
	return out
}

func queryGit(t *testing.T, dir string, argv ...string) string {
	t.Helper()
	g := &gitx.Runner{Dir: dir}
	out, err := g.Query(context.Background(), argv...)
	if err != nil {
		t.Fatalf("git %v (in %s): %v", argv, dir, err)
	}
	return strings.TrimSpace(out)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// peerCommit clones the bare remote, commits a change, and pushes it,
// simulating a teammate.
func peerCommit(t *testing.T, base, filename, content string) {
	t.Helper()
	peer := filepath.Join(base, "peer")
	g := &gitx.Runner{Dir: peer}
	ctx := context.Background()
	if !g.Ok(ctx, "rev-parse", "--is-inside-work-tree") {
		runGit(t, base, "clone", filepath.Join(base, "remote.git"), peer)
		for _, argv := range [][]string{
			{"config", "user.name", "Peer"},
			{"config", "user.email", "peer@example.com"},
		} {
			if _, err := g.Query(ctx, argv...); err != nil {
				t.Fatalf("peer config %v: %v", argv, err)
			}
		}
	}
	writeFile(t, filepath.Join(peer, filename), content)
	runGit(t, peer, "add", ".")
	runGit(t, peer, "commit", "-m", "peer: "+filename)
	runGit(t, peer, "push")
}

func TestSwitchStashAndRestore(t *testing.T) {
	w, buf, _, work := setup(t)
	ctx := context.Background()

	writeFile(t, filepath.Join(work, "a.txt"), "dirty\n")
	if err := w.Switch(ctx, "feature"); err != nil {
		t.Fatal(err)
	}
	if got := queryGit(t, work, "rev-parse", "--abbrev-ref", "HEAD"); got != "feature" {
		t.Fatalf("branch = %q, want feature", got)
	}
	if got := readFile(t, filepath.Join(work, "a.txt")); got != "one\n" {
		t.Errorf("a.txt on feature = %q, want clean one", got)
	}
	if !strings.Contains(buf.String(), "Saving local changes.") {
		t.Errorf("output missing stash notice: %q", buf.String())
	}

	if err := w.Switch(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(work, "a.txt")); got != "dirty\n" {
		t.Errorf("a.txt restored = %q, want dirty", got)
	}

	// Ledger must be empty after a clean round trip.
	led, err := safety.LoadLedger(ctx, &gitx.Runner{Dir: work})
	if err != nil {
		t.Fatal(err)
	}
	if len(led.Entries) != 0 {
		t.Errorf("ledger entries = %v, want none", led.Entries)
	}
}

func TestSwitchUnknownBranch(t *testing.T) {
	w, _, _, _ := setup(t)
	err := w.Switch(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected error for unknown branch")
	}
}

func TestSwitchAlreadyOnBranch(t *testing.T) {
	w, buf, _, _ := setup(t)
	if err := w.Switch(context.Background(), "main"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Already on main.") {
		t.Errorf("output = %q, want already-on notice", buf.String())
	}
}

func TestSyncNoopWhenCurrent(t *testing.T) {
	w, buf, _, _ := setup(t)
	ctx := context.Background()

	if err := w.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "Pushing") {
		t.Error("sync pushed with nothing to push")
	}
	if strings.Contains(buf.String(), "Integrating") {
		t.Error("sync integrated an up-to-date branch")
	}
}

func TestSyncPushesLocalCommits(t *testing.T) {
	w, _, base, work := setup(t)
	ctx := context.Background()

	writeFile(t, filepath.Join(work, "b.txt"), "local\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "local commit")

	if err := w.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := queryGit(t, work, "rev-list", "--count", "origin/main..main"); got != "0" {
		t.Errorf("unpushed commits = %s, want 0", got)
	}
	_ = base
}

func TestSyncRebasesOntoRemote(t *testing.T) {
	w, _, base, work := setup(t)
	ctx := context.Background()

	peerCommit(t, base, "p.txt", "peer\n")

	writeFile(t, filepath.Join(work, "l.txt"), "local\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "local commit")

	if err := w.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"p.txt", "l.txt"} {
		if _, err := os.Stat(filepath.Join(work, name)); err != nil {
			t.Errorf("%s missing after rebase: %v", name, err)
		}
	}
	if got := queryGit(t, work, "rev-list", "--count", "origin/main..main"); got != "0" {
		t.Errorf("unpushed commits = %s, want 0", got)
	}
}

func TestSyncConflictStopsAndKeepsState(t *testing.T) {
	w, _, base, work := setup(t)
	ctx := context.Background()

	peerCommit(t, base, "a.txt", "peer\n")

	writeFile(t, filepath.Join(work, "a.txt"), "local\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "local change")

	err := w.Sync(ctx, SyncOptions{})
	if err == nil {
		t.Fatal("expected conflict error")
	}
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error is %T, want *ConflictError", err)
	}

	// The conflicted state must be left in place for manual resolution.
	kind, kerr := safety.InProgressOp(ctx, &gitx.Runner{Dir: work})
	if kerr != nil {
		t.Fatal(kerr)
	}
	if kind != "rebase" {
		t.Errorf("in-progress op = %q, want rebase (state must be kept)", kind)
	}
	runGit(t, work, "rebase", "--abort")
}

func TestSyncFakeChangesNothing(t *testing.T) {
	_, _, base, work := setup(t)
	ctx := context.Background()

	writeFile(t, filepath.Join(work, "a.txt"), "dirty\n")
	g := &gitx.Runner{Dir: work, Fake: true}
	var echo bytes.Buffer
	g.Echo = &echo
	var buf bytes.Buffer
	w := New(repo.Open(g), &buf)

	if err := w.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(work, "a.txt")); got != "dirty\n" {
		t.Errorf("fake sync modified work tree: %q", got)
	}
	if out := queryGit(t, work, "stash", "list"); out != "" {
		t.Errorf("fake sync created stashes: %q", out)
	}
	if !strings.Contains(echo.String(), "FAKE git stash push") {
		t.Errorf("echo output missing fake stash: %q", echo.String())
	}
	_ = base
}

func TestSyncExternalBranchRoundTrip(t *testing.T) {
	w, _, base, work := setup(t)
	ctx := context.Background()

	runGit(t, work, "push", "-u", "origin", "feature")
	peerCommit(t, base, "p.txt", "peer\n")

	if err := w.Sync(ctx, SyncOptions{Target: "feature"}); err != nil {
		t.Fatal(err)
	}
	if got := queryGit(t, work, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("final branch = %q, want main (returned to original)", got)
	}
}

func TestSyncUnpublishedBranch(t *testing.T) {
	w, _, _, _ := setup(t)
	err := w.Sync(context.Background(), SyncOptions{Target: "feature"})
	if err == nil || !strings.Contains(err.Error(), "not published") {
		t.Fatalf("err = %v, want not-published error", err)
	}
}

func TestPublishAndUnpublish(t *testing.T) {
	w, _, _, work := setup(t)
	ctx := context.Background()

	if err := w.Publish(ctx, "feature"); err != nil {
		t.Fatal(err)
	}
	r := repo.Open(&gitx.Runner{Dir: work})
	if !r.HasRemoteBranch(ctx, "origin", "feature") {
		t.Error("feature not on remote after publish")
	}

	if err := w.Publish(ctx, "feature"); err == nil {
		t.Error("publishing twice should fail")
	}

	if err := w.Unpublish(ctx, "feature"); err != nil {
		t.Fatal(err)
	}
	if r.HasRemoteBranch(ctx, "origin", "feature") {
		t.Error("feature still on remote after unpublish")
	}

	if err := w.Unpublish(ctx, "feature"); err == nil {
		t.Error("unpublishing an unpublished branch should fail")
	}
}

func TestUndoRemovesLastCommit(t *testing.T) {
	w, buf, _, work := setup(t)
	ctx := context.Background()

	writeFile(t, filepath.Join(work, "b.txt"), "two\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "second")
	root := queryGit(t, work, "rev-parse", "HEAD~1")

	if err := w.Undo(ctx, false); err != nil {
		t.Fatal(err)
	}
	if got := queryGit(t, work, "rev-parse", "HEAD"); got != root {
		t.Errorf("HEAD after undo = %s, want %s", got, root)
	}
	if !strings.Contains(buf.String(), "Removed") {
		t.Errorf("output = %q, want removal notice", buf.String())
	}
}

func TestSwitchToRemoteOnlyBranch(t *testing.T) {
	w, _, base, work := setup(t)
	ctx := context.Background()

	// A peer pushes a branch that has never existed locally.
	peer := filepath.Join(base, "peer")
	if _, _, err := runGit2(t, base, "clone", filepath.Join(base, "remote.git"), peer); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"config", "user.name", "Peer"},
		{"config", "user.email", "peer@example.com"},
		{"checkout", "-b", "side"},
	} {
		if _, _, err := runGit2(t, peer, argv...); err != nil {
			t.Fatalf("peer %v: %v", argv, err)
		}
	}
	if err := os.WriteFile(filepath.Join(peer, "side.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{{"add", "."}, {"commit", "-m", "side work"}, {"push", "-u", "origin", "side"}} {
		if _, _, err := runGit2(t, peer, argv...); err != nil {
			t.Fatalf("peer %v: %v", argv, err)
		}
	}

	runGit(t, work, "fetch", "origin")
	if err := w.Switch(ctx, "side"); err != nil {
		t.Fatalf("switch to remote-only branch: %v", err)
	}
	if got := queryGit(t, work, "rev-parse", "--abbrev-ref", "HEAD"); got != "side" {
		t.Errorf("branch = %q, want side", got)
	}
	// The new local branch must track the remote one.
	if got := queryGit(t, work, "rev-parse", "--abbrev-ref", "side@{upstream}"); got != "origin/side" {
		t.Errorf("upstream = %q, want origin/side", got)
	}
	if _, err := os.Stat(filepath.Join(work, "side.txt")); err != nil {
		t.Errorf("side.txt missing after switch: %v", err)
	}
}

func runGit2(t *testing.T, dir string, argv ...string) (string, string, error) {
	t.Helper()
	g := &gitx.Runner{Dir: dir}
	out, err := g.Mutate(context.Background(), argv...)
	return out, "", err
}

func TestUndoRootOnly(t *testing.T) {
	dir := t.TempDir()
	g := &gitx.Runner{Dir: dir}
	ctx := context.Background()
	for _, argv := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.com"},
		{"commit", "--allow-empty", "-m", "root"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	var buf bytes.Buffer
	w := New(repo.Open(g), &buf)
	if err := w.Undo(ctx, false); err == nil {
		t.Error("undo on root-only repo should fail")
	}
}

func TestUnbornRepositoryRefused(t *testing.T) {
	dir := t.TempDir()
	g := &gitx.Runner{Dir: dir}
	ctx := context.Background()
	for _, argv := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.com"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	var buf bytes.Buffer
	w := New(repo.Open(g), &buf)
	err := w.Switch(ctx, "main")
	if err == nil || !strings.Contains(err.Error(), "no commits yet") {
		t.Fatalf("err = %v, want no-commits-yet error", err)
	}
}

func TestFakeSyncWritesNothingToGitDir(t *testing.T) {
	_, _, base, work := setup(t)
	ctx := context.Background()

	writeFile(t, filepath.Join(work, "a.txt"), "dirty\n")
	g := &gitx.Runner{Dir: work, Fake: true}
	var echo bytes.Buffer
	g.Echo = &echo
	var buf bytes.Buffer
	w := New(repo.Open(g), &buf)
	if err := w.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}

	common := queryGit(t, work, "rev-parse", "--git-common-dir")
	if !filepath.IsAbs(common) {
		common = filepath.Join(work, common)
	}
	for _, name := range []string{"elgit.lock", "elgit"} {
		if _, err := os.Stat(filepath.Join(common, name)); err == nil {
			t.Errorf("fake run created %s inside the git dir", name)
		}
	}
	_ = base
}

func TestDashBranchNamesAreSafe(t *testing.T) {
	w, _, _, work := setup(t)
	ctx := context.Background()

	runGit(t, work, "update-ref", "refs/heads/-dash", "HEAD")

	if err := w.Publish(ctx, "-dash"); err != nil {
		t.Fatalf("publish -dash: %v", err)
	}
	r := repo.Open(&gitx.Runner{Dir: work})
	if !r.HasRemoteBranch(ctx, "origin", "-dash") {
		t.Fatal("remote branch -dash missing after publish")
	}

	writeFile(t, filepath.Join(work, "a.txt"), "dirty\n")
	if err := w.Switch(ctx, "-dash"); err != nil {
		t.Fatalf("switch to -dash: %v", err)
	}
	if got := queryGit(t, work, "rev-parse", "--abbrev-ref", "HEAD"); got != "-dash" {
		t.Fatalf("branch = %q, want -dash", got)
	}
	if err := w.Switch(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(work, "a.txt")); got != "dirty\n" {
		t.Errorf("a.txt = %q, want restored dirty", got)
	}

	if err := w.Unpublish(ctx, "-dash"); err != nil {
		t.Fatalf("unpublish -dash: %v", err)
	}
	if r.HasRemoteBranch(ctx, "origin", "-dash") {
		t.Error("remote branch -dash still present after unpublish")
	}
}

func TestRestoreNotConfusedByInterloperStash(t *testing.T) {
	w, _, _, work := setup(t)
	ctx := context.Background()

	writeFile(t, filepath.Join(work, "a.txt"), "elgit-dirty\n")
	if err := w.Switch(ctx, "feature"); err != nil {
		t.Fatal(err)
	}

	// An external stash lands on the stack while we are away, shifting
	// every index. The restore must still find our entry by OID.
	writeFile(t, filepath.Join(work, "a.txt"), "feature-dirty\n")
	runGit(t, work, "stash", "push", "-m", "interloper")

	if err := w.Switch(ctx, "main"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(work, "a.txt")); got != "elgit-dirty\n" {
		t.Errorf("a.txt = %q, want elgit-dirty restored by OID", got)
	}
	if out := queryGit(t, work, "stash", "list"); !strings.Contains(out, "interloper") {
		t.Errorf("interloper stash was consumed: %q", out)
	}
}

func TestPullRebaseMergesHonored(t *testing.T) {
	_, _, base, work := setup(t)
	ctx := context.Background()

	peerCommit(t, base, "p.txt", "peer\n")
	writeFile(t, filepath.Join(work, "l.txt"), "local\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "local commit")
	runGit(t, work, "config", "legit.smartMerge", "false")
	runGit(t, work, "config", "pull.rebase", "merges")

	g := &gitx.Runner{Dir: work, Verbose: true}
	var echo bytes.Buffer
	g.Echo = &echo
	var buf bytes.Buffer
	w := New(repo.Open(g), &buf)
	if err := w.Sync(ctx, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(echo.String(), "git rebase refs/remotes/origin/main") {
		t.Errorf("echo = %q, want rebase chosen via pull.rebase=merges", echo.String())
	}
}

// stashShim prepends a directory to PATH whose git intercepts
// `git stash push --include-untracked` and runs body in its place, with
// $REAL bound to the real git. It injects concurrent-actor actions into
// the push-to-readback window that the elgit lock cannot exclude.
func stashShim(t *testing.T, body string) {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := `#!/bin/sh
REAL=` + realGit + `
if [ "$1" = stash ] && [ "$2" = push ] && [ "$3" = --include-untracked ]; then
` + body + `
fi
exec "$REAL" "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestStashAwayRefusesForeignTop covers the hijack window: a concurrent
// git stash push landing between elgit's push and its read-back must
// fail loudly instead of being recorded as elgit's own.
func TestStashAwayRefusesForeignTop(t *testing.T) {
	w, buf, _, work := setup(t)
	ctx := context.Background()

	stashShim(t, `"$REAL" "$@"
rc=$?
printf 'x' >> a.txt
"$REAL" stash push -m "foreign concurrent stash" >/dev/null 2>&1
exit $rc
`)

	writeFile(t, filepath.Join(work, "a.txt"), "dirty\n")
	led, err := safety.LoadLedger(ctx, w.Repo.Git)
	if err != nil {
		t.Fatal(err)
	}
	_, stashed, err := w.stashAway(ctx, led, "main", purposeSwitch)
	if err == nil {
		t.Fatalf("stashAway recorded a foreign top-of-stack stash (stashed=%v, buf=%q)", stashed, buf.String())
	}
	if !strings.Contains(err.Error(), "concurrent stash push") {
		t.Errorf("err = %v, want a concurrent stash push report", err)
	}

	led2, err := safety.LoadLedger(ctx, w.Repo.Git)
	if err != nil {
		t.Fatal(err)
	}
	if _, found := led2.Find("main", purposeSwitch); found {
		t.Errorf("ledger recorded an entry for a stash elgit did not create")
	}
	// Both stashes survive for manual recovery.
	out := queryGit(t, work, "stash", "list", "--format=%gs")
	if !strings.Contains(out, "foreign concurrent stash") || !strings.Contains(out, "elgit: stashing before") {
		t.Errorf("stash list lost an entry: %q", out)
	}
}

// TestStashAwayNoopWithCleanedTree keeps the benign no-op: another actor
// cleans the tree before elgit's push runs, the push stashes nothing,
// and a pre-existing elgit stash stays untouched on the stack.
func TestStashAwayNoopWithCleanedTree(t *testing.T) {
	w, buf, _, work := setup(t)
	ctx := context.Background()

	stashShim(t, `"$REAL" checkout -- a.txt
"$REAL" "$@"
`)

	runGit(t, work, "stash", "push", "-m", "elgit: stashing before switching main [123]")
	writeFile(t, filepath.Join(work, "a.txt"), "dirty\n")

	led, err := safety.LoadLedger(ctx, w.Repo.Git)
	if err != nil {
		t.Fatal(err)
	}
	entry, stashed, err := w.stashAway(ctx, led, "main", purposeSwitch)
	if err != nil {
		t.Fatalf("stashAway: %v", err)
	}
	if stashed || entry.StashOID != "" {
		t.Errorf("stashAway = (%+v, %v), want no stash for a no-op push", entry, stashed)
	}
	if len(led.Entries) != 0 {
		t.Errorf("ledger gained %+v on a no-op push", led.Entries)
	}
	if !strings.Contains(buf.String(), "Saving local changes.") {
		t.Errorf("buf = %q, want the saving notice", buf.String())
	}
}

// TestStrategySide pins the ours/theirs inversion: a rebase replays
// local commits onto the remote, so the remote side is "ours" there and
// "theirs" in a merge.
func TestStrategySide(t *testing.T) {
	cases := []struct {
		verb   string
		prefer Prefer
		want   string
	}{
		{"rebase", PreferRemote, "ours"},
		{"rebase", PreferLocal, "theirs"},
		{"merge", PreferRemote, "theirs"},
		{"merge", PreferLocal, "ours"},
	}
	for _, c := range cases {
		if got := strategySide(c.verb, c.prefer); got != c.want {
			t.Errorf("strategySide(%s, %s) = %s, want %s", c.verb, c.prefer, got, c.want)
		}
	}
}

// divergedMain stages the classic conflict: a peer rewrote a.txt on the
// remote while the local clone rewrote the same line and committed.
func divergedMain(t *testing.T, base, work string) {
	t.Helper()
	peerCommit(t, base, "a.txt", "remote\n")
	writeFile(t, filepath.Join(work, "a.txt"), "local\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "local: rewrite a.txt")
}

// TestSyncPreferRemoteResolvesConflict covers a diverged main: prefer
// remote auto-resolves the colliding hunk with the remote side and
// pushes the rebased branch.
func TestSyncPreferRemoteResolvesConflict(t *testing.T) {
	w, buf, base, work := setup(t)
	ctx := context.Background()

	divergedMain(t, base, work)
	if err := w.Sync(ctx, SyncOptions{Prefer: PreferRemote}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := readFile(t, filepath.Join(work, "a.txt")); got != "remote\n" {
		t.Errorf("a.txt = %q, want the remote side to win", got)
	}
	if !strings.Contains(buf.String(), "prefer remote") {
		t.Errorf("buf = %q, want the strategy named", buf.String())
	}
	if local, remote := queryGit(t, work, "rev-parse", "HEAD"), queryGit(t, work, "rev-parse", "refs/remotes/origin/main"); local != remote {
		t.Errorf("push missing: local %s != remote %s", local, remote)
	}
	if out := queryGit(t, work, "status", "--porcelain"); out != "" {
		t.Errorf("dirty after sync: %q", out)
	}
}

// TestSyncPreferLocalResolvesConflict mirrors prefer remote: the local
// side wins colliding hunks.
func TestSyncPreferLocalResolvesConflict(t *testing.T) {
	w, _, base, work := setup(t)
	ctx := context.Background()

	divergedMain(t, base, work)
	if err := w.Sync(ctx, SyncOptions{Prefer: PreferLocal}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got := readFile(t, filepath.Join(work, "a.txt")); got != "local\n" {
		t.Errorf("a.txt = %q, want the local side to win", got)
	}
	if local, remote := queryGit(t, work, "rev-parse", "HEAD"), queryGit(t, work, "rev-parse", "refs/remotes/origin/main"); local != remote {
		t.Errorf("push missing: local %s != remote %s", local, remote)
	}
}

// TestSyncAskDecidesStrategy covers the pre-stash prompt: it fires only
// when both sides moved, receives the counts, and its choice drives the
// integration.
func TestSyncAskDecidesStrategy(t *testing.T) {
	w, _, base, work := setup(t)
	ctx := context.Background()

	divergedMain(t, base, work)

	calls := 0
	var gotRemoteAhead, gotLocalAhead int
	ask := func(_, _ string, remoteAhead, localAhead int) (Prefer, error) {
		calls++
		gotRemoteAhead, gotLocalAhead = remoteAhead, localAhead
		return PreferRemote, nil
	}
	if err := w.Sync(ctx, SyncOptions{Ask: ask}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if calls != 1 {
		t.Fatalf("ask ran %d times, want 1", calls)
	}
	if gotRemoteAhead != 1 || gotLocalAhead != 1 {
		t.Errorf("counts = (%d, %d), want (1, 1)", gotRemoteAhead, gotLocalAhead)
	}
	if got := readFile(t, filepath.Join(work, "a.txt")); got != "remote\n" {
		t.Errorf("a.txt = %q, want the asked-for side to win", got)
	}
}

// TestSyncAskSkippedOnFastForward: with no local commits there is
// nothing to conflict, so the prompt must not fire.
func TestSyncAskSkippedOnFastForward(t *testing.T) {
	w, _, base, _ := setup(t)
	ctx := context.Background()

	peerCommit(t, base, "p.txt", "peer\n")
	calls := 0
	ask := func(_, _ string, _, _ int) (Prefer, error) {
		calls++
		return PreferRemote, nil
	}
	if err := w.Sync(ctx, SyncOptions{Ask: ask}); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if calls != 0 {
		t.Errorf("ask ran %d times on a fast-forward, want 0", calls)
	}
}

// TestSyncAskAbortLeavesTreeAlone pins the fetch-first order: the ask
// runs before anything is stashed, so aborting it changes nothing.
func TestSyncAskAbortLeavesTreeAlone(t *testing.T) {
	w, _, base, work := setup(t)
	ctx := context.Background()

	divergedMain(t, base, work)
	head := queryGit(t, work, "rev-parse", "HEAD")

	ask := func(_, _ string, _, _ int) (Prefer, error) {
		return PreferNone, errors.New("user aborted")
	}
	err := w.Sync(ctx, SyncOptions{Ask: ask})
	if err == nil || !strings.Contains(err.Error(), "user aborted") {
		t.Fatalf("err = %v, want the abort error", err)
	}
	if out := queryGit(t, work, "status", "--porcelain"); out != "" {
		t.Errorf("tree changed despite abort: %q", out)
	}
	if out := queryGit(t, work, "stash", "list"); out != "" {
		t.Errorf("stash created despite abort: %q", out)
	}
	if queryGit(t, work, "rev-parse", "HEAD") != head {
		t.Error("HEAD moved despite abort")
	}
}

// TestSyncAskAbortExternalTargetStaysPut: aborting the ask must be a
// zero-side-effect exit even for an external target branch, so the ask
// runs before the switch that parks the starting branch's work.
func TestSyncAskAbortExternalTargetStaysPut(t *testing.T) {
	w, _, base, work := setup(t)
	ctx := context.Background()

	runGit(t, work, "push", "-u", "origin", "feature")
	runGit(t, base, "clone", filepath.Join(base, "remote.git"), filepath.Join(base, "peer"))
	peer := filepath.Join(base, "peer")
	runGit(t, peer, "config", "user.name", "Peer")
	runGit(t, peer, "config", "user.email", "peer@example.com")
	writeFile(t, filepath.Join(peer, "a.txt"), "remote\n")
	runGit(t, peer, "add", ".")
	runGit(t, peer, "commit", "-m", "peer: rewrite a.txt")
	runGit(t, peer, "push", "origin", "HEAD:refs/heads/feature")
	runGit(t, work, "checkout", "-q", "feature")
	writeFile(t, filepath.Join(work, "a.txt"), "local\n")
	runGit(t, work, "add", ".")
	runGit(t, work, "commit", "-m", "local: rewrite a.txt")
	runGit(t, work, "checkout", "-q", "main")
	writeFile(t, filepath.Join(work, "a.txt"), "dirty\n")

	var gotBranch string
	var gotAhead, gotLocal int
	ask := func(_, branch string, remoteAhead, localAhead int) (Prefer, error) {
		gotBranch = branch
		gotAhead, gotLocal = remoteAhead, localAhead
		return PreferNone, errors.New("user aborted")
	}
	err := w.Sync(ctx, SyncOptions{Target: "feature", Ask: ask})
	if err == nil || !strings.Contains(err.Error(), "user aborted") {
		t.Fatalf("err = %v, want the abort error", err)
	}
	if gotBranch != "feature" || gotAhead != 1 || gotLocal != 1 {
		t.Errorf("ask saw branch %q counts (%d, %d), want feature (1, 1)", gotBranch, gotAhead, gotLocal)
	}
	if got := queryGit(t, work, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("branch = %q, want to stay on main after the abort", got)
	}
	if got := readFile(t, filepath.Join(work, "a.txt")); got != "dirty\n" {
		t.Errorf("a.txt = %q, want the dirty worktree untouched", got)
	}
	if out := queryGit(t, work, "stash", "list"); out != "" {
		t.Errorf("stash created despite abort: %q", out)
	}
}
