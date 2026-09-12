package cli

import (
	"context"
	"flag"
	"fmt"
	"text/tabwriter"

	"github.com/mariusae/over/internal/over"
	"github.com/mariusae/over/internal/pathspec"
	"github.com/mariusae/over/internal/spec"
)

func init() {
	Register(ruleCmd)
}

var (
	ruleFlagIgnore bool
	ruleFlagRm     bool
)

var ruleCmd = &Command{
	Name:  "rule",
	Usage: "rule [-ignore] [-rm] <layer> [pattern...]",
	Short: "show or change a layer's tracking rules",
	Long: `Rule prints a layer's tracking rules, or adds to them when
given patterns. The -ignore flag adds to the layer's ignore rules
instead, and -rm takes a rule back.

A tracking rule answers, once and for every machine, the question "over
track" otherwise answers one file at a time: which local files does this
layer claim? A file matching a track rule is written to the layer on the
next sync though nobody named it.

	over rule mariusae/config:editors .config/ion/...
	over rule -ignore mariusae/config:editors .config/ion/cache/...

Rules bear on claiming only. What a layer holds is what a layer holds: a
file already in it is synced whatever the rules say, because somebody
published it deliberately. An ignore rule is the counterweight to a
track rule, not a way to disown content -- for that, see "over exclude",
which outranks every layer's rules.

Rules belong to the layer, so they are kept in its repository and
changing one changes it for everybody: rule commits and pushes, as "over
init" does. They are written relative to the layer's root, so that they
mean the same thing wherever it is materialized; patterns given here are
path arguments as usual, and are converted.

"over track" with a wildcard in it adds a track rule, which is the
shortest way to write one.`,
	Flags: func(fs *flag.FlagSet) {
		fs.BoolVar(&ruleFlagIgnore, "ignore", false, "add to the layer's ignore rules rather than its track rules")
		fs.BoolVar(&ruleFlagRm, "rm", false, "remove the patterns from the layer's rules")
	},
	Run: runRule,
}

func runRule(ctx context.Context, env *Env, args []string) error {
	if len(args) == 0 {
		return Usagef("expected a layer")
	}
	layerArg, patterns := args[0], args[1:]
	if _, err := spec.Parse(layerArg); err != nil {
		return Usagef("%v; the first argument names the layer", err)
	}
	o, err := openOver(env)
	if err != nil {
		return err
	}
	layers, err := o.Layers(ctx, false)
	if err != nil {
		return err
	}
	if len(layers) == 0 {
		return over.ErrNoLayers
	}
	l := findLayer(layers, layerArg)
	if l == nil {
		return fmt.Errorf("%s: not a configured layer; run 'over status' for the list", layerArg)
	}
	if len(patterns) == 0 {
		if ruleFlagRm {
			return Usagef("expected at least one pattern")
		}
		return listRules(env, o, l)
	}

	rules, err := rulesFor(env, o, l, patterns)
	if err != nil {
		return err
	}
	kind := over.RuleTrack
	if ruleFlagIgnore {
		kind = over.RuleIgnore
	}
	changed, err := o.Rules(ctx, l.Spec, kind, rules, ruleFlagRm, over.LocalOrigin(Version()))
	if err != nil {
		return err
	}
	if len(changed) == 0 {
		if ruleFlagRm {
			return fmt.Errorf("%s: not a rule of %s; run 'over rule %s' for the list",
				patterns[0], l, l)
		}
		env.Printf("already a rule of %s\n", l)
		return nil
	}
	for _, rule := range changed {
		if ruleFlagRm {
			env.Printf("%s dropped from %s\n", rule, l)
		} else {
			env.Printf("%s %s in %s\n", rule, ruleVerb[kind], l)
		}
	}
	return nil
}

// ruleVerb is how each kind of rule reads when it has been added.
var ruleVerb = map[string]string{
	over.RuleTrack:  "tracked",
	over.RuleIgnore: "ignored",
}

// rulesFor converts path arguments into the rules they express. A
// pattern that is already spelled as a rule is taken as one, so that a
// rule can be removed as "over rule" printed it, from anywhere.
func rulesFor(env *Env, o *over.Over, l *over.Layer, patterns []string) ([]string, error) {
	existing := map[string]bool{}
	for _, rule := range append(o.LayerRules(l.Spec, over.RuleTrack), o.LayerRules(l.Spec, over.RuleIgnore)...) {
		existing[rule] = true
	}
	rules := make([]string, 0, len(patterns))
	for _, arg := range patterns {
		if existing[arg] {
			rules = append(rules, arg)
			continue
		}
		pat, err := pathspec.Parse(env.Dir, arg)
		if err != nil {
			return nil, err
		}
		rule, err := over.RelativeRule(l, pat)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// listRules prints a layer's rules.
func listRules(env *Env, o *over.Over, l *over.Layer) error {
	track := o.LayerRules(l.Spec, over.RuleTrack)
	ignore := o.LayerRules(l.Spec, over.RuleIgnore)
	if len(track) == 0 && len(ignore) == 0 {
		env.Printf("no rules; %s holds only what is tracked by name\n", l)
		return nil
	}
	tw := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	defer tw.Flush()
	for _, rule := range track {
		fmt.Fprintf(tw, "track\t%s\n", rule)
	}
	for _, rule := range ignore {
		fmt.Fprintf(tw, "ignore\t%s\n", rule)
	}
	return nil
}
