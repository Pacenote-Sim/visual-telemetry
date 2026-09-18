package visualtelemetry

import (
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/pacenote-sim/protocol/wire"
)

// syntheticLap is a plausible lap of a made-up 5 km circuit, driven by a small
// model of a GT car rather than drawn by hand: the speed between corners is
// what braking and power allow, gears saw the revs, the brake spikes and trails
// off into the apex, and the circuit is a smooth loop whose corners are where
// its bends are. offset shifts the pace, so two laps differ the way two
// drivers do.
func syntheticLap(stint string, n int, driver string, offsetKmh float64) Lap {
	pts, corners, length := modelLap(offsetKmh)
	return Lap{
		StintID: stint, Lap: n, DriverSlug: driver, DriverName: driver, Session: "qualifying",
		Track: "Circuito de Prueba", Car: "Ferrari 296 GT3", TrackLengthM: length, LapMs: pts[len(pts)-1].OffsetMs,
		Compound: "SOFT", Sectors: []float64{0, 0.33, 0.71}, Corners: corners, Trace: pts,
		CreatedAt: time.Date(2026, 9, 18, 15, 0, 0, 0, time.UTC),
	}
}

// The circuit: waypoints of a loop, and which of them are corners with the
// speed a GT3 carries through each.
var (
	waypoints = [][2]float64{
		{0, 0},
		{900, -30},
		{1500, 80},
		{1900, 350},
		{1750, 650},
		{1300, 720},
		{1100, 950},
		{700, 1000},
		{350, 850},
		{450, 560},
		{250, 330},
		{60, 150},
	}
	cornerAt = map[int]float64{2: 175, 3: 95, 4: 130, 5: 70, 6: 150, 7: 60, 8: 110, 9: 85, 10: 145, 11: 120}
)

// modelLap drives the circuit at five-metre steps.
func modelLap(offsetKmh float64) ([]wire.TracePoint, []cornerMark, int) {
	const (
		ds     = 5.0
		vTop   = 292.0
		brake  = 13.0 // m/s², what the tyres give
		length = 5000
	)
	path, atWaypoint := circuit(length, ds)
	steps := len(path) - 1

	// What each corner allows over its arc, how hard a driver brakes for it
	// (all of it for a hairpin, a dab for a fast sweep), and how much throttle
	// holds the car through it.
	limit := make([]float64, steps+1)
	brakeFor := make([]float64, steps+1) // fraction of the tyres' braking used ahead of here
	holdThr := make([]float64, steps+1)  // throttle held through the corner here
	for i := range limit {
		limit[i] = (vTop + offsetKmh) / 3.6
		brakeFor[i] = 1
	}
	type cornerSpot struct{ s, vmin float64 }
	var spots []cornerSpot
	var marks []cornerMark
	turn := 0
	for w, s := range atWaypoint {
		vmin, isCorner := cornerAt[w]
		if !isCorner {
			continue
		}
		turn++
		marks = append(marks, cornerMark{Turn: turn, ApexPct: int(s / length * 1000)})
		spots = append(spots, cornerSpot{s, vmin})
		v := (vmin + offsetKmh) / 3.6
		half := 20 + vmin/4 // a fast corner is a long arc
		for i := range limit {
			if d := math.Abs(float64(i)*ds - s); d < half {
				limit[i] = math.Min(limit[i], v+(d/half)*(d/half)*8)
				holdThr[i] = 18 + vmin/2.6 // a hairpin on a quarter throttle, a sweep on most of it
			}
		}
	}
	// Braking for the next corner uses as much of the tyres as that corner asks.
	for i := range brakeFor {
		next, best := 1.0, math.Inf(1)
		for _, c := range spots {
			if d := c.s - float64(i)*ds; d >= -5 && d < best {
				best, next = d, math.Max(0.5, math.Min(1, 1.15-c.vmin/300))
			}
		}
		brakeFor[i] = next
	}
	// Forward: what power allows; backward: what braking allows.
	accel := func(v float64) float64 {
		return math.Max(0.4, math.Min(8.5, 12*math.Pow(1-v/((vTop+offsetKmh)/3.6+1), 1.3)))
	}
	fwd := make([]float64, steps+1)
	fwd[0] = math.Min(limit[0], 78)
	for i := 1; i <= steps; i++ {
		fwd[i] = math.Min(limit[i], math.Sqrt(fwd[i-1]*fwd[i-1]+2*accel(fwd[i-1])*ds))
	}
	bwd := make([]float64, steps+1)
	bwd[steps] = fwd[steps]
	for i := steps - 1; i >= 0; i-- {
		bwd[i] = math.Min(limit[i], math.Sqrt(bwd[i+1]*bwd[i+1]+2*brake*brakeFor[i]*ds))
	}
	v := make([]float64, steps+1)
	for i := range v {
		v[i] = math.Min(fwd[i], bwd[i])
	}
	// A car has mass and a driver has feet: round the kinks where power, brakes
	// and corner meet, so the deceleration builds and trails off instead of
	// switching. Three passes of a short moving average, about 35 m wide.
	for range 3 {
		sm := make([]float64, steps+1)
		for i := range v {
			sum, n := 0.0, 0.0
			for k := -3; k <= 3; k++ {
				if j := i + k; j >= 0 && j <= steps {
					sum, n = sum+v[j], n+1
				}
			}
			sm[i] = sum / n
		}
		v = sm
	}

	gears := []float64{0, 75, 120, 165, 210, 255, 310}
	pts := make([]wire.TracePoint, 0, steps+1)
	t := 0.0
	// The pedals follow the demand at a foot's pace: the throttle is squeezed
	// on and comes off quickly, the brake goes on quickly and bleeds off into
	// the apex. Percent per second.
	const (
		throttleOn, throttleOff = 60.0, 250.0 // squeezed on over a second and a half, lifted in half of one
		brakeOn, brakeOff       = 280.0, 90.0 // hit in a third of a second, trailed off over one
		pedalLag                = 0.18        // seconds; the travel of the pedal itself
	)
	thrNow, brkNow := 100.0, 0.0
	thrPed, brkPed := 100.0, 0.0
	// Sensor and driver texture: a bounded random walk on each channel, the
	// same lap every time.
	rng := rand.New(rand.NewPCG(7, 11))
	wobble := func(w *float64, amp float64) float64 {
		*w = math.Max(-amp, math.Min(amp, *w+(rng.Float64()-0.5)*amp*0.6))
		return *w
	}
	var wv, wthr, wbrk, wrpm float64
	gear := 6
	for i := 0; i <= steps; i++ {
		dt := 0.0
		if i > 0 {
			dt = ds / ((v[i-1] + v[i]) / 2)
			t += dt
		}
		kmh := v[i] * 3.6
		next := v[int(math.Min(float64(i+1), float64(steps)))]
		dv := (next*next - v[i]*v[i]) / (2 * ds) // m/s² along the road
		var wantThr, wantBrk float64
		switch {
		case dv < -0.3:
			// As much pedal as the deceleration takes; it trails off with it.
			wantBrk = math.Min(100, dv/-brake*105)
		case dv >= 0 && v[i] < limit[i]-0.3, kmh > vTop+offsetKmh-3:
			// Flat out: gaining speed on a straight, or holding the top of it.
			wantThr = 100
		default:
			// Holding a corner's speed on the throttle it takes.
			wantThr = holdThr[i]
		}
		// The foot moves at its pace, then the pedal's travel rounds it off.
		thrNow = towards(thrNow, wantThr, throttleOn*dt, throttleOff*dt)
		brkNow = towards(brkNow, wantBrk, brakeOn*dt, brakeOff*dt)
		a := dt / (pedalLag + dt)
		thrPed += a * (thrNow - thrPed)
		brkPed += a * (brkNow - brkPed)
		thr := int(math.Round(math.Max(0, math.Min(100, thrPed+wobble(&wthr, 3)))))
		brk := int(math.Round(brkPed))
		if brk > 0 {
			brk = int(math.Round(math.Max(1, math.Min(100, float64(brk)+wobble(&wbrk, 4)))))
		}
		// The gearbox: up a little past the ratio, down a little under it, so a
		// speed hovering at a threshold does not flap the gear.
		switch {
		case gear < 6 && kmh > gears[gear]+4:
			gear++
		case gear > 1 && kmh < gears[gear-1]-6:
			gear--
		}
		rpm := 4200 + int((kmh-gears[gear-1])/(gears[gear]-gears[gear-1])*3600+wobble(&wrpm, 60))
		kmh += wobble(&wv, 1)
		x, y := path[i][0], path[i][1]
		pts = append(pts, wire.TracePoint{
			OffsetMs: int(t * 1000), SpeedKmh: int(kmh), Throttle: thr, Brake: brk, Gear: gear, RPM: rpm,
			DistPct: int(math.Round(float64(i) * ds / length * 1000)),
			La:      int(math.Round(50000 + y)), Lo: int(math.Round(100000 + x)),
		})
	}
	return pts, marks, length
}

// towards moves a pedal from where it is to where the driver wants it, no
// faster than a foot moves in the time given.
func towards(now, want, up, down float64) float64 {
	switch {
	case want > now:
		return math.Min(want, now+up)
	case want < now:
		return math.Max(want, now-down)
	}
	return now
}

// circuit is the loop through the waypoints, smoothed, resampled every ds
// metres for a lap of the given length, and where along it each waypoint is.
func circuit(length, ds float64) (path [][2]float64, atWaypoint map[int]float64) {
	n := len(waypoints)
	at := func(i int) [2]float64 { return waypoints[((i%n)+n)%n] }
	// Fine samples along Catmull-Rom segments, with their cumulative length.
	fine := make([][2]float64, 0, 40*n+1)
	cum := make([]float64, 0, 40*n+1)
	wpAt := make([]float64, n)
	total := 0.0
	for seg := range n {
		p0, p1, p2, p3 := at(seg-1), at(seg), at(seg+1), at(seg+2)
		wpAt[seg] = total
		for k := range 40 {
			u := float64(k) / 40
			x := 0.5 * ((2 * p1[0]) + (-p0[0]+p2[0])*u + (2*p0[0]-5*p1[0]+4*p2[0]-p3[0])*u*u + (-p0[0]+3*p1[0]-3*p2[0]+p3[0])*u*u*u)
			y := 0.5 * ((2 * p1[1]) + (-p0[1]+p2[1])*u + (2*p0[1]-5*p1[1]+4*p2[1]-p3[1])*u*u + (-p0[1]+3*p1[1]-3*p2[1]+p3[1])*u*u*u)
			if len(fine) > 0 {
				total += math.Hypot(x-fine[len(fine)-1][0], y-fine[len(fine)-1][1])
			}
			fine = append(fine, [2]float64{x, y})
			cum = append(cum, total)
		}
	}
	fine = append(fine, fine[0])
	total += math.Hypot(fine[0][0]-fine[len(fine)-2][0], fine[0][1]-fine[len(fine)-2][1])
	cum = append(cum, total)

	// Resample to the lap's length, so the loop's own size does not matter.
	scale := length / total
	steps := int(length / ds)
	path = make([][2]float64, 0, steps+1)
	j := 0
	for i := 0; i <= steps; i++ {
		want := float64(i) * ds / scale
		for j < len(cum)-2 && cum[j+1] < want {
			j++
		}
		f := 0.0
		if cum[j+1] > cum[j] {
			f = (want - cum[j]) / (cum[j+1] - cum[j])
		}
		path = append(path, [2]float64{fine[j][0] + f*(fine[j+1][0]-fine[j][0]), fine[j][1] + f*(fine[j+1][1]-fine[j][1])})
	}
	atWaypoint = make(map[int]float64, n)
	for w, s := range wpAt {
		atWaypoint[w] = s * scale
	}
	return path, atWaypoint
}

// TestWriteSampleChart draws the fixture to a directory for a person to look
// at. It runs only when asked, because a picture is not an assertion.
func TestWriteSampleChart(t *testing.T) {
	t.Parallel()
	dir := os.Getenv("VT_SAMPLE_DIR")
	if dir == "" {
		t.Skip("set VT_SAMPLE_DIR to write the sample charts")
	}
	r := require.New(t)
	cfg := configOf(nil)
	cfg.team = "Iberian GT"
	one := syntheticLap("stint-a", 7, "Mihai", 0)
	two := syntheticLap("stint-b", 3, "Ana", -6)

	r.NoError(os.WriteFile(filepath.Join(dir, "lap.svg"), chartSVG([]Lap{one}, cfg), 0o644))
	pic, err := chartPNG([]Lap{one}, cfg)
	r.NoError(err)
	r.NoError(os.WriteFile(filepath.Join(dir, "lap.png"), pic, 0o644))
	pic, err = chartPNG([]Lap{one, two}, cfg)
	r.NoError(err)
	r.NoError(os.WriteFile(filepath.Join(dir, "compare.png"), pic, 0o644))
}
