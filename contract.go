package visualtelemetry

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/pacenote-sim/plugin"
	"github.com/pacenote-sim/protocol/trace"
	"github.com/pacenote-sim/protocol/wire"
)

// What a client posts, and what is kept.
//
// The server keeps a lap's trace in a codec of its own and does not hand it to
// plugins, so the client — which has the trace at full rate anyway — posts it
// here, in the protocol's own point shape. This plugin keeps every lap it is
// given, so that any of them can be drawn again and any of them overlaid.
//
// Unknown fields are ignored: a client that sends something new tomorrow must
// not break the chart it draws today.

// MaxTracePoints is the most points one lap may carry: the protocol's own cap,
// which at ten samples a second is a lap of nearly seven minutes.
const MaxTracePoints = trace.MaxPoints

// MaxSectors bounds the sector boundaries. Three is racing; eight is generous.
const MaxSectors = 8

// LapUpload is one lap as the client measured it.
type LapUpload struct {
	// StintID and Lap are the server's own identifiers, from the upload.
	StintID string `json:"stint_id"`
	Lap     int    `json:"lap"`
	// LapMs is the lap time. Zero means the client did not say, and the last
	// sample's time stands in.
	LapMs   int                `json:"lap_ms,omitempty"`
	Session plugin.SessionType `json:"session,omitempty"`
	Track   string             `json:"track,omitempty"`
	Car     string             `json:"car,omitempty"`
	// TrackLengthM turns a thousandth of a lap into metres on the axis. Zero
	// draws the axis in thousandths.
	TrackLengthM int `json:"track_length_m,omitempty"`
	// Sectors are the boundaries as fractions of the lap, ascending, the first
	// conventionally zero — the stint's, repeated here so the chart does not
	// have to look them up.
	Sectors []float64 `json:"sectors,omitempty"`
	// Compound is the tyre, as the simulator spells it, for the legend.
	Compound string `json:"compound,omitempty"`
	// Corners place the turn numbers on the circuit map.
	Corners []cornerMark `json:"corners,omitempty"`
	// Trace is the lap, sampled. At least two points, at most MaxTracePoints,
	// each with its distance round the lap.
	Trace []wire.TracePoint `json:"trace"`
}

// cornerMark is a turn number and where its apex is, in thousandths of the lap.
type cornerMark struct {
	Turn    int `json:"turn"`
	ApexPct int `json:"apex_pct"`
}

var errBadLap = errors.New("visual-telemetry: the lap cannot be drawn")

// parseLapUpload reads a posted lap and refuses one that cannot be drawn.
func parseLapUpload(body []byte) (LapUpload, error) {
	var up LapUpload
	if err := json.Unmarshal(body, &up); err != nil {
		return LapUpload{}, fmt.Errorf("%w: the body is not readable JSON: %w", errBadLap, err)
	}
	if err := up.validate(); err != nil {
		return LapUpload{}, fmt.Errorf("%w: %w", errBadLap, err)
	}
	// Points in the order they are round the lap, whatever order they came in.
	sort.SliceStable(up.Trace, func(i, j int) bool {
		if up.Trace[i].DistPct != up.Trace[j].DistPct {
			return up.Trace[i].DistPct < up.Trace[j].DistPct
		}
		return up.Trace[i].OffsetMs < up.Trace[j].OffsetMs
	})
	return up, nil
}

func (up LapUpload) validate() error {
	switch {
	case strings.TrimSpace(up.StintID) == "" || len(up.StintID) > 64 || !identifier(up.StintID):
		return errors.New("stint_id is required and has to be an identifier")
	case up.Lap <= 0:
		return errors.New("lap must be the simulator's lap number, from 1")
	case len(up.Trace) < 2:
		return errors.New("a trace needs at least two points")
	case len(up.Trace) > MaxTracePoints:
		return fmt.Errorf("%d points is more than a lap may carry; the limit is %d", len(up.Trace), MaxTracePoints)
	case up.LapMs < 0 || up.TrackLengthM < 0:
		return errors.New("a time or a length cannot be negative")
	case len(up.Sectors) > MaxSectors:
		return fmt.Errorf("%d sector boundaries is more than a circuit has; the limit is %d", len(up.Sectors), MaxSectors)
	case len(up.Track) > 120 || len(up.Car) > 120 || len(up.Compound) > 24:
		return errors.New("a name is longer than a name")
	}
	for i, p := range up.Trace {
		if p.DistPct < 0 || p.DistPct > 1000 {
			return fmt.Errorf("point %d is outside the lap", i)
		}
		if p.OffsetMs < 0 {
			return fmt.Errorf("point %d is before the lap started", i)
		}
	}
	for i, s := range up.Sectors {
		if s < 0 || s > 1 || (i > 0 && s <= up.Sectors[i-1]) {
			return errors.New("sectors have to be fractions of the lap, ascending")
		}
	}
	for _, c := range up.Corners {
		if c.Turn <= 0 || c.ApexPct < 0 || c.ApexPct > 1000 {
			return errors.New("a corner needs a turn number from 1 and an apex inside the lap")
		}
	}
	return nil
}

func identifier(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) || unicode.IsSpace(r) || r == '/' {
			return false
		}
	}
	return true
}

// Lap is one kept lap, as drawn.
type Lap struct {
	StintID      string
	Lap          int
	DriverSlug   string
	DriverName   string
	Session      string
	Track        string
	Car          string
	TrackLengthM int
	LapMs        int
	Compound     string
	Sectors      []float64
	Corners      []cornerMark
	Trace        []wire.TracePoint
	CreatedAt    time.Time
}

// Ref is how a lap is named in an address: "<stint>/<lap>".
func (l Lap) Ref() string { return l.StintID + "/" + strconv.Itoa(l.Lap) }

// parseRef reads "<stint>/<lap>" back.
func parseRef(ref string) (stint string, lap int, ok bool) {
	stint, n, found := strings.Cut(ref, "/")
	if !found || stint == "" || !identifier(stint) || len(stint) > 64 {
		return "", 0, false
	}
	lap, err := strconv.Atoi(n)
	if err != nil || lap <= 0 {
		return "", 0, false
	}
	return stint, lap, true
}

// lapOf is a stored lap from an upload and the driver the host says posted it.
func lapOf(up LapUpload, who plugin.Caller, at time.Time) Lap {
	return Lap{
		StintID: up.StintID, Lap: up.Lap, DriverSlug: who.DriverSlug, DriverName: who.DriverName,
		Session: string(up.Session), Track: up.Track, Car: up.Car, TrackLengthM: up.TrackLengthM,
		LapMs: up.LapMs, Compound: up.Compound, Sectors: up.Sectors, Corners: up.Corners,
		Trace: up.Trace, CreatedAt: at,
	}
}
