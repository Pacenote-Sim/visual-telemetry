package visualtelemetry

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pacenote-sim/plugin"
)

// The routes without a database: who may reach what, and what each says when
// there is nothing to draw from. The rest is in store_postgres_test.go.

func request(method, path, query string, caller plugin.Caller, body []byte) plugin.HTTPRequest {
	return plugin.HTTPRequest{Method: method, Path: path, Query: query, Body: body, Caller: caller, Prefix: "/plugin/visual-telemetry"}
}

var (
	driver = plugin.Caller{DriverSlug: "mihai", DriverName: "Mihai"}
	admin  = plugin.Caller{AdminEmail: "ana@example.com"}
)

func TestWhoMayReachWhat(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	v := New(nil, nil)
	body := uploadOf(syntheticLap("s-1", 7, "mihai", 0))

	for _, tc := range []struct {
		name string
		req  plugin.HTTPRequest
		want int
	}{
		{"a stranger posting a lap", request(http.MethodPost, "/laps", "", plugin.Caller{}, body), http.StatusUnauthorized},
		{"a driver posting a lap, with no database", request(http.MethodPost, "/laps", "", driver, body), http.StatusServiceUnavailable},
		{"a stranger opening a chart", request(http.MethodGet, "/charts/s-1/7.svg", "", plugin.Caller{}, nil), http.StatusUnauthorized},
		{"a driver opening a chart, with no database", request(http.MethodGet, "/charts/s-1/7.svg", "", driver, nil), http.StatusServiceUnavailable},
		{"an administrator opening a chart, with no database", request(http.MethodGet, "/charts/s-1/7.png", "", admin, nil), http.StatusServiceUnavailable},
		{"a stranger's page", request(http.MethodGet, "/me", "", plugin.Caller{}, nil), http.StatusUnauthorized},
		{"a stranger opening the charts page", request(http.MethodGet, "/charts", "", plugin.Caller{}, nil), http.StatusUnauthorized},
		{"a driver's charts page, with no database", request(http.MethodGet, "/charts", "", driver, nil), http.StatusOK},
		{"the charts page with a slash, with no database", request(http.MethodGet, "/charts/", "", admin, nil), http.StatusOK},
		{"a driver's page, with no database", request(http.MethodGet, "/me", "", driver, nil), http.StatusOK},
		{"the administrator's page, with no database", request(http.MethodGet, "/", "", admin, nil), http.StatusOK},
		{"anyone fetching the stylesheet", request(http.MethodGet, "/style.css", "", plugin.Caller{}, nil), http.StatusOK},
		{"an address that is not one", request(http.MethodGet, "/nowhere", "", admin, nil), http.StatusNotFound},
		{"a chart that is neither svg nor png", request(http.MethodGet, "/charts/s-1/7.gif", "", admin, nil), http.StatusNotFound},
		{"posting to charts", request(http.MethodPost, "/charts/s-1/7.svg", "", driver, nil), http.StatusNotFound},
	} {
		res, err := v.ServeHTTP(context.Background(), tc.req)
		r.NoError(err, tc.name)
		r.Equalf(tc.want, res.Status, "%s: %s", tc.name, res.Body)
	}

	res, err := v.ServeHTTP(context.Background(), request(http.MethodGet, "/me", "", driver, nil))
	r.NoError(err)
	r.Contains(string(res.Body), "no database")
	r.Contains(string(res.Body), "Your laps")

	// The server's content security policy allows stylesheets from its own
	// address only, so the plugin's styling is a file of its own, linked.
	css, err := v.ServeHTTP(context.Background(), request(http.MethodGet, "/style.css", "", plugin.Caller{}, nil))
	r.NoError(err)
	r.Equal("text/css; charset=utf-8", css.Header.Get("Content-Type"))
	r.Contains(string(css.Body), ".chart img")
	r.Contains(string(res.Body), `<link rel="stylesheet" href="/plugin/visual-telemetry/style.css">`)
	r.NotContains(string(res.Body), "<style", "a style block in the page is ignored by the browser")
}

// The list of laps: links, the form, and nothing from outside as markup.
func TestTheLapListIsAFormThatOverlays(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	l := syntheticLap("s-1", 7, "mihai", 0)
	l.DriverName = `<b>Mihai</b>`
	l.Track = `Spa <script>`
	html := lapList("/plugin/visual-telemetry", []Lap{l}, true)
	r.Contains(html, `action="/plugin/visual-telemetry/charts"`, "the form opens the page, not the file")
	r.Contains(html, `<input type="checkbox" name="lap" value="s-1/7">`)
	r.Contains(html, `href="/plugin/visual-telemetry/charts?lap=s-1%2F7"`)
	r.Contains(html, `href="/plugin/visual-telemetry/charts/s-1/7.png"`)
	r.Contains(html, "&lt;b&gt;Mihai&lt;/b&gt;")
	r.NotContains(html, "<script>")
	r.Contains(lapList("/p", nil, false), "Nothing yet")
	r.NotContains(lapList("/p", []Lap{l}, false), "Mihai", "a driver's own page names the driver on every row")
	r.NotContains(page("<title>", "body", "/p", true, false), "<title><title>")
	r.Contains(page("t", "b", "/p", true, false), `href="/admin/plugins/visual-telemetry"`, "an administrator goes back to the panel")
	r.Contains(page("t", "b", "/p", false, false), `href="/p/me"`, "a driver goes back to their laps")

	r.Contains(page("t", "b", "/p", true, true), `class="wrap wide chartpage"`, "a chart gets the width it needs")
	r.Contains(page("t", "b", "/p", true, false), `class="wrap wide"`, "a list keeps the panel's width")

	// The chart on its page: one lap's own file, or the comparison with its laps.
	r.Equal("/p/charts/s-1/7.svg", chartAddress("/p", []string{"s-1/7"}, ".svg"))
	r.Equal("/p/charts/compare.png?lap=s-1%2F7&lap=s-2%2F3", chartAddress("/p", []string{"s-1/7", "s-2/3"}, ".png"))
	fig := chartFigure("/p", []string{"s-1/7", "s-2/3"})
	r.Contains(fig, `<img src="/p/charts/compare.svg?lap=s-1%2F7&amp;lap=s-2%2F3"`)
	r.Contains(fig, `href="/p/charts/compare.png?lap=s-1%2F7&amp;lap=s-2%2F3" download`)
}
