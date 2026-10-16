package gitx

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStreamWritesCallerWriter(t *testing.T) {
	r, _ := setupRepo(t)
	var out bytes.Buffer
	err := r.Stream(context.Background(), &out, "log", "--oneline", "-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "init") {
		t.Errorf("stream output %q missing the commit subject", out.String())
	}
}

func TestStreamErrorCarriesStderr(t *testing.T) {
	r, _ := setupRepo(t)
	var out bytes.Buffer
	err := r.Stream(context.Background(), &out, "rev-parse", "definitely-not-a-ref")
	if err == nil {
		t.Fatal("expected error for unknown ref")
	}
	var gErr *Error
	if !errors.As(err, &gErr) {
		t.Fatalf("error %v is not a *Error", err)
	}
	if gErr.ExitCode == 0 {
		t.Error("exit code should be nonzero")
	}
	if !strings.Contains(gErr.Stderr, "definitely-not-a-ref") {
		t.Errorf("stderr %q should name the bad ref", gErr.Stderr)
	}
}
