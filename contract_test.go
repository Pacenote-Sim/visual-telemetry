package visualtelemetry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pacenote-sim/plugin"
	"github.com/pacenote-sim/protocol/wire"
)

func uploadOf(l Lap) []byte {
	b, _ := json.Marshal(LapUpload{
		StintID: l.StintID, Lap: l.Lap, LapMs: l.LapMs, Session: plugin.SessionType(l.Session), Track: l.Track,
		Car: l.Car, TrackLengthM: l.TrackLengthM, Sectors: l.Sectors, Compound: l.Compound, Corners: l.Corners,
		Trace: l.Trace,
	})
	return b
}

func TestAnUploadIsReadLenientlyAndOrdered(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	// Unknown fields are ignored; points arrive in any order and leave in
	// distance order.
	up, err := parseLapUpload([]byte(`{"stint_id":"s-1","lap":4,"session":"race","new_field":1,
		"trace":[{"t":9000,"p":500,"v":200,"future":true},{"t":0,"p":0,"v":100},{"t":18000,"p":1000,"v":150}]}`))
	r.NoError(err)
	r.Equal([]int{0, 500, 1000}, []int{up.Trace[0].DistPct, up.Trace[1].DistPct, up.Trace[2].DistPct})
	r.Equal(plugin.SessionRace, up.Session)

	full, err := parseLapUpload(uploadOf(syntheticLap("s-2", 7, "mihai", 0)))
	r.NoError(err)
	r.Len(full.Trace, 1001)
	r.Equal([]float64{0, 0.33, 0.71}, full.Sectors)
	r.Len(full.Corners, 10)
}

func TestWhatCannotBeDrawnIsRefused(t *testing.T) {
	t.Parallel()

	two := `[{"t":0,"p":0},{"t":1000,"p":1000}]`
	for _, tc := range []struct{ name, body, says string }{
		{"not json", `{nope`, "not readable JSON"},
		{"no stint", `{"lap":1,"trace":` + two + `}`, "stint_id"},
		{"a stint with a slash in it", `{"stint_id":"a/b","lap":1,"trace":` + two + `}`, "identifier"},
		{"no lap", `{"stint_id":"s","trace":` + two + `}`, "lap must be"},
		{"one point", `{"stint_id":"s","lap":1,"trace":[{"t":0,"p":0}]}`, "at least two"},
		{"a point outside the lap", `{"stint_id":"s","lap":1,"trace":[{"t":0,"p":0},{"t":1,"p":1001}]}`, "outside the lap"},
		{"a point before the start", `{"stint_id":"s","lap":1,"trace":[{"t":-1,"p":0},{"t":1,"p":10}]}`, "before the lap"},
		{"sectors out of order", `{"stint_id":"s","lap":1,"sectors":[0,0.7,0.3],"trace":` + two + `}`, "ascending"},
		{"a corner with no number", `{"stint_id":"s","lap":1,"corners":[{"turn":0,"apex_pct":5}],"trace":` + two + `}`, "turn number"},
		{"a negative time", `{"stint_id":"s","lap":1,"lap_ms":-5,"trace":` + two + `}`, "negative"},
		{"a name that is not one", `{"stint_id":"s","lap":1,"compound":"` + strings.Repeat("x", 30) + `","trace":` + two + `}`, "longer than a name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := parseLapUpload([]byte(tc.body))
			require.ErrorIs(t, err, errBadLap)
			require.ErrorContains(t, err, tc.says)
		})
	}

	t.Run("too many points, too many sectors", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		pts := make([]wire.TracePoint, MaxTracePoints+1)
		for i := range pts {
			pts[i] = wire.TracePoint{OffsetMs: i, DistPct: i * 1000 / len(pts)}
		}
		b, _ := json.Marshal(LapUpload{StintID: "s", Lap: 1, Trace: pts})
		_, err := parseLapUpload(b)
		r.ErrorContains(err, "more than a lap may carry")
		sectors := make([]float64, MaxSectors+1)
		for i := range sectors {
			sectors[i] = float64(i) / float64(len(sectors))
		}
		b, _ = json.Marshal(LapUpload{StintID: "s", Lap: 1, Sectors: sectors, Trace: pts[:2]})
		_, err = parseLapUpload(b)
		r.ErrorContains(err, "more than a circuit has")
	})
}

func TestALapIsNamedByItsStintAndNumber(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	l := syntheticLap("7f0c2e1a", 7, "mihai", 0)
	r.Equal("7f0c2e1a/7", l.Ref())
	stint, n, ok := parseRef("7f0c2e1a/7")
	r.True(ok)
	r.Equal("7f0c2e1a", stint)
	r.Equal(7, n)
	for _, bad := range []string{"", "7f0c2e1a", "/7", "7f0c2e1a/", "7f0c2e1a/zero", "7f0c2e1a/0", "a b/1", strings.Repeat("x", 65) + "/1"} {
		_, _, ok := parseRef(bad)
		r.Falsef(ok, "%q was taken for a lap", bad)
	}

	who := plugin.Caller{DriverSlug: "mihai", DriverName: "Mihai"}
	up, err := parseLapUpload(uploadOf(l))
	r.NoError(err)
	kept := lapOf(up, who, l.CreatedAt)
	r.Equal("mihai", kept.DriverSlug)
	r.Equal("Mihai", kept.DriverName)
	r.Equal(l.Trace, kept.Trace)
}
