package visualtelemetry

import (
	"fmt"
	"math"
	"strings"

	"github.com/pacenote-sim/protocol/wire"
)

// The chart, the way a broadcast analyst draws a lap: four panels sharing a
// distance axis — speed, revs, throttle, brake — the sectors marked through all
// of them, the circuit in the corner with its turns numbered, and a legend
// with what each lap was. One lap, or several overlaid: the first lap sets the
// axis and the colour scheme, and every other is drawn against it, with how
// far behind it was on the right-hand axis.

// The picture's size, and where things are in it.
const (
	chartW = 1400.0
	chartH = 650.0
	left   = 70.0
	right  = 1330.0
)

// The palette is the server panel's own — its ground, its cards, its border,
// its white, its muted greys — so a chart on the operator's server looks like
// the server drew it. The lines carry the colour.
const (
	colBg    = "#0A0A0A"
	colPanel = "#0F0F0F"
	colGrid  = "#1F1F1F"
	colAxis  = "#6A6A6A"
	colText  = "#FFFFFF"
	colMuted = "#A0A0A0"
)

// lapColours after the team's own: the panel's good and bad hues, then blue.
// Four laps on one chart is as many as anyone can read.
var lapColours = []string{"#7DD87D", "#FF6B6B", "#3DA5FF"}

// sectorColours mark the circuit map and the sector labels, in the panel's
// hues first.
var sectorColours = []string{"#7DD87D", "#FFD23D", "#FF6B6B", "#3DA5FF", "#FF8AD6", "#FFFFFF", "#FFA64D", "#8AFFEA"}

// chamfer is the cut the panel puts on a card's top-left and bottom-right
// corners.
const chamfer = 12.0

// MaxLapsOnAChart is how many laps may be overlaid.
const MaxLapsOnAChart = 4

// panel is one horizontal band of the chart.
type panel struct{ y0, y1 float64 }

var (
	pSpeed = panel{92, 372}
	pRPM   = panel{380, 418}
	pThr   = panel{426, 514}
	pBrk   = panel{522, 560}
)

// colourOf is the colour the i-th lap on a chart is drawn in.
func colourOf(i int, accent string) string {
	if i == 0 {
		return accent
	}
	return lapColours[(i-1)%len(lapColours)]
}

// xOf is where a distance round the lap falls on the axis.
func xOf(pct float64) float64 { return left + (right-left)*pct/1000 }

// render draws laps on a canvas. The first lap is the one the chart is about.
func render(c canvas, laps []Lap, cfg config) {
	c.Rect(0, 0, chartW, chartH, colBg)
	if len(laps) > MaxLapsOnAChart {
		laps = laps[:MaxLapsOnAChart]
	}
	drawHeader(c, laps, cfg)
	if len(laps) == 0 {
		c.Text(chartW/2, chartH/2, "Nothing to draw: no lap was given.", 20, colMuted, "middle", false)
		return
	}
	base := laps[0]

	for _, p := range []panel{pSpeed, pRPM, pThr, pBrk} {
		card(c, left, p.y0, right, p.y1)
	}
	drawDistanceAxis(c, base)
	drawSectors(c, base)

	// Speed, with the grid it is read against.
	var top float64
	for i := range laps {
		kmh, _ := laps[i].maxSpeed()
		top = math.Max(top, float64(kmh))
	}
	step := niceStep(top, 8)
	speedMax := math.Ceil(top*1.04/step) * step
	if speedMax <= 0 {
		speedMax = step
	}
	for v := 0.0; v <= speedMax; v += step {
		y := yOf(pSpeed, v, 0, speedMax)
		c.Line(left, y, right, y, colGrid, 1, false)
		c.Label(left-8, y+4, fmt.Sprintf("%.0f", v), 10, colAxis, "end")
	}
	c.Label(22, (pSpeed.y0+pSpeed.y1)/2, "km/h", 9, colAxis, "middle")

	// Revs, throttle, brake: their own scales.
	var rpmMax float64
	for i := range laps {
		for _, p := range laps[i].Trace {
			rpmMax = math.Max(rpmMax, float64(p.RPM))
		}
	}
	rpmStep := niceStep(rpmMax, 3)
	rpmTop := math.Ceil(rpmMax/rpmStep) * rpmStep
	if rpmTop > 0 {
		for v := rpmStep; v <= rpmTop; v += rpmStep {
			y := yOf(pRPM, v, 0, rpmTop)
			c.Line(left, y, right, y, colGrid, 1, false)
			// The top line's label would sit on the speed panel's last one.
			if v < rpmTop {
				c.Label(right+8, y+4, fmt.Sprintf("%.0f", v), 9, colAxis, "start")
			}
		}
		c.Label(chartW-22, (pRPM.y0+pRPM.y1)/2, "rpm", 9, colAxis, "middle")
	}
	for _, v := range []float64{0, 0.5, 1} {
		y := yOf(pThr, v, 0, 1)
		c.Line(left, y, right, y, colGrid, 1, false)
		c.Label(left-8, y+4, fmt.Sprintf("%.1f", v), 9, colAxis, "end")
	}
	c.Label(22, (pThr.y0+pThr.y1)/2, "thr", 9, colAxis, "middle")
	for _, v := range []float64{0, 1} {
		y := yOf(pBrk, v, 0, 1)
		c.Line(left, y, right, y, colGrid, 1, false)
		c.Label(right+8, y+4, fmt.Sprintf("%.1f", v), 9, colAxis, "start")
	}
	c.Label(chartW-22, (pBrk.y0+pBrk.y1)/2, "brk", 9, colAxis, "middle")

	// The other laps' time against the first, on the speed panel's right axis.
	if len(laps) > 1 {
		drawDelta(c, laps, cfg.accent)
	}

	// The lines themselves, the first lap last so it is on top.
	for i := len(laps) - 1; i >= 0; i-- {
		l, colour := laps[i], colourOf(i, cfg.accent)
		width := 1.6
		if i == 0 {
			width = 2.2
		}
		drawSeries(c, l, colour, width, pSpeed, speedMax, func(p wire.TracePoint) float64 { return float64(p.SpeedKmh) })
		if rpmTop > 0 {
			drawSeries(c, l, colour, width*0.8, pRPM, rpmTop, func(p wire.TracePoint) float64 { return float64(p.RPM) })
		}
		drawSeries(c, l, colour, width, pThr, 1, func(p wire.TracePoint) float64 { return float64(p.Throttle) / 100 })
		drawSeries(c, l, colour, width, pBrk, 1, func(p wire.TracePoint) float64 { return float64(p.Brake) / 100 })
	}

	drawMap(c, base)
	drawLegend(c, laps, cfg.accent)
}

// card fills a panel the way the server draws a card: chamfered at the
// top-left and bottom-right.
func card(c canvas, x0, y0, x1, y1 float64) {
	cut := math.Min(chamfer, (y1-y0)/3)
	c.Polygon(
		[]float64{x0 + cut, x1, x1, x1 - cut, x0, x0},
		[]float64{y0, y0, y1 - cut, y1, y1, y0 + cut},
		colPanel)
}

// yOf is where a value falls in a panel.
func yOf(p panel, v, lo, hi float64) float64 {
	if hi <= lo {
		return p.y1
	}
	f := (v - lo) / (hi - lo)
	f = math.Max(0, math.Min(1, f))
	return p.y1 - f*(p.y1-p.y0)
}

// drawSeries draws one channel of one lap into a panel.
func drawSeries(c canvas, l Lap, colour string, width float64, p panel, hi float64, value func(wire.TracePoint) float64) {
	xs := make([]float64, 0, len(l.Trace))
	ys := make([]float64, 0, len(l.Trace))
	for _, pt := range l.Trace {
		xs = append(xs, xOf(float64(pt.DistPct)))
		ys = append(ys, yOf(p, value(pt), 0, hi))
	}
	c.Polyline(xs, ys, colour, width)
}

// drawDistanceAxis labels the bottom in metres, or thousandths when the
// circuit's length is not known.
func drawDistanceAxis(c canvas, base Lap) {
	total := base.distanceOf(1000)
	step := niceStep(total, 10)
	for v := 0.0; v <= total+1e-9; v += step {
		x := xOf(v / total * 1000)
		c.Line(x, pSpeed.y0, x, pBrk.y1, colGrid, 1, false)
		label := fmt.Sprintf("%.0f", v)
		if v+step > total {
			label += " " + base.axisUnit()
		}
		c.Label(x, pBrk.y1+16, label, 9, colAxis, "middle")
	}
}

// drawSectors marks the boundaries through every panel and names the sectors
// between them, in the colours the map uses for them.
func drawSectors(c canvas, base Lap) {
	if len(base.Sectors) == 0 {
		return
	}
	bounds := append([]float64{}, base.Sectors...)
	if bounds[0] != 0 {
		bounds = append([]float64{0}, bounds...)
	}
	for i, b := range bounds {
		if b > 0 {
			x := xOf(b * 1000)
			c.Line(x, pSpeed.y0, x, pBrk.y1, colAxis, 1, true)
		}
		end := 1.0
		if i+1 < len(bounds) {
			end = bounds[i+1]
		}
		mid := xOf((b + end) / 2 * 1000)
		c.Label(mid, pSpeed.y0+16, fmt.Sprintf("S%d", i+1), 11, sectorColours[i%len(sectorColours)], "middle")
	}
}

// drawDelta draws, for every lap after the first, how far behind the first it
// was at each distance — the line an analyst reads before anything else.
func drawDelta(c canvas, laps []Lap, accent string) {
	base := laps[0]
	var most float64
	deltas := make([][]float64, len(laps))
	for i := 1; i < len(laps); i++ {
		deltas[i] = laps[i].deltaTo(base)
		for _, d := range deltas[i] {
			most = math.Max(most, math.Abs(d))
		}
	}
	most = math.Max(500, niceStep(most, 2)*math.Ceil(most/niceStep(most, 2)))
	zero := yOf(pSpeed, 0, -most, most)
	c.Line(left, zero, right, zero, colAxis, 1, true)
	for _, v := range []float64{-most, -most / 2, 0, most / 2, most} {
		y := yOf(pSpeed, v, -most, most)
		c.Label(right+8, y+4, fmt.Sprintf("%+.1f", v/1000), 9, colAxis, "start")
	}
	c.Label(chartW-22, (pSpeed.y0+pSpeed.y1)/2, "Δt s", 9, colAxis, "middle")
	for i := 1; i < len(laps); i++ {
		l := laps[i]
		xs := make([]float64, 0, len(l.Trace))
		ys := make([]float64, 0, len(l.Trace))
		for j, pt := range l.Trace {
			xs = append(xs, xOf(float64(pt.DistPct)))
			ys = append(ys, yOf(pSpeed, deltas[i][j], -most, most))
		}
		c.Polyline(xs, ys, colourOf(i, accent), 1.2)
	}
}

// The map in the corner: the circuit from GPS, one colour per sector, the
// turns numbered where their apexes are.
const (
	mapX0, mapY0 = 620.0, 4.0
	mapW, mapH   = 300.0, 86.0
)

func drawMap(c canvas, base Lap) {
	xs, ys, ok := gpsOf(base.Trace)
	if !ok {
		return
	}
	minX, maxX, minY, maxY := xs[0], xs[0], ys[0], ys[0]
	for i := range xs {
		minX, maxX = math.Min(minX, xs[i]), math.Max(maxX, xs[i])
		minY, maxY = math.Min(minY, ys[i]), math.Max(maxY, ys[i])
	}
	spanX, spanY := maxX-minX, maxY-minY
	if spanX == 0 || spanY == 0 {
		return
	}
	scale := math.Min((mapW-20)/spanX, (mapH-20)/spanY)
	project := func(x, y float64) (float64, float64) {
		// North up: the map's y grows downward, the world's upward.
		px := mapX0 + (mapW-spanX*scale)/2 + (x-minX)*scale
		py := mapY0 + (mapH-spanY*scale)/2 + (maxY-y)*scale
		return px, py
	}

	// One polyline per sector, in the sector's colour; the whole lap in grey
	// underneath so a gap between fixes is still a circuit.
	all := make([][2]float64, 0, len(base.Trace))
	for _, p := range base.Trace {
		if p.HasGPS() {
			x, y := project(float64(p.Lo), float64(p.La))
			all = append(all, [2]float64{x, y})
		}
	}
	c.Polyline(column(all, 0), column(all, 1), colGrid, 4)
	bounds := append([]float64{}, base.Sectors...)
	if len(bounds) == 0 || bounds[0] != 0 {
		bounds = append([]float64{0}, bounds...)
	}
	for i, b := range bounds {
		end := 1.0
		if i+1 < len(bounds) {
			end = bounds[i+1]
		}
		var seg [][2]float64
		for _, p := range base.Trace {
			pct := float64(p.DistPct) / 1000
			if !p.HasGPS() || pct < b || pct > end {
				continue
			}
			x, y := project(float64(p.Lo), float64(p.La))
			seg = append(seg, [2]float64{x, y})
		}
		c.Polyline(column(seg, 0), column(seg, 1), sectorColours[i%len(sectorColours)], 2.5)
	}

	// Start/finish, and the turns.
	if x, y, ok := base.positionAt(0); ok {
		px, py := project(x, y)
		c.Circle(px, py, 3, colText)
	}
	for _, corner := range base.Corners {
		x, y, ok := base.positionAt(float64(corner.ApexPct))
		if !ok {
			continue
		}
		px, py := project(x, y)
		c.Circle(px, py, 2, colMuted)
		c.Label(px+4, py-4, fmt.Sprintf("T%d", corner.Turn), 8, colMuted, "start")
	}
}

// column is one coordinate of a list of points.
func column(pts [][2]float64, i int) []float64 {
	out := make([]float64, len(pts))
	for j, p := range pts {
		out[j] = p[i]
	}
	return out
}

// drawHeader is the title, the team, and where the lap was.
func drawHeader(c canvas, laps []Lap, cfg config) {
	c.Text(40, 58, strings.ToUpper(cfg.title), 34, colText, "start", true)
	if cfg.team != "" {
		c.Label(40, 82, cfg.team, 10, colMuted, "start")
	}
	if len(laps) == 0 {
		return
	}
	base := laps[0]
	where := plainOr(base.Track, "an unnamed circuit")
	c.Text(right, 58, where, 24, colText, "end", false)
	var sub []string
	if base.Car != "" {
		sub = append(sub, base.Car)
	}
	if base.Session != "" {
		sub = append(sub, base.Session)
	}
	if len(sub) > 0 {
		c.Label(right, 82, strings.Join(sub, " · "), 10, colMuted, "end")
	}
}

// drawLegend is one row per lap: colour, driver, lap time, top speed, tyre,
// and the sectors.
func drawLegend(c canvas, laps []Lap, accent string) {
	// Four rows fit under the axis, one per lap a chart may carry.
	y := 594.0
	for i := range laps {
		l := &laps[i]
		colour := colourOf(i, accent)
		c.Rect(40, y-10, 10, 12, colour)
		c.Text(58, y, plainOr(l.DriverName, l.DriverSlug), 13, colText, "start", true)
		c.Text(260, y, "lap "+fmt.Sprint(l.Lap)+" · "+lapTime(l.endMs()), 12, colText, "start", false)
		kmh, _ := l.maxSpeed()
		c.Text(470, y, fmt.Sprintf("max %d km/h", kmh), 12, colText, "start", false)
		if l.Compound != "" {
			c.Label(620, y, l.Compound, 10, colMuted, "start")
		}
		if times := l.sectorTimes(); len(times) > 0 {
			parts := make([]string, 0, len(times))
			for j, t := range times {
				parts = append(parts, fmt.Sprintf("S%d %s", j+1, sectorTime(t)))
			}
			c.Label(right, y, strings.Join(parts, "  "), 9, colMuted, "end")
		}
		y += 15
	}
}

// plainOr is a value or a word for not having one.
func plainOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// chartSVG is the chart as a document.
func chartSVG(laps []Lap, cfg config) []byte {
	c := newSVG(chartW, chartH)
	render(c, laps, cfg)
	return c.bytes()
}

// chartPNG is the chart as a picture.
func chartPNG(laps []Lap, cfg config) ([]byte, error) {
	c, err := newRaster(int(chartW), int(chartH))
	if err != nil {
		return nil, err
	}
	render(c, laps, cfg)
	return c.bytes()
}
