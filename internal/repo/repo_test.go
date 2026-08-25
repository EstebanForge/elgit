package repo

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/EstebanForge/elgit/internal/gitx"
)

// setup creates a work repository with a bare remote: main published,
// feature local and unpublished.
func setup(t *testing.T) (*Repo, string) {
	t.Helper()
	base := t.TempDir()
	work := filepath.Join(base, "work")
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
		{"init", "--bare", "-b", "main", filepath.Join(base, "remote.git")},
		{"remote", "add", "origin", filepath.Join(base, "remote.git")},
		{"push", "-u", "origin", "main"},
		{"branch", "feature"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	return Open(g), work
}

func TestCurrentBranch(t *testing.T) {
	r, _ := setup(t)
	branch, err := r.CurrentBranch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Errorf("CurrentBranch = %q, want main", branch)
	}
}

func TestIsDirty(t *testing.T) {
	r, work := setup(t)
	ctx := context.Background()

	dirty, err := r.IsDirty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if dirty {
		t.Error("IsDirty = true on clean tree")
	}

	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, err = r.IsDirty(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Error("IsDirty = false on modified file")
	}
}

func TestBranches(t *testing.T) {
	r, _ := setup(t)
	branches, err := r.Branches(context.Background(), "origin")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Branch{
		"main":    {Name: "main", IsPublished: true, IsCurrent: true},
		"feature": {Name: "feature", IsPublished: false, IsCurrent: false},
	}
	if len(branches) != len(want) {
		t.Fatalf("Branches = %v, want %v", branches, want)
	}
	for _, b := range branches {
		w, ok := want[b.Name]
		if !ok {
			t.Fatalf("unexpected branch %q", b.Name)
		}
		if b.IsPublished != w.IsPublished || b.IsCurrent != w.IsCurrent {
			t.Errorf("branch %q = %+v, want %+v", b.Name, b, w)
		}
	}
}

func TestBranchesLocalOnly(t *testing.T) {
	r, _ := setup(t)
	branches, err := r.Branches(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range branches {
		if b.IsPublished {
			t.Errorf("branch %q marked published without remote listing", b.Name)
		}
	}
}

func TestRemoteDefault(t *testing.T) {
	r, _ := setup(t)
	remote, err := r.Remote(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if remote != "origin" {
		t.Errorf("Remote = %q, want origin", remote)
	}
}

func TestRemoteConfigured(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()

	for _, argv := range [][]string{
		{"config", "legit.remote", "nope"},
	} {
		if _, err := r.Git.Mutate(ctx, argv...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Remote(ctx); err == nil {
		t.Error("Remote with unknown legit.remote should fail without fallback")
	}

	if _, err := r.Git.Mutate(ctx, "config", "legit.remoteFallback", "true"); err != nil {
		t.Fatal(err)
	}
	remote, err := r.Remote(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if remote != "origin" {
		t.Errorf("Remote fallback = %q, want origin", remote)
	}
}

func TestConfigBool(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()

	if got := r.ConfigBool(ctx, "legit.smartMerge", true); !got {
		t.Error("ConfigBool default = false, want true")
	}
	if _, err := r.Git.Mutate(ctx, "config", "legit.smartMerge", "false"); err != nil {
		t.Fatal(err)
	}
	if got := r.ConfigBool(ctx, "legit.smartMerge", true); got {
		t.Error("ConfigBool(explicit false) = true, want false")
	}
}

func TestFuzzyMatchBranch(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()

	tests := []struct {
		in, want string
		found    bool
	}{
		{"main", "main", true},
		{"fea", "feature", true},
		{"f", "feature", true},
		{"zzz", "zzz", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, found := r.FuzzyMatchBranch(ctx, "origin", tt.in)
		if got != tt.want || found != tt.found {
			t.Errorf("FuzzyMatchBranch(%q) = (%q, %v), want (%q, %v)", tt.in, got, found, tt.want, tt.found)
		}
	}
}

func TestHasRemoteBranch(t *testing.T) {
	r, _ := setup(t)
	ctx := context.Background()
	if !r.HasRemoteBranch(ctx, "origin", "main") {
		t.Error("HasRemoteBranch(main) = false, want true")
	}
	if r.HasRemoteBranch(ctx, "origin", "feature") {
		t.Error("HasRemoteBranch(feature) = true, want false")
	}
}
