package repo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/EstebanForge/elgit/internal/gitx"
)

// changesRepo builds a repository holding one of every change class:
// unstaged modification, staged+unstaged modification, staged addition,
// staged rename, staged binary, and an untracked file. It returns the
// repo dir and the expected entries keyed by path.
func changesRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	g := &gitx.Runner{Dir: dir}
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
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("unstaged.txt", "one\ntwo\nthree\n")
	write("partial.txt", "one\ntwo\n")
	write("old-name.txt", "moved body\n")
	write("bin.dat", "BINARY\x00\x01")
	for _, argv := range [][]string{
		{"add", "."},
		{"commit", "-m", "init"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	write("unstaged.txt", "one\nTWO\nthree\n")
	write("partial.txt", "one\ntwo\nthree\nfour\n")
	if _, err := g.Mutate(ctx, "add", "partial.txt"); err != nil {
		t.Fatal(err)
	}
	write("partial.txt", "one\ntwo\nthree\nfive\n")
	write("added.txt", "new file\n")
	if _, err := g.Mutate(ctx, "add", "added.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "mv", "old-name.txt", "new-name.txt"); err != nil {
		t.Fatal(err)
	}
	write("new-name.txt", "moved body\nx\n")
	if _, err := g.Mutate(ctx, "add", "new-name.txt"); err != nil {
		t.Fatal(err)
	}
	write("bin.dat", "BINARY\x00\x01\x02\x03")
	if _, err := g.Mutate(ctx, "add", "bin.dat"); err != nil {
		t.Fatal(err)
	}
	write("note.txt", "scratch\n")
	return dir
}

func TestChangesClassifiesFiles(t *testing.T) {
	dir := changesRepo(t)
	changes, err := Open(&gitx.Runner{Dir: dir}).Changes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	got := make(map[string]FileChange, len(changes))
	for _, c := range changes {
		got[c.Path] = c
	}
	want := map[string]FileChange{
		"unstaged.txt": {Code: " M", Path: "unstaged.txt", Added: 1, Deleted: 1, Counted: true},
		"partial.txt":  {Code: "MM", Path: "partial.txt", Added: 2, Deleted: 0, Counted: true},
		"added.txt":    {Code: "A ", Path: "added.txt", Added: 1, Deleted: 0, Counted: true},
		// Renames are keyed by the new path in numstat -z output; the
		// 0/0 of a pure rename renders no counts.
		"new-name.txt": {Code: "R ", Path: "new-name.txt", Added: 1, Deleted: 0, Counted: true},
		"bin.dat":      {Code: "M ", Path: "bin.dat", Binary: true, Counted: true},
		"note.txt":     {Code: "??", Path: "note.txt"},
	}
	if len(changes) != len(want) {
		t.Fatalf("Changes returned %d entries, want %d: %+v", len(changes), len(want), changes)
	}
	for path, w := range want {
		if got[path] != w {
			t.Errorf("%s: got %+v, want %+v", path, got[path], w)
		}
	}
}

func TestChangesClean(t *testing.T) {
	dir := t.TempDir()
	g := &gitx.Runner{Dir: dir}
	ctx := context.Background()
	for _, argv := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.com"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		if _, err := g.Query(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	changes, err := Open(&gitx.Runner{Dir: dir}).Changes(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("clean tree returned %+v", changes)
	}
}

func TestChangesUnborn(t *testing.T) {
	dir := t.TempDir()
	g := &gitx.Runner{Dir: dir}
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
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changes, err := Open(g).Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0] != (FileChange{Code: "??", Path: "a.txt"}) {
		t.Errorf("unborn repo returned %+v", changes)
	}
}
