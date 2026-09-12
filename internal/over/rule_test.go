package over

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mariusae/over/internal/pathspec"
)

func TestParseRule(t *testing.T) {
	root := pathspec.Resolve(t.TempDir())
	for _, rule := range []string{".apex/...", ".vimrc", ".config/.../init.lua"} {
		if _, err := ParseRule(root, rule); err != nil {
			t.Errorf("ParseRule(%q): %v", rule, err)
		}
	}
	// A rule has to mean the same thing on every machine that adds the
	// layer, so it is relative to the root and stays under it.
	for _, rule := range []string{"", "...", "/etc/hosts", "../outside/..."} {
		if _, err := ParseRule(root, rule); err == nil {
			t.Errorf("ParseRule(%q) succeeded, want an error", rule)
		}
	}
}

func TestClaims(t *testing.T) {
	root := pathspec.Resolve(t.TempDir())
	at := func(rel string) string { return filepath.Join(root, rel) }
	write := func(rel, body string) string {
		t.Helper()
		path := at(rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	mustParse := func(rules ...string) []pathspec.Pattern {
		t.Helper()
		pats, err := ParseRules(root, rules)
		if err != nil {
			t.Fatal(err)
		}
		return pats
	}
	excl, err := pathspec.ParseAll(root, []string{at(".apex/private/...")})
	if err != nil {
		t.Fatal(err)
	}

	script := write("bin/hello", "#!/bin/sh\necho hello\n")
	program := write("bin/hello-bin", "\x7fELF\x00\x00\x00compiled")
	attach := write(".apex/attach", "attach\n")
	deep := write(".apex/deep/down/here", "deep\n")
	vimrc := write(".vimrc", "set nocompatible\n")
	cached := write(".apex/cache/blob", "junk\n")
	private := write(".apex/private/key", "secret\n")
	absent := at(".zshrc")

	l := &Layer{
		Root:    root,
		Track:   mustParse(".apex/...", ".vimrc", "bin/..."),
		Ignore:  mustParse(".apex/cache/..."),
		Exclude: excl,
	}
	for _, test := range []struct {
		path  string
		claim bool
	}{
		{attach, true},
		{deep, true},
		{vimrc, true},
		{script, true},
		{program, false}, // binary, and the layer does not take those
		{cached, false},  // an ignore rule carves it out
		{private, false}, // the client's exclusions outrank the layer
		{absent, false},  // no rule matches, and nothing is there
	} {
		if got := l.Claims(test.path); got != test.claim {
			t.Errorf("Claims(%q) = %v, want %v", test.path, got, test.claim)
		}
	}

	// A layer that holds binary files considers nothing binary.
	l.IncludeBin = true
	if !l.Claims(program) {
		t.Errorf("Claims(%q) = false with IncludeBin set", program)
	}
	// The other reasons still stand.
	if l.Claims(cached) || l.Claims(private) {
		t.Error("IncludeBin overrode an ignore rule or an exclusion")
	}
}

// TestClaimReportsBinaries checks that the files a rule passes over for
// their contents are reported rather than silently dropped.
func TestClaimReportsBinaries(t *testing.T) {
	root := pathspec.Resolve(t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"script":  "#!/bin/sh\necho hi\n",
		"another": "#!/usr/bin/env python3\nprint()\n",
		"program": "\x7fELF\x00\x00compiled",
	} {
		if err := os.WriteFile(filepath.Join(root, "bin", name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	track, err := ParseRules(root, []string{"bin/..."})
	if err != nil {
		t.Fatal(err)
	}
	l := &Layer{Root: root, Track: track}
	claim, err := l.Claim()
	if err != nil {
		t.Fatal(err)
	}
	if len(claim.Paths) != 2 {
		t.Errorf("claimed %v, want the two scripts", claim.Paths)
	}
	if len(claim.Binaries) != 1 || claim.Binaries[0] != "bin/program" {
		t.Errorf("binaries = %v, want [bin/program]", claim.Binaries)
	}

	l.IncludeBin = true
	claim, err = l.Claim()
	if err != nil {
		t.Fatal(err)
	}
	if len(claim.Paths) != 3 || len(claim.Binaries) != 0 {
		t.Errorf("with IncludeBin: claimed %v, skipped %v", claim.Paths, claim.Binaries)
	}
}

// TestWalkRoots checks that overlapping rules are walked once, from the
// outermost anchor.
func TestWalkRoots(t *testing.T) {
	root := pathspec.Resolve(t.TempDir())
	pats, err := ParseRules(root, []string{".config/ion/...", ".config/...", ".local/share/..."})
	if err != nil {
		t.Fatal(err)
	}
	roots := walkRoots(pats)
	want := []string{filepath.Join(root, ".config"), filepath.Join(root, ".local/share")}
	if len(roots) != len(want) {
		t.Fatalf("walkRoots = %v, want %v", roots, want)
	}
	for i := range want {
		if roots[i] != want[i] {
			t.Fatalf("walkRoots = %v, want %v", roots, want)
		}
	}
}

// TestRelativeRule checks that a path argument is stored as a rule
// relative to the layer's root, wildcards intact.
func TestRelativeRule(t *testing.T) {
	root := pathspec.Resolve(t.TempDir())
	l := &Layer{Root: root}
	pat, err := pathspec.Parse(root, ".apex/...")
	if err != nil {
		t.Fatal(err)
	}
	rule, err := RelativeRule(l, pat)
	if err != nil {
		t.Fatal(err)
	}
	if rule != ".apex/..." {
		t.Errorf("RelativeRule = %q", rule)
	}
	outside, err := pathspec.Parse(filepath.Dir(root), "elsewhere/...")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RelativeRule(l, outside); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("RelativeRule of a path outside the root = %v", err)
	}
}
