package visualtelemetry

import (
	"fmt"
	"math"
	"sort"

	"github.com/pacenote-sim/protocol/wire"
)

// The arithmetic behind a chart. Every number here is derived from the trace
// by interpolation and nothing else: where the car was at a distance, when it
// got there, how far behind another lap it was at the same point.

// distanceOf is a point's place on the axis: metres when the circuit length is
// known, thousandths of the lap otherwise.
func (l Lap) distanceOf(pct float64) float64 {
	if l.TrackLengthM > 0 {
		return pct / 1000 * float64(l.TrackLengthM)
	}
	return pct
}

// axisUnit is what the distance axis is in.
func (l Lap) axisUnit() string {
	if l.TrackLengthM > 0 {
		return "m"
	}
	return "‰"
}

// endMs is when the lap ended: the lap time when the client said, the last
// sample otherwise.
func (l Lap) endMs() int {
	if l.LapMs > 0 {
		return l.LapMs
	}
	if n := len(l.Trace); n > 0 {
		return l.Trace[n-1].OffsetMs
	}
	return 0
}

// timeAt is when the car passed a distance, in milliseconds, interpolated
// between the two samples either side; the lap's end past the last sample.
func (l Lap) timeAt(pct float64) float64 {
	pts := l.Trace
	if len(pts) == 0 {
		return 0
	}
	if pct <= float64(pts[0].DistPct) {
		return float64(pts[0].OffsetMs)
	}
	i := sort.Search(len(pts), func(i int) bool { return float64(pts[i].DistPct) >= pct })
	if i >= len(pts) {
		return float64(l.endMs())
	}
	// Search found the first sample at or past pct, so the one before it is
	// strictly before: the two never share a distance.
	a, b := pts[i-1], pts[i]
	f := (pct - float64(a.DistPct)) / float64(b.DistPct-a.DistPct)
	return float64(a.OffsetMs) + f*float64(b.OffsetMs-a.OffsetMs)
}

// sectorTimes is how long each sector took, from the boundaries: the first
// from the start, the last to the lap's end. None without boundaries.
func (l Lap) sectorTimes() []float64 {
	if len(l.Sectors) == 0 {
		return nil
	}
	out := make([]float64, 0, len(l.Sectors))
	for i := range l.Sectors {
		start := l.timeAt(l.Sectors[i] * 1000)
		var end float64
		if i+1 < len(l.Sectors) {
			end = l.timeAt(l.Sectors[i+1] * 1000)
		} else {
			end = float64(l.endMs())
		}
		out = append(out, end-start)
	}
	return out
}

// maxSpeed is the fastest sample, and where it was.
func (l Lap) maxSpeed() (kmh, pct int) {
	for _, p := range l.Trace {
		if p.SpeedKmh > kmh {
			kmh, pct = p.SpeedKmh, p.DistPct
		}
	}
	return kmh, pct
}

// deltaTo is how far behind the other lap this one is at each of its samples,
// in milliseconds: positive is slower. Both laps are compared at the same
// distance, which is the only fair way to compare two laps.
func (l Lap) deltaTo(other Lap) []float64 {
	out := make([]float64, len(l.Trace))
	for i, p := range l.Trace {
		out[i] = float64(p.OffsetMs) - other.timeAt(float64(p.DistPct))
	}
	return out
}

// gpsOf is the map: the samples with a fix, as x and y in the unit the
// simulator sent, and whether there were enough to draw.
func gpsOf(pts []wire.TracePoint) (xs, ys []float64, ok bool) {
	for _, p := range pts {
		if p.HasGPS() {
			xs = append(xs, float64(p.Lo))
			ys = append(ys, float64(p.La))
		}
	}
	return xs, ys, len(xs) >= 3
}

// positionAt is where on the map a distance is, interpolated between the fixes
// either side.
func (l Lap) positionAt(pct float64) (x, y float64, ok bool) {
	var before, after *wire.TracePoint
	for i := range l.Trace {
		p := &l.Trace[i]
		if !p.HasGPS() {
			continue
		}
		if float64(p.DistPct) <= pct {
			before = p
		}
		if float64(p.DistPct) >= pct {
			after = p
			break
		}
	}
	switch {
	case before == nil && after == nil:
		return 0, 0, false
	case before == nil:
		return float64(after.Lo), float64(after.La), true
	case after == nil || after.DistPct == before.DistPct:
		return float64(before.Lo), float64(before.La), true
	}
	f := (pct - float64(before.DistPct)) / float64(after.DistPct-before.DistPct)
	return float64(before.Lo) + f*float64(after.Lo-before.Lo), float64(before.La) + f*float64(after.La-before.La), true
}

// lapTime says a lap time the way a timing screen does: 1:36.459.
func lapTime(ms int) string {
	if ms <= 0 {
		return "–"
	}
	m := ms / 60000
	s := float64(ms%60000) / 1000
	if m > 0 {
		return fmt.Sprintf("%d:%06.3f", m, s)
	}
	return fmt.Sprintf("%.3f", s)
}

// sectorTime says a sector: 28.412.
func sectorTime(ms float64) string { return fmt.Sprintf("%.3f", ms/1000) }

// niceStep is a grid step for a range: 1, 2, 5 × a power of ten, so that there
// are about the number of lines asked for.
func niceStep(span float64, lines int) float64 {
	if span <= 0 || lines <= 0 {
		return 1
	}
	raw := span / float64(lines)
	mag := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 5, 10} {
		if step := m * mag; step >= raw {
			return step
		}
	}
	return 10 * mag
}
