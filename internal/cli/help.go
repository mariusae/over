package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

func init() {
	Register(helpCmd)
}

var helpCmd = &Command{
	Name:  "help",
	Usage: "help [command]",
	Short: "show help for over or a command",
	Long: `Help shows the usage of over itself, or, given a command name,
the usage of that command.`,
	Run: runHelp,
}

func runHelp(ctx context.Context, env *Env, args []string) error {
	switch len(args) {
	case 0:
		usage(env.Stdout, globalFlags())
		return nil
	case 1:
		cmd := lookup(args[0])
		if cmd == nil {
			return fmt.Errorf("unknown command %q; run 'over help' for the list of commands", args[0])
		}
		fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		if cmd.Flags != nil {
			cmd.Flags(fs)
		}
		commandUsage(env.Stdout, cmd, fs)
		return nil
	default:
		return Usagef("too many arguments")
	}
}

// globalFlags returns a flag set describing over's global flags. It
// mirrors the set constructed in Run, and exists so that help can render
// the flags without running a command.
func globalFlags() *flag.FlagSet {
	fs := flag.NewFlagSet("over", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("C", "", "run as if over was started in `dir`")
	fs.Bool("v", false, "log details of what over is doing")
	return fs
}

// usage writes over's top-level usage message to w.
func usage(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprint(w, `over manages overlays.

Usage:

	over [flags] <command> [arguments]

Commands:

`)
	tw := tabwriter.NewWriter(w, 0, 4, 4, ' ', 0)
	for _, cmd := range listedCommands() {
		fmt.Fprintf(tw, "\t%s\t%s\n", cmd.Name, cmd.Short)
	}
	tw.Flush()

	fmt.Fprint(w, "\nFlags:\n\n")
	writeFlags(w, fs)

	fmt.Fprint(w, "\nUse \"over help <command>\" for more information about a command.\n")
}

// commandUsage writes the usage message for cmd to w.
func commandUsage(w io.Writer, cmd *Command, fs *flag.FlagSet) {
	fmt.Fprintf(w, "Usage:\n\n\tover %s\n", cmd.Usage)
	if long := strings.TrimSpace(cmd.Long); long != "" {
		fmt.Fprintf(w, "\n%s\n", long)
	}
	if hasFlags(fs) {
		fmt.Fprint(w, "\nFlags:\n\n")
		writeFlags(w, fs)
	}
}

// writeFlags writes the flags of fs to w, one per line.
func writeFlags(w io.Writer, fs *flag.FlagSet) {
	tw := tabwriter.NewWriter(w, 0, 4, 4, ' ', 0)
	fs.VisitAll(func(f *flag.Flag) {
		name, usage := flag.UnquoteUsage(f)
		if name != "" {
			name = " " + name
		}
		fmt.Fprintf(tw, "\t-%s%s\t%s\n", f.Name, name, usage)
	})
	tw.Flush()
}

// hasFlags reports whether any flags are defined on fs.
func hasFlags(fs *flag.FlagSet) bool {
	n := 0
	fs.VisitAll(func(*flag.Flag) { n++ })
	return n > 0
}
