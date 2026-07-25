// Command notifier runs the notification orchestration service.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/evercifuentes/notifier/internal/app"
	"github.com/evercifuentes/notifier/internal/config"
	"github.com/evercifuentes/notifier/internal/platform/log"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "notifier: %v\n", err)
		os.Exit(1)
	}
}

// run keeps main free of logic so every failure path exits through one place
// and deferred cleanup actually runs (os.Exit skips defers).
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := log.New(os.Stdout, cfg.Log.Level, cfg.Log.Format)

	// SIGINT and SIGTERM start a graceful drain rather than killing the
	// process: work already accepted from clients gets a chance to finish.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return app.New(cfg, logger, app.Dependencies{}).Run(ctx)
}
