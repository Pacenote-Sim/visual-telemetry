package visualtelemetry

import "testing"

// What a lap costs to draw. A chart is made when somebody opens it, so this is
// a page's latency; the picture is the expensive one and it should stay under
// a tenth of a second on ordinary hardware.

func BenchmarkDrawingADocument(b *testing.B) {
	laps := []Lap{syntheticLap("s-1", 7, "Mihai", 0), syntheticLap("s-2", 3, "Ana", -6)}
	cfg := configOf(nil)
	b.ReportAllocs()
	for b.Loop() {
		_ = chartSVG(laps, cfg)
	}
}

func BenchmarkDrawingAPicture(b *testing.B) {
	laps := []Lap{syntheticLap("s-1", 7, "Mihai", 0), syntheticLap("s-2", 3, "Ana", -6)}
	cfg := configOf(nil)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := chartPNG(laps, cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkReadingAnUpload(b *testing.B) {
	body := uploadOf(syntheticLap("s-1", 7, "Mihai", 0))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parseLapUpload(body); err != nil {
			b.Fatal(err)
		}
	}
}
