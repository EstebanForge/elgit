// Command elgit is a git workflow helper: switch, sync, publish, unpublish,
// undo, branches. A safer, faster rewrite of the Python tool legit.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/EstebanForge/elgit/internal/cli"
)

func main() {
	// Ctrl-C and SIGTERM cancel the context behind every git call, so an
	// interrupted elgit stops spawning new commands instead of hanging.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.NewRootCmd().ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
