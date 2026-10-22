package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/noklash/netprobe/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	exitCode := cli.Run(
		ctx,
		os.Args[1:],
		os.Stdout,
		os.Stderr,
	)

	os.Exit(exitCode)
}
