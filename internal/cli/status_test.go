package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EstebanForge/elgit/internal/gitx"
)

func TestStatusClean(t *testing.T) {
	dir := setupRepo(t)
	out, err := execute(t, dir, "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"On branch main", "origin/main", "up to date", "Working tree clean"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output %q missing %q", out, want)
		}
	}
}

func TestStatusCounts(t *testing.T) {
	dir := setupRepo(t)
	ctx := context.Background()
	g := &gitx.Runner{Dir: dir}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Mutate(ctx, "add", "b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "c.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, dir, "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 staged", "1 modified", "1 untracked"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output %q missing %q", out, want)
		}
	}
}

func TestStatusAheadBehind(t *testing.T) {
	dir := setupRepo(t)
	ctx := context.Background()
	g := &gitx.Runner{Dir: dir}

	// Ahead: a local commit the remote lacks.
	if err := os.WriteFile(filepath.Join(dir, "ahead.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"add", "ahead.txt"},
		{"commit", "-m", "local only"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}

	// Behind: another clone pushes, the repo fetches without merging.
	clone := filepath.Join(dir, "..", "clone")
	for _, argv := range [][]string{
		{"clone", filepath.Join(dir, "..", "remote.git"), clone},
		{"-C", clone, "config", "user.name", "Other"},
		{"-C", clone, "config", "user.email", "other@example.com"},
	} {
		if _, err := g.Query(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	if err := os.WriteFile(filepath.Join(clone, "remote.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"-C", clone, "add", "remote.txt"},
		{"-C", clone, "commit", "-m", "remote only"},
		{"-C", clone, "push", "origin", "main"},
		{"fetch", "origin"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}

	out, err := execute(t, dir, "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1 ahead", "1 behind"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output %q missing %q", out, want)
		}
	}
}

func TestStatusNoUpstream(t *testing.T) {
	dir := setupRepo(t)
	ctx := context.Background()
	g := &gitx.Runner{Dir: dir}
	if _, err := g.Mutate(ctx, "switch", "feature"); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, dir, "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"On branch feature", "No upstream"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output %q missing %q", out, want)
		}
	}
}

func TestStatusDetached(t *testing.T) {
	dir := setupRepo(t)
	ctx := context.Background()
	g := &gitx.Runner{Dir: dir}
	if _, err := g.Mutate(ctx, "checkout", "--detach", "HEAD"); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, dir, "status")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "detached") {
		t.Errorf("status output %q missing detached state", out)
	}
}

func TestStatusNoCommits(t *testing.T) {
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
	out, err := execute(t, dir, "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"On branch main", "No commits yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output %q missing %q", out, want)
		}
	}
}

func TestStatusNoCommitsWithFiles(t *testing.T) {
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
	// An unborn repository can still hold files; the glance must count
	// them instead of dropping them after the "No commits yet" line.
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execute(t, dir, "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"On branch main", "No commits yet", "1 untracked"} {
		if !strings.Contains(out, want) {
			t.Errorf("status output %q missing %q", out, want)
		}
	}
}

func TestStatusOutsideRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := execute(t, dir, "status")
	if err == nil {
		t.Fatal("expected an error outside a repository")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Errorf("error %q should mention the repository", err)
	}
}
