package diff

import (
	"math/rand"
	"strings"
	"testing"
)

func TestUnified(t *testing.T) {
	for _, test := range []struct{ name, a, b, want string }{
		{
			name: "identical",
			a:    "one\ntwo\n",
			b:    "one\ntwo\n",
			want: "",
		},
		{
			name: "change one line",
			a:    "one\ntwo\nthree\n",
			b:    "one\n2\nthree\n",
			want: "--- a\n+++ b\n@@ -1,3 +1,3 @@\n one\n-two\n+2\n three\n",
		},
		{
			name: "append",
			a:    "one\n",
			b:    "one\ntwo\n",
			want: "--- a\n+++ b\n@@ -1 +1,2 @@\n one\n+two\n",
		},
		{
			name: "delete all",
			a:    "one\ntwo\n",
			b:    "",
			want: "--- a\n+++ b\n@@ -1,2 +0,0 @@\n-one\n-two\n",
		},
		{
			name: "create",
			a:    "",
			b:    "one\n",
			want: "--- a\n+++ b\n@@ -0,0 +1 @@\n+one\n",
		},
		{
			name: "no trailing newline",
			a:    "one\ntwo",
			b:    "one\n2",
			want: "--- a\n+++ b\n@@ -1,2 +1,2 @@\n one\n-two\n\\ No newline at end of file\n+2\n\\ No newline at end of file\n",
		},
		{
			name: "binary",
			a:    "one\x00two",
			b:    "three",
			want: "Binary files a and b differ\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := Unified("a", "b", []byte(test.a), []byte(test.b))
			if got != test.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, test.want)
			}
		})
	}
}

// TestSeparateHunks checks that changes far apart are reported as
// separate hunks, and that each hunk carries its context.
func TestSeparateHunks(t *testing.T) {
	a := new(strings.Builder)
	b := new(strings.Builder)
	for i := 0; i < 40; i++ {
		line := "line\n"
		a.WriteString(line)
		switch i {
		case 0, 30:
			b.WriteString("changed\n")
		default:
			b.WriteString(line)
		}
	}
	got := Unified("a", "b", []byte(a.String()), []byte(b.String()))
	if n := strings.Count(got, "@@"); n != 4 { // two per hunk header
		t.Errorf("got %d hunk markers, want 4:\n%s", n/2, got)
	}
}

// TestApply is the property that matters: applying the diff's edit
// script to a must yield b, for arbitrary pairs of line sequences.
func TestApply(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 200; i++ {
		a := randomLines(rng)
		b := randomLines(rng)
		var got []string
		for _, e := range script(a, b) {
			switch e.op {
			case opEqual:
				if a[e.a] != b[e.b] {
					t.Fatalf("equal edit over unequal lines %q %q", a[e.a], b[e.b])
				}
				got = append(got, a[e.a])
			case opInsert:
				got = append(got, b[e.b])
			}
		}
		if strings.Join(got, "\n") != strings.Join(b, "\n") {
			t.Fatalf("applying diff of %q to %q gave %q", a, b, got)
		}
	}
}

func randomLines(rng *rand.Rand) []string {
	n := rng.Intn(12)
	lines := make([]string, n)
	for i := range lines {
		lines[i] = string(rune('a' + rng.Intn(4)))
	}
	return lines
}
