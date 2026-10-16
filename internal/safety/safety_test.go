package safety

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EstebanForge/elgit/internal/gitx"
)

func setup(t *testing.T) *gitx.Runner {
	t.Helper()
	dir := t.TempDir()
	g := &gitx.Runner{Dir: dir}
	ctx := context.Background()
	for _, argv := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.com"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	return g
}

func TestInProgressClean(t *testing.T) {
	g := setup(t)
	kind, err := InProgressOp(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "" {
		t.Errorf("InProgressOp = %q, want empty", kind)
	}
}

func TestInProgressMerge(t *testing.T) {
	g := setup(t)
	dir := g.Dir
	ctx := context.Background()

	for _, argv := range [][]string{
		{"checkout", "-b", "side"},
		{"commit", "--allow-empty", "-m", "side"},
		{"checkout", "main"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	// Force a real merge conflict.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "commit", "-m", "main change"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "checkout", "side"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "commit", "-m", "side change"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "checkout", "main"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "merge", "side"); err == nil {
		t.Fatal("expected merge conflict")
	}

	kind, err := InProgressOp(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "merge" {
		t.Errorf("InProgressOp = %q, want merge", kind)
	}
}

func TestInProgressRebase(t *testing.T) {
	g := setup(t)
	ctx := context.Background()

	for _, argv := range [][]string{
		{"checkout", "-b", "topic"},
		{"commit", "--allow-empty", "-m", "topic"},
		{"checkout", "main"},
		{"commit", "--allow-empty", "-m", "main moves"},
		{"checkout", "topic"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	if err := os.WriteFile(filepath.Join(g.Dir, "conflict.txt"), []byte("topic\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "commit", "-m", "topic conflict file"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "checkout", "main"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.Dir, "conflict.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "commit", "-m", "main conflict file"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "checkout", "topic"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "rebase", "main"); err == nil {
		t.Fatal("expected rebase conflict")
	}

	kind, err := InProgressOp(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	if kind != "rebase" {
		t.Errorf("InProgressOp = %q, want rebase", kind)
	}
}

func TestLockAcquireRelease(t *testing.T) {
	g := setup(t)
	ctx := context.Background()

	lock, err := Acquire(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(ctx, g); err == nil {
		t.Fatal("second acquire should fail while held")
	} else if !strings.Contains(err.Error(), "elgit process") {
		t.Errorf("err = %v, want pid message", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	again, err := Acquire(ctx, g)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	if err := again.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestLockTakesOverStale(t *testing.T) {
	g := setup(t)
	ctx := context.Background()

	out, err := g.Query(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		t.Fatal(err)
	}
	path := resolvePath(g.Dir, strings.TrimSpace(out))
	path = filepath.Join(path, "elgit.lock")
	// Pid 999999 is not running on this system.
	if err := os.WriteFile(path, []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lock, err := Acquire(ctx, g)
	if err != nil {
		t.Fatalf("acquire over stale lock: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
}

func TestLockConcurrentStealSingleWinner(t *testing.T) {
	g := setup(t)
	ctx := context.Background()

	out, err := g.Query(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(resolvePath(g.Dir, strings.TrimSpace(out)), "elgit.lock")
	if err := os.WriteFile(path, []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Two racers fight over the same stale lock; exactly one must win.
	won := make(chan *Lock, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if lock, err := Acquire(ctx, g); err == nil {
				won <- lock
			}
		}()
	}
	wg.Wait()
	close(won)

	var winners []*Lock
	for lock := range won {
		winners = append(winners, lock)
	}
	if len(winners) != 1 {
		t.Fatalf("lock winners = %d, want exactly 1", len(winners))
	}
	if err := winners[0].Release(); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseDoesNotDeleteForeignLock(t *testing.T) {
	g := setup(t)
	ctx := context.Background()

	out, err := g.Query(ctx, "rev-parse", "--git-common-dir")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(resolvePath(g.Dir, strings.TrimSpace(out)), "elgit.lock")
	// A live-looking foreign pid; another process owns this lock.
	if err := os.WriteFile(path, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lock := &Lock{path: path}
	if err := lock.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("foreign lock was deleted by Release: %v", err)
	}
}

func TestLedgerRoundtrip(t *testing.T) {
	g := setup(t)
	ctx := context.Background()

	led, err := LoadLedger(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	first := Entry{StashOID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Branch: "main", Purpose: "switching"}
	second := Entry{StashOID: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Branch: "dev", Purpose: "syncing"}
	if err := led.Record(first); err != nil {
		t.Fatal(err)
	}
	if err := led.Record(second); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadLedger(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(reloaded.Entries))
	}
	got, ok := reloaded.Find("main", "switching")
	if !ok || got.StashOID != first.StashOID {
		t.Errorf("Find = (%v, %v), want first entry", got, ok)
	}
	if _, ok := reloaded.Find("main", "syncing"); ok {
		t.Error("Find(main, syncing) should miss")
	}

	if err := reloaded.Remove(first.StashOID); err != nil {
		t.Fatal(err)
	}
	final, err := LoadLedger(ctx, g)
	if err != nil {
		t.Fatal(err)
	}
	if len(final.Entries) != 1 || final.Entries[0].StashOID != second.StashOID {
		t.Errorf("entries after remove = %v, want only second", final.Entries)
	}

	var buf bytes.Buffer
	final.WarnPending(&buf, "dev")
	if !strings.Contains(buf.String(), "unrestored elgit stash") {
		t.Errorf("WarnPending output = %q", buf.String())
	}
	// Entries for other branches are parked by design and stay silent.
	var quiet bytes.Buffer
	final.WarnPending(&quiet, "main")
	if strings.Contains(quiet.String(), "unrestored") {
		t.Errorf("WarnPending for other branch printed: %q", quiet.String())
	}
}

func TestStashIndexByOID(t *testing.T) {
	g := setup(t)
	dir := g.Dir
	ctx := context.Background()

	// A stash needs a tracked modification; untracked files are not stashed
	// without --include-untracked.
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "commit", "-m", "add f"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "stash", "push", "-m", "test stash"); err != nil {
		t.Fatal(err)
	}
	out, err := g.Query(ctx, "rev-parse", "stash@{0}")
	if err != nil {
		t.Fatal(err)
	}
	oid := strings.TrimSpace(out)
	idx, ok := StashIndexByOID(ctx, g, oid)
	if !ok || idx != 0 {
		t.Errorf("StashIndexByOID = (%d, %v), want (0, true)", idx, ok)
	}
	if _, ok := StashIndexByOID(ctx, g, "0000000000000000000000000000000000000000"); ok {
		t.Error("StashIndexByOID for unknown OID should miss")
	}
}

// TestWarnPendingEmptyBranchWarnsAll covers detached HEAD: with no branch
// name to filter by, every parked entry must warn instead of staying
// silent.
func TestWarnPendingEmptyBranchWarnsAll(t *testing.T) {
	l := &Ledger{Entries: []Entry{
		{StashOID: strings.Repeat("a", 40), Branch: "main", Created: time.Unix(0, 0).UTC()},
		{StashOID: strings.Repeat("b", 40), Branch: "feature", Created: time.Unix(0, 0).UTC()},
	}}

	var all bytes.Buffer
	l.WarnPending(&all, "")
	for _, want := range []string{`branch "main"`, `branch "feature"`} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("WarnPending(\"\") = %q, want it to name %s", all.String(), want)
		}
	}

	var onlyMain bytes.Buffer
	l.WarnPending(&onlyMain, "main")
	if strings.Contains(onlyMain.String(), "feature") {
		t.Errorf("WarnPending(main) = %q, want no feature entry", onlyMain.String())
	}
}
