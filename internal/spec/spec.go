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
//
// A repository also groups its layers into sets, which are named with a
// doubled colon:
//
//	[host/]owner/repo::set
//
// Layers and sets are separate namespaces, and the delimiter is what
// distinguishes them. Nothing is inferred from the name, so a repository
// holding both a layer and a set called "mac" has no ambiguity to
// resolve -- and neither can come to shadow the other when somebody
// commits a directory.
package spec

import (
	"fmt"
	"strings"
)

// DefaultHost is the host assumed when a specification omits one.
const DefaultHost = "github.com"

// SetSeparator delimits a set name, where a single colon delimits a
// layer name.
const SetSeparator = "::"

// A Spec identifies a layer within a repository.
type Spec struct {
	Host  string // repository host, e.g. "github.com"
	Owner string // repository owner
	Repo  string // repository name
	Name  string // layer name; empty means the repository as a whole
}

// Parse parses a layer specification. A specification naming a set is an
// error: only [ParseRef] accepts one, because only the commands that can
// expand a set should take one.
func Parse(s string) (Spec, error) {
	var sp Spec
	if s == "" {
		return sp, fmt.Errorf("empty layer specification")
	}
	if i := strings.Index(s, SetSeparator); i >= 0 {
		return sp, fmt.Errorf("%s: names a set, not a layer; the layer would be %s:%s",
			s, s[:i], s[i+len(SetSeparator):])
	}
	repo, name, hasName := strings.Cut(s, ":")
	if hasName {
		if err := checkName(s, "layer", name); err != nil {
			return sp, err
		}
		sp.Name = name
	}
	if err := parseRepo(s, repo, &sp); err != nil {
		return Spec{}, err
	}
	return sp, nil
}

// parseRepo fills in the repository half of sp from repo, which is a
// specification with any layer or set name already cut away. The whole
// specification s is used only in error messages.
func parseRepo(s, repo string, sp *Spec) error {
	parts := strings.Split(repo, "/")
	switch len(parts) {
	case 2:
		sp.Host, sp.Owner, sp.Repo = DefaultHost, parts[0], parts[1]
	case 3:
		sp.Host, sp.Owner, sp.Repo = parts[0], parts[1], parts[2]
	default:
		return fmt.Errorf("%s: want [host/]owner/repo[:layer]", s)
	}
	for _, p := range []string{sp.Host, sp.Owner, sp.Repo} {
		if p == "" {
			return fmt.Errorf("%s: want [host/]owner/repo[:layer]", s)
		}
	}
	sp.Repo = strings.TrimSuffix(sp.Repo, ".git")
	return nil
}

// checkName rejects a layer or set name that is not a single path
// element. The whole specification s is used only in error messages.
func checkName(s, kind, name string) error {
	if name == "" {
		return fmt.Errorf("%s: empty %s name", s, kind)
	}
	if strings.ContainsAny(name, "/:") {
		return fmt.Errorf("%s: %s name %q must be a single path element", s, kind, name)
	}
	return nil
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

// A Ref is what a layer argument may name: one layer, one set of layers,
// or a repository as a whole. Resolving a Ref yields layers; see
// Over.ResolveSpecs.
type Ref struct {
	// Spec is the repository, and in Spec.Name the layer or set within
	// it. An empty Spec.Name means the repository as a whole.
	Spec Spec

	// Set reports that Spec.Name names a set rather than a layer.
	Set bool
}

// ParseRef parses a specification that may name a set:
//
//	[host/]owner/repo          every layer the repository provides
//	[host/]owner/repo:layer    one layer
//	[host/]owner/repo::set     one set, which expands to its members
func ParseRef(s string) (Ref, error) {
	if s == "" {
		return Ref{}, fmt.Errorf("empty layer specification")
	}
	repo, name, isSet := strings.Cut(s, SetSeparator)
	if !isSet {
		sp, err := Parse(s)
		return Ref{Spec: sp}, err
	}
	if err := checkName(s, "set", name); err != nil {
		return Ref{}, err
	}
	var sp Spec
	if err := parseRepo(s, repo, &sp); err != nil {
		return Ref{}, err
	}
	sp.Name = name
	return Ref{Spec: sp, Set: true}, nil
}

// ParseMember parses one member of a set declared in the repository in.
// A member is written relative to that repository: a bare name is one of
// its layers, and "::name" is another of its sets. Anything else is a
// whole specification, which is how a set reaches layers and sets in
// other repositories.
func ParseMember(s string, in Spec) (Ref, error) {
	switch {
	case s == "":
		return Ref{}, fmt.Errorf("empty set member")
	case strings.HasPrefix(s, SetSeparator):
		return ParseRef(in.Repository().String() + s)
	case strings.ContainsAny(s, "/:"):
		return ParseRef(s)
	default:
		return Ref{Spec: in.Repository().WithName(s)}, nil
	}
}

// SetRef returns a Ref naming the set of the given name in s's
// repository.
func SetRef(s Spec, name string) Ref {
	return Ref{Spec: s.Repository().WithName(name), Set: true}
}

// String returns the canonical form of the reference.
func (r Ref) String() string {
	if !r.Set {
		return r.Spec.String()
	}
	return r.Spec.Repository().String() + SetSeparator + r.Spec.Name
}

// Member returns the form r takes as a member of a set declared in the
// repository in: relative to it where it can be, and whole otherwise, so
// that it reads back the way it was written.
func (r Ref) Member(in Spec) string {
	if r.Spec.Repository() != in.Repository() {
		return r.String()
	}
	if r.Set {
		return SetSeparator + r.Spec.Name
	}
	return r.Spec.Name
}
