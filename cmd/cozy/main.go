package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"cozy/internal/panel/cli"
	"cozy/web"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.Run(ctx, os.Args[1:], version, web.Dist()); err != nil {
		fmt.Fprintln(os.Stderr, "cozy:", err)
		os.Exit(1)
	}
}
