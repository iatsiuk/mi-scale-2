package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

var version = "dev"

// exit codes
const (
	exitOK    = 0
	exitError = 1
	exitINT   = 130
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := newRootCmd().ExecuteContext(ctx)
	ctxErr := ctx.Err()
	stop()

	switch {
	case err != nil && ctxErr != nil:
		os.Exit(exitINT)
	case err != nil:
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(exitError)
	case ctxErr != nil:
		os.Exit(exitINT)
	}
	os.Exit(exitOK)
}
