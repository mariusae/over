// Package cli implements over's command line interface.
//
// Commands are values of type [Command], registered with [Register] from
// each command's own file. [Main] parses the global flags, selects a
// command, parses its flags, and runs it.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
)

// Exit codes returned by [Main].
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

// Env is the environment in which a command runs. It carries the
// command's output streams and the global flag values, so that commands
// do not reach for process-wide state directly.
type Env struct {
	// Stdout and Stderr are the command's output streams.
	Stdout io.Writer
	Stderr io.Writer

	// Dir is the directory in which over was invoked, as modified by
	// the global -C flag. It is always absolute.
	Dir string

	// Verbose reports whether the global -v flag was given. Commands
	// should log their intermediate steps to Stderr when it is set.
	Verbose bool
}

// Printf formats according to a format specifier and writes to the
// environment's standard output.
func (e *Env) Printf(format string, args ...any) {
	fmt.Fprintf(e.Stdout, format, args...)
}

// Logf formats according to a format specifier and writes to the
// environment's standard error if the -v flag was given.
func (e *Env) Logf(format string, args ...any) {
	if !e.Verbose {
		return
	}
	fmt.Fprintf(e.Stderr, format, args...)
	if !strings.HasSuffix(format, "\n") {
		fmt.Fprintln(e.Stderr)
	}
}

// A Command is a single over subcommand.
type Command struct {
	// Name is the name by which the command is invoked.
	Name string

	// Usage is a one-line usage string, beginning with the command's
	// name and listing its flags and arguments, e.g.
	// "sync [-n] [layer...]".
	Usage string

	// Short is a one-line description shown in the command list. It
	// begins with a lowercase letter and has no trailing period.
	Short string

	// Long is the command's full description, shown by "over help
	// <command>". It is displayed as written.
	Long string

	// Flags registers the command's flags on fs. It may be nil.
	Flags func(fs *flag.FlagSet)

	// Run executes the command with the arguments remaining after flag
	// parsing. Returning a UsageError prints the command's usage and
	// exits with status 2; any other error is reported and exits with
	// status 1.
	Run func(ctx context.Context, env *Env, args []string) error
}

// commands holds the registered commands, keyed by name.
var commands = map[string]*Command{}

// Register makes a command available to the CLI. It panics if a command
// with the same name is already registered, and is intended to be called
// from an init function.
func Register(cmds ...*Command) {
	for _, cmd := range cmds {
		if _, dup := commands[cmd.Name]; dup {
			panic("cli: command registered twice: " + cmd.Name)
		}
		commands[cmd.Name] = cmd
	}
}

// lookup returns the command named name, or nil if there is none.
func lookup(name string) *Command { return commands[name] }

// sortedCommands returns the registered commands ordered by name.
func sortedCommands() []*Command {
	cmds := make([]*Command, 0, len(commands))
	for _, cmd := range commands {
		cmds = append(cmds, cmd)
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	return cmds
}

// A UsageError indicates that a command was invoked incorrectly. Its
// message, if any, is printed before the command's usage.
type UsageError struct {
	// Cmd is the command that was misused. It is filled in by the
	// dispatcher when it is nil.
	Cmd *Command
	// Err is the underlying error; it may be nil.
	Err error
}

// Usagef returns a UsageError with a formatted message.
func Usagef(format string, args ...any) *UsageError {
	return &UsageError{Err: fmt.Errorf(format, args...)}
}

func (u *UsageError) Error() string {
	if u.Err == nil {
		return "usage error"
	}
	return u.Err.Error()
}

func (u *UsageError) Unwrap() error { return u.Err }

// Main runs the command named by args (typically os.Args[1:]) and
// returns the process exit code.
func Main(ctx context.Context, args []string) int {
	return Run(ctx, os.Stdout, os.Stderr, args)
}

// Run is Main with explicit output streams. It is used by tests.
func Run(ctx context.Context, stdout, stderr io.Writer, args []string) int {
	var (
		fs  = flag.NewFlagSet("over", flag.ContinueOnError)
		dir = fs.String("C", "", "run as if over was started in `dir`")
		env = &Env{Stdout: stdout, Stderr: stderr}
	)
	fs.BoolVar(&env.Verbose, "v", false, "log details of what over is doing")
	fs.SetOutput(stderr)
	fs.Usage = func() { usage(stderr, fs) }

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage(stdout, fs)
			return exitOK
		}
		return exitUsage
	}

	var err error
	if env.Dir, err = resolveDir(*dir); err != nil {
		return fail(stderr, err)
	}

	rest := fs.Args()
	if len(rest) == 0 {
		usage(stderr, fs)
		return exitUsage
	}

	name, rest := rest[0], rest[1:]
	cmd := lookup(name)
	if cmd == nil {
		fmt.Fprintf(stderr, "over: unknown command %q\n", name)
		fmt.Fprintf(stderr, "Run 'over help' for the list of commands.\n")
		return exitUsage
	}

	return runCommand(ctx, env, cmd, rest)
}

// runCommand parses cmd's flags and runs it.
func runCommand(ctx context.Context, env *Env, cmd *Command, args []string) int {
	fs := flag.NewFlagSet(cmd.Name, flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.Usage = func() { commandUsage(env.Stderr, cmd, fs) }
	if cmd.Flags != nil {
		cmd.Flags(fs)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			commandUsage(env.Stdout, cmd, fs)
			return exitOK
		}
		return exitUsage
	}

	err := cmd.Run(ctx, env, fs.Args())
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, context.Canceled):
		// The user interrupted us; exit as if by signal.
		fmt.Fprintln(env.Stderr, "over: interrupted")
		return exitError
	}

	var uerr *UsageError
	if errors.As(err, &uerr) {
		if uerr.Err != nil {
			fmt.Fprintf(env.Stderr, "over %s: %v\n", cmd.Name, uerr.Err)
		}
		commandUsage(env.Stderr, cmd, fs)
		return exitUsage
	}
	return fail(env.Stderr, err)
}

// resolveDir returns the absolute path of the working directory,
// changing to dir first if it is non-empty.
func resolveDir(dir string) (string, error) {
	if dir != "" {
		if err := os.Chdir(dir); err != nil {
			return "", err
		}
	}
	return os.Getwd()
}

// fail reports err and returns the error exit code.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "over: %v\n", err)
	return exitError
}
