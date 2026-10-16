package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EstebanForge/elgit/internal/gitx"
)

// diffRepo builds a repository with one modified tracked file and one
// untracked file.
func diffRepo(t *testing.T) string {
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
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "commit", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDiffClean(t *testing.T) {
	dir := setupRepo(t)
	out, err := execute(t, dir, "diff")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Working tree clean") {
		t.Errorf("diff output %q should report a clean tree", out)
	}
}

func TestDiffListsFilesAndShowsDiff(t *testing.T) {
	dir := diffRepo(t)
	out, err := execute(t, dir, "diff")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"On branch main",
		"tracked.txt", "+1 -1",
		"note.txt", "untracked",
		"TWO", // the streamed diff body
	} {
		if !strings.Contains(out, want) {
			t.Errorf("diff output missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "--all") {
		t.Errorf("diff output should hint at --all for untracked content:\n%s", out)
	}
	if strings.Contains(out, "scratch") {
		t.Errorf("untracked content must stay out without --all:\n%s", out)
	}
}

func TestDiffAllShowsUntrackedContent(t *testing.T) {
	dir := diffRepo(t)
	out, err := execute(t, dir, "diff", "--all")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"tracked.txt", "note.txt", "scratch"} {
		if !strings.Contains(out, want) {
			t.Errorf("diff --all output missing %q in:\n%s", want, out)
		}
	}
}

func TestDiffUnborn(t *testing.T) {
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
	out, err := execute(t, dir, "diff")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.txt") {
		t.Errorf("unborn repo diff output should list the untracked file:\n%s", out)
	}
}

func TestDiffOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := execute(t, dir, "diff")
	if err == nil {
		t.Fatal("expected an error outside a repository")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error %q should mention the repository", err)
	}
}

// TestDiffAllNonASCIIUntracked: ls-files output must survive unusual
// names byte-for-byte, or the untracked content expansion diffs a quoted
// path that does not exist.
func TestDiffAllNonASCIIUntracked(t *testing.T) {
	dir := diffRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "café.txt"), []byte("café content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, dir, "diff", "--all")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "café content") {
		t.Errorf("diff --all should show the non-ASCII file's content:\n%s", out)
	}
}

// TestDiffUnbornShowsWorktreeEdits: on an unborn HEAD the diff base is
// the empty tree, so unstaged edits count on top of staged ones.
func TestDiffUnbornShowsWorktreeEdits(t *testing.T) {
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
	if _, err := g.Mutate(ctx, "add", "a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, dir, "diff")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"two", "+2 -0"} {
		if !strings.Contains(out, want) {
			t.Errorf("unborn diff output missing %q in:\n%s", want, out)
		}
	}
}
