package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestStoreRoundTrip(t *testing.T) {
	s := NewStore(filepath.Join(t.TempDir(), "auth"))
	if got, err := s.Load("github.com"); err != nil || got != nil {
		t.Fatalf("Load of an empty store = %v, %v", got, err)
	}
	want := &Token{
		Host:   "github.com",
		Access: "gho_secret",
		Login:  "mariusae",
		Source: FromDevice,
	}
	if err := s.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.Load("github.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.Access != want.Access || got.Login != want.Login || got.Source != want.Source {
		t.Errorf("Load = %+v, want %+v", got, want)
	}
	if hosts, err := s.Hosts(); err != nil || len(hosts) != 1 || hosts[0] != "github.com" {
		t.Errorf("Hosts = %v, %v", hosts, err)
	}
	if removed, err := s.Remove("github.com"); err != nil || !removed {
		t.Errorf("Remove = %v, %v", removed, err)
	}
	if removed, err := s.Remove("github.com"); err != nil || removed {
		t.Errorf("second Remove = %v, %v", removed, err)
	}
}

// TestStoreIsPrivate checks the thing that matters most about the store:
// nobody else can read it.
func TestStoreIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no file modes")
	}
	dir := filepath.Join(t.TempDir(), "auth")
	s := NewStore(dir)
	if err := s.Save(&Token{Host: "github.com", Access: "x"}); err != nil {
		t.Fatal(err)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode %o, want 700", perm)
	}
	path, err := s.Path("github.com")
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("token mode %o, want 600", perm)
	}
}

// TestStoreRejectsBadHost checks that a host name cannot send a token
// somewhere other than the store.
func TestStoreRejectsBadHost(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, host := range []string{"", ".", "..", "../../etc/passwd", "a/b"} {
		if _, err := s.Path(host); err == nil {
			t.Errorf("Path(%q) was accepted", host)
		}
	}
}

func TestTokenExpiry(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name                       string
		token                      Token
		expired, renewable, usable bool
	}{
		{"no expiry", Token{Access: "x"}, false, false, true},
		{"fresh", Token{Access: "x", Expiry: now.Add(time.Hour)}, false, false, true},
		{"expired", Token{Access: "x", Expiry: now.Add(-time.Hour)}, true, false, false},
		{
			"expired but renewable",
			Token{Access: "x", Expiry: now.Add(-time.Hour), Refresh: "r"},
			true, true, false,
		},
		{
			"refresh also expired",
			Token{Access: "x", Expiry: now.Add(-time.Hour), Refresh: "r", RefreshExpiry: now.Add(-time.Minute)},
			true, false, false,
		},
		{
			// Inside the slack, so treated as gone: a token that dies
			// mid-push is worse than one renewed a minute early.
			"expiring within the slack",
			Token{Access: "x", Expiry: now.Add(Slack / 2)},
			true, false, false,
		},
	} {
		tok := test.token
		if got := tok.Expired(now); got != test.expired {
			t.Errorf("%s: Expired = %v, want %v", test.name, got, test.expired)
		}
		if got := tok.Renewable(now); got != test.renewable {
			t.Errorf("%s: Renewable = %v, want %v", test.name, got, test.renewable)
		}
		if got := tok.Usable(now); got != test.usable {
			t.Errorf("%s: Usable = %v, want %v", test.name, got, test.usable)
		}
	}
	var nilToken *Token
	if nilToken.Usable(now) {
		t.Error("a nil token is usable")
	}
}
