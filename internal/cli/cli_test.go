package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EstebanForge/elgit/internal/gitx"
)

// setupRepo creates a repository with main and feature branches.
func setupRepo(t *testing.T) string {
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
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{
		{"add", "."},
		{"commit", "-m", "init"},
		{"init", "--bare", "-b", "main", filepath.Join(dir, "..", "remote.git")},
		{"remote", "add", "origin", filepath.Join(dir, "..", "remote.git")},
		{"push", "-u", "origin", "main"},
		{"branch", "feature"},
	} {
		if _, err := g.Mutate(ctx, argv...); err != nil {
			t.Fatalf("setup %v: %v", argv, err)
		}
	}
	return dir
}

func execute(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	var out bytes.Buffer
	root := NewRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestBranchesCommand(t *testing.T) {
	dir := setupRepo(t)
	out, err := execute(t, dir, "branches")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"main", "feature", "(unpublished)"} {
		if !strings.Contains(out, want) {
			t.Errorf("branches output %q missing %q", out, want)
		}
	}
}

func TestBranchesPattern(t *testing.T) {
	dir := setupRepo(t)
	out, err := execute(t, dir, "branches", "fea*")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "main") {
		t.Errorf("pattern output %q should not contain main", out)
	}
	if !strings.Contains(out, "feature") {
		t.Errorf("pattern output %q missing feature", out)
	}
}

func TestSwitchCommand(t *testing.T) {
	dir := setupRepo(t)
	if _, err := execute(t, dir, "switch", "feature"); err != nil {
		t.Fatal(err)
	}
	out, _ := gitxRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if got := strings.TrimSpace(out); got != "feature" {
		t.Errorf("branch = %q, want feature", got)
	}

	if _, err := execute(t, dir, "sw", "main"); err != nil {
		t.Fatal(err)
	}
	out, _ = gitxRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if got := strings.TrimSpace(out); got != "main" {
		t.Errorf("branch via alias = %q, want main", got)
	}
}

func TestSyncRequiresRepository(t *testing.T) {
	_, err := execute(t, t.TempDir(), "sync")
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("err = %v, want not-a-repository error", err)
	}
}

func TestFakeSyncLeavesTreeAlone(t *testing.T) {
	dir := setupRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := execute(t, dir, "--fake", "sync"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "a.txt"))
	if string(data) != "dirty\n" {
		t.Errorf("a.txt = %q after fake sync, want dirty", data)
	}
	out, _ := gitxRun(t, dir, "stash", "list")
	if strings.TrimSpace(out) != "" {
		t.Errorf("fake sync left stashes: %q", out)
	}
}

func TestUndoCommand(t *testing.T) {
	dir := setupRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, argv := range [][]string{{"add", "."}, {"commit", "-m", "second"}} {
		if _, _, err := runHelper(t, dir, argv...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := execute(t, dir, "undo"); err != nil {
		t.Fatal(err)
	}
	out, _ := gitxRun(t, dir, "log", "--oneline")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "init") {
		t.Errorf("log after undo = %q, want only init", out)
	}
}

func TestAliasesResolve(t *testing.T) {
	root := NewRootCmd()
	for _, alias := range []string{"sw", "switch", "sy", "pub", "unpub"} {
		if cmd, _, err := root.Find([]string{alias}); err != nil || cmd == nil {
			t.Errorf("alias %q not found: %v", alias, err)
		}
	}
}

func TestVersionFlag(t *testing.T) {
	dir := setupRepo(t)
	out, err := execute(t, dir, "--version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "elgit version") {
		t.Errorf("version output = %q", out)
	}
}

func TestUnknownCommandFails(t *testing.T) {
	dir := setupRepo(t)
	_, err := execute(t, dir, "definitely-not-a-command")
	if err == nil {
		t.Error("unknown command should fail")
	}
}

func TestConfigCommand(t *testing.T) {
	dir := setupRepo(t)
	out, err := execute(t, dir, "config")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "legit.smartMerge") || !strings.Contains(out, "(unset)") {
		t.Errorf("config output = %q", out)
	}
}

func TestSwitchInteractiveNumbered(t *testing.T) {
	dir := setupRepo(t) // main (current) + feature
	out, err := executeIn(t, dir, strings.NewReader("1\n"), "switch")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Switching to feature.") {
		t.Errorf("output = %q, want switch to feature", out)
	}
	branch, _ := gitxRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if got := strings.TrimSpace(branch); got != "feature" {
		t.Errorf("branch = %q, want feature", got)
	}
}

func TestSwitchInteractiveAbortKeepsBranch(t *testing.T) {
	dir := setupRepo(t)
	_, err := executeIn(t, dir, strings.NewReader("0\n"), "sw")
	if err != nil {
		t.Fatal(err)
	}
	branch, _ := gitxRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD")
	if got := strings.TrimSpace(branch); got != "main" {
		t.Errorf("branch after cancel = %q, want main", got)
	}
}

func TestAliasInstallAndUninstall(t *testing.T) {
	dir := setupRepo(t)
	cfg := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", cfg) // isolate --global writes to a scratch file

	if _, err := executeIn(t, dir, strings.NewReader("y\n"), "--install"); err != nil {
		t.Fatal(err)
	}

	g := &gitx.Runner{Dir: dir}
	ctx := context.Background()
	val, err := g.Query(ctx, "config", "--global", "--get", "alias.sw")
	if err != nil {
		t.Fatalf("alias.sw not installed: %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(val), "sw") {
		t.Errorf("alias.sw = %q, must run elgit sw", val)
	}
	if _, err := g.Query(ctx, "config", "--global", "--get", "alias.switch"); err == nil {
		t.Error("alias.switch must never be installed: it shadows native git switch")
	}
	for _, name := range []string{"sync", "publish", "unpublish", "undo", "branches"} {
		if _, err := g.Query(ctx, "config", "--global", "--get", "alias."+name); err != nil {
			t.Errorf("alias.%s not installed: %v", name, err)
		}
	}

	if _, err := executeIn(t, dir, strings.NewReader(""), "--uninstall"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"sw", "sync", "publish", "switch"} {
		if _, err := g.Query(ctx, "config", "--global", "--get", "alias."+name); err == nil {
			t.Errorf("alias.%s still present after uninstall", name)
		}
	}
}

// executeIn runs the CLI with a controlled stdin (non-terminal, so the
// picker uses its numbered mode).
func executeIn(t *testing.T, dir string, in *strings.Reader, args ...string) (string, error) {
	t.Helper()
	t.Chdir(dir)
	var out bytes.Buffer
	root := NewRootCmd()
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(in)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func gitxRun(t *testing.T, dir string, argv ...string) (string, error) {
	t.Helper()
	g := &gitx.Runner{Dir: dir}
	return g.Query(context.Background(), argv...)
}

// runHelper executes a mutating git command in dir for test setup.
func runHelper(t *testing.T, dir string, argv ...string) (string, string, error) {
	t.Helper()
	g := &gitx.Runner{Dir: dir}
	out, err := g.Mutate(context.Background(), argv...)
	return out, "", err
}
