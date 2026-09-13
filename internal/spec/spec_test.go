package spec

import (
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	for _, test := range []struct {
		in   string
		want Spec
		str  string
	}{
		{"mariusae/config:editors", Spec{"github.com", "mariusae", "config", "editors"}, "mariusae/config:editors"},
		{"mariusae/config", Spec{"github.com", "mariusae", "config", ""}, "mariusae/config"},
		{"mariusae/config.git:editors", Spec{"github.com", "mariusae", "config", "editors"}, "mariusae/config:editors"},
		{"git.example.com/m/c:etc", Spec{"git.example.com", "m", "c", "etc"}, "git.example.com/m/c:etc"},
		{"github.com/m/c:etc", Spec{"github.com", "m", "c", "etc"}, "m/c:etc"},
	} {
		got, err := Parse(test.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", test.in, err)
			continue
		}
		if got != test.want {
			t.Errorf("Parse(%q) = %+v, want %+v", test.in, got, test.want)
		}
		if s := got.String(); s != test.str {
			t.Errorf("Parse(%q).String() = %q, want %q", test.in, s, test.str)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{
		"",
		"config",
		"mariusae/config:",
		"mariusae/config:a/b",
		"a/b/c/d:e",
		"/config:editors",
		"mariusae/:editors",
	} {
		if got, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %+v, want an error", in, got)
		}
	}
}

func TestRepository(t *testing.T) {
	s, err := Parse("mariusae/config:editors")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Repository().String(); got != "mariusae/config" {
		t.Errorf("Repository() = %q", got)
	}
	if got := s.WithName("etc").String(); got != "mariusae/config:etc" {
		t.Errorf("WithName() = %q", got)
	}
}

func TestParseRef(t *testing.T) {
	for _, test := range []struct {
		in   string
		want Ref
		str  string
	}{
		{"mariusae/env::mac", Ref{Spec{"github.com", "mariusae", "env", "mac"}, true}, "mariusae/env::mac"},
		{"mariusae/env:mac", Ref{Spec{"github.com", "mariusae", "env", "mac"}, false}, "mariusae/env:mac"},
		{"mariusae/env", Ref{Spec{"github.com", "mariusae", "env", ""}, false}, "mariusae/env"},
		{"git.example.com/m/c::mac", Ref{Spec{"git.example.com", "m", "c", "mac"}, true}, "git.example.com/m/c::mac"},
		{"mariusae/env.git::mac", Ref{Spec{"github.com", "mariusae", "env", "mac"}, true}, "mariusae/env::mac"},
	} {
		got, err := ParseRef(test.in)
		if err != nil {
			t.Errorf("ParseRef(%q): %v", test.in, err)
			continue
		}
		if got != test.want {
			t.Errorf("ParseRef(%q) = %+v, want %+v", test.in, got, test.want)
		}
		if s := got.String(); s != test.str {
			t.Errorf("ParseRef(%q).String() = %q, want %q", test.in, s, test.str)
		}
	}
}

func TestParseRefErrors(t *testing.T) {
	for _, in := range []string{
		"",
		"env::mac",
		"mariusae/env::",
		"mariusae/env:::mac",
		"mariusae/env::a/b",
		"/env::mac",
	} {
		if got, err := ParseRef(in); err == nil {
			t.Errorf("ParseRef(%q) = %+v, want an error", in, got)
		}
	}
}

// TestParseRejectsSet is the point of the doubled colon: a command that
// can only act on a layer will not silently take a set for one, and says
// how the layer would have been spelled.
func TestParseRejectsSet(t *testing.T) {
	_, err := Parse("mariusae/env::mac")
	if err == nil {
		t.Fatal("Parse took a set specification")
	}
	if !strings.Contains(err.Error(), "mariusae/env:mac") {
		t.Errorf("error does not spell the layer: %v", err)
	}
}

func TestParseMember(t *testing.T) {
	in, err := Parse("mariusae/env:mac")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		member string
		want   string
	}{
		{"editors", "mariusae/env:editors"},
		{"::base", "mariusae/env::base"},
		{"mariusae/work:overrides", "mariusae/work:overrides"},
		{"mariusae/work::linux", "mariusae/work::linux"},
		{"mariusae/work", "mariusae/work"},
		{"git.example.com/m/c:etc", "git.example.com/m/c:etc"},
	} {
		got, err := ParseMember(test.member, in)
		if err != nil {
			t.Errorf("ParseMember(%q): %v", test.member, err)
			continue
		}
		if got.String() != test.want {
			t.Errorf("ParseMember(%q) = %q, want %q", test.member, got, test.want)
		}
		// A member reads back the way it was written.
		if back := got.Member(in); back != test.member {
			t.Errorf("ParseMember(%q).Member() = %q", test.member, back)
		}
	}
	if got, err := ParseMember("", in); err == nil {
		t.Errorf("ParseMember(\"\") = %+v, want an error", got)
	}
}

// TestMemberInOtherRepository checks that a member of a set in one
// repository spells itself whole when it is read back against another.
func TestMemberInOtherRepository(t *testing.T) {
	in, err := Parse("mariusae/work")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := ParseRef("mariusae/env::mac")
	if err != nil {
		t.Fatal(err)
	}
	if got := ref.Member(in); got != "mariusae/env::mac" {
		t.Errorf("Member() = %q", got)
	}
}
