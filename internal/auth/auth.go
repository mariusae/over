// Package auth keeps the credentials over needs to reach a layer
// repository it may not read anonymously, or may not write at all.
//
// The credential over wants is an ordinary bearer token, handed to git
// through a credential helper. Where it comes from is the interesting
// part: a token may be pasted in, taken from the environment, or --
// the point of the exercise -- obtained by sending the user to the host
// to authorize over, which is the only way that works on a machine with
// no key on it yet.
//
// Nothing here is interactive. Obtaining a token needs the user to do
// something in a browser, so the caller supplies a function that shows
// them what; this package waits for the result. That keeps the terminal
// out of the packages that do the work, which matters because the same
// code has to run where there is no terminal at all.
package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Slack is how long before a token's stated expiry it is treated as
// expired. A token that dies mid-push is worse than one refreshed a
// minute early.
const Slack = 2 * time.Minute

// A Token is a credential for one host.
type Token struct {
	// Host is the host the token authenticates to, e.g. "github.com".
	Host string `json:"host"`

	// Access is the bearer token git is given.
	Access string `json:"access_token"`

	// Refresh exchanges for a new access token once Expiry passes. It
	// is empty for tokens that do not expire.
	Refresh string `json:"refresh_token,omitempty"`

	// Expiry is when Access stops working. The zero time means it does
	// not expire on its own.
	Expiry time.Time `json:"expiry,omitempty"`

	// RefreshExpiry is when Refresh stops working, after which the
	// user has to authorize again.
	RefreshExpiry time.Time `json:"refresh_expiry,omitempty"`

	// Scope is what the token is allowed to do, as the host describes
	// it. Reported by "over auth -status" and not otherwise used:
	// whether a token is sufficient is the host's ruling, not over's
	// guess.
	Scope string `json:"scope,omitempty"`

	// Login is the account the token belongs to, when the host says.
	Login string `json:"login,omitempty"`

	// Source records how the token was obtained, so that "over auth
	// -status" can say, and so that a token over did not obtain is not
	// silently refreshed as though it could be.
	Source Source `json:"source,omitempty"`

	// Obtained is when over got the token.
	Obtained time.Time `json:"obtained,omitempty"`
}

// A Source is how a token was come by.
type Source string

const (
	// FromDevice means the user authorized over at the host.
	FromDevice Source = "device"

	// FromPaste means the token was given to "over auth -token".
	FromPaste Source = "token"

	// FromEnv means the token came from the environment, and is not
	// stored at all.
	FromEnv Source = "env"
)

// Expired reports whether the access token has passed its expiry, or is
// about to.
func (t *Token) Expired(now time.Time) bool {
	return !t.Expiry.IsZero() && now.Add(Slack).After(t.Expiry)
}

// Renewable reports whether an expired token can be exchanged for a new
// one without troubling the user.
func (t *Token) Renewable(now time.Time) bool {
	if t.Refresh == "" {
		return false
	}
	return t.RefreshExpiry.IsZero() || now.Before(t.RefreshExpiry)
}

// Usable reports whether the token can be handed to git as it stands.
func (t *Token) Usable(now time.Time) bool {
	return t != nil && t.Access != "" && !t.Expired(now)
}

// A Store holds tokens on disk, one file per host.
//
// The directory is over's alone and must never become layer content: a
// token published to a git repository is a token to revoke. over refuses
// to manage it whatever the configured exclusions say, which is why this
// type reports its own directory rather than leaving callers to
// reconstruct it.
type Store struct {
	dir string
}

// NewStore returns the store kept in dir.
func NewStore(dir string) *Store { return &Store{dir: dir} }

// Dir returns the directory the store occupies.
func (s *Store) Dir() string { return s.dir }

// Path returns the file a host's token is kept in.
func (s *Store) Path(host string) (string, error) {
	if err := checkHost(host); err != nil {
		return "", err
	}
	return filepath.Join(s.dir, host+".json"), nil
}

// checkHost rejects a host that cannot safely name a file. Hosts are
// domain names, so this never fires in practice; it fires on the typo or
// the injection that would otherwise write somewhere surprising.
func checkHost(host string) error {
	if host == "" {
		return fmt.Errorf("empty host")
	}
	if host == "." || host == ".." || strings.ContainsAny(host, `/\`) {
		return fmt.Errorf("%q is not a host name", host)
	}
	return nil
}

// Load returns the token stored for a host, or nil if there is none.
func (s *Store) Load(host string) (*Token, error) {
	path, err := s.Path(host)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	t := new(Token)
	if err := json.Unmarshal(data, t); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if t.Host == "" {
		t.Host = host
	}
	return t, nil
}

// Save writes a token, replacing whatever was there. The file is created
// unreadable to anyone else, and written atomically so that a token is
// never half on disk.
func (s *Store) Save(t *Token) error {
	path, err := s.Path(t.Host)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".token-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Remove forgets a host's token, reporting whether there was one.
func (s *Store) Remove(host string) (bool, error) {
	path, err := s.Path(host)
	if err != nil {
		return false, err
	}
	if err := os.Remove(path); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// Hosts returns the hosts the store holds tokens for, sorted.
func (s *Store) Hosts() ([]string, error) {
	ents, err := os.ReadDir(s.dir)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var hosts []string
	for _, ent := range ents {
		name := ent.Name()
		if ent.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		hosts = append(hosts, strings.TrimSuffix(name, ".json"))
	}
	sort.Strings(hosts)
	return hosts, nil
}
