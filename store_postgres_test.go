//go:build postgres

package visualtelemetry

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/pacenote-sim/plugin"
)

const EnvURL = "PACENOTE_TEST_DATABASE_URL"

func plugged(t *testing.T) *Store {
	t.Helper()
	r := require.New(t)
	dsn := os.Getenv(EnvURL)
	if dsn == "" {
		t.Skipf("%s is not set, so there is nothing to run this against", EnvURL)
	}
	ctx := context.Background()
	var b [6]byte
	_, err := rand.Read(b[:])
	r.NoError(err)
	schema := "plugin_vt_test_" + hex.EncodeToString(b[:])
	adminConn, err := pgx.Connect(ctx, dsn)
	r.NoError(err)
	defer func() { _ = adminConn.Close(ctx) }()
	_, err = adminConn.Exec(ctx, `CREATE SCHEMA `+schema)
	r.NoError(err)
	t.Cleanup(func() {
		c, connErr := pgx.Connect(context.Background(), dsn)
		if connErr != nil {
			return
		}
		defer func() { _ = c.Close(context.Background()) }()
		_, _ = c.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
	})
	files, err := filepath.Glob("migrations/*.sql")
	r.NoError(err)
	r.NotEmpty(files)
	sort.Strings(files)
	for _, f := range files {
		migration, readErr := os.ReadFile(f)
		r.NoError(readErr)
		_, err = adminConn.Exec(ctx, `SET search_path = `+schema+`;`+string(migration))
		r.NoErrorf(err, "%s would not apply", f)
	}
	u, err := url.Parse(dsn)
	r.NoError(err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	store, err := Open(ctx, u.String())
	r.NoError(err)
	t.Cleanup(store.Close)
	return store
}

func TestALapIsKeptWholeAndReadBack(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	store := plugged(t)
	ctx := context.Background()

	_, err := store.Get(ctx, "s-1", 7)
	r.ErrorIs(err, ErrNoRows)

	l := syntheticLap("s-1", 7, "mihai", 0)
	r.NoError(store.Put(ctx, l))
	got, err := store.Get(ctx, "s-1", 7)
	r.NoError(err)
	r.Equal(l.Trace, got.Trace, "the trace did not survive the round trip")
	r.Equal(l.Sectors, got.Sectors)
	r.Equal(l.Corners, got.Corners)
	r.Equal("SOFT", got.Compound)
	r.Equal(5000, got.TrackLengthM)
	r.Equal("Circuito de Prueba", got.Track)

	// The same lap again replaces it.
	again := l
	again.Compound = "MEDIUM"
	r.NoError(store.Put(ctx, again))
	got, err = store.Get(ctx, "s-1", 7)
	r.NoError(err)
	r.Equal("MEDIUM", got.Compound)

	// Lists carry no trace, newest first, the driver's own or everyone's.
	other := syntheticLap("s-2", 3, "ana", -6)
	other.CreatedAt = l.CreatedAt.Add(time.Hour)
	r.NoError(store.Put(ctx, other))
	mine, err := store.Recent(ctx, "mihai", 10)
	r.NoError(err)
	r.Len(mine, 1)
	r.Empty(mine[0].Trace)
	all, err := store.RecentAll(ctx, 0)
	r.NoError(err)
	r.Len(all, 2)
	r.Equal("ana", all[0].DriverSlug, "the newest is not first")

	// Retention by age.
	old := syntheticLap("s-old", 1, "mihai", 0)
	old.CreatedAt = time.Now().Add(-100 * 24 * time.Hour)
	r.NoError(store.Put(ctx, old))
	n, err := store.Prune(ctx, 0)
	r.NoError(err)
	r.Zero(n)
	n, err = store.Prune(ctx, DefaultKeepDays)
	r.NoError(err)
	r.EqualValues(1, n)
}

func TestAStoreThatIsNotThere(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	ctx := context.Background()
	var none *Store
	r.Error(none.Put(ctx, syntheticLap("s", 1, "m", 0)))
	_, err := none.Get(ctx, "s", 1)
	r.Error(err)
	_, err = none.Recent(ctx, "m", 5)
	r.Error(err)
	_, err = none.RecentAll(ctx, 5)
	r.Error(err)
	n, err := none.Prune(ctx, 30)
	r.NoError(err)
	r.Zero(n)
	r.NotPanics(none.Close)
	_, err = Open(ctx, "not a connection string")
	r.Error(err)
	short, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err = Open(short, "postgres://nobody:nothing@127.0.0.1:1/x?sslmode=disable&connect_timeout=1")
	r.Error(err)
	_, err = unpackTrace([]byte("not gzip"))
	r.Error(err)
}

// The whole plugin: a client posts, the chart is there for anyone in the team,
// two laps overlay, and the pages list them.
func TestAPostedLapIsDrawnForTheTeam(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	v := New(plugged(t), nil)
	ctx := context.Background()

	res, err := v.ServeHTTP(ctx, request(http.MethodPost, "/laps", "", driver, uploadOf(syntheticLap("s-1", 7, "mihai", 0))))
	r.NoError(err)
	r.Equal(http.StatusOK, res.Status, string(res.Body))
	var out posted
	r.NoError(json.Unmarshal(res.Body, &out))
	r.Equal("s-1/7", out.Lap)
	r.Equal(1001, out.Points)
	r.Equal("/plugin/visual-telemetry/charts/s-1/7.svg", out.SVG)
	r.Equal("/plugin/visual-telemetry/charts/s-1/7.png", out.PNG)

	res, err = v.ServeHTTP(ctx, request(http.MethodPost, "/laps", "", driver, []byte(`{"stint_id":"s","lap":0}`)))
	r.NoError(err)
	r.Equal(http.StatusBadRequest, res.Status)

	// The driver's chart, as a document and as a picture; the administrator
	// sees it too; a lap that was never posted is not found.
	res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/charts/s-1/7.svg", "", driver, nil))
	r.NoError(err)
	r.Equal(http.StatusOK, res.Status)
	r.Equal("image/svg+xml; charset=utf-8", res.Header.Get("Content-Type"))
	r.Contains(string(res.Body), ">Mihai<")
	res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/charts/s-1/7.png", "", admin, nil))
	r.NoError(err)
	r.Equal(http.StatusOK, res.Status)
	r.Equal("image/png", res.Header.Get("Content-Type"))
	img, err := png.Decode(bytes.NewReader(res.Body))
	r.NoError(err)
	r.Equal(1400, img.Bounds().Dx())
	res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/charts/s-9/1.svg", "", driver, nil))
	r.NoError(err)
	r.Equal(http.StatusNotFound, res.Status)
	res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/charts/not-a-lap.svg", "", driver, nil))
	r.NoError(err)
	r.Equal(http.StatusBadRequest, res.Status)

	// A second lap, from another driver, and the two overlaid.
	ana := plugin.Caller{DriverSlug: "ana", DriverName: "Ana"}
	res, err = v.ServeHTTP(ctx, request(http.MethodPost, "/laps", "", ana, uploadOf(syntheticLap("s-2", 3, "ana", -6))))
	r.NoError(err)
	r.Equal(http.StatusOK, res.Status)
	res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/charts/compare.svg", "lap=s-1/7&lap=s-2/3", driver, nil))
	r.NoError(err)
	r.Equal(http.StatusOK, res.Status, string(res.Body))
	r.Contains(string(res.Body), ">Ana<")
	r.Contains(string(res.Body), ">ΔT S<")
	res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/charts/compare.svg", "lap=s-1/7", driver, nil))
	r.NoError(err)
	r.Equal(http.StatusBadRequest, res.Status)
	res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/charts/compare.png", "lap=s-1/7&lap=s-9/9", driver, nil))
	r.NoError(err)
	r.Equal(http.StatusNotFound, res.Status)

	// The chart on its page, in the panel's shell: the file it shows, the laps
	// under it, and what is refused.
	res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/charts", "lap=s-1/7&lap=s-2/3", driver, nil))
	r.NoError(err)
	r.Equal(http.StatusOK, res.Status, string(res.Body))
	r.Contains(string(res.Body), `<img src="/plugin/visual-telemetry/charts/compare.svg?lap=s-1%2F7&amp;lap=s-2%2F3"`)
	r.Contains(string(res.Body), `value="s-1/7"`)
	r.NotContains(string(res.Body), `value="s-2/3"`, "a driver's chart page listed another driver's lap to pick")
	res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/charts", "lap=s-1/7", admin, nil))
	r.NoError(err)
	r.Equal(http.StatusOK, res.Status)
	r.Contains(string(res.Body), `<img src="/plugin/visual-telemetry/charts/s-1/7.svg"`)
	r.Contains(string(res.Body), `value="s-2/3"`, "an administrator picks from everyone's laps")
	res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/charts", "", driver, nil))
	r.NoError(err)
	r.Equal(http.StatusOK, res.Status)
	r.NotContains(string(res.Body), "<img", "no lap named, no chart")
	for query, want := range map[string]int{
		"lap=s-9/9":     http.StatusNotFound,
		"lap=not-a-lap": http.StatusBadRequest,
		"lap=s-1/7&lap=s-1/7&lap=s-1/7&lap=s-1/7&lap=s-1/7": http.StatusBadRequest,
	} {
		res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/charts", query, driver, nil))
		r.NoError(err)
		r.Equal(want, res.Status, query)
	}

	// The pages.
	res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/me", "", driver, nil))
	r.NoError(err)
	r.Contains(string(res.Body), `value="s-1/7"`)
	r.NotContains(string(res.Body), `value="s-2/3"`, "a driver's page listed another driver's lap")
	res, err = v.ServeHTTP(ctx, request(http.MethodGet, "/", "", admin, nil))
	r.NoError(err)
	r.Contains(string(res.Body), `value="s-1/7"`)
	r.Contains(string(res.Body), `value="s-2/3"`)
	r.Contains(string(res.Body), "Ana")
}
