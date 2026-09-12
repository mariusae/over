package over

import (
	"fmt"
	"os"
	"os/user"
	"sort"
	"strings"
	"time"
)

// over records where each change it publishes came from, so that the
// history of a file answers not just what changed but who changed it,
// from which machine, and why. The record lives in the commit message:
// one line per file naming the action and the reason for it, followed by
// trailers describing the machine that ran the sync.
//
//	editors: .vimrc
//
//	write editors/.vimrc (ack)
//
//	Over-Host: mariusmac
//	Over-User: marius
//	Over-Time: 2026-09-12T15:32:58Z
//	Over-Version: devel (10ed6a9)
//
// Commits made by hand carry none of this, and over reports what git
// knows about them instead.

// Trailer keys over writes on the commits it makes.
const (
	trailerHost    = "Over-Host"
	trailerUser    = "Over-User"
	trailerTime    = "Over-Time"
	trailerVersion = "Over-Version"
)

// The reasons a local file is ahead of its layer. An ordinary edit has
// no reason; the others record a deliberate act, which is worth keeping
// because it explains an overwrite that would otherwise look wrong.
const (
	ReasonAck     = "ack"
	ReasonTrack   = "track"
	ReasonRestore = "restore"
)

// An Origin describes the machine and account a sync ran on.
type Origin struct {
	Host    string
	User    string
	Version string
	Time    time.Time
}

// LocalOrigin returns the origin of a sync running here.
func LocalOrigin(version string) Origin {
	o := Origin{
		Host:    "unknown",
		User:    "unknown",
		Version: version,
		Time:    time.Now().UTC().Truncate(time.Second),
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		o.Host = host
	}
	switch {
	case os.Getenv("USER") != "":
		o.User = os.Getenv("USER")
	case os.Getenv("LOGNAME") != "":
		o.User = os.Getenv("LOGNAME")
	default:
		if u, err := user.Current(); err == nil && u.Username != "" {
			o.User = u.Username
		}
	}
	return o
}

// String renders an origin as "user@host", the form the log reports.
func (o Origin) String() string {
	if o.Host == "" && o.User == "" {
		return ""
	}
	return o.User + "@" + o.Host
}

// A Record is what over recorded in a commit about one file.
type Record struct {
	Origin Origin

	// Action is "write" or "delete".
	Action string

	// Reason is why the local file was ahead: one of the Reason
	// constants, or empty for an ordinary edit.
	Reason string
}

// CommitMessage composes the message for a set of outgoing changes, all
// of which belong to one repository.
func CommitMessage(cs []*Change, origin Origin) string {
	names := map[string]bool{}
	lines := make([]string, 0, len(cs))
	for _, c := range cs {
		names[c.Layer.Spec.Name] = true
		lines = append(lines, recordLine(c))
	}
	sort.Strings(lines)

	layers := make([]string, 0, len(names))
	for name := range names {
		layers = append(layers, name)
	}
	sort.Strings(layers)

	subject := fmt.Sprintf("%s: %d files", strings.Join(layers, ", "), len(cs))
	if len(cs) == 1 {
		subject = fmt.Sprintf("%s: %s", strings.Join(layers, ", "), cs[0].Path)
	}

	var b strings.Builder
	b.WriteString(subject)
	b.WriteString("\n\n")
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "%s: %s\n", trailerHost, origin.Host)
	fmt.Fprintf(&b, "%s: %s\n", trailerUser, origin.User)
	fmt.Fprintf(&b, "%s: %s\n", trailerTime, origin.Time.UTC().Format(time.RFC3339))
	if origin.Version != "" {
		fmt.Fprintf(&b, "%s: %s\n", trailerVersion, origin.Version)
	}
	return b.String()
}

// recordLine renders the body line describing one change.
func recordLine(c *Change) string {
	action := "write"
	if c.Status == PushDelete {
		action = "delete"
	}
	line := action + " " + c.RepoFile()
	if c.Base != nil && c.Base.Reason != "" {
		line += " (" + c.Base.Reason + ")"
	}
	return line
}

// ParseCommit extracts what over recorded in a commit message about the
// given repository-relative file. It reports false for a commit over did
// not make, or one that says nothing about the file.
func ParseCommit(body, file string) (Record, bool) {
	var (
		rec   Record
		found bool
	)
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if key, value, ok := strings.Cut(line, ": "); ok {
			switch key {
			case trailerHost:
				rec.Origin.Host = value
				continue
			case trailerUser:
				rec.Origin.User = value
				continue
			case trailerTime:
				if t, err := time.Parse(time.RFC3339, value); err == nil {
					rec.Origin.Time = t
				}
				continue
			case trailerVersion:
				rec.Origin.Version = value
				continue
			}
		}
		if action, rest, ok := strings.Cut(line, " "); ok && (action == "write" || action == "delete") {
			name, reason := rest, ""
			if open := strings.LastIndex(rest, " ("); open >= 0 && strings.HasSuffix(rest, ")") {
				name, reason = rest[:open], rest[open+2:len(rest)-1]
			}
			if name == file {
				rec.Action, rec.Reason, found = action, reason, true
			}
		}
	}
	if rec.Origin.Host == "" && !found {
		return Record{}, false
	}
	return rec, found || rec.Origin.Host != ""
}
