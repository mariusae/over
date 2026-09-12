package pathspec

import (
	"path/filepath"
	"testing"
)

func TestMatch(t *testing.T) {
	// The pattern is canonicalized as it is parsed, so the directory it
	// is resolved against has to be a real one.
	dir := Resolve(t.TempDir())
	at := func(rel string) string { return filepath.Join(dir, rel) }
	for _, test := range []struct {
		arg   string
		path  string
		match bool
	}{
		{".vimrc", at(".vimrc"), true},
		{".vimrc", at(".vimrcx"), false},
		{".vimrc", at("sub/.vimrc"), false},
		{at("abs/file"), at("abs/file"), true},
		{".config", at(".config/ion/config"), true},
		{".config", at(".configuration"), false},
		{".config/...", at(".config/ion/config"), true},
		{".config/...", at(".config"), false},
		{".config/...", at(".vimrc"), false},
		{"...", "/anywhere/at/all", true},
		{".../config", at(".config/ion/config"), true},
		{".../config", at(".config/ion/configx"), false},
		{"...rc", at(".vimrc"), true},
		{"./.vimrc", at(".vimrc"), true},
		{"sub/../.vimrc", at(".vimrc"), true},
	} {
		p, err := Parse(dir, test.arg)
		if err != nil {
			t.Errorf("Parse(%q): %v", test.arg, err)
			continue
		}
		if got := p.Match(test.path); got != test.match {
			t.Errorf("Parse(%q).Match(%q) = %v, want %v", test.arg, test.path, got, test.match)
		}
	}
}

func TestParseError(t *testing.T) {
	if _, err := Parse(t.TempDir(), ""); err == nil {
		t.Error("Parse(\"\") succeeded")
	}
}

func TestBase(t *testing.T) {
	dir := Resolve(t.TempDir())
	at := func(rel string) string { return filepath.Join(dir, rel) }
	for _, test := range []struct{ arg, base string }{
		{".vimrc", at(".vimrc")},
		{".config", at(".config")},
		{".config/...", at(".config")},
		{".config/.../x", at(".config")},
		{"...", ""},
	} {
		p, err := Parse(dir, test.arg)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.Base(); got != test.base {
			t.Errorf("Parse(%q).Base() = %q, want %q", test.arg, got, test.base)
		}
	}
}

func TestPatternsMatchAllWhenEmpty(t *testing.T) {
	if !(Patterns(nil)).Match("/anything") {
		t.Error("empty pattern set did not match")
	}
}
