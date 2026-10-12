package files

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"io"
)

// ThumbMax is the longest side of a thumbnail in pixels.
const ThumbMax = 320

// thumbSlots bounds concurrent thumbnail decodes: a 40 megapixel image needs a
// few hundred MB to decode, so only a couple may run at once.
var thumbSlots = make(chan struct{}, 2)

// MakeThumbnail decodes a JPEG, PNG or GIF (the standard library decoders; WebP
// has no thumbnail) and returns a JPEG of at most ThumbMax pixels on a side.
// JPEG orientation from EXIF is applied, so a phone photo taken in portrait
// stays upright. Transparent areas are composited on white. The caller has
// already checked the pixel count with ImageSize.
func MakeThumbnail(ctx context.Context, src io.Reader, stored string) ([]byte, error) {
	select {
	case thumbSlots <- struct{}{}:
		defer func() { <-thumbSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	raw, err := io.ReadAll(io.LimitReader(src, 64<<20))
	if err != nil {
		return nil, err
	}
	// Re-check the dimensions here too, so this function is safe on its own.
	if _, _, err := ImageSize(stored, bytes.NewReader(raw)); err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	orientation := 1
	if stored == "image/jpeg" {
		orientation = exifOrientation(raw)
	}
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dw, dh := sw, sh
	if sw > ThumbMax || sh > ThumbMax {
		if sw >= sh {
			dw, dh = ThumbMax, max(1, sh*ThumbMax/sw)
		} else {
			dw, dh = max(1, sw*ThumbMax/sh), ThumbMax
		}
	}
	out := orient(scaleBox(img, dw, dh), orientation)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// scaleBox shrinks src to dw x dh by averaging up to 4x4 samples per output
// pixel (enough for a preview, and bounded work on huge sources), flattening
// transparency on white.
func scaleBox(src image.Image, dw, dh int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		y0, y1 := y*sh/dh, max((y+1)*sh/dh, y*sh/dh+1)
		ystep := max(1, (y1-y0)/4)
		for x := range dw {
			x0, x1 := x*sw/dw, max((x+1)*sw/dw, x*sw/dw+1)
			xstep := max(1, (x1-x0)/4)
			var r, g, bl, a, n uint64
			for yy := y0; yy < y1; yy += ystep {
				for xx := x0; xx < x1; xx += xstep {
					cr, cg, cb, ca := src.At(b.Min.X+xx, b.Min.Y+yy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			// Premultiplied average over a white backdrop.
			white := 0xffff - a/n
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(min(255, (r/n+white)>>8)), G: uint8(min(255, (g/n+white)>>8)), B: uint8(min(255, (bl/n+white)>>8)), A: 0xff, //nolint:gosec // clamped to a byte
			})
		}
	}
	return dst
}

// orient applies an EXIF orientation (1-8) so the result displays upright.
func orient(src *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		for x := range dw {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			dst.SetRGBA(x, y, src.RGBAAt(sx, sy))
		}
	}
	return dst
}

// exifOrientation reads the orientation tag (0x0112) from a JPEG's Exif block;
// 1 (upright) when absent or malformed.
func exifOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		if marker == 0xDA || marker == 0xD9 { // start of scan / end: no more metadata
			return 1
		}
		size := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if size < 2 || i+2+size > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+size]
		if marker == 0xE1 && len(seg) >= 14 && string(seg[:6]) == "Exif\x00\x00" {
			return tiffOrientation(seg[6:])
		}
		i += 2 + size
	}
	return 1
}

func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	off := int(bo.Uint32(t[4:8]))
	if off < 8 || off+2 > len(t) {
		return 1
	}
	n := int(bo.Uint16(t[off : off+2]))
	for k := range n {
		e := off + 2 + k*12
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:e+2]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8 : e+10])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}
