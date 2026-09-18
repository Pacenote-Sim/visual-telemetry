package visualtelemetry

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pacenote-sim/protocol/wire"
)

// A straight-line lap: 1000 m long, ten points, a constant 100 km/h — so every
// derived number can be worked out by hand.
func straightLap() Lap {
	pts := make([]wire.TracePoint, 0, 11)
	for i := range 11 {
		pts = append(pts, wire.TracePoint{OffsetMs: i * 3600, DistPct: i * 100, SpeedKmh: 100, La: 10, Lo: 100 + i})
	}
	return Lap{StintID: "s", Lap: 1, TrackLengthM: 1000, Sectors: []float64{0, 0.5}, Trace: pts}
}

func TestTheArithmeticBehindTheChart(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	l := straightLap()

	// Distance is metres when the length is known, thousandths otherwise.
	r.InDelta(500, l.distanceOf(500), 0.001)
	r.Equal("m", l.axisUnit())
	bare := l
	bare.TrackLengthM = 0
	r.InDelta(500, bare.distanceOf(500), 0.001)
	r.Equal("‰", bare.axisUnit())

	// Time at a distance is interpolated; past the last sample it is the end.
	r.InDelta(0, l.timeAt(0), 0.001)
	r.InDelta(1800, l.timeAt(50), 0.001)
	r.InDelta(18000, l.timeAt(500), 0.001)
	r.InDelta(36000, l.timeAt(1000), 0.001)
	r.InDelta(36000, l.timeAt(1500), 0.001, "past the last sample is the lap's end")
	r.Equal(36000, l.endMs(), "no lap time given is the last sample")
	l.LapMs = 36500
	r.Equal(36500, l.endMs())
	r.InDelta(36500, l.timeAt(1500), 0.001)

	// Sectors: the first from the start, the last to the end.
	times := l.sectorTimes()
	r.Len(times, 2)
	r.InDelta(18000, times[0], 0.001)
	r.InDelta(18500, times[1], 0.001)
	r.Nil(Lap{}.sectorTimes())

	kmh, at := l.maxSpeed()
	r.Equal(100, kmh)
	r.Equal(0, at)

	// A lap ten percent slower is behind by ten percent of the time at every
	// point.
	slow := straightLap()
	for i := range slow.Trace {
		slow.Trace[i].OffsetMs = slow.Trace[i].OffsetMs * 11 / 10
	}
	d := slow.deltaTo(straightLap())
	r.InDelta(0, d[0], 0.001)
	r.InDelta(1800, d[5], 0.001)
	r.InDelta(3600, d[10], 0.001)

	// The map: fixes, and where a distance is between them.
	xs, ys, ok := gpsOf(l.Trace)
	r.True(ok)
	r.Len(xs, 11)
	r.InDelta(10, ys[0], 0.001)
	x, y, ok := l.positionAt(250)
	r.True(ok)
	r.InDelta(102.5, x, 0.001)
	r.InDelta(10, y, 0.001)
	_, _, ok = gpsOf([]wire.TracePoint{{}, {}})
	r.False(ok, "a trace without a fix has no map")
	_, _, ok = Lap{Trace: []wire.TracePoint{{DistPct: 0}}}.positionAt(5)
	r.False(ok)
	x, _, ok = l.positionAt(2000)
	r.True(ok)
	r.InDelta(110, x, 0.001, "past the last fix is the last fix")

	// Times the way a timing screen writes them.
	r.Equal("1:36.459", lapTime(96459))
	r.Equal("42.100", lapTime(42100))
	r.Equal("–", lapTime(0))
	r.Equal("28.412", sectorTime(28412))

	// Grid steps are 1, 2, 5 × a power of ten.
	r.InDelta(50, niceStep(290, 8), 0.001)
	r.InDelta(500, niceStep(5000, 10), 0.001)
	r.InDelta(1, niceStep(0, 5), 0.001)
	r.InDelta(2, niceStep(9, 5), 0.001)
}

// A trace with a fix only some of the time, and points that repeat a distance,
// still draws.
func TestUnevenTracesStillDraw(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	l := straightLap()
	l.Trace[3].La, l.Trace[3].Lo = 0, 0
	l.Trace[4].DistPct = l.Trace[3].DistPct
	// Two samples at one distance: the first of them is where the car was.
	r.InDelta(float64(l.Trace[3].OffsetMs), l.timeAt(float64(l.Trace[3].DistPct)), 0.001)
	_, _, ok := l.positionAt(300)
	r.True(ok)
	r.NotEmpty(chartSVG([]Lap{l}, configOf(nil)))
}
