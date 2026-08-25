package picker

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func items() []Item {
	return []Item{
		{Name: "feature/one", Detail: "(published)"},
		{Name: "main", Detail: "(published)"},
		{Name: "peer-work", Detail: "(remote only)"},
	}
}

// Inputs here are strings.Readers, not terminals, so Pick takes the
// numbered path: exactly what scripts and CI get.
func TestPickNumberedSelects(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"1\n", "feature/one"},
		{"3\n", "peer-work"},
		{" 2 \n", "main"},
		{"9\n0\n", ""}, // first answer out of range aborts
	}
	for _, tt := range tests {
		var out bytes.Buffer
		got, err := Pick(&out, strings.NewReader(tt.in), "Pick:", items())
		if tt.want == "" {
			if !errors.Is(err, ErrAborted) {
				t.Errorf("Pick(%q) err = %v, want ErrAborted", tt.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("Pick(%q): %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Pick(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPickNumberedAborts(t *testing.T) {
	for _, in := range []string{"0\n", "nope\n", "", "\n"} {
		var out bytes.Buffer
		got, err := Pick(&out, strings.NewReader(in), "Pick:", items())
		if !errors.Is(err, ErrAborted) {
			t.Errorf("input %q: err = %v, want ErrAborted", in, err)
		}
		if got != "" {
			t.Errorf("input %q: got = %q, want empty", in, got)
		}
	}
}

func TestPickNumberedShowsDetails(t *testing.T) {
	var out bytes.Buffer
	_, err := Pick(&out, strings.NewReader("1\n"), "Pick:", items())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Pick:", "feature/one", "(remote only)", "peer-work"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q missing %q", out.String(), want)
		}
	}
}

func TestPickEmptyListAborts(t *testing.T) {
	var out bytes.Buffer
	if _, err := Pick(&out, strings.NewReader("1\n"), "Pick:", nil); !errors.Is(err, ErrAborted) {
		t.Errorf("err = %v, want ErrAborted", err)
	}
}
