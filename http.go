package visualtelemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pacenote-sim/plugin"
)

// The addresses: a client posts a lap; anyone in the team opens a chart; a
// driver's page lists their laps, the administrator's everyone's, and both
// let you pick laps to overlay.

var _ plugin.Server = (*VisualTelemetry)(nil)

// ServeHTTP answers the routes the manifest declares.
func (v *VisualTelemetry) ServeHTTP(ctx context.Context, r plugin.HTTPRequest) (plugin.HTTPResponse, error) {
	switch {
	case r.Path == "/laps" && r.Method == http.MethodPost:
		return v.postLap(ctx, r)
	case (r.Path == "/charts" || r.Path == "/charts/") && r.Method == http.MethodGet:
		return v.pageCharts(ctx, r)
	case strings.HasPrefix(r.Path, "/charts/") && r.Method == http.MethodGet:
		return v.getChart(ctx, r)
	case r.Path == "/style.css" && r.Method == http.MethodGet:
		return plugin.HTTPResponse{
			Status: http.StatusOK,
			Header: http.Header{"Content-Type": {"text/css; charset=utf-8"}},
			Body:   []byte(styleSheet),
		}, nil
	case r.Path == "/me" && r.Method == http.MethodGet:
		return v.pageMine(ctx, r)
	case r.Path == "/" && r.Method == http.MethodGet:
		return v.pageAll(ctx, r)
	default:
		return plugin.Text(http.StatusNotFound, "There is nothing at that address."), nil
	}
}

// posted is the answer to a lap: where its chart is.
type posted struct {
	Lap    string `json:"lap"`
	Points int    `json:"points"`
	SVG    string `json:"svg"`
	PNG    string `json:"png"`
}

// postLap is POST /laps: a lap in, kept, and the addresses of its chart out.
func (v *VisualTelemetry) postLap(ctx context.Context, r plugin.HTTPRequest) (plugin.HTTPResponse, error) {
	if !r.Caller.SignedIn() {
		return plugin.Text(http.StatusUnauthorized, "Only a signed-in driver's client can post a lap."), nil
	}
	if v.Store == nil {
		return plugin.Text(http.StatusServiceUnavailable, "This plugin has no database, so a lap cannot be kept or drawn later."), nil
	}
	up, err := parseLapUpload(r.Body)
	if err != nil {
		return plugin.Text(http.StatusBadRequest, err.Error()), nil
	}
	l := lapOf(up, r.Caller, v.clock())
	if err = v.Store.Put(ctx, l); err != nil {
		v.Log.LogAttrs(ctx, slog.LevelWarn, "a lap could not be kept", slog.String("reason", err.Error()))
		return plugin.Text(http.StatusBadGateway, "The lap could not be kept just now."), nil
	}
	cfg := configOf(r.Settings)
	if n, pruneErr := v.Store.Prune(ctx, cfg.keepDays); pruneErr != nil {
		v.Log.LogAttrs(ctx, slog.LevelWarn, "old laps could not be removed", slog.String("reason", pruneErr.Error()))
	} else if n > 0 {
		v.Log.LogAttrs(ctx, slog.LevelInfo, "old laps were removed", slog.Int64("laps", n))
	}
	out, err := json.Marshal(posted{
		Lap: l.Ref(), Points: len(l.Trace),
		SVG: r.Prefix + "/charts/" + l.Ref() + ".svg",
		PNG: r.Prefix + "/charts/" + l.Ref() + ".png",
	})
	if err != nil {
		return plugin.Text(http.StatusInternalServerError, "The answer could not be encoded."), nil
	}
	return plugin.HTTPResponse{
		Status: http.StatusOK,
		Header: http.Header{"Content-Type": {"application/json; charset=utf-8"}},
		Body:   out,
	}, nil
}

// getChart is GET /charts/<stint>/<lap>.svg|.png, or /charts/compare.svg|.png
// with ?lap=<stint>/<lap> two to four times. The route is the plugin's to
// decide, and it decides: a signed-in driver or an administrator, nobody else.
func (v *VisualTelemetry) getChart(ctx context.Context, r plugin.HTTPRequest) (plugin.HTTPResponse, error) {
	if !r.Caller.SignedIn() && r.Caller.AdminEmail == "" {
		return plugin.Text(http.StatusUnauthorized, "Sign in to look at a chart."), nil
	}
	name := strings.TrimPrefix(r.Path, "/charts/")
	ext := ""
	for _, e := range []string{".svg", ".png"} {
		if strings.HasSuffix(name, e) {
			ext, name = e, strings.TrimSuffix(name, e)
		}
	}
	if ext == "" {
		return plugin.Text(http.StatusNotFound, "A chart is .svg or .png."), nil
	}
	if v.Store == nil {
		return plugin.Text(http.StatusServiceUnavailable, "This plugin has no database, so there are no laps to draw."), nil
	}

	var refs []string
	if name == "compare" {
		q, _ := url.ParseQuery(r.Query)
		refs = q["lap"]
		if len(refs) < 2 || len(refs) > MaxLapsOnAChart {
			return plugin.Text(http.StatusBadRequest, fmt.Sprintf("Name two to %d laps to compare: ?lap=<stint>/<lap>&lap=…", MaxLapsOnAChart)), nil
		}
	} else {
		refs = []string{name}
	}
	laps := make([]Lap, 0, len(refs))
	for _, ref := range refs {
		stint, n, ok := parseRef(ref)
		if !ok {
			return plugin.Text(http.StatusBadRequest, "A lap is named <stint>/<lap>."), nil
		}
		l, err := v.Store.Get(ctx, stint, n)
		switch {
		case isNoRows(err):
			return plugin.Text(http.StatusNotFound, "No such lap was posted: "+ref), nil
		case err != nil:
			return plugin.Text(http.StatusBadGateway, "The lap could not be read just now."), nil
		}
		laps = append(laps, l)
	}

	cfg := configOf(r.Settings)
	if ext == ".svg" {
		return plugin.HTTPResponse{
			Status: http.StatusOK,
			Header: http.Header{"Content-Type": {"image/svg+xml; charset=utf-8"}},
			Body:   chartSVG(laps, cfg),
		}, nil
	}
	pic, err := chartPNG(laps, cfg)
	if err != nil {
		v.Log.LogAttrs(ctx, slog.LevelError, "a chart could not be drawn", slog.String("reason", err.Error()))
		return plugin.Text(http.StatusInternalServerError, "The chart could not be drawn."), nil
	}
	return plugin.HTTPResponse{
		Status: http.StatusOK,
		Header: http.Header{"Content-Type": {"image/png"}},
		Body:   pic,
	}, nil
}

// pageCharts is GET /charts: a chart in the panel's own shell. With
// ?lap=<stint>/<lap> once it is that lap's chart; two to four times, the laps
// overlaid. The laps to pick from follow either way, and stand alone when no
// lap is named. The files themselves are at /charts/<...>.svg and .png.
func (v *VisualTelemetry) pageCharts(ctx context.Context, r plugin.HTTPRequest) (plugin.HTTPResponse, error) {
	if !r.Caller.SignedIn() && r.Caller.AdminEmail == "" {
		return plugin.Text(http.StatusUnauthorized, "Sign in to look at a chart."), nil
	}
	admin := r.Caller.AdminEmail != ""
	if v.Store == nil {
		return plugin.HTML(http.StatusOK, page("Charts", noDatabase, r.Prefix, admin, false)), nil
	}
	q, _ := url.ParseQuery(r.Query)
	refs := q["lap"]
	if len(refs) > MaxLapsOnAChart {
		return plugin.Text(http.StatusBadRequest, fmt.Sprintf("A chart overlays at most %d laps.", MaxLapsOnAChart)), nil
	}
	for _, ref := range refs {
		stint, n, ok := parseRef(ref)
		if !ok {
			return plugin.Text(http.StatusBadRequest, "A lap is named <stint>/<lap>."), nil
		}
		if _, err := v.Store.Get(ctx, stint, n); err != nil {
			if isNoRows(err) {
				return plugin.Text(http.StatusNotFound, "No such lap was posted: "+ref), nil
			}
			return plugin.Text(http.StatusBadGateway, "The lap could not be read just now."), nil
		}
	}

	var laps []Lap
	var err error
	if admin {
		laps, err = v.Store.RecentAll(ctx, 200)
	} else {
		laps, err = v.Store.Recent(ctx, r.Caller.DriverSlug, 100)
	}
	if err != nil {
		return plugin.HTML(http.StatusBadGateway, page("Charts", `  <div class="card"><p class="warn">The laps could not be read just now.</p></div>`, r.Prefix, admin, false)), nil
	}
	title := "Charts"
	var b strings.Builder
	if len(refs) > 0 {
		title = "Chart"
		b.WriteString(chartFigure(r.Prefix, refs))
	}
	b.WriteString(lapList(r.Prefix, laps, admin))
	return plugin.HTML(http.StatusOK, page(title, b.String(), r.Prefix, admin, len(refs) > 0)), nil
}

// chartAddress is where the file for these laps is: one lap's own address, or
// the comparison with the laps in its query.
func chartAddress(prefix string, refs []string, ext string) string {
	if len(refs) == 1 {
		return prefix + "/charts/" + refs[0] + ext
	}
	return prefix + "/charts/compare" + ext + "?" + url.Values{"lap": refs}.Encode()
}

// chartFigure is the chart on the page: the document shown at the page's
// width, the picture to save beside it.
func chartFigure(prefix string, refs []string) string {
	svg, png := chartAddress(prefix, refs, ".svg"), chartAddress(prefix, refs, ".png")
	return fmt.Sprintf(`  <div class="card chart">
    <img src="%s" alt="The telemetry chart" width="1400" height="650">
    <div class="actions"><a class="btn" href="%s" download>Save the picture</a> <a class="ghost" href="%s">Open the document</a></div>
  </div>
`, esc(svg), esc(png), esc(svg))
}

// pageMine is the driver's own laps, and the form to overlay some.
func (v *VisualTelemetry) pageMine(ctx context.Context, r plugin.HTTPRequest) (plugin.HTTPResponse, error) {
	if !r.Caller.SignedIn() {
		return plugin.Text(http.StatusUnauthorized, "Sign in to see your laps."), nil
	}
	if v.Store == nil {
		return plugin.HTML(http.StatusOK, page("Your laps", noDatabase, r.Prefix, false, false)), nil
	}
	laps, err := v.Store.Recent(ctx, r.Caller.DriverSlug, 100)
	if err != nil {
		return plugin.HTML(http.StatusBadGateway, page("Your laps", `  <div class="card"><p class="warn">Your laps could not be read just now.</p></div>`, r.Prefix, false, false)), nil
	}
	return plugin.HTML(http.StatusOK, page("Your laps", lapList(r.Prefix, laps, false), r.Prefix, false, false)), nil
}

// pageAll is everyone's laps, for the administrator.
func (v *VisualTelemetry) pageAll(ctx context.Context, r plugin.HTTPRequest) (plugin.HTTPResponse, error) {
	if v.Store == nil {
		return plugin.HTML(http.StatusOK, page("Laps", noDatabase, r.Prefix, true, false)), nil
	}
	laps, err := v.Store.RecentAll(ctx, 200)
	if err != nil {
		return plugin.HTML(http.StatusBadGateway, page("Laps", `  <div class="card"><p class="warn">The laps could not be read just now.</p></div>`, r.Prefix, true, false)), nil
	}
	return plugin.HTML(http.StatusOK, page("Laps", lapList(r.Prefix, laps, true), r.Prefix, true, false)), nil
}

const noDatabase = `  <div class="card"><p class="muted">This plugin has no database, so no lap can be kept or drawn.</p></div>`

// lapList is the table of laps with a link to each chart, and the form that
// overlays the ticked ones. The form is a plain GET: the browser builds the
// compare address from the boxes, and no script is needed.
func lapList(prefix string, laps []Lap, withDriver bool) string {
	if len(laps) == 0 {
		return `  <div class="card">
    <h2>Nothing yet</h2>
    <p class="muted">A lap appears here when a client posts its trace.</p>
  </div>
`
	}
	var b strings.Builder
	fmt.Fprintf(&b, `  <form method="get" action="%s">
  <div class="card">
    <p class="muted">Open a lap's chart, or tick two to four laps — anyone's — and overlay them.</p>
    <table class="facts rows">
`, esc(prefix+"/charts"))
	for i := range laps {
		l := &laps[i]
		who := ""
		if withDriver {
			who = esc(plainOr(l.DriverName, l.DriverSlug)) + " · "
		}
		fmt.Fprintf(&b, `      <tr><th><label><input type="checkbox" name="lap" value="%s"> lap %d</label></th>
        <td>%s%s · %s · %s<div class="drill">%s · <a href="%s">chart</a> · <a href="%s">picture</a></div></td></tr>
`,
			esc(l.Ref()), l.Lap, who, esc(plainOr(l.Track, "an unnamed circuit")), esc(lapTime(l.endMs())),
			esc(plainOr(l.Session, "")), esc(l.CreatedAt.Format(time.RFC822)),
			esc(prefix+"/charts?lap="+url.QueryEscape(l.Ref())), esc(prefix+"/charts/"+l.Ref()+".png"))
	}
	b.WriteString(`    </table>
    <div class="actions"><button class="btn" type="submit">Overlay the ticked laps</button></div>
  </div>
  </form>
`)
	return b.String()
}

// styleSheet is what this plugin adds to the panel's own stylesheet. It is a
// file at /style.css and not a <style> block in the page: the server sends
// every page with a content security policy that allows stylesheets from its
// own address only, so a block inside the page is ignored by the browser.
const styleSheet = `.drill { color: var(--muted); }
.drill a { color: var(--text); }
.card + .card { margin-top: 14px; }
label { cursor: pointer; }
.wrap.chartpage { max-width: 1440px; }
.chart { padding: 0; border: none; background: none; clip-path: none; }
.chart img { display: block; width: 100%; height: auto; aspect-ratio: 1400 / 650; }
.chart .actions { margin-top: 12px; }
`

// page wraps a body in the server's own shell, as every plugin does. An
// administrator goes back to the panel, a driver to their own laps.
func page(title, body, prefix string, admin, chart bool) string {
	back, backLabel := prefix+"/me", "Your laps"
	if admin {
		back, backLabel = "/admin/plugins/visual-telemetry", "Back to the panel"
	}
	wrap := "wrap wide"
	if chart {
		wrap = "wrap wide chartpage"
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s — visual telemetry</title>
<link rel="stylesheet" href="/assets/app.css">
<link rel="stylesheet" href="%s">
</head>
<body>
<div class="%s">
  <header class="top">
    <span class="name">visual telemetry</span>
    <span class="wm">plugin</span>
    <span class="spacer"></span>
    <a class="ghost" href="%s">%s</a>
  </header>
  <h1>%s</h1>
`, esc(title), esc(prefix+"/style.css"), wrap, esc(back), esc(backLabel), esc(title))
	b.WriteString(body)
	b.WriteString(`
  <footer class="foot">Served by the visual-telemetry plugin, not by the server.</footer>
</div>
</body>
</html>`)
	return b.String()
}

func esc(s string) string { return html.EscapeString(s) }
