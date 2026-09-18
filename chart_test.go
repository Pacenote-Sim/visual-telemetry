package visualtelemetry

import (
	"bytes"
	"fmt"
	"image/png"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pacenote-sim/plugin"
)

// What is asserted about a picture is its bones, not its beauty: every part
// is present, in the colours asked for, at the size promised.

func TestTheChartHasEverythingOnIt(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	cfg := configOf(plugin.Values{SettingTitle: "quali analysis", SettingTeam: "Iberian GT", SettingAccent: "#ff8000"})
	l := syntheticLap("s-1", 7, "Mihai", 0)
	svg := string(chartSVG([]Lap{l}, cfg))

	r.True(strings.HasPrefix(svg, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1400 650"`), "a document scales to what shows it")
	root, _, _ := strings.Cut(svg, ">")
	r.NotContains(root, `width=`, "a fixed size would be cut in a narrower window")
	r.True(strings.HasSuffix(svg, "</svg>\n"))
	r.Contains(svg, ">QUALI ANALYSIS<", "the title is the operator's, in capitals")
	r.Contains(svg, ">IBERIAN GT<", "the team is a panel label: mono, capitals")
	r.Contains(svg, ">Circuito de Prueba<")
	r.Contains(svg, ">FERRARI 296 GT3 · QUALIFYING<")
	r.Contains(svg, `stroke="#FF8000"`, "the first lap is not in the team colour")
	r.Contains(svg, `fill="#0A0A0A"`, "the ground is not the panel's")
	r.Contains(svg, `fill="#0F0F0F"`, "the cards are not the panel's")
	for _, s := range []string{">S1<", ">S2<", ">S3<", ">T1<", ">T10<", ">KM/H<", ">RPM<", ">THR<", ">BRK<", ">5000 M<", `letter-spacing="1.5"`, "<polygon"} {
		r.Containsf(svg, s, "%s is not on the chart", s)
	}
	r.Contains(svg, ">Mihai<")
	r.Contains(svg, ">lap 7 · "+lapTime(l.LapMs)+"<")
	kmh, _ := l.maxSpeed()
	r.Contains(svg, fmt.Sprintf(">max %d km/h<", kmh))
	r.Contains(svg, ">SOFT<")
	r.Contains(svg, "S1 ")
	r.Contains(svg, `stroke-dasharray`, "the sector boundaries are not dashed")
	r.NotContains(svg, "ΔT", "a single lap has no delta axis")
	r.Equal(8, strings.Count(svg, "<polyline"), "speed, revs, throttle, brake; the map's ground and its three sectors")

	// A comparison adds the delta axis and a legend row per lap, and every
	// lap after the first in the next colour.
	two := syntheticLap("s-2", 3, "Ana", -6)
	cmp := string(chartSVG([]Lap{l, two}, cfg))
	r.Contains(cmp, ">ΔT S<")
	r.Contains(cmp, ">Ana<")
	r.Contains(cmp, `stroke="#7DD87D"`, "the second lap is not in the panel's green")
	r.Contains(cmp, ">+0.0<")

	// Anything from outside is escaped.
	l.Track = `<script>alert(1)</script>`
	r.NotContains(string(chartSVG([]Lap{l}, cfg)), "<script>")

	// A chart of nothing says so; more laps than can be read are cut.
	none := string(chartSVG(nil, cfg))
	r.Contains(none, "Nothing to draw")
	many := []Lap{l, two, l, two, l, two}
	r.Contains(string(chartSVG(many, cfg)), ">Ana<")
	r.Equal(MaxLapsOnAChart, strings.Count(string(chartSVG(many, cfg)), ">lap "))
}

// The axis is in thousandths when the client did not say how long the circuit
// is, and a lap without GPS has no map and no turn numbers.
func TestAChartWithoutALengthOrAMap(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	l := syntheticLap("s-1", 7, "Mihai", 0)
	l.TrackLengthM = 0
	for i := range l.Trace {
		l.Trace[i].La, l.Trace[i].Lo = 0, 0
	}
	l.Sectors = nil
	svg := string(chartSVG([]Lap{l}, configOf(nil)))
	r.Contains(svg, ">1000 ‰<")
	r.NotContains(svg, ">T1<")
	r.NotContains(svg, ">S1<")
	r.Contains(svg, ">TELEMETRY ANALYSIS<", "the default title")
}

func TestThePictureIsThePictureOfTheDocument(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	cfg := configOf(plugin.Values{SettingAccent: "#FF8000"})
	l := syntheticLap("s-1", 7, "Mihai", 0)
	pic, err := chartPNG([]Lap{l, syntheticLap("s-2", 3, "Ana", -6)}, cfg)
	r.NoError(err)
	img, err := png.Decode(bytes.NewReader(pic))
	r.NoError(err)
	r.Equal(1400, img.Bounds().Dx())
	r.Equal(650, img.Bounds().Dy())

	// The ground is dark, a panel is a shade lighter, and the speed line at
	// the first sample — 290 km/h, full throttle, at the start line — is the
	// team's colour where the first lap runs on top.
	bg := parseColour(colBg)
	c := img.At(5, 5)
	rr, gg, bb, _ := c.RGBA()
	r.EqualValues(bg.R, rr>>8)
	r.EqualValues(bg.G, gg>>8)
	r.EqualValues(bg.B, bb>>8)
	var orange int
	for x := 70; x < 1330; x++ {
		for y := 95; y < 380; y++ {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			if cr>>8 > 200 && cg>>8 > 90 && cg>>8 < 160 && cb>>8 < 60 {
				orange++
			}
		}
	}
	r.Greater(orange, 2000, "the first lap's line is not on the picture in the team colour")

	// The rasteriser paints text and shapes without a font file on disk.
	rc, err := newRaster(20, 20)
	r.NoError(err)
	rc.Text(2, 15, "S1", 10, "#FFFFFF", "start", true)
	rc.Text(10, 15, "S1", 10, "#FFFFFF", "middle", false)
	rc.Text(18, 15, "S1", 10, "not-a-colour", "end", false)
	rc.Circle(10, 10, 3, "#3DA5FF")
	rc.Line(0, 0, 20, 20, "#FFFFFF", 1, true)
	rc.Polyline([]float64{1}, []float64{1}, "#FFFFFF", 1)
	rc.Line(5, 5, 5, 5, "#FFFFFF", 1, true)
	_, err = rc.bytes()
	r.NoError(err)
	r.Equal(parseColour("#FFFFFF"), parseColour("nope"))
	r.Equal(parseColour("#FFFFFF"), parseColour("#GGGGGG"))
	num := func(v float64) string {
		var c svgCanvas
		c.num(v)
		return c.b.String()
	}
	r.Equal("12.5", num(12.5))
	r.Equal("12", num(12.04))
	r.Equal("0", num(-0.04), "a negative zero is a zero")
	r.Equal("-3.5", num(-3.5))
	r.Equal("1000000", num(1e6), "never in exponent form")
}

func TestTheDeclaredSettingsAreValid(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	declared, err := New(nil, nil).Settings(t.Context())
	r.NoError(err)
	r.NoError(plugin.ValidateSettings(declared))
	values, err := plugin.ValidateValues(declared, plugin.Values{})
	r.NoError(err, "nothing is required: the chart has defaults")
	cfg := configOf(values)
	r.Equal(DefaultTitle, cfg.title)
	r.Equal(DefaultAccent, cfg.accent)
	r.Equal(DefaultKeepDays, cfg.keepDays)
	r.Empty(cfg.team)

	// A colour that is not one is the default; one that is, is kept, upper case.
	r.Equal(DefaultAccent, configOf(plugin.Values{SettingAccent: "orange"}).accent)
	r.Equal("#3DA5FF", configOf(plugin.Values{SettingAccent: " #3da5ff "}).accent)
	r.Equal(12, configOf(plugin.Values{SettingKeepDays: "12"}).keepDays)

	use, err := New(nil, nil).Notify(t.Context(), plugin.Event{Kind: plugin.EventLapCompleted})
	r.NoError(err)
	r.Zero(use.Total())
	v := New(nil, nil)
	v.now = nil
	r.False(v.clock().IsZero())
}
