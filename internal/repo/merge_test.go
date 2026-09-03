package repo

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAheadBehindAll(t *testing.T) {
	r, work := setup(t)
	ctx := context.Background()
	g := r.Git

	// feature gains one commit main lacks; main stays put.
	if err := os.WriteFile(filepath.Join(work, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"checkout", "-q", "feature"},
		{"add", "f.txt"},
		{"commit", "-m", "feature work"},
		{"checkout", "-q", "main"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}

	counts, err := r.AheadBehindAll(ctx, "origin")
	if err != nil {
		t.Fatal(err)
	}

	// ahead is commits the branch has that HEAD (main) lacks.
	if got := counts["refs/heads/feature"]; got != [2]int{1, 0} {
		t.Errorf("feature ahead/behind %v, want [1 0]", got)
	}
	if got := counts["refs/heads/main"]; got != [2]int{0, 0} {
		t.Errorf("main ahead/behind %v, want [0 0]", got)
	}
	// origin/main tracks main, which has no unique commits: [0 0].
	if got := counts["refs/remotes/origin/main"]; got != [2]int{0, 0} {
		t.Errorf("origin/main ahead/behind %v, want [0 0]", got)
	}
}

func TestLogOneline(t *testing.T) {
	r, work := setup(t)
	ctx := context.Background()
	g := r.Git

	if err := os.WriteFile(filepath.Join(work, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"checkout", "-q", "feature"},
		{"add", "f.txt"},
		{"commit", "-m", "one"},
		{"commit", "--allow-empty", "-m", "two"},
		{"commit", "--allow-empty", "-m", "three"},
		{"checkout", "-q", "main"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}

	lines, err := r.LogOneline(ctx, "main", "feature", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("got %d lines (%v), want the 2 capped", len(lines), lines)
	}
	if !strings.Contains(lines[0], "three") {
		t.Errorf("newest line %q should be the latest commit", lines[0])
	}
}
