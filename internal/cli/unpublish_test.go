package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/EstebanForge/elgit/internal/gitx"
	"github.com/EstebanForge/elgit/internal/repo"
)

func TestPublishedBranchItems(t *testing.T) {
	branches := []repo.Branch{
		{Name: "main", IsLocal: true, IsPublished: true, IsCurrent: true},
		{Name: "feature", IsLocal: true, IsPublished: true},
		{Name: "local-only", IsLocal: true},
		{Name: "remote-only", IsPublished: true},
		{Name: "gone", IsPublished: false},
	}

	items := publishedBranchItems(branches)
	if len(items) != 3 {
		t.Fatalf("got %d items (%v), want 3 published branches", len(items), items)
	}

	byName := map[string]string{}
	for _, it := range items {
		byName[it.Name] = it.Detail
	}
	if got := byName["main"]; !strings.Contains(got, "current branch") {
		t.Errorf("main detail %q should mark the current branch", got)
	}
	if got := byName["feature"]; got != "(published)" {
		t.Errorf("feature detail %q, want the plain published marker", got)
	}
	if got := byName["remote-only"]; got != "(remote only)" {
		t.Errorf("remote-only detail %q, want the remote-only marker (no local copy)", got)
	}
	if _, ok := byName["local-only"]; ok {
		t.Error("local-only branch must not be offered for unpublishing")
	}
}

func TestUnpublishBareRequiresBranchWithoutTTY(t *testing.T) {
	dir := setupRepo(t)
	// Destructive command: no terminal means no prompt and no deletion.
	// The error must say what to pass, never offer a numbered list.
	_, err := execute(t, dir, "unpub")
	if err == nil {
		t.Fatal("expected an error without a terminal")
	}
	if !strings.Contains(err.Error(), "branch required") {
		t.Errorf("error %q should require a branch", err)
	}
}

func TestUnpublishWithArgStillWorks(t *testing.T) {
	dir := setupRepo(t)
	ctx := context.Background()
	g := &gitx.Runner{Dir: dir}
	if _, err := g.Mutate(ctx, "push", "origin", "feature"); err != nil {
		t.Fatal(err)
	}

	out, err := execute(t, dir, "unpub", "feature")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Unpublishing feature") {
		t.Errorf("output %q should report the unpublish", out)
	}
	if repo.Open(g).HasRemoteBranch(ctx, "origin", "feature") {
		t.Error("feature should be gone from the remote")
	}
}
