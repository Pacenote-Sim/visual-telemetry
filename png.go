package visualtelemetry

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// The raster canvas: the same chart as pixels, for the picture a driver posts
// somewhere. Pure Go — a rasteriser and the Go fonts — so a server needs
// nothing installed to draw one.

// fonts are parsed once for the process.
var (
	fontsOnce   sync.Once
	fontRegular *sfnt.Font
	fontBold    *sfnt.Font
	fontMono    *sfnt.Font
	fontErr     error
)

func loadFonts() error {
	fontsOnce.Do(func() {
		for _, f := range []struct {
			ttf []byte
			dst **sfnt.Font
		}{{goregular.TTF, &fontRegular}, {gobold.TTF, &fontBold}, {gomono.TTF, &fontMono}} {
			if fontErr != nil {
				return
			}
			*f.dst, fontErr = opentype.Parse(f.ttf)
		}
	})
	return fontErr
}

// faceStyle is which of the three faces.
type faceStyle int

const (
	styleRegular faceStyle = iota
	styleBold
	styleMono
)

type faceKey struct {
	size  float64
	style faceStyle
}

// rasterCanvas paints into an RGBA image.
//
// One rasteriser serves every shape, sized to the shape's own bounding box
// each time: a rasteriser the size of the whole picture costs four megabytes
// to allocate and the whole picture to sweep, and a chart is a few hundred
// shapes. Sized to the shape, a grid line costs its own few pixels.
type rasterCanvas struct {
	img   *image.RGBA
	ras   *vector.Rasterizer
	faces map[faceKey]font.Face
}

func newRaster(w, h int) (*rasterCanvas, error) {
	if err := loadFonts(); err != nil {
		return nil, err
	}
	return &rasterCanvas{
		img:   image.NewRGBA(image.Rect(0, 0, w, h)),
		ras:   vector.NewRasterizer(w, h),
		faces: map[faceKey]font.Face{},
	}, nil
}

// clip is the pixel rectangle a set of shapes touches, grown by the stroke
// and kept inside the picture, and the offset that maps picture coordinates
// into a rasteriser of that size.
func (c *rasterCanvas) clip(minX, minY, maxX, maxY, grow float64) (image.Rectangle, bool) {
	r := image.Rect(int(math.Floor(minX-grow)), int(math.Floor(minY-grow)), int(math.Ceil(maxX+grow))+1, int(math.Ceil(maxY+grow))+1)
	r = r.Intersect(c.img.Bounds())
	return r, !r.Empty()
}

// paint draws whatever shapes add to the rasteriser, translated into r, onto
// the picture in one colour.
func (c *rasterCanvas) paint(r image.Rectangle, fill string, shapes func(ox, oy float64)) {
	c.ras.Reset(r.Dx(), r.Dy())
	shapes(float64(r.Min.X), float64(r.Min.Y))
	c.ras.Draw(c.img, r, image.NewUniform(parseColour(fill)), image.Point{})
}

func (c *rasterCanvas) Rect(x, y, w, h float64, fill string) {
	r := image.Rect(int(math.Round(x)), int(math.Round(y)), int(math.Round(x+w)), int(math.Round(y+h)))
	draw.Draw(c.img, r, image.NewUniform(parseColour(fill)), image.Point{}, draw.Src)
}

func (c *rasterCanvas) Line(x1, y1, x2, y2 float64, stroke string, width float64, dashed bool) {
	if !dashed {
		c.strokes([][4]float64{{x1, y1, x2, y2}}, stroke, width)
		return
	}
	// Dashes of six and gaps of four, the SVG's pattern.
	dx, dy := x2-x1, y2-y1
	length := math.Hypot(dx, dy)
	if length == 0 {
		return
	}
	ux, uy := dx/length, dy/length
	var segs [][4]float64
	for at := 0.0; at < length; at += 10 {
		end := math.Min(at+6, length)
		segs = append(segs, [4]float64{x1 + ux*at, y1 + uy*at, x1 + ux*end, y1 + uy*end})
	}
	c.strokes(segs, stroke, width)
}

func (c *rasterCanvas) Polyline(xs, ys []float64, stroke string, width float64) {
	if len(xs) < 2 {
		return
	}
	segs := make([][4]float64, 0, len(xs)-1)
	for i := 1; i < len(xs); i++ {
		segs = append(segs, [4]float64{xs[i-1], ys[i-1], xs[i], ys[i]})
	}
	c.strokes(segs, stroke, width)
}

// strokes paints segments as thin quadrilaterals in one pass, with a disc at
// every joint so a sharp turn in the line does not open a notch.
func (c *rasterCanvas) strokes(segs [][4]float64, stroke string, width float64) {
	if len(segs) == 0 {
		return
	}
	minX, minY, maxX, maxY := segs[0][0], segs[0][1], segs[0][0], segs[0][1]
	for _, s := range segs {
		minX, maxX = math.Min(minX, math.Min(s[0], s[2])), math.Max(maxX, math.Max(s[0], s[2]))
		minY, maxY = math.Min(minY, math.Min(s[1], s[3])), math.Max(maxY, math.Max(s[1], s[3]))
	}
	r, ok := c.clip(minX, minY, maxX, maxY, width)
	if !ok {
		return
	}
	half := width / 2
	c.paint(r, stroke, func(ox, oy float64) {
		// Every shape is added with the same winding. The rasteriser
		// accumulates signed area, so two shapes wound opposite ways cancel
		// where they overlap — a line of holes at every joint, and was.
		for _, s := range segs {
			dx, dy := s[2]-s[0], s[3]-s[1]
			length := math.Hypot(dx, dy)
			if length == 0 {
				continue
			}
			nx, ny := -dy/length*half, dx/length*half
			polygon(c.ras, [][2]float64{
				{s[0] + nx - ox, s[1] + ny - oy},
				{s[2] + nx - ox, s[3] + ny - oy},
				{s[2] - nx - ox, s[3] - ny - oy},
				{s[0] - nx - ox, s[1] - ny - oy},
			})
		}
		if width >= 2 {
			for i, s := range segs {
				if i > 0 {
					disc(c.ras, s[0]-ox, s[1]-oy, half)
				}
			}
		}
	})
}

// polygon adds a closed shape to a path, wound clockwise on the screen
// whichever way its points were given.
func polygon(r *vector.Rasterizer, pts [][2]float64) {
	var area float64
	for i := range pts {
		j := (i + 1) % len(pts)
		area += pts[i][0]*pts[j][1] - pts[j][0]*pts[i][1]
	}
	if area < 0 {
		for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 {
			pts[i], pts[j] = pts[j], pts[i]
		}
	}
	r.MoveTo(float32(pts[0][0]), float32(pts[0][1]))
	for _, p := range pts[1:] {
		r.LineTo(float32(p[0]), float32(p[1]))
	}
	r.ClosePath()
}

// disc adds a polygonal disc to a path, wound the same way as every polygon.
func disc(r *vector.Rasterizer, cx, cy, radius float64) {
	const sides = 16
	pts := make([][2]float64, 0, sides)
	for i := range sides {
		a := 2 * math.Pi * float64(i) / sides
		pts = append(pts, [2]float64{cx + radius*math.Cos(a), cy + radius*math.Sin(a)})
	}
	polygon(r, pts)
}

func (c *rasterCanvas) Circle(cx, cy, radius float64, fill string) {
	r, ok := c.clip(cx-radius, cy-radius, cx+radius, cy+radius, 1)
	if !ok {
		return
	}
	c.paint(r, fill, func(ox, oy float64) { disc(c.ras, cx-ox, cy-oy, radius) })
}

func (c *rasterCanvas) Polygon(xs, ys []float64, fill string) {
	if len(xs) < 3 {
		return
	}
	minX, minY, maxX, maxY := xs[0], ys[0], xs[0], ys[0]
	for i := range xs {
		minX, maxX = math.Min(minX, xs[i]), math.Max(maxX, xs[i])
		minY, maxY = math.Min(minY, ys[i]), math.Max(maxY, ys[i])
	}
	r, ok := c.clip(minX, minY, maxX, maxY, 1)
	if !ok {
		return
	}
	c.paint(r, fill, func(ox, oy float64) {
		pts := make([][2]float64, len(xs))
		for i := range xs {
			pts[i] = [2]float64{xs[i] - ox, ys[i] - oy}
		}
		polygon(c.ras, pts)
	})
}

func (c *rasterCanvas) Text(x, y float64, s string, size float64, fill, anchor string, bold bool) {
	style := styleRegular
	if bold {
		style = styleBold
	}
	c.write(x, y, s, size, fill, anchor, style, 0)
}

func (c *rasterCanvas) Label(x, y float64, s string, size float64, fill, anchor string) {
	c.write(x, y, strings.ToUpper(s), size, fill, anchor, styleMono, 1.5)
}

// write draws a string in a face, with extra space between letters when the
// panel's labels have it.
func (c *rasterCanvas) write(x, y float64, s string, size float64, fill, anchor string, style faceStyle, spacing float64) {
	face, err := c.face(size, style)
	if err != nil {
		return
	}
	d := &font.Drawer{Dst: c.img, Src: image.NewUniform(parseColour(fill)), Face: face}
	extra := fixed.Int26_6(spacing * 64 * float64(max(0, utf8.RuneCountInString(s)-1)))
	width := d.MeasureString(s) + extra
	dot := fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y * 64)}
	switch anchor {
	case "middle":
		dot.X -= width / 2
	case "end":
		dot.X -= width
	}
	d.Dot = dot
	if spacing == 0 {
		d.DrawString(s)
		return
	}
	for _, r := range s {
		d.DrawString(string(r))
		d.Dot.X += fixed.Int26_6(spacing * 64)
	}
}

// face is a font face at a size, made once.
func (c *rasterCanvas) face(size float64, style faceStyle) (font.Face, error) {
	k := faceKey{size, style}
	if face, ok := c.faces[k]; ok {
		return face, nil
	}
	src := fontRegular
	switch style {
	case styleBold:
		src = fontBold
	case styleMono:
		src = fontMono
	case styleRegular:
	}
	face, err := opentype.NewFace(src, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return nil, err
	}
	c.faces[k] = face
	return face, nil
}

// bytes is the picture, encoded.
func (c *rasterCanvas) bytes() ([]byte, error) {
	var out bytes.Buffer
	if err := png.Encode(&out, c.img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// parseColour reads #RRGGBB. Anything else is white, which is visible, which
// is what a wrong colour should be.
func parseColour(s string) color.RGBA {
	if len(s) != 7 || s[0] != '#' {
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 255} //nolint:gosec // G115: three bytes of a 24-bit colour.
}
