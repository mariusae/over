package cli

import (
	"strings"
	"testing"
)

// TestTrackWildcardMakesARule is the case that prompted rules: a
// wildcard names no particular file, so it is recorded rather than
// expanded once.
func TestTrackWildcardMakesARule(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	c.write(".apex/attach", "attach\n")
	c.write(".apex/profile", "profile\n")

	out := c.mustOver("track", "mariusae/config:editors", ".apex/...")
	if !strings.Contains(out, ".apex/... tracked in mariusae/config:editors (rule, 2 files)") {
		t.Errorf("track = %q", out)
	}
	// The rule claims what is there now.
	out = c.mustOver("sync")
	for _, want := range []string{
		".apex/attach to mariusae/config:editors (claimed)",
		".apex/profile to mariusae/config:editors (claimed)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sync output missing %q:\n%s", want, out)
		}
	}

	// And what appears later, which is the whole point.
	c.write(".apex/newthing", "new\n")
	if out := c.mustOver("sync"); !strings.Contains(out, ".apex/newthing to mariusae/config:editors (claimed)") {
		t.Errorf("sync = %q", out)
	}
	r.pull()
	if got := r.read("editors/.apex/newthing"); got != "new\n" {
		t.Errorf("layer copy = %q", got)
	}

	// The rule is stored with the layer, relative to its root.
	if got := r.read("config.yaml"); !strings.Contains(got, ".apex/...") {
		t.Errorf("config.yaml = %q", got)
	}
}

// TestTrackNamedFileIsNotARule checks the other half: a plain path still
// tracks just that file.
func TestTrackNamedFileIsNotARule(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write(".apex/attach", "attach\n")
	c.write(".apex/profile", "profile\n")

	c.mustOver("track", "mariusae/config:editors", ".apex/attach")
	if out := c.mustOver("rule", "mariusae/config:editors"); !strings.Contains(out, "no rules") {
		t.Errorf("a named path made a rule: %q", out)
	}
	out := c.mustOver("sync")
	if !strings.Contains(out, ".apex/attach to") {
		t.Errorf("sync = %q", out)
	}
	if strings.Contains(out, ".apex/profile") {
		t.Errorf("sync took a file nobody named:\n%s", out)
	}
}

// TestRuleIgnore checks the counterweight to a track rule.
func TestRuleIgnore(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write(".apex/attach", "attach\n")
	c.write(".apex/cache/blob", "junk\n")
	c.write(".apex/token", "secret\n")
	c.mustOver("track", "mariusae/config:editors", ".apex/...")

	c.mustOver("rule", "-ignore", "mariusae/config:editors", ".apex/cache/...", ".apex/token")
	out := c.mustOver("sync")
	if !strings.Contains(out, ".apex/attach to") {
		t.Errorf("sync = %q", out)
	}
	for _, unwanted := range []string{".apex/cache/blob", ".apex/token"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("sync took an ignored file %q:\n%s", unwanted, out)
		}
	}

	rules := c.mustOver("rule", "mariusae/config:editors")
	for _, want := range []string{"track   .apex/...", "ignore  .apex/cache/...", "ignore  .apex/token"} {
		if !strings.Contains(collapse(rules), collapse(want)) {
			t.Errorf("rule listing missing %q:\n%s", want, rules)
		}
	}
}

// TestRuleIgnoreDoesNotDisownContent checks the line between an ignore
// rule and an exclusion: a file the layer already holds keeps syncing,
// because somebody published it deliberately.
func TestRuleIgnoreDoesNotDisownContent(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	c.write(".apex/attach", "attach\n")
	c.mustOver("track", "mariusae/config:editors", ".apex/...")
	c.mustOver("sync")

	c.mustOver("rule", "-ignore", "mariusae/config:editors", ".apex/...")
	r.pull()
	r.write("editors/.apex/attach", "changed elsewhere\n")
	r.commit("remote edit")
	if out := c.mustOver("sync"); !strings.Contains(out, ".apex/attach from mariusae/config:editors") {
		t.Errorf("an ignore rule stopped syncing content the layer holds:\n%s", out)
	}
	if got := c.read(".apex/attach"); got != "changed elsewhere\n" {
		t.Errorf(".apex/attach = %q", got)
	}
}

// TestExcludeOutranksRules checks the top of the three tiers: a path the
// client refuses to manage is never claimed, whatever a layer asks for.
func TestExcludeOutranksRules(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write(".apex/attach", "attach\n")
	c.write(".apex/private/key", "secret\n")
	c.mustOver("track", "mariusae/config:editors", ".apex/...")
	c.mustOver("exclude", "$HOME/.apex/private/...")

	out := c.mustOver("sync")
	if !strings.Contains(out, ".apex/attach to") {
		t.Errorf("sync = %q", out)
	}
	if strings.Contains(out, ".apex/private/key") {
		t.Errorf("sync took an excluded file:\n%s", out)
	}
}

// TestUntrackAgainstARule checks that untracking says what it cannot do:
// it is a one-time edit, and a rule is standing.
func TestUntrackAgainstARule(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write(".apex/attach", "attach\n")
	c.mustOver("track", "mariusae/config:editors", ".apex/...")
	c.mustOver("sync")

	out := c.mustOver("untrack", ".apex/attach")
	if !strings.Contains(out, "a rule still claims it") {
		t.Errorf("untrack = %q", out)
	}
	if !strings.Contains(out, "over rule -ignore mariusae/config:editors .apex/attach") {
		t.Errorf("untrack does not say how to carve it out:\n%s", out)
	}
}

// TestRuleTravels checks that a rule is a property of the layer: another
// machine adding it claims its own files by the same rule.
func TestRuleTravels(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write(".apex/attach", "attach\n")
	c.mustOver("track", "mariusae/config:editors", ".apex/...")
	c.mustOver("sync")

	other := c.sub(t)
	other.mustOver("add", "mariusae/config:editors")
	other.mustOver("sync")
	other.write(".apex/onlyhere", "local to this machine\n")
	if out := other.mustOver("sync"); !strings.Contains(out, ".apex/onlyhere to mariusae/config:editors (claimed)") {
		t.Errorf("the rule did not travel with the layer:\n%s", out)
	}
}

func TestRuleRemove(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write(".apex/attach", "attach\n")
	c.mustOver("track", "mariusae/config:editors", ".apex/...")

	if out := c.mustOver("rule", "-rm", "mariusae/config:editors", ".apex/..."); !strings.Contains(out, "dropped from") {
		t.Errorf("rule -rm = %q", out)
	}
	if out := c.mustOver("rule", "mariusae/config:editors"); !strings.Contains(out, "no rules") {
		t.Errorf("rule listing after -rm = %q", out)
	}
	// Nothing is claimed any more.
	if out := c.mustOver("sync"); strings.Contains(out, ".apex/attach") {
		t.Errorf("sync still claims the file:\n%s", out)
	}
}

func TestRuleErrors(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")

	// A rule has to stay under the layer's root and name somewhere.
	code, _, stderr := c.over("track", "mariusae/config:editors", "...")
	if code != exitError || !strings.Contains(stderr, "too broad") {
		t.Errorf("bare wildcard: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = c.over("rule", "mariusae/config:editors", "/etc/...")
	if code != exitError || !strings.Contains(stderr, "outside") {
		t.Errorf("outside the root: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = c.over("rule", "-rm", "mariusae/config:editors", ".apex/...")
	if code != exitError || !strings.Contains(stderr, "not a rule of") {
		t.Errorf("removing a rule that is not there: exit %d, stderr %q", code, stderr)
	}
	code, _, stderr = c.over("rule", "mariusae/config:nosuch", ".apex/...")
	if code != exitError || !strings.Contains(stderr, "not a configured layer") {
		t.Errorf("unknown layer: exit %d, stderr %q", code, stderr)
	}
	if code, _, _ := c.over("rule"); code != exitUsage {
		t.Errorf("no layer: exit %d, want %d", code, exitUsage)
	}
}

// binaryFile is content that looks like a compiled program: a NUL early
// on is what tells over it is not text.
const binaryFile = "\x7fELF\x02\x01\x01\x00\x00\x00compiled"

// TestRuleSkipsBinaries is the case content sniffing exists for: a
// directory of scripts with a few compiled programs sitting in it.
func TestRuleSkipsBinaries(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	c.write("bin/hello", "#!/bin/sh\necho hello\n")
	c.write("bin/hi.py", "#!/usr/bin/env python3\nprint()\n")
	c.write("bin/program", binaryFile)

	out := c.mustOver("track", "mariusae/config:editors", "bin/...")
	if !strings.Contains(out, "rule, 2 files") {
		t.Errorf("track = %q, want the two scripts", out)
	}
	// The skip is reported rather than silent, and says how to undo it.
	if !strings.Contains(out, "1 binary skipped, pass -includebin to take them") {
		t.Errorf("track does not report the skipped binary: %q", out)
	}

	out = c.mustOver("sync")
	for _, want := range []string{"bin/hello to", "bin/hi.py to"} {
		if !strings.Contains(out, want) {
			t.Errorf("sync output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "bin/program") {
		t.Errorf("sync took the binary:\n%s", out)
	}
	r.pull()
	if r.exists("editors/bin/program") {
		t.Error("the binary reached the layer")
	}
}

// TestTrackRefusesANamedBinary checks the other half of the default: a
// binary named outright is refused rather than quietly skipped, since
// naming it says you meant it.
func TestTrackRefusesANamedBinary(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write("bin/program", binaryFile)

	code, _, stderr := c.over("track", "mariusae/config:editors", "bin/program")
	if code != exitError {
		t.Errorf("exit %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr, "a binary file") || !strings.Contains(stderr, "-includebin") {
		t.Errorf("stderr = %q", stderr)
	}
}

// TestIncludeBin checks the opt-in, and that it is a property of the
// layer rather than of the command that set it.
func TestIncludeBin(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	c.write("bin/hello", "#!/bin/sh\necho hello\n")
	c.write("bin/program", binaryFile)

	out := c.mustOver("track", "-includebin", "mariusae/config:editors", "bin/...")
	if !strings.Contains(out, "now holds binary files") {
		t.Errorf("track -includebin = %q", out)
	}
	if !strings.Contains(out, "rule, 2 files") || strings.Contains(out, "skipped") {
		t.Errorf("track -includebin skipped something: %q", out)
	}
	c.mustOver("sync")
	r.pull()
	if got := r.read("editors/bin/program"); got != binaryFile {
		t.Errorf("the binary did not survive the round trip: %q", got)
	}

	// The setting is recorded with the layer, and listed.
	if got := r.read("config.yaml"); !strings.Contains(got, "includebin: true") {
		t.Errorf("config.yaml = %q", got)
	}
	if out := c.mustOver("rule", "mariusae/config:editors"); !strings.Contains(collapse(out), "includebin true") {
		t.Errorf("rule listing = %q", out)
	}

	// A binary added later is claimed by the same rule, with no further
	// ceremony.
	c.write("bin/another", binaryFile)
	if out := c.mustOver("sync"); !strings.Contains(out, "bin/another to mariusae/config:editors (claimed)") {
		t.Errorf("sync = %q", out)
	}
}

// TestIncludeBinTravels checks that the setting reaches every machine
// that adds the layer, as a rule does.
func TestIncludeBinTravels(t *testing.T) {
	c, _ := newLayer(t)
	c.mustOver("sync")
	c.write("bin/hello", "#!/bin/sh\necho hello\n")
	c.mustOver("track", "-includebin", "mariusae/config:editors", "bin/...")
	c.mustOver("sync")

	other := c.sub(t)
	other.mustOver("add", "mariusae/config:editors")
	other.mustOver("sync")
	other.write("bin/program", binaryFile)
	if out := other.mustOver("sync"); !strings.Contains(out, "bin/program to mariusae/config:editors (claimed)") {
		t.Errorf("the setting did not travel with the layer:\n%s", out)
	}
}

// TestBinaryRulesBearOnClaimingOnly checks the line drawn elsewhere for
// ignore rules: a file over already tracks keeps syncing, whatever its
// contents become.
func TestBinaryRulesBearOnClaimingOnly(t *testing.T) {
	c, r := newLayer(t)
	c.mustOver("sync")
	c.write("bin/tool", "#!/bin/sh\necho hi\n")
	c.mustOver("track", "mariusae/config:editors", "bin/...")
	c.mustOver("sync")

	// The script is replaced by a compiled program of the same name.
	c.write("bin/tool", binaryFile)
	if out := c.mustOver("sync"); !strings.Contains(out, "bin/tool to mariusae/config:editors") {
		t.Errorf("a tracked file stopped syncing when it became binary:\n%s", out)
	}
	r.pull()
	if got := r.read("editors/bin/tool"); got != binaryFile {
		t.Errorf("layer copy = %q", got)
	}
}
