# over

`over` manages overlays.

The specification is still being written; this repository currently holds
the scaffolding: the module, the command entry point, and the command
line plumbing.

## Building

	make build     # writes ./over
	make test
	make lint

Or with the Go tool directly:

	go build ./cmd/over
	go test ./...

## Layout

	cmd/over        the over command; a thin main that calls internal/cli
	internal/cli    command registration, flag parsing, and dispatch

## Adding a command

Each command lives in its own file under `internal/cli` and registers
itself from an `init` function:

```go
func init() {
	Register(exampleCmd)
}

var exampleFlagN bool

var exampleCmd = &Command{
	Name:  "example",
	Usage: "example [-n] <arg>",
	Short: "do the example thing",
	Long:  `Example does the example thing to arg.`,
	Flags: func(fs *flag.FlagSet) {
		fs.BoolVar(&exampleFlagN, "n", false, "print what would be done, but do nothing")
	},
	Run: func(ctx context.Context, env *Env, args []string) error {
		if len(args) != 1 {
			return Usagef("expected exactly one argument")
		}
		env.Printf("example %s\n", args[0])
		return nil
	},
}
```

`Run` returns an error to fail with exit status 1, or a `*UsageError`
(from `Usagef`) to print the command's usage and exit with status 2.
Output goes to `env.Stdout` and `env.Stderr` rather than to the process
streams, so commands remain testable; `env.Logf` writes only under the
global `-v` flag.
