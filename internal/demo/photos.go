package demo

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"math/rand/v2"
	"strings"
)

// Sample "field photos" for the biosecurity part of the demo. Nothing is
// downloaded: each picture is drawn with the standard library at seed time, a
// coloured gradient with a dark leaf or insect silhouette and a caption in a
// built-in 5x7 bitmap font. They are small JPEGs (a few KB each) stored through
// the files service like any upload.

const (
	photoW = 240
	photoH = 180
)

type photoKind int

const (
	kindPlant photoKind = iota
	kindInsect
	kindSite
	kindTrap
)

type photoSpec struct {
	File    string // file name when uploaded
	Caption string // short, upper case, drawn on the picture
	Kind    photoKind
	Top     color.RGBA // gradient colours
	Bottom  color.RGBA
	Body    color.RGBA // silhouette colour
	Accent  color.RGBA // stripes / veins
	Angle   float64    // silhouette rotation, radians
}

// photoSpecs holds one picture per demo species (same order as species) plus
// two general ones.
var photoSpecs = []photoSpec{
	{"knotweed.jpg", "KNOTWEED", kindPlant, rgb(122, 168, 96), rgb(52, 98, 58), rgb(28, 54, 30), rgb(176, 214, 140), -0.5},
	{"asian-hornet.jpg", "ASIAN HORNET", kindInsect, rgb(214, 182, 96), rgb(120, 88, 40), rgb(34, 24, 14), rgb(244, 190, 40), 0.4},
	{"lanternfly.jpg", "LANTERNFLY", kindInsect, rgb(150, 190, 214), rgb(70, 104, 136), rgb(70, 60, 66), rgb(200, 60, 70), -0.3},
	{"stink-bug.jpg", "STINK BUG", kindInsect, rgb(206, 170, 130), rgb(124, 92, 64), rgb(86, 64, 44), rgb(220, 196, 160), 0.7},
	{"box-tree-moth.jpg", "BOX TREE MOTH", kindInsect, rgb(158, 200, 150), rgb(62, 110, 74), rgb(232, 232, 224), rgb(78, 52, 30), -0.15},
	{"hogweed.jpg", "HOGWEED", kindPlant, rgb(190, 210, 150), rgb(86, 120, 70), rgb(36, 62, 38), rgb(236, 240, 220), 0.25},
	{"site-view.jpg", "SITE VIEW", kindSite, rgb(140, 190, 230), rgb(90, 130, 70), rgb(52, 84, 50), rgb(200, 220, 240), 0},
	{"trap-catch.jpg", "TRAP CATCH", kindTrap, rgb(230, 214, 160), rgb(160, 130, 80), rgb(60, 44, 30), rgb(250, 240, 200), 0},
}

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

// renderPhoto draws one picture and encodes it as a JPEG.
func renderPhoto(p photoSpec, seed uint64) []byte {
	rnd := rand.New(rand.NewPCG(seed, 0x5eed)) //nolint:gosec // deterministic sample data
	img := image.NewRGBA(image.Rect(0, 0, photoW, photoH))
	for y := 0; y < photoH; y++ {
		t := float64(y) / float64(photoH-1)
		for x := 0; x < photoW; x++ {
			// Vertical gradient plus a soft diagonal light and a little grain.
			light := 1 + 0.18*(float64(x)/photoW-0.5) - 0.08*t
			n := float64(rnd.IntN(9) - 4)
			img.SetRGBA(x, y, color.RGBA{
				clamp8(mix(p.Top.R, p.Bottom.R, t)*light + n),
				clamp8(mix(p.Top.G, p.Bottom.G, t)*light + n),
				clamp8(mix(p.Top.B, p.Bottom.B, t)*light + n), 255})
		}
	}
	cx, cy := float64(photoW)/2, float64(photoH)/2-12
	switch p.Kind {
	case kindPlant:
		for i, a := range []float64{p.Angle - 0.9, p.Angle, p.Angle + 0.9} {
			s := 1.0
			if i == 1 {
				s = 1.25
			}
			drawLeaf(img, cx+math.Sin(a)*8, cy+34-math.Cos(a)*4, a, 58*s, 21*s, p.Body, p.Accent)
		}
	case kindInsect:
		drawInsect(img, cx, cy, p)
	case kindSite:
		// Horizon, a row of crop stripes and a few trees.
		fillRect(img, 0, photoH/2+6, photoW, photoH, p.Body)
		for i := 0; i < 8; i++ {
			y := photoH/2 + 14 + i*i*2
			fillRect(img, 0, y, photoW, y+3, mixRGBA(p.Body, p.Accent, 0.25))
		}
		for _, x := range []float64{44, 98, 186} {
			fillCircle(img, x, float64(photoH)/2-8, 15, darker(p.Body))
			fillRect(img, int(x)-2, photoH/2+2, int(x)+2, photoH/2+14, rgb(70, 50, 30))
		}
	case kindTrap:
		// A bottle trap: body, cap and a few dark specks inside.
		fillRoundRect(img, cx-26, cy-44, cx+26, cy+50, 12, mixRGBA(p.Accent, rgb(255, 255, 255), 0.35))
		fillRect(img, int(cx)-12, int(cy)-56, int(cx)+12, int(cy)-44, p.Body)
		fillRect(img, int(cx)-26, int(cy)+16, int(cx)+26, int(cy)+50, mixRGBA(p.Body, p.Accent, 0.5))
		for i := 0; i < 14; i++ {
			fillCircle(img, cx-18+rnd.Float64()*36, cy+22+rnd.Float64()*24, 1.6+rnd.Float64()*1.4, p.Body)
		}
	}
	// Caption band.
	caption := p.Caption
	scale := 2
	tw := len(caption) * 6 * scale
	bandTop := photoH - 26
	for y := bandTop; y < photoH; y++ {
		for x := 0; x < photoW; x++ {
			c := img.RGBAAt(x, y)
			img.SetRGBA(x, y, color.RGBA{c.R / 3, c.G / 3, c.B / 3, 255})
		}
	}
	drawText(img, (photoW-tw)/2, bandTop+6, caption, scale, rgb(250, 250, 244))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 72}); err != nil {
		panic("demo: encode sample photo: " + err.Error()) // an in-memory encode of a fixed-size image cannot fail
	}
	return buf.Bytes()
}

func mix(a, b uint8, t float64) float64 { return float64(a)*(1-t) + float64(b)*t }

func clamp8(v float64) uint8 {
	switch {
	case v < 0:
		return 0
	case v > 255:
		return 255
	}
	return uint8(v)
}

func mixRGBA(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{clamp8(mix(a.R, b.R, t)), clamp8(mix(a.G, b.G, t)), clamp8(mix(a.B, b.B, t)), 255}
}

func darker(c color.RGBA) color.RGBA { return mixRGBA(c, rgb(0, 0, 0), 0.25) }

func blend(img *image.RGBA, x, y int, c color.RGBA, alpha float64) {
	if !(image.Point{x, y}).In(img.Rect) {
		return
	}
	o := img.RGBAAt(x, y)
	img.SetRGBA(x, y, color.RGBA{clamp8(mix(o.R, c.R, alpha)), clamp8(mix(o.G, c.G, alpha)), clamp8(mix(o.B, c.B, alpha)), 255})
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			blend(img, x, y, c, 1)
		}
	}
}

func fillCircle(img *image.RGBA, cx, cy, r float64, c color.RGBA) {
	fillEllipse(img, cx, cy, r, r, 0, c, 1)
}

// fillEllipse fills an ellipse rotated by angle, softening the edge by one pixel.
func fillEllipse(img *image.RGBA, cx, cy, rx, ry, angle float64, c color.RGBA, alpha float64) {
	sin, cos := math.Sincos(angle)
	r := math.Max(rx, ry) + 2
	for y := int(cy - r); y <= int(cy+r); y++ {
		for x := int(cx - r); x <= int(cx+r); x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			u, v := dx*cos+dy*sin, -dx*sin+dy*cos
			d := math.Sqrt((u/rx)*(u/rx)+(v/ry)*(v/ry)) - 1
			if d < 0 {
				blend(img, x, y, c, alpha)
			} else if d*math.Min(rx, ry) < 1 {
				blend(img, x, y, c, alpha*(1-d*math.Min(rx, ry)))
			}
		}
	}
}

func fillRoundRect(img *image.RGBA, x0, y0, x1, y1, r float64, c color.RGBA) {
	for y := int(y0); y < int(y1); y++ {
		for x := int(x0); x < int(x1); x++ {
			fx, fy := float64(x), float64(y)
			dx := math.Max(math.Max(x0+r-fx, fx-(x1-r)), 0)
			dy := math.Max(math.Max(y0+r-fy, fy-(y1-r)), 0)
			if dx*dx+dy*dy <= r*r {
				blend(img, x, y, c, 1)
			}
		}
	}
}

func drawLine(img *image.RGBA, x0, y0, x1, y1, w float64, c color.RGBA) {
	n := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))*2) + 1
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		fillCircle(img, x0+(x1-x0)*t, y0+(y1-y0)*t, w/2, c)
	}
}

// drawLeaf draws a pointed leaf (the lens between two circles) from its base
// at (bx, by), pointing along angle (0 is up), with a midrib.
func drawLeaf(img *image.RGBA, bx, by, angle, length, halfWidth float64, body, vein color.RGBA) {
	sin, cos := math.Sincos(angle)
	// Leaf frame: u along the leaf (0 at base, length at tip), v across.
	r := (length*length/4 + halfWidth*halfWidth) / (2 * halfWidth)
	box := length + 4
	for y := int(by - box); y <= int(by+box); y++ {
		for x := int(bx - box); x <= int(bx+box); x++ {
			dx, dy := float64(x)-bx, float64(y)-by
			u := dx*sin - dy*cos
			v := dx*cos + dy*sin
			if u < 0 || u > length {
				continue
			}
			m := u - length/2
			off := r - halfWidth
			if math.Hypot(m, v+off) < r && math.Hypot(m, v-off) < r {
				blend(img, x, y, body, 1)
			}
		}
	}
	drawLine(img, bx, by, bx+length*sin*0.92, by-length*cos*0.92, 1.4, vein)
	for _, f := range []float64{0.35, 0.55, 0.75} {
		px, py := bx+length*f*sin, by-length*f*cos
		for _, s := range []float64{-1, 1} {
			a := angle + s*0.9
			drawLine(img, px, py, px+halfWidth*0.8*math.Sin(a)*(1-f*0.4), py-halfWidth*0.8*math.Cos(a)*(1-f*0.4), 1, vein)
		}
	}
}

// drawInsect draws a top-down insect: abdomen, thorax, head, wings, legs and
// antennae, rotated by the spec's angle.
func drawInsect(img *image.RGBA, cx, cy float64, p photoSpec) {
	sin, cos := math.Sincos(p.Angle)
	at := func(u, v float64) (float64, float64) { return cx + u*cos - v*sin, cy + u*sin + v*cos }
	// Legs and antennae first, under the body.
	for _, u := range []float64{-4, 6, 16} {
		for _, s := range []float64{-1, 1} {
			x0, y0 := at(u, s*8)
			x1, y1 := at(u+8, s*34)
			drawLine(img, x0, y0, x1, y1, 2, p.Body)
		}
	}
	for _, s := range []float64{-1, 1} {
		x0, y0 := at(-30, s*3)
		x1, y1 := at(-52, s*16)
		drawLine(img, x0, y0, x1, y1, 1.5, p.Body)
	}
	// Wings.
	for _, s := range []float64{-1, 1} {
		wx, wy := at(2, s*20)
		fillEllipse(img, wx, wy, 34, 13, p.Angle+s*0.35, mixRGBA(p.Accent, rgb(255, 255, 255), 0.5), 0.55)
	}
	ax, ay := at(26, 0)
	fillEllipse(img, ax, ay, 26, 15, p.Angle, p.Body, 1)
	tx, ty := at(-4, 0)
	fillEllipse(img, tx, ty, 15, 13, p.Angle, p.Body, 1)
	hx, hy := at(-26, 0)
	fillEllipse(img, hx, hy, 11, 10, p.Angle, p.Body, 1)
	// Stripes across the abdomen.
	for _, u := range []float64{16, 28, 40} {
		x0, y0 := at(u, -11)
		x1, y1 := at(u, 11)
		drawLine(img, x0, y0, x1, y1, 4, p.Accent)
	}
}

// ---- bitmap font --------------------------------------------------------

var font5x7 = map[rune][7]string{
	'A': {" ### ", "#   #", "#   #", "#####", "#   #", "#   #", "#   #"},
	'B': {"#### ", "#   #", "#   #", "#### ", "#   #", "#   #", "#### "},
	'C': {" ### ", "#   #", "#    ", "#    ", "#    ", "#   #", " ### "},
	'D': {"#### ", "#   #", "#   #", "#   #", "#   #", "#   #", "#### "},
	'E': {"#####", "#    ", "#    ", "#### ", "#    ", "#    ", "#####"},
	'F': {"#####", "#    ", "#    ", "#### ", "#    ", "#    ", "#    "},
	'G': {" ### ", "#   #", "#    ", "# ###", "#   #", "#   #", " ### "},
	'H': {"#   #", "#   #", "#   #", "#####", "#   #", "#   #", "#   #"},
	'I': {" ### ", "  #  ", "  #  ", "  #  ", "  #  ", "  #  ", " ### "},
	'J': {"  ###", "   # ", "   # ", "   # ", "   # ", "#  # ", " ##  "},
	'K': {"#   #", "#  # ", "# #  ", "##   ", "# #  ", "#  # ", "#   #"},
	'L': {"#    ", "#    ", "#    ", "#    ", "#    ", "#    ", "#####"},
	'M': {"#   #", "## ##", "# # #", "# # #", "#   #", "#   #", "#   #"},
	'N': {"#   #", "##  #", "# # #", "#  ##", "#   #", "#   #", "#   #"},
	'O': {" ### ", "#   #", "#   #", "#   #", "#   #", "#   #", " ### "},
	'P': {"#### ", "#   #", "#   #", "#### ", "#    ", "#    ", "#    "},
	'Q': {" ### ", "#   #", "#   #", "#   #", "# # #", "#  # ", " ## #"},
	'R': {"#### ", "#   #", "#   #", "#### ", "# #  ", "#  # ", "#   #"},
	'S': {" ####", "#    ", "#    ", " ### ", "    #", "    #", "#### "},
	'T': {"#####", "  #  ", "  #  ", "  #  ", "  #  ", "  #  ", "  #  "},
	'U': {"#   #", "#   #", "#   #", "#   #", "#   #", "#   #", " ### "},
	'V': {"#   #", "#   #", "#   #", "#   #", "#   #", " # # ", "  #  "},
	'W': {"#   #", "#   #", "#   #", "# # #", "# # #", "## ##", "#   #"},
	'X': {"#   #", "#   #", " # # ", "  #  ", " # # ", "#   #", "#   #"},
	'Y': {"#   #", "#   #", " # # ", "  #  ", "  #  ", "  #  ", "  #  "},
	'Z': {"#####", "    #", "   # ", "  #  ", " #   ", "#    ", "#####"},
}

func drawText(img *image.RGBA, x, y int, s string, scale int, c color.RGBA) {
	for i, r := range strings.ToUpper(s) {
		g, ok := font5x7[r]
		if !ok {
			continue
		}
		for row, line := range g {
			for col, ch := range line {
				if ch == '#' {
					fillRect(img, x+i*6*scale+col*scale, y+row*scale, x+i*6*scale+(col+1)*scale, y+(row+1)*scale, c)
				}
			}
		}
	}
}
