package photo

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"math/rand/v2"
	"testing"
)

func process(t *testing.T, data []byte) (Result, image.Image) {
	t.Helper()
	res, err := Process(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	img, err := jpeg.Decode(bytes.NewReader(res.JPEG))
	if err != nil {
		t.Fatalf("output is not a JPEG: %v", err)
	}
	return res, img
}

func TestProcess_ResizesLongEdge(t *testing.T) {
	tests := []struct{ inW, inH, wantW, wantH int }{
		{3000, 2000, 1200, 800},
		{2000, 3000, 800, 1200},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%dx%d", tt.inW, tt.inH), func(t *testing.T) {
			res, img := process(t, jpegBytes(t, tt.inW, tt.inH, red))
			if res.Width != tt.wantW || res.Height != tt.wantH {
				t.Errorf("Result = %dx%d, want %dx%d", res.Width, res.Height, tt.wantW, tt.wantH)
			}
			if b := img.Bounds(); b.Dx() != tt.wantW || b.Dy() != tt.wantH {
				t.Errorf("image = %dx%d, want %dx%d", b.Dx(), b.Dy(), tt.wantW, tt.wantH)
			}
		})
	}
}

func TestProcess_NoUpscale(t *testing.T) {
	res, _ := process(t, jpegBytes(t, 300, 200, red))
	if res.Width != 300 || res.Height != 200 {
		t.Errorf("got %dx%d, want 300x200", res.Width, res.Height)
	}
}

func TestProcess_Orientation(t *testing.T) {
	// Where the red top-left quadrant of the 40x20 source ends up, as
	// fractions of the output (0 = left/top, 1 = right/bottom).
	tests := []struct {
		orient       uint16
		wantW, wantH int
		right, below bool
	}{
		{1, 40, 20, false, false},
		{2, 40, 20, true, false}, // flip horizontally
		{3, 40, 20, true, true},  // rotate 180
		{4, 40, 20, false, true}, // flip vertically
		{5, 20, 40, false, false},
		{6, 20, 40, true, false}, // rotate 90 clockwise
		{7, 20, 40, true, true},
		{8, 20, 40, false, true}, // rotate 90 counter-clockwise
	}
	src := redQuadrantJPEG(t)
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for _, tt := range tests {
			t.Run(fmt.Sprintf("%v/%d", order, tt.orient), func(t *testing.T) {
				res, img := process(t, withExif(src, order, tt.orient, ""))
				if res.Width != tt.wantW || res.Height != tt.wantH {
					t.Fatalf("size = %dx%d, want %dx%d", res.Width, res.Height, tt.wantW, tt.wantH)
				}
				// Sample the centre of each quadrant: red in exactly one.
				for _, right := range []bool{false, true} {
					for _, below := range []bool{false, true} {
						x, y := res.Width/4, res.Height/4
						if right {
							x = res.Width * 3 / 4
						}
						if below {
							y = res.Height * 3 / 4
						}
						isRed := redish(img.At(x, y))
						wantRed := right == tt.right && below == tt.below
						if isRed != wantRed {
							t.Errorf("quadrant right=%v below=%v: red=%v, want %v (pixel %v)",
								right, below, isRed, wantRed, img.At(x, y))
						}
					}
				}
			})
		}
	}
}

func redish(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r>>8 > 180 && g>>8 < 90 && b>>8 < 90
}

func TestProcess_StripsMetadata(t *testing.T) {
	const marker = "GPS-SECRET-MARKER"
	in := withExif(jpegBytes(t, 50, 50, red), binary.LittleEndian, 1, marker)
	if !bytes.Contains(in, []byte(marker)) {
		t.Fatal("test input does not contain the marker")
	}
	res, _ := process(t, in)
	if bytes.Contains(res.JPEG, []byte(marker)) {
		t.Error("output contains the marker")
	}
	if bytes.Contains(res.JPEG, []byte("Exif")) {
		t.Error("output contains an Exif header")
	}
	if m := appMarkers(t, res.JPEG); len(m) != 0 {
		t.Errorf("output has APP segments: %x", m)
	}
}

// appMarkers lists the APPn markers (0xE0-0xEF) before the start of scan.
func appMarkers(t *testing.T, data []byte) []byte {
	t.Helper()
	var found []byte
	pos := 2
	for pos+4 <= len(data) && data[pos] == 0xFF && data[pos+1] != 0xDA {
		if m := data[pos+1]; m >= 0xE0 && m <= 0xEF {
			found = append(found, m)
		}
		pos += 2 + int(binary.BigEndian.Uint16(data[pos+2:]))
	}
	return found
}

func TestProcess_TransparentPNG(t *testing.T) {
	_, img := process(t, pngBytes(t, image.NewNRGBA(image.Rect(0, 0, 10, 10))))
	r, g, b, _ := img.At(5, 5).RGBA()
	if r>>8 <= 240 || g>>8 <= 240 || b>>8 <= 240 {
		t.Errorf("centre pixel = (%d,%d,%d), want all > 240", r>>8, g>>8, b>>8)
	}
}

func TestProcess_RejectsBomb(t *testing.T) {
	_, err := Process(bytes.NewReader(bombPNG(10000, 10000)))
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestProcess_RejectsOversizeBytes(t *testing.T) {
	_, err := Process(bytes.NewReader(make([]byte, MaxBytes+1)))
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("err = %v, want ErrTooLarge", err)
	}
}

func TestProcess_RejectsUnsupported(t *testing.T) {
	var buf bytes.Buffer
	if err := gif.Encode(&buf, image.NewPaletted(image.Rect(0, 0, 4, 4), color.Palette{red, blue}), nil); err != nil {
		t.Fatal(err)
	}
	_, err := Process(&buf)
	if !errors.Is(err, ErrUnsupportedFormat) {
		t.Errorf("err = %v, want ErrUnsupportedFormat", err)
	}
}

func TestProcess_RejectsInvalid(t *testing.T) {
	random := make([]byte, 1000)
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range random {
		random[i] = byte(rng.UintN(256))
	}
	whole := jpegBytes(t, 200, 200, red)
	tests := map[string][]byte{
		"random bytes":   random,
		"empty":          {},
		"truncated JPEG": whole[:len(whole)/2],
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Process(bytes.NewReader(data))
			if !errors.Is(err, ErrInvalidImage) {
				t.Errorf("err = %v, want ErrInvalidImage", err)
			}
		})
	}
}

func BenchmarkProcess_12MP(b *testing.B) {
	data := jpegBytes(b, 4000, 3000, red)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := Process(bytes.NewReader(data)); err != nil {
			b.Fatal(err)
		}
	}
}
