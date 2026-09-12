// Package spec parses and formats layer specifications.
//
// A layer specification names an overlay stored in a directory of a git
// repository, written as
//
//	[host/]owner/repo[:layer]
//
// for example "mariusae/config:editors", which is the "editors"
// directory of https://github.com/mariusae/config. The host defaults to
// github.com. The layer name may be omitted to refer to the repository
// as a whole; commands that accept such a specification expand it to
// every layer the repository declares.
package spec

import (
	"fmt"
	"strings"
)

// DefaultHost is the host assumed when a specification omits one.
const DefaultHost = "github.com"

// A Spec identifies a layer within a repository.
type Spec struct {
	Host  string // repository host, e.g. "github.com"
	Owner string // repository owner
	Repo  string // repository name
	Name  string // layer name; empty means the repository as a whole
}

// Parse parses a layer specification.
func Parse(s string) (Spec, error) {
	var sp Spec
	if s == "" {
		return sp, fmt.Errorf("empty layer specification")
	}
	repo, name, hasName := strings.Cut(s, ":")
	if hasName {
		if name == "" {
			return sp, fmt.Errorf("%s: empty layer name", s)
		}
		if strings.ContainsAny(name, "/:") {
			return sp, fmt.Errorf("%s: layer name %q must be a single path element", s, name)
		}
		sp.Name = name
	}

	parts := strings.Split(repo, "/")
	switch len(parts) {
	case 2:
		sp.Host, sp.Owner, sp.Repo = DefaultHost, parts[0], parts[1]
	case 3:
		sp.Host, sp.Owner, sp.Repo = parts[0], parts[1], parts[2]
	default:
		return Spec{}, fmt.Errorf("%s: want [host/]owner/repo[:layer]", s)
	}
	for _, p := range []string{sp.Host, sp.Owner, sp.Repo} {
		if p == "" {
			return Spec{}, fmt.Errorf("%s: want [host/]owner/repo[:layer]", s)
		}
	}
	sp.Repo = strings.TrimSuffix(sp.Repo, ".git")
	return sp, nil
}

// String returns the canonical form of the specification. The host is
// elided when it is the default.
func (s Spec) String() string {
	b := new(strings.Builder)
	if s.Host != DefaultHost && s.Host != "" {
		b.WriteString(s.Host)
		b.WriteByte('/')
	}
	b.WriteString(s.Owner)
	b.WriteByte('/')
	b.WriteString(s.Repo)
	if s.Name != "" {
		b.WriteByte(':')
		b.WriteString(s.Name)
	}
	return b.String()
}

// Repository returns the specification of s's repository, without a
// layer name.
func (s Spec) Repository() Spec {
	s.Name = ""
	return s
}

// WithName returns a copy of s naming the layer name.
func (s Spec) WithName(name string) Spec {
	s.Name = name
	return s
}
