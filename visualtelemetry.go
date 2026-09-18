// Package visualtelemetry is the plugin that draws a lap: speed, revs, throttle
// and brake against distance, the sectors marked, the circuit in the corner,
// for one lap or for laps of the team overlaid.
//
// The client posts each lap's trace to this plugin's own route; the plugin
// keeps it and draws it, as a document (SVG) for the pages and as a picture
// (PNG) for sharing, both from the same drawing code. It calls nothing outside
// the machine and spends nothing.
package visualtelemetry

import (
	"context"
	"log/slog"
	"time"

	"github.com/pacenote-sim/plugin"
)

// VisualTelemetry implements the plugin contract.
type VisualTelemetry struct {
	// Store keeps the laps. Without one nothing can be kept, so nothing can be
	// drawn later: the plugin answers every post with a plain refusal.
	Store *Store
	// Log receives this plugin's own lines.
	Log *slog.Logger
	// now is the clock, injectable for tests.
	now func() time.Time
}

// New builds the plugin.
func New(store *Store, log *slog.Logger) *VisualTelemetry {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &VisualTelemetry{Store: store, Log: log, now: time.Now}
}

// Settings declares what the operator has to configure.
func (v *VisualTelemetry) Settings(context.Context) ([]plugin.Setting, error) { return Settings(), nil }

// Notify is told something happened. This plugin asks for no events.
func (v *VisualTelemetry) Notify(context.Context, plugin.Event) (plugin.Usage, error) {
	return plugin.Usage{}, nil
}

func (v *VisualTelemetry) clock() time.Time {
	if v.now == nil {
		return time.Now()
	}
	return v.now()
}
