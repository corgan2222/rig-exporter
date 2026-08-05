// Command genicon packs the application artwork into the forms Windows and the
// web interface need, and writes them to internal/assets.
//
// The gauge used to be drawn here in code. It is a designed asset now, kept as
// PNG under docs/images, and this only repackages it: an ICO for the tray, a
// resource object so Explorer and the taskbar show it too, and one PNG the web
// interface serves. Deriving all three from one source is the point — three
// pictures of the same program that disagree is worse than none of them.
//
// Regenerate with: .\build.ps1 -Icon
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
)

// artwork is the largest rendering of the mark, and the source every output
// below is filtered down from.
const artwork = "docs/images/rig-exporter-entity-512.png"

// servedSize is the PNG the web interface serves, for its own header and for
// the picture on the Home Assistant update card. The card draws it at about
// forty pixels and a dense display asks for three times that.
const servedSize = 256

func main() {
	out := filepath.Join("internal", "assets", "icon.ico")
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fail(err)
	}

	src, err := loadArtwork(artwork)
	if err != nil {
		fail(err)
	}

	// Tray, taskbar and Alt-Tab never ask for more than 64px here, and each
	// larger frame costs w*h*4 bytes uncompressed in the binary.
	sizes := []int{16, 24, 32, 48, 64}
	frames := make([]*image.RGBA, 0, len(sizes))
	for _, s := range sizes {
		frames = append(frames, downsample(src, s))
	}

	data, err := encodeICO(frames)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %s (%d bytes, %d frames)\n", out, len(data), len(frames))

	pngPath := filepath.Join(filepath.Dir(out), "icon.png")
	var buf bytes.Buffer
	if err := png.Encode(&buf, downsample(src, servedSize)); err != nil {
		fail(err)
	}
	if err := os.WriteFile(pngPath, buf.Bytes(), 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", pngPath, buf.Len())

	// The same icon again, as a Windows resource object. The Go linker picks
	// any .syso up out of the main package directory, which is what gives the
	// executable its icon in Explorer and on the taskbar.
	syso, err := encodeSyso(frames)
	if err != nil {
		fail(err)
	}
	// It has to sit in the main package's directory, which is where this is
	// run from.
	sysoPath := sysoName
	if len(os.Args) > 2 {
		sysoPath = os.Args[2]
	}
	if err := os.WriteFile(sysoPath, syso, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", sysoPath, len(syso))
}

// sysoName restricts the resource object to the architecture it was built
// for, which is how the Go toolchain decides whether to link it.
const sysoName = "rsrc_windows_amd64.syso"

func fail(err error) {
	fmt.Fprintln(os.Stderr, "genicon:", err)
	os.Exit(1)
}

// loadArtwork reads the source PNG and hands back pixels the filters below can
// work on. Nothing else in this program knows how the mark is drawn.
func loadArtwork(path string) (*image.RGBA, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	bounds := decoded.Bounds()
	if bounds.Dx() != bounds.Dy() {
		return nil, fmt.Errorf("%s is %dx%d; the mark has to be square", path, bounds.Dx(), bounds.Dy())
	}

	// Into RGBA at the origin, because every filter here indexes from nought
	// and expects premultiplied alpha.
	img := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(img, img.Bounds(), decoded, bounds.Min, draw.Src)
	return img, nil
}

// downsample box-filters src down to size x size. Averaging is done on
// premultiplied values, which is what image.RGBA already stores, so edge
// pixels blend toward transparent instead of toward black.
func downsample(src *image.RGBA, size int) *image.RGBA {
	n := src.Bounds().Dx()
	if size == n {
		clone := image.NewRGBA(src.Bounds())
		copy(clone.Pix, src.Pix)
		return clone
	}
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	step := float64(n) / float64(size)
	for y := 0; y < size; y++ {
		y0, y1 := boxRange(y, step, n)
		for x := 0; x < size; x++ {
			x0, x1 := boxRange(x, step, n)
			var sr, sg, sb, sa, count float64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := src.RGBAAt(sx, sy)
					sr += float64(c.R)
					sg += float64(c.G)
					sb += float64(c.B)
					sa += float64(c.A)
					count++
				}
			}
			if count == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{
				R: uint8(sr/count + 0.5),
				G: uint8(sg/count + 0.5),
				B: uint8(sb/count + 0.5),
				A: uint8(sa/count + 0.5),
			})
		}
	}
	return dst
}

func boxRange(i int, step float64, limit int) (int, int) {
	lo := int(float64(i) * step)
	hi := int(float64(i+1) * step)
	if hi <= lo {
		hi = lo + 1
	}
	if hi > limit {
		hi = limit
	}
	return lo, hi
}

// encodeICO packs the frames into an ICO file using 32-bit BMP payloads.
func encodeICO(frames []*image.RGBA) ([]byte, error) {
	if len(frames) == 0 || len(frames) > 0xFFFF {
		return nil, fmt.Errorf("encodeICO: unsupported frame count %d", len(frames))
	}

	payloads := make([][]byte, len(frames))
	for i, f := range frames {
		payloads[i] = bmpPayload(f)
	}

	var buf bytes.Buffer
	write(&buf, uint16(0), uint16(1), uint16(len(frames))) // ICONDIR

	offset := uint32(6 + 16*len(frames))
	for i, f := range frames {
		w, h := f.Bounds().Dx(), f.Bounds().Dy()
		buf.WriteByte(dimByte(w))
		buf.WriteByte(dimByte(h))
		buf.WriteByte(0) // palette entries
		buf.WriteByte(0) // reserved
		write(&buf, uint16(1), uint16(32), uint32(len(payloads[i])), offset)
		offset += uint32(len(payloads[i]))
	}
	for _, p := range payloads {
		buf.Write(p)
	}
	return buf.Bytes(), nil
}

// dimByte encodes an ICO dimension; 256 is stored as 0.
func dimByte(v int) byte {
	if v >= 256 {
		return 0
	}
	return byte(v)
}

func bmpPayload(img *image.RGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	maskStride := ((w + 31) / 32) * 4
	xorSize := w * h * 4
	andSize := maskStride * h

	var b bytes.Buffer
	// BITMAPINFOHEADER. Height is doubled because it covers XOR + AND bitmaps.
	write(&b,
		uint32(40), int32(w), int32(h*2),
		uint16(1), uint16(32),
		uint32(0), uint32(xorSize+andSize),
		int32(0), int32(0), uint32(0), uint32(0),
	)

	// XOR bitmap: bottom-up rows of straight-alpha BGRA.
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			r, g, bl, a := unpremultiply(img.RGBAAt(x, y))
			b.WriteByte(bl)
			b.WriteByte(g)
			b.WriteByte(r)
			b.WriteByte(a)
		}
	}
	// AND bitmap: all zeros, since the alpha channel already carries the mask.
	b.Write(make([]byte, andSize))
	return b.Bytes()
}

func unpremultiply(c color.RGBA) (r, g, b, a uint8) {
	if c.A == 0 {
		return 0, 0, 0, 0
	}
	if c.A == 255 {
		return c.R, c.G, c.B, 255
	}
	scale := func(v uint8) uint8 {
		out := int(v) * 255 / int(c.A)
		if out > 255 {
			out = 255
		}
		return uint8(out)
	}
	return scale(c.R), scale(c.G), scale(c.B), c.A
}

func write(b *bytes.Buffer, values ...any) {
	for _, v := range values {
		// bytes.Buffer never fails a write, so the error cannot be actioned.
		_ = binary.Write(b, binary.LittleEndian, v)
	}
}
