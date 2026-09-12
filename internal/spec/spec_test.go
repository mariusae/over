package spec

import "testing"

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
