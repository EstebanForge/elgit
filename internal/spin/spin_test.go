package spin

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRunWithoutTTYRunsWorkAndPrintsMessage(t *testing.T) {
	var out bytes.Buffer
	ran := false
	err := Run(&out, "Fetching branches", func() error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !ran {
		t.Error("work did not run")
	}
	// Without a terminal the spinner degrades to one plain line: CI logs
	// and piped output stay readable, nothing animated.
	if !strings.Contains(out.String(), "Fetching branches") {
		t.Errorf("output %q should name the work", out.String())
	}
	if strings.ContainsRune(out.String(), '\r') {
		t.Errorf("output %q should not animate (carriage returns found)", out.String())
	}
}

func TestRunPropagatesWorkError(t *testing.T) {
	var out bytes.Buffer
	want := errors.New("network down")
	err := Run(&out, "Fetching branches", func() error { return want })
	if !errors.Is(err, want) {
		t.Errorf("err %v, want %v", err, want)
	}
}
