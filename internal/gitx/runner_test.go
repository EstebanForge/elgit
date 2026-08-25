package gitx

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupRepo creates a real git repository with one commit on branch main.
func setupRepo(t *testing.T) (*Runner, string) {
	t.Helper()
	dir := t.TempDir()
	r := &Runner{Dir: dir}

	ctx := context.Background()
	steps := [][]string{
		{"init", "-b", "main"},
		{"config", "user.name", "Test"},
		{"config", "user.email", "test@example.com"},
	}
	for _, argv := range steps {
		if _, err := r.Query(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Mutate(ctx, "add", "."); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Mutate(ctx, "commit", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	return r, dir
}

func TestQueryReadsRepo(t *testing.T) {
	r, _ := setupRepo(t)
	out, err := r.Query(context.Background(), "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	if got := strings.TrimSpace(out); got != "main" {
		t.Errorf("branch = %q, want main", got)
	}
}

func TestQueryErrorCarriesDetails(t *testing.T) {
	r, _ := setupRepo(t)
	_, err := r.Query(context.Background(), "rev-parse", "definitely-not-a-ref")
	if err == nil {
		t.Fatal("expected error for unknown ref")
	}
	var gErr *Error
	if !errors.As(err, &gErr) {
		t.Fatalf("error is %T, want *gitx.Error", err)
	}
	if gErr.ExitCode == 0 {
		t.Error("ExitCode = 0, want nonzero")
	}
	if !strings.Contains(gErr.Argv[0], "rev-parse") {
		t.Errorf("Argv = %v, want rev-parse", gErr.Argv)
	}
}

func TestMutateChangesState(t *testing.T) {
	r, _ := setupRepo(t)
	ctx := context.Background()
	if _, err := r.Mutate(ctx, "checkout", "-b", "feature"); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	out, _ := r.Query(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if got := strings.TrimSpace(out); got != "feature" {
		t.Errorf("branch = %q, want feature", got)
	}
}

func TestFakeMutateChangesNothing(t *testing.T) {
	r, _ := setupRepo(t)
	ctx := context.Background()

	var echo bytes.Buffer
	r.Fake = true
	r.Echo = &echo

	_, err := r.Mutate(ctx, "checkout", "-b", "feature")
	if !r.FakeOK(err) {
		t.Fatalf("fake mutate should be fake-ok, got %v", err)
	}
	if !errors.Is(err, ErrFakeMutation) {
		t.Error("expected ErrFakeMutation sentinel")
	}

	// Repository state unchanged.
	out, _ := r.Query(ctx, "rev-parse", "--abbrev-ref", "HEAD")
	if got := strings.TrimSpace(out); got != "main" {
		t.Errorf("branch = %q, want main (fake must not mutate)", got)
	}
	if !strings.Contains(echo.String(), "git checkout -b feature") {
		t.Errorf("echo = %q, want the fake command line", echo.String())
	}
}

func TestFakeStillQueries(t *testing.T) {
	r, _ := setupRepo(t)
	r.Fake = true
	// Queries must execute even in fake mode.
	out, err := r.Query(context.Background(), "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		t.Fatalf("query in fake mode: %v", err)
	}
	if got := strings.TrimSpace(out); got != "main" {
		t.Errorf("branch = %q, want main", got)
	}
}

func TestOkProbe(t *testing.T) {
	r, _ := setupRepo(t)
	ctx := context.Background()
	if !r.Ok(ctx, "rev-parse", "HEAD") {
		t.Error("Ok(rev-parse HEAD) = false, want true")
	}
	if r.Ok(ctx, "rev-parse", "no-such-ref") {
		t.Error("Ok(rev-parse no-such-ref) = true, want false")
	}
}

func TestVerboseEchoesCommand(t *testing.T) {
	r, _ := setupRepo(t)
	var echo bytes.Buffer
	r.Verbose = true
	r.Echo = &echo

	if _, err := r.Query(context.Background(), "status", "--porcelain"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(echo.String(), ">>> git status --porcelain") {
		t.Errorf("echo = %q, want verbose command line", echo.String())
	}
}

func TestNotARepository(t *testing.T) {
	r := &Runner{Dir: t.TempDir()}
	_, err := r.Query(context.Background(), "status", "--porcelain")
	if err == nil {
		t.Fatal("expected error outside a repository")
	}
	var gErr *Error
	if !errors.As(err, &gErr) {
		t.Fatalf("error is %T, want *gitx.Error", err)
	}
}

func TestEmptyArgv(t *testing.T) {
	r := &Runner{Dir: t.TempDir()}
	if _, err := r.Query(context.Background()); err == nil {
		t.Fatal("expected error for empty argv")
	}
}
