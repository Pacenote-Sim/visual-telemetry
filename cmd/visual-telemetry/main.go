// Command visual-telemetry is the plugin that draws a lap's telemetry.
//
// It is started by the Pacenote server, not by a person. Running it from a
// shell prints the handshake line and exits.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/pacenote-sim/plugin"

	visualtelemetry "github.com/pacenote-sim/visual-telemetry"
)

const openTimeout = 10 * time.Second

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	store, err := openStore(log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "visual-telemetry: %v\n", err)
		os.Exit(1)
	}
	if store != nil {
		defer store.Close()
	}
	plugin.Serve(visualtelemetry.New(store, log))
}

// openStore connects to the database the host handed over. None is a warning:
// the plugin starts, and refuses every lap plainly until it has one.
func openStore(log *slog.Logger) (*visualtelemetry.Store, error) {
	dsn, err := plugin.DatabaseURL()
	if err != nil {
		log.Warn("no database was provided, so no lap can be kept or drawn", "reason", err)
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	return visualtelemetry.Open(ctx, dsn)
}
