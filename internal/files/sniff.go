package files

import (
	"bytes"
	"encoding/binary"
	"image"
	_ "image/gif"  // register the GIF decoder for image.DecodeConfig and thumbnails
	_ "image/jpeg" // register the JPEG decoder
	_ "image/png"  // register the PNG decoder
	"io"
	"net/http"
	"path"
	"strings"

	"abidata/internal/fault"
)

// MaxPixels is the largest image (width x height) accepted: 40 megapixels. The
// dimensions come from image.DecodeConfig, which reads only the header, so a
// decompression bomb is refused before anything is decoded.
const MaxPixels = 40_000_000

const sniffLen = 512

// servable lists the media types a file may be served under. Anything else is
// served as application/octet-stream, so a stored "text/html" or "image/svg+xml"
// can never be interpreted by a browser.
var servable = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/bmp": true,
	"image/heic": true, "image/heif": true, "image/vnd.microsoft.icon": true,
	"application/pdf": true, "text/plain": true, "text/csv": true, "text/markdown": true, "application/json": true,
	"application/zip": true, "application/gzip": true,
	"audio/mpeg": true, "audio/ogg": true, "audio/wave": true, "audio/aiff": true, "audio/midi": true,
	"video/mp4": true, "video/webm": true, "video/mpeg": true, "video/avi": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
	"application/vnd.oasis.opendocument.text":                                   true,
	"application/vnd.oasis.opendocument.spreadsheet":                            true,
	"application/vnd.oasis.opendocument.presentation":                           true,
	"application/octet-stream":                                                  true,
}

// inlineTypes are safe raster images a browser may render in place.
var inlineTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// textByExt refines "this is plain text" using the extension. It never widens
// what a browser may execute: every result is a passive text type.
var textByExt = map[string]string{
	".csv": "text/csv", ".json": "application/json", ".md": "text/markdown", ".markdown": "text/markdown", ".txt": "text/plain",
	".log": "text/plain", ".tsv": "text/plain",
}

// zipByExt refines a ZIP container (Office and OpenDocument files are ZIPs).
var zipByExt = map[string]string{
	".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".odt":  "application/vnd.oasis.opendocument.text",
	".ods":  "application/vnd.oasis.opendocument.spreadsheet",
	".odp":  "application/vnd.oasis.opendocument.presentation",
}

// opaqueByExt names phone and camera formats http.DetectContentType does not know.
var opaqueByExt = map[string]string{".heic": "image/heic", ".heif": "image/heif"}

// SniffType decides a file's content type from its first bytes (and its name
// for containers whose signature is generic). The type the client declared is
// never consulted. The result is a bare media type without parameters.
func SniffType(name string, head []byte) string {
	if len(head) > sniffLen {
		head = head[:sniffLen]
	}
	t := http.DetectContentType(head)
	if i := strings.IndexByte(t, ';'); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	if isWebP(head) {
		return "image/webp"
	}
	ext := strings.ToLower(path.Ext(name))
	switch t {
	case "text/plain":
		if r, ok := textByExt[ext]; ok {
			return r
		}
	case "application/zip":
		if r, ok := zipByExt[ext]; ok {
			return r
		}
	case "application/octet-stream":
		if r, ok := opaqueByExt[ext]; ok && isHEIF(head) {
			return r
		}
	}
	return t
}

func isWebP(b []byte) bool {
	return len(b) >= 12 && string(b[0:4]) == "RIFF" && string(b[8:12]) == "WEBP"
}

func isHEIF(b []byte) bool {
	return len(b) >= 12 && string(b[4:8]) == "ftyp"
}

// ServeType is the media type to send for a stored content type.
func ServeType(stored string) string {
	if servable[stored] {
		return stored
	}
	return "application/octet-stream"
}

// Inline reports whether stored is a safe raster image that may be shown in place.
func Inline(stored string) bool { return inlineTypes[stored] }

// IsImage reports whether stored is a raster image this package reads dimensions for.
func IsImage(stored string) bool { return inlineTypes[stored] }

// ImageSize reads an image's dimensions from its header (r starts at the file's
// first byte) and refuses images over MaxPixels, without decoding any pixels.
func ImageSize(stored string, r io.Reader) (w, h int, err error) {
	if stored == "image/webp" {
		head := make([]byte, 64)
		n, _ := io.ReadFull(r, head)
		w, h, ok := webpSize(head[:n])
		if !ok {
			return 0, 0, fault.New(fault.BadRequest, "The file is not a readable WebP image.")
		}
		return w, h, checkPixels(w, h)
	}
	cfg, _, derr := image.DecodeConfig(r)
	if derr != nil {
		return 0, 0, fault.New(fault.BadRequest, "The file is not a readable image.")
	}
	return cfg.Width, cfg.Height, checkPixels(cfg.Width, cfg.Height)
}

func checkPixels(w, h int) error {
	if w <= 0 || h <= 0 {
		return fault.New(fault.BadRequest, "The image has no pixels.")
	}
	if int64(w)*int64(h) > MaxPixels {
		return fault.Newf(fault.BadRequest, "The image is too large: %d x %d pixels is more than the %d megapixel limit.", w, h, MaxPixels/1_000_000)
	}
	return nil
}

// webpSize reads the canvas size from a VP8, VP8L or VP8X WebP header.
func webpSize(b []byte) (w, h int, ok bool) {
	if !isWebP(b) || len(b) < 30 {
		return 0, 0, false
	}
	switch string(b[12:16]) {
	case "VP8X":
		w = 1 + int(b[24]) | int(b[25])<<8 | int(b[26])<<16
		h = 1 + int(b[27]) | int(b[28])<<8 | int(b[29])<<16
		return w, h, true
	case "VP8L":
		if b[20] != 0x2f {
			return 0, 0, false
		}
		v := binary.LittleEndian.Uint32(b[21:25])
		return int(v&0x3fff) + 1, int((v>>14)&0x3fff) + 1, true
	case "VP8 ":
		if !bytes.Equal(b[23:26], []byte{0x9d, 0x01, 0x2a}) {
			return 0, 0, false
		}
		return int(binary.LittleEndian.Uint16(b[26:28]) & 0x3fff), int(binary.LittleEndian.Uint16(b[28:30]) & 0x3fff), true
	}
	return 0, 0, false
}
