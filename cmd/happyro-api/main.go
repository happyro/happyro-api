package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/happyro/happyro-api/internal/cli"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Execute(ctx)
	cancel()
	os.Exit(code)
}
