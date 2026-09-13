package gitrepo

import (
	"errors"
	"testing"
)

// TestClassify covers the reason this exists: git reports every remote
// failure as exit status 128, so the only thing to go on is what it
// said.
func TestClassify(t *testing.T) {
	for _, test := range []struct {
		msg  string
		want error
	}{
		{
			"fatal: could not read Username for 'https://github.com': terminal prompts disabled",
			ErrAuth,
		},
		{"remote: Invalid username or password.\nfatal: Authentication failed", ErrAuth},
		{"fatal: unable to access 'https://github.com/m/c.git/': The requested URL returned error: 403", ErrAuth},
		{"remote: Write access to repository not granted.", ErrAuth},
		{"git@github.com: Permission denied (publickey).", ErrAuth},
		{"ERROR: Permission to m/c.git denied to other.", ErrAuth},
		{"remote: Repository not found.", ErrNotFound},
		{"fatal: repository 'https://github.com/m/c.git/' not found", ErrNotFound},
		{"fatal: '/tmp/x' does not appear to be a git repository", ErrNotFound},
		{"error: your local changes would be overwritten", nil},
		{"", nil},
	} {
		if got := classify(test.msg); got != test.want {
			t.Errorf("classify(%q) = %v, want %v", test.msg, got, test.want)
		}
	}
}

// TestRemoteErrorIs checks that both the kind and the exit status are
// reachable through the error, since callers want either.
func TestRemoteErrorIs(t *testing.T) {
	exit := errors.New("exit status 128")
	err := error(&RemoteError{Kind: ErrAuth, Op: "fetch", Msg: "remote: no", err: exit})
	if !errors.Is(err, ErrAuth) {
		t.Error("the kind is not reachable")
	}
	if !errors.Is(err, exit) {
		t.Error("the underlying error is not reachable")
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("an auth error looks like a missing repository")
	}
	if got, want := err.Error(), "git fetch: remote: no"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestConfigArgs checks that over's helper displaces whatever the user's
// git installs, rather than queueing behind it.
func TestConfigArgs(t *testing.T) {
	if got := (Config{}).Args(); got != nil {
		t.Errorf("an empty Config contributes %q", got)
	}
	got := Config{Helper: "!over credential"}.Args()
	want := []string{"-c", "credential.helper=", "-c", "credential.helper=!over credential"}
	if len(got) != len(want) {
		t.Fatalf("Args = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Args = %q, want %q", got, want)
		}
	}
}
