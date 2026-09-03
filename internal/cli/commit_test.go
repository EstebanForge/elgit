package cli

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/EstebanForge/elgit/internal/gitx"
)

// lastSubject returns the subject of HEAD.
func lastSubject(t *testing.T, dir string) string {
	t.Helper()
	out, err := (&gitx.Runner{Dir: dir}).Query(context.Background(), "log", "-1", "--pretty=%s")
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	return strings.TrimSpace(out)
}

// commitCount returns the number of commits on HEAD.
func commitCount(t *testing.T, dir string) int {
	t.Helper()
	out, err := (&gitx.Runner{Dir: dir}).Query(context.Background(), "rev-list", "--count", "HEAD")
	if err != nil {
		t.Fatalf("rev-list: %v", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		t.Fatalf("rev-list count %q: %v", out, err)
	}
	return n
}

func TestCommitStagesTrackedOnly(t *testing.T) {
	dir := setupRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "z.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, dir, "commit", "-m", "update a")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Committed to main") {
		t.Errorf("output %q should report the commit", out)
	}
	if got := lastSubject(t, dir); got != "update a" {
		t.Errorf("HEAD subject %q, want %q", got, "update a")
	}
	// The untracked file must survive untouched and unstaged.
	if _, err := os.Stat(filepath.Join(dir, "z.txt")); err != nil {
		t.Errorf("untracked file was removed: %v", err)
	}
	g := &gitx.Runner{Dir: dir}
	status, err := g.Query(context.Background(), "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(status, "??") {
		t.Errorf("status %q should still list the untracked file", status)
	}
	if strings.Contains(status, "z.txt") && !strings.HasPrefix(status, "??") {
		t.Errorf("status %q should not stage z.txt", status)
	}
}

func TestCommitAllIncludesUntracked(t *testing.T) {
	dir := setupRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "z.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, dir, "commit", "--all", "-m", "add z"); err != nil {
		t.Fatal(err)
	}
	if got := lastSubject(t, dir); got != "add z" {
		t.Errorf("HEAD subject %q, want %q", got, "add z")
	}
	g := &gitx.Runner{Dir: dir}
	status, err := g.Query(context.Background(), "status", "--porcelain")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(status, "z.txt") {
		t.Errorf("status %q should be clean of z.txt", status)
	}
}

func TestCommitStagedFilesWithoutAll(t *testing.T) {
	dir := setupRepo(t)
	ctx := context.Background()
	g := &gitx.Runner{Dir: dir}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "add", "b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, dir, "commit", "-m", "add b"); err != nil {
		t.Fatal(err)
	}
	if got := lastSubject(t, dir); got != "add b" {
		t.Errorf("HEAD subject %q, want %q", got, "add b")
	}
}

func TestCommitCleanTree(t *testing.T) {
	dir := setupRepo(t)
	out, err := execute(t, dir, "commit", "-m", "nothing")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Nothing to commit") {
		t.Errorf("output %q should report nothing to commit", out)
	}
	if strings.Contains(out, "Would commit") {
		t.Errorf("output %q must not fake-report a commit after the gate", out)
	}
	if got := lastSubject(t, dir); got != "init" {
		t.Errorf("HEAD subject %q should still be init", got)
	}
}

func TestCommitUntrackedOnly(t *testing.T) {
	dir := setupRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "z.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, dir, "commit", "-m", "nope")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No tracked changes") {
		t.Errorf("output %q should suggest --all", out)
	}
	if strings.Contains(out, "Would commit") {
		t.Errorf("output %q must not fake-report a commit after the gate", out)
	}
	if got := lastSubject(t, dir); got != "init" {
		t.Errorf("HEAD subject %q should still be init", got)
	}
}

func TestCommitAmendRewords(t *testing.T) {
	dir := setupRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, dir, "commit", "-m", "first"); err != nil {
		t.Fatal(err)
	}
	// Clean-tree amend: a pure reword must go through.
	if _, err := execute(t, dir, "commit", "--amend", "-m", "reworded"); err != nil {
		t.Fatal(err)
	}
	if got := lastSubject(t, dir); got != "reworded" {
		t.Errorf("HEAD subject %q, want %q", got, "reworded")
	}
	if n := commitCount(t, dir); n != 2 {
		t.Errorf("commit count %d, want 2 (amend must not add a commit)", n)
	}
}

func TestCommitDescription(t *testing.T) {
	dir := setupRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, dir, "commit", "-m", "subject line", "-d", "longer body"); err != nil {
		t.Fatal(err)
	}
	out, err := (&gitx.Runner{Dir: dir}).Query(context.Background(), "log", "-1", "--pretty=%B")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "subject line") || !strings.Contains(out, "longer body") {
		t.Errorf("message %q should hold subject and description", out)
	}
}

func TestCommitInitialRepo(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	g := &gitx.Runner{Dir: dir}
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
	if _, err := execute(t, dir, "commit", "--all", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	if got := lastSubject(t, dir); got != "init" {
		t.Errorf("HEAD subject %q, want %q", got, "init")
	}
}

func TestCommitFakeLeavesTree(t *testing.T) {
	dir := setupRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, dir, "commit", "--fake", "-m", "fake commit")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "fake") {
		t.Errorf("output %q should mention fake mode", out)
	}
	if got := lastSubject(t, dir); got != "init" {
		t.Errorf("HEAD subject %q should still be init", got)
	}
}

func TestCommitRefusesMidMerge(t *testing.T) {
	dir := setupRepo(t)
	ctx := context.Background()
	g := &gitx.Runner{Dir: dir}
	head, err := g.Query(ctx, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	mergeHead := filepath.Join(dir, ".git", "MERGE_HEAD")
	if err := os.WriteFile(mergeHead, []byte(strings.TrimSpace(head)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = execute(t, dir, "commit", "-m", "nope")
	if err == nil {
		t.Fatal("expected a mid-merge refusal")
	}
	if !strings.Contains(err.Error(), "merge is in progress") {
		t.Errorf("error %q should name the in-progress operation", err)
	}
}

func TestCommitCleanTreeNoMessageExitsClean(t *testing.T) {
	dir := setupRepo(t)
	// Regression: the message prompt (and its non-TTY error) must not run
	// before the emptiness gate. A clean tree exits 0 without -m.
	out, err := execute(t, dir, "commit")
	if err != nil {
		t.Fatalf("clean-tree commit without -m should exit 0, got %v", err)
	}
	if !strings.Contains(out, "Nothing to commit") {
		t.Errorf("output %q should report nothing to commit", out)
	}
	if strings.Contains(out, "-m") {
		t.Errorf("output %q must not demand -m on a clean tree", out)
	}
}

func TestCommitRefusesDetachedHead(t *testing.T) {
	dir := setupRepo(t)
	ctx := context.Background()
	g := &gitx.Runner{Dir: dir}
	if _, err := g.Mutate(ctx, "checkout", "--detach", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := execute(t, dir, "commit", "-m", "nope")
	if err == nil {
		t.Fatal("expected a detached-HEAD refusal")
	}
	if !strings.Contains(err.Error(), "detached") {
		t.Errorf("error %q should name the detached state", err)
	}
}

func TestCommitReportsHookLeftFiles(t *testing.T) {
	dir := setupRepo(t)
	// A pre-commit hook that edits a tracked file leaves it unstaged;
	// the commit must land and the leftover must be reported.
	hook := filepath.Join(dir, ".git", "hooks", "pre-commit")
	script := "#!/bin/sh\necho hooked >> a.txt\n"
	if err := os.WriteFile(hook, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, dir, "commit", "--all", "-m", "with hook")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Committed to main") {
		t.Errorf("output %q should report the commit", out)
	}
	if !strings.Contains(out, "hooks left 1 tracked file(s) modified") {
		t.Errorf("output %q should name the hook-modified file", out)
	}
	if got := lastSubject(t, dir); got != "with hook" {
		t.Errorf("HEAD subject %q, want %q", got, "with hook")
	}
}

func TestCommitRequiresMessage(t *testing.T) {
	dir := setupRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := execute(t, dir, "commit")
	if err == nil {
		t.Fatal("expected an error without -m on a non-terminal")
	}
	if !strings.Contains(err.Error(), "-m") {
		t.Errorf("error %q should point at -m", err)
	}
}
