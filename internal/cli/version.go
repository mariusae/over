package cli

import (
	"context"
	"runtime/debug"
)

func init() {
	Register(versionCmd)
}

// version is the tool's version. It is "devel" for unstamped builds, and
// may be overridden at link time:
//
//	go build -ldflags "-X github.com/mariusae/over/internal/cli.version=v1.2.3" ./cmd/over
var version = "devel"

var versionCmd = &Command{
	Name:  "version",
	Usage: "version",
	Short: "print the over version",
	Long:  `Version prints over's version, followed by the revision it was built from, if known.`,
	Run:   runVersion,
}

func runVersion(ctx context.Context, env *Env, args []string) error {
	if len(args) > 0 {
		return Usagef("too many arguments")
	}
	env.Printf("over %s\n", Version())
	return nil
}

// Version returns over's version, including the VCS revision recorded by
// the Go toolchain when it is available.
func Version() string {
	if version != "devel" {
		// Stamped at link time.
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return version
	}
	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			if s.Value == "true" {
				modified = "-dirty"
			}
		}
	}
	if revision != "" {
		// Built from a checkout: the revision is more informative than
		// the pseudo-version derived from it.
		if len(revision) > 12 {
			revision = revision[:12]
		}
		return version + " (" + revision + modified + ")"
	}
	// Installed as a module, e.g. by "go install ...@v1.2.3".
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return version
}
