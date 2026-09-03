package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EstebanForge/elgit/internal/gitx"
)

// commitFile writes a file on the current branch and commits it.
func commitFile(t *testing.T, dir, path, content, msg string) {
	t.Helper()
	g := &gitx.Runner{Dir: dir}
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"add", path},
		{"commit", "-m", msg},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
}

// switchTo checks out a branch.
func switchTo(t *testing.T, dir, branch string) {
	t.Helper()
	if _, err := (&gitx.Runner{Dir: dir}).Mutate(context.Background(), "switch", branch); err != nil {
		t.Fatal(err)
	}
}

// headParents returns the parents of HEAD, so tests can tell a
// fast-forward from a merge commit.
func headParents(t *testing.T, dir string) []string {
	t.Helper()
	out, err := (&gitx.Runner{Dir: dir}).Query(context.Background(), "log", "-1", "--pretty=%P")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(strings.TrimSpace(out))
}

// divergeRepo gives main and feature one commit each past the shared
// init commit, with main checked out.
func divergeRepo(t *testing.T) string {
	t.Helper()
	dir := setupRepo(t)
	switchTo(t, dir, "feature")
	commitFile(t, dir, "feature.txt", "feature\n", "feature work")
	switchTo(t, dir, "main")
	commitFile(t, dir, "main.txt", "main\n", "main work")
	return dir
}

func TestMergeFastForward(t *testing.T) {
	dir := setupRepo(t)
	switchTo(t, dir, "feature")
	commitFile(t, dir, "feature.txt", "feature\n", "feature work")
	switchTo(t, dir, "main")

	featureHead, err := (&gitx.Runner{Dir: dir}).Query(context.Background(), "rev-parse", "feature")
	if err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, dir, "merge", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Merged feature into main") {
		t.Errorf("output %q should report the merge", out)
	}
	if !strings.Contains(out, "Run elgit sync to push") {
		t.Errorf("output %q should hand off to sync", out)
	}
	mainHead, err := (&gitx.Runner{Dir: dir}).Query(context.Background(), "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(mainHead) != strings.TrimSpace(featureHead) {
		t.Errorf("HEAD %s should equal feature %s after a fast-forward", mainHead, featureHead)
	}
}

func TestMergeDivergedCreatesMergeCommit(t *testing.T) {
	dir := divergeRepo(t)

	out, err := execute(t, dir, "merge", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Merged feature into main") {
		t.Errorf("output %q should report the merge", out)
	}
	if parents := headParents(t, dir); len(parents) != 2 {
		t.Errorf("HEAD parents %v, want two (merge commit)", parents)
	}
	if n := commitCount(t, dir); n != 4 {
		t.Errorf("commit count %d, want 4 (init, main, feature, merge)", n)
	}
}

func TestMergeHeaderListsCommits(t *testing.T) {
	dir := divergeRepo(t)
	out, err := execute(t, dir, "merge", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Merging 1 commit from feature into main") {
		t.Errorf("output %q should name the merge in a header", out)
	}
	if !strings.Contains(out, "feature work") {
		t.Errorf("output %q should list the incoming commit", out)
	}
}

func TestMergeAlreadyUpToDate(t *testing.T) {
	dir := setupRepo(t) // feature sits at the same commit as main
	out, err := execute(t, dir, "merge", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Already up to date") {
		t.Errorf("output %q should short-circuit", out)
	}
}

func TestMergeRefusesDirtyTree(t *testing.T) {
	dir := setupRepo(t)
	switchTo(t, dir, "feature")
	commitFile(t, dir, "feature.txt", "feature\n", "feature work")
	switchTo(t, dir, "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := execute(t, dir, "merge", "feature")
	if err == nil {
		t.Fatal("expected a refusal on a dirty tree")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("error %q should name the dirty tree", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); statErr == nil {
		t.Error("MERGE_HEAD should not exist: no merge may start on a dirty tree")
	}
}

func TestMergeConflictStops(t *testing.T) {
	dir := setupRepo(t)
	switchTo(t, dir, "feature")
	commitFile(t, dir, "a.txt", "feature wins\n", "feature edit")
	switchTo(t, dir, "main")
	commitFile(t, dir, "a.txt", "main wins\n", "main edit")

	out, err := execute(t, dir, "merge", "feature")
	if err == nil {
		t.Fatal("expected a conflict error")
	}
	if !strings.Contains(out, "resolve the conflicts") {
		t.Errorf("output %q should print remedies", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".git", "MERGE_HEAD")); statErr != nil {
		t.Error("MERGE_HEAD should exist: elgit must not abort the conflicted merge")
	}
}

func TestMergeSelfRefused(t *testing.T) {
	dir := setupRepo(t)
	_, err := execute(t, dir, "merge", "main")
	if err == nil {
		t.Fatal("expected a self-merge refusal")
	}
	if !strings.Contains(err.Error(), "already checked out") {
		t.Errorf("error %q should name the self-merge", err)
	}
}

func TestMergeRemoteOnlyBranch(t *testing.T) {
	dir := setupRepo(t)
	switchTo(t, dir, "feature")
	commitFile(t, dir, "feature.txt", "feature\n", "feature work")
	g := &gitx.Runner{Dir: dir}
	if _, err := g.Mutate(context.Background(), "push", "origin", "feature"); err != nil {
		t.Fatal(err)
	}
	switchTo(t, dir, "main")
	if _, err := g.Mutate(context.Background(), "fetch", "origin"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(context.Background(), "branch", "-D", "feature"); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, dir, "merge", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Merged feature into main") {
		t.Errorf("output %q should report the merge", out)
	}
	// main has no unique commits, so git fast-forwards: one parent, and
	// the merged file must be present.
	if parents := headParents(t, dir); len(parents) != 1 {
		t.Errorf("HEAD parents %v, want one (fast-forward)", parents)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "feature.txt")); statErr != nil {
		t.Errorf("feature.txt should exist after merging the remote-only branch: %v", statErr)
	}
}

func TestMergeFakeDoesNotMerge(t *testing.T) {
	dir := divergeRepo(t)
	out, err := execute(t, dir, "merge", "--fake", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "fake") {
		t.Errorf("output %q should mention fake mode", out)
	}
	if parents := headParents(t, dir); len(parents) != 1 {
		t.Errorf("HEAD parents %v, want one: fake mode must not merge", parents)
	}
}

func TestMergeRemoteOnlyPrefix(t *testing.T) {
	dir := setupRepo(t)
	switchTo(t, dir, "feature")
	commitFile(t, dir, "feature.txt", "feature\n", "feature work")
	g := &gitx.Runner{Dir: dir}
	if _, err := g.Mutate(context.Background(), "push", "origin", "feature"); err != nil {
		t.Fatal(err)
	}
	switchTo(t, dir, "main")
	if _, err := g.Mutate(context.Background(), "fetch", "origin"); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(context.Background(), "branch", "-D", "feature"); err != nil {
		t.Fatal(err)
	}

	// Regression: the targeted fetch built its refspec from the typed
	// prefix (refs/heads/fea) instead of the resolved branch name, and
	// the fetch failed with "couldn't find remote ref".
	out, err := execute(t, dir, "merge", "fea")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Merged feature into main") {
		t.Errorf("output %q should report the resolved merge", out)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "feature.txt")); statErr != nil {
		t.Errorf("feature.txt should exist after the merge: %v", statErr)
	}
}

func TestMergeDirtyTreeRefusedBeforePickerNetwork(t *testing.T) {
	dir := setupRepo(t)
	// Regression: the interactive path fetched and prompted before the
	// dirty-tree gate ran. The gate must win without an argument too.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, dir, "merge") // no branch: would open the picker
	if err == nil {
		t.Fatal("expected a refusal on a dirty tree")
	}
	if !strings.Contains(err.Error(), "uncommitted changes") {
		t.Errorf("error %q should name the dirty tree", err)
	}
	if strings.Contains(out, "Switch to") || strings.Contains(out, "Merge into") {
		t.Errorf("output %q must not open the picker on a dirty tree", out)
	}
}

func TestMergeUnknownBranch(t *testing.T) {
	dir := setupRepo(t)
	_, err := execute(t, dir, "merge", "nope")
	if err == nil {
		t.Fatal("expected an unknown-branch error")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error %q should name the missing branch", err)
	}
}

func TestMergePrefixResolves(t *testing.T) {
	dir := setupRepo(t)
	switchTo(t, dir, "feature")
	commitFile(t, dir, "feature.txt", "feature\n", "feature work")
	switchTo(t, dir, "main")

	out, err := execute(t, dir, "merge", "fea")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Merged feature into main") {
		t.Errorf("output %q should report the resolved merge", out)
	}
}
