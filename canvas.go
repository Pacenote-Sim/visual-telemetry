package visualtelemetry

import (
	"html"
	"strconv"
	"strings"
)

// A chart is drawn once, against a canvas, and the canvas is what decides
// whether the result is a document or a picture: the SVG canvas writes
// elements, the raster canvas paints pixels. Every line of the chart is drawn
// by the same code either way, so the two never disagree.

// canvas is what a chart needs to be able to draw.
type canvas interface {
	// Rect fills a rectangle.
	Rect(x, y, w, h float64, fill string)
	// Line draws one segment; dashed draws it in dashes.
	Line(x1, y1, x2, y2 float64, stroke string, width float64, dashed bool)
	// Polyline draws a path through the points, unfilled.
	Polyline(xs, ys []float64, stroke string, width float64)
	// Polygon fills a closed shape.
	Polygon(xs, ys []float64, fill string)
	// Text writes s with its baseline at y, anchored start, middle or end at x,
	// in the panel's own sans-serif.
	Text(x, y float64, s string, size float64, fill, anchor string, bold bool)
	// Label writes a small label the way the panel writes its own: monospace,
	// capitals, letter-spaced.
	Label(x, y float64, s string, size float64, fill, anchor string)
	// Circle fills a disc.
	Circle(cx, cy, r float64, fill string)
}

// svgCanvas writes an SVG document. Coordinates are appended to the one
// builder with strconv, never formatted into strings of their own: a chart has
// tens of thousands of them.
type svgCanvas struct {
	w, h float64
	b    strings.Builder
}

func newSVG(w, h float64) *svgCanvas {
	c := &svgCanvas{w: w, h: h}
	c.b.Grow(64 << 10)
	// A viewBox and no size: the document fills whatever shows it, a browser
	// window or an <img>, and keeps its shape.
	c.b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 `)
	c.num(w)
	c.b.WriteByte(' ')
	c.num(h)
	c.b.WriteString(`" preserveAspectRatio="xMidYMid meet" font-family="system-ui, -apple-system, Segoe UI, Roboto, sans-serif">` + "\n")
	return c
}

// attr writes name="value" for a number.
func (c *svgCanvas) attr(name string, v float64) {
	c.b.WriteByte(' ')
	c.b.WriteString(name)
	c.b.WriteString(`="`)
	c.num(v)
	c.b.WriteByte('"')
}

// str writes name="value" for a string that is known to be safe in an attribute.
func (c *svgCanvas) str(name, v string) {
	c.b.WriteByte(' ')
	c.b.WriteString(name)
	c.b.WriteString(`="`)
	c.b.WriteString(v)
	c.b.WriteByte('"')
}

// num writes a coordinate as SVG reads it: one decimal, no trailing zero, and
// never "1e+06".
func (c *svgCanvas) num(v float64) {
	var buf [24]byte
	out := strconv.AppendFloat(buf[:0], v, 'f', 1, 64)
	if len(out) > 2 && out[len(out)-1] == '0' && out[len(out)-2] == '.' {
		out = out[:len(out)-2]
	}
	if len(out) == 2 && out[0] == '-' && out[1] == '0' {
		out = out[1:]
	}
	c.b.Write(out)
}

func (c *svgCanvas) points(xs, ys []float64) {
	c.b.WriteString(` points="`)
	for i := range xs {
		if i > 0 {
			c.b.WriteByte(' ')
		}
		c.num(xs[i])
		c.b.WriteByte(',')
		c.num(ys[i])
	}
	c.b.WriteByte('"')
}

func (c *svgCanvas) Rect(x, y, w, h float64, fill string) {
	c.b.WriteString("<rect")
	c.attr("x", x)
	c.attr("y", y)
	c.attr("width", w)
	c.attr("height", h)
	c.str("fill", fill)
	c.b.WriteString("/>\n")
}

func (c *svgCanvas) Line(x1, y1, x2, y2 float64, stroke string, width float64, dashed bool) {
	c.b.WriteString("<line")
	c.attr("x1", x1)
	c.attr("y1", y1)
	c.attr("x2", x2)
	c.attr("y2", y2)
	c.str("stroke", stroke)
	c.attr("stroke-width", width)
	if dashed {
		c.b.WriteString(` stroke-dasharray="6 4"`)
	}
	c.b.WriteString("/>\n")
}

func (c *svgCanvas) Polyline(xs, ys []float64, stroke string, width float64) {
	if len(xs) < 2 {
		return
	}
	c.b.WriteString("<polyline")
	c.points(xs, ys)
	c.str("fill", "none")
	c.str("stroke", stroke)
	c.attr("stroke-width", width)
	c.b.WriteString(` stroke-linejoin="round" stroke-linecap="round"/>` + "\n")
}

func (c *svgCanvas) Polygon(xs, ys []float64, fill string) {
	if len(xs) < 3 {
		return
	}
	c.b.WriteString("<polygon")
	c.points(xs, ys)
	c.str("fill", fill)
	c.b.WriteString("/>\n")
}

func (c *svgCanvas) Label(x, y float64, s string, size float64, fill, anchor string) {
	c.text(x, y, strings.ToUpper(s), size, fill, anchor,
		` font-family="ui-monospace, SFMono-Regular, Menlo, monospace" letter-spacing="1.5"`)
}

func (c *svgCanvas) Text(x, y float64, s string, size float64, fill, anchor string, bold bool) {
	extra := ""
	if bold {
		extra = ` font-weight="700"`
	}
	c.text(x, y, s, size, fill, anchor, extra)
}

func (c *svgCanvas) text(x, y float64, s string, size float64, fill, anchor, extra string) {
	c.b.WriteString("<text")
	c.attr("x", x)
	c.attr("y", y)
	c.attr("font-size", size)
	c.str("fill", fill)
	c.str("text-anchor", anchor)
	c.b.WriteString(extra)
	c.b.WriteByte('>')
	c.b.WriteString(html.EscapeString(s))
	c.b.WriteString("</text>\n")
}

func (c *svgCanvas) Circle(cx, cy, r float64, fill string) {
	c.b.WriteString("<circle")
	c.attr("cx", cx)
	c.attr("cy", cy)
	c.attr("r", r)
	c.str("fill", fill)
	c.b.WriteString("/>\n")
}

// bytes is the document.
func (c *svgCanvas) bytes() []byte {
	return []byte(c.b.String() + "</svg>\n")
}
