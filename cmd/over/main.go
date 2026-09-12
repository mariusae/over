// Command over is a tool for managing overlays.
//
// Usage:
//
//	over [flags] <command> [arguments]
//
// Run "over help" for the list of commands.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/mariusae/over/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(cli.Main(ctx, os.Args[1:]))
}
