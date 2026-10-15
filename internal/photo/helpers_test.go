package photo

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

var (
	red  = color.RGBA{R: 255, A: 255}
	blue = color.RGBA{B: 255, A: 255}
)

// jpegBytes encodes a w x h JPEG filled with c.
func jpegBytes(tb testing.TB, w, h int, c color.Color) []byte {
	tb.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill(img, img.Bounds(), c)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// redQuadrantJPEG is a 40x20 JPEG: blue, with a red top-left quadrant.
func redQuadrantJPEG(tb testing.TB) []byte {
	tb.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	fill(img, img.Bounds(), blue)
	fill(img, image.Rect(0, 0, 20, 10), red)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

func fill(img *image.RGBA, r image.Rectangle, c color.Color) {
	cr := color.RGBAModel.Convert(c).(color.RGBA)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, cr)
		}
	}
}

func pngBytes(tb testing.TB, img image.Image) []byte {
	tb.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		tb.Fatal(err)
	}
	return buf.Bytes()
}

// exifPayload builds the body of an APP1 segment: "Exif\0\0", a TIFF header
// in the given byte order, and an IFD0 holding one orientation entry. extra
// is appended after the TIFF data.
func exifPayload(order binary.ByteOrder, orient uint16, extra string) []byte {
	var tiff bytes.Buffer
	if order == binary.LittleEndian {
		tiff.WriteString("II")
	} else {
		tiff.WriteString("MM")
	}
	put16 := func(v uint16) { _ = binary.Write(&tiff, order, v) }
	put32 := func(v uint32) { _ = binary.Write(&tiff, order, v) }
	put16(42)
	put32(8)      // IFD0 follows the header
	put16(1)      // one entry
	put16(0x0112) // orientation
	put16(3)      // SHORT
	put32(1)      // count
	put16(orient) // value, left-justified in the 4-byte field
	put16(0)
	put32(0) // no next IFD
	tiff.WriteString(extra)
	return append([]byte("Exif\x00\x00"), tiff.Bytes()...)
}

// app1 wraps a payload as an APP1 segment.
func app1(payload []byte) []byte {
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(payload)+2))
	return append(seg, payload...)
}

// withSegment inserts seg right after the SOI marker of a JPEG.
func withSegment(jpg, seg []byte) []byte {
	out := append([]byte{}, jpg[:2]...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

// withExif inserts an Exif APP1 segment right after SOI.
func withExif(jpg []byte, order binary.ByteOrder, orient uint16, extra string) []byte {
	return withSegment(jpg, app1(exifPayload(order, orient, extra)))
}

// bombPNG has a valid signature and IHDR declaring w x h, followed by
// garbage instead of image data.
func bombPNG(w, h uint32) []byte {
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8] = 8 // bit depth
	ihdr[9] = 2 // colour type: truecolour

	out := []byte("\x89PNG\r\n\x1a\n")
	out = binary.BigEndian.AppendUint32(out, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	out = append(out, chunk...)
	out = binary.BigEndian.AppendUint32(out, crc32.ChecksumIEEE(chunk))
	return append(out, bytes.Repeat([]byte{0xAB}, 64)...)
}
