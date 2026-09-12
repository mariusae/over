package over

import (
	"strings"
	"testing"
	"time"

	"github.com/mariusae/over/internal/spec"
	"github.com/mariusae/over/internal/state"
)

func testLayer(t *testing.T, name string) *Layer {
	t.Helper()
	s, err := spec.Parse("mariusae/config:" + name)
	if err != nil {
		t.Fatal(err)
	}
	return &Layer{Spec: s, Root: "/home/u"}
}

func testOrigin() Origin {
	return Origin{
		Host:    "mariusmac",
		User:    "marius",
		Version: "devel",
		Time:    time.Date(2026, 9, 12, 15, 32, 58, 0, time.UTC),
	}
}

func TestCommitMessage(t *testing.T) {
	l := testLayer(t, "editors")
	changes := []*Change{{
		Layer:  l,
		Path:   ".vimrc",
		Status: Push,
		Base:   &state.File{Hash: "abc", Reason: ReasonAck},
	}}
	msg := CommitMessage(changes, testOrigin())
	for _, want := range []string{
		"editors: .vimrc\n",
		"write editors/.vimrc (ack)\n",
		"Over-Host: mariusmac\n",
		"Over-User: marius\n",
		"Over-Time: 2026-09-12T15:32:58Z\n",
		"Over-Version: devel\n",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("commit message missing %q:\n%s", want, msg)
		}
	}
}

func TestCommitMessageMultiple(t *testing.T) {
	editors, shell := testLayer(t, "editors"), testLayer(t, "shell")
	changes := []*Change{
		{Layer: editors, Path: ".vimrc", Status: Push, Base: &state.File{Hash: "a"}},
		{Layer: shell, Path: ".zshrc", Status: PushDelete, Base: &state.File{Hash: "b"}},
	}
	msg := CommitMessage(changes, testOrigin())
	if !strings.HasPrefix(msg, "editors, shell: 2 files\n") {
		t.Errorf("subject = %q", strings.SplitN(msg, "\n", 2)[0])
	}
	for _, want := range []string{"write editors/.vimrc\n", "delete shell/.zshrc\n"} {
		if !strings.Contains(msg, want) {
			t.Errorf("commit message missing %q:\n%s", want, msg)
		}
	}
}

// TestParseCommitRoundTrip checks that what over writes it can read back.
func TestParseCommitRoundTrip(t *testing.T) {
	for _, reason := range []string{"", ReasonAck, ReasonTrack, ReasonRestore} {
		l := testLayer(t, "editors")
		changes := []*Change{{
			Layer:  l,
			Path:   ".vimrc",
			Status: Push,
			Base:   &state.File{Hash: "abc", Reason: reason},
		}}
		msg := CommitMessage(changes, testOrigin())
		_, body, _ := strings.Cut(msg, "\n")

		rec, ok := ParseCommit(body, "editors/.vimrc")
		if !ok {
			t.Fatalf("reason %q: ParseCommit found nothing in:\n%s", reason, msg)
		}
		if rec.Action != "write" || rec.Reason != reason {
			t.Errorf("reason %q: got action %q reason %q", reason, rec.Action, rec.Reason)
		}
		if got := rec.Origin.String(); got != "marius@mariusmac" {
			t.Errorf("origin = %q", got)
		}
		if !rec.Origin.Time.Equal(testOrigin().Time) {
			t.Errorf("time = %v", rec.Origin.Time)
		}
	}
}

func TestParseCommitDelete(t *testing.T) {
	l := testLayer(t, "editors")
	msg := CommitMessage([]*Change{{
		Layer: l, Path: ".oldrc", Status: PushDelete, Base: &state.File{Hash: "abc"},
	}}, testOrigin())
	rec, ok := ParseCommit(msg, "editors/.oldrc")
	if !ok || rec.Action != "delete" {
		t.Errorf("ParseCommit = %+v, %v", rec, ok)
	}
}

// TestParseCommitByHand checks that a commit over did not make is
// reported as such, so that log can fall back to git's own author.
func TestParseCommitByHand(t *testing.T) {
	if rec, ok := ParseCommit("just a normal commit message\n", "editors/.vimrc"); ok {
		t.Errorf("ParseCommit of a hand-written message = %+v, want not found", rec)
	}
	// An over commit that says nothing about this particular file still
	// carries its origin.
	body := "write editors/.zshrc\n\nOver-Host: mariusmac\nOver-User: marius\n"
	rec, ok := ParseCommit(body, "editors/.vimrc")
	if !ok {
		t.Fatal("ParseCommit found nothing")
	}
	if rec.Action != "" {
		t.Errorf("action = %q, want none", rec.Action)
	}
	if got := rec.Origin.String(); got != "marius@mariusmac" {
		t.Errorf("origin = %q", got)
	}
}

// TestParseCommitIgnoresOtherFiles checks that the line for one file is
// not mistaken for another's.
func TestParseCommitIgnoresOtherFiles(t *testing.T) {
	body := "write editors/.vimrc (ack)\nwrite editors/.vimrc.bak\n\nOver-Host: h\nOver-User: u\n"
	rec, _ := ParseCommit(body, "editors/.vimrc.bak")
	if rec.Reason != "" {
		t.Errorf("reason = %q, want none", rec.Reason)
	}
	rec, _ = ParseCommit(body, "editors/.vimrc")
	if rec.Reason != ReasonAck {
		t.Errorf("reason = %q, want %q", rec.Reason, ReasonAck)
	}
}
