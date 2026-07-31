// Command genicon renders the tray icon and writes it to
// internal/assets/icon.ico, where it is embedded into the binary.
//
// The icon is drawn once at high resolution and box-filtered down to each of
// the sizes Windows asks for, which keeps the small variants readable without
// hand-tuning them. Output is a classic BMP-payload ICO so it loads on every
// Windows version systray supports.
//
// Regenerate with: go run ./tools/genicon
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
)

// master is the resolution the gauge is drawn at before downsampling.
const master = 256

// Gauge geometry, in master-resolution pixels.
const (
	discRadius    = 124.0
	rimInner      = 116.0
	arcInner      = 86.0
	arcOuter      = 110.0
	needleLength  = 82.0
	needleHalf    = 6.0
	hubRadius     = 13.0
	sweepStartDeg = 135.0 // lower left
	sweepDeg      = 270.0 // clockwise through the top to the lower right
	needleAt      = 0.76  // position of the needle along the sweep, 0..1
)

var (
	discColor   = color.RGBA{17, 24, 39, 255}
	rimColor    = color.RGBA{51, 65, 85, 255}
	needleColor = color.RGBA{241, 245, 249, 255}
	arcStops    = []color.RGBA{
		{34, 197, 94, 255}, // green
		{234, 179, 8, 255}, // amber
		{239, 68, 68, 255}, // red
	}
)

func main() {
	out := filepath.Join("internal", "assets", "icon.ico")
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fail(err)
	}

	src := renderGauge(master)
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

// renderGauge draws the speedometer at size n with alpha-premultiplied pixels.
func renderGauge(n int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, n, n))
	center := float64(n) / 2
	scale := float64(n) / master

	needleRad := angleAt(needleAt) * math.Pi / 180
	tipX := center + math.Cos(needleRad)*needleLength*scale
	tipY := center + math.Sin(needleRad)*needleLength*scale

	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			px := float64(x) + 0.5
			py := float64(y) + 0.5
			dx := px - center
			dy := py - center
			r := math.Hypot(dx, dy) / scale

			var c color.RGBA
			switch {
			case r > discRadius:
				continue // transparent outside the disc
			case r > rimInner:
				c = rimColor
			default:
				c = discColor
			}

			if u, ok := sweepPosition(dx, dy); ok && r >= arcInner && r <= arcOuter {
				c = arcColorAt(u)
			}

			if pointToSegment(px, py, center, center, tipX, tipY) <= needleHalf*scale ||
				math.Hypot(dx, dy) <= hubRadius*scale {
				c = needleColor
			}

			img.SetRGBA(x, y, c)
		}
	}
	return img
}

// angleAt maps a sweep position in 0..1 to a screen-space angle in degrees
// (0 = right, 90 = down), walking clockwise from the lower left.
func angleAt(u float64) float64 {
	return math.Mod(sweepStartDeg+u*sweepDeg, 360)
}

// sweepPosition is the inverse of angleAt: it reports where the given offset
// from the center falls along the gauge sweep, and whether it falls on it.
func sweepPosition(dx, dy float64) (float64, bool) {
	deg := math.Mod(math.Atan2(dy, dx)*180/math.Pi+360, 360)
	rel := deg - sweepStartDeg
	if rel < 0 {
		rel += 360
	}
	if rel > sweepDeg {
		return 0, false
	}
	return rel / sweepDeg, true
}

func arcColorAt(u float64) color.RGBA {
	span := float64(len(arcStops) - 1)
	pos := clamp(u, 0, 1) * span
	i := int(pos)
	if i >= len(arcStops)-1 {
		return arcStops[len(arcStops)-1]
	}
	return lerpColor(arcStops[i], arcStops[i+1], pos-float64(i))
}

func lerpColor(a, b color.RGBA, t float64) color.RGBA {
	mix := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return color.RGBA{mix(a.R, b.R), mix(a.G, b.G), mix(a.B, b.B), 255}
}

// pointToSegment returns the distance from (px,py) to the segment (ax,ay)-(bx,by).
func pointToSegment(px, py, ax, ay, bx, by float64) float64 {
	vx, vy := bx-ax, by-ay
	wx, wy := px-ax, py-ay
	lenSq := vx*vx + vy*vy
	if lenSq == 0 {
		return math.Hypot(wx, wy)
	}
	t := clamp((wx*vx+wy*vy)/lenSq, 0, 1)
	return math.Hypot(wx-t*vx, wy-t*vy)
}

func clamp(v, lo, hi float64) float64 {
	return math.Min(math.Max(v, lo), hi)
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
