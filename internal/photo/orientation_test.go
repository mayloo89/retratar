package photo

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

func TestOrientation_Valid(t *testing.T) {
	base := jpegBytes(t, 8, 8, red)
	for _, order := range []binary.ByteOrder{binary.LittleEndian, binary.BigEndian} {
		for want := 1; want <= 8; want++ {
			got := orientation(withExif(base, order, uint16(want), ""))
			if got != want {
				t.Errorf("%v orientation %d: got %d", order, want, got)
			}
		}
	}
}

func TestOrientation_FillBytes(t *testing.T) {
	seg := app1(exifPayload(binary.LittleEndian, 6, ""))
	for _, fill := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d fill bytes", fill), func(t *testing.T) {
			data := withSegment(jpegBytes(t, 8, 8, red), append(bytes.Repeat([]byte{0xFF}, fill), seg...))
			if got := orientation(data); got != 6 {
				t.Errorf("got %d, want 6", got)
			}
		})
	}
}

func TestOrientation_NoExif(t *testing.T) {
	if got := orientation(jpegBytes(t, 8, 8, red)); got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

// malformedCases are inputs orientation must answer 1 for, without panicking.
func malformedCases() map[string][]byte {
	good := exifPayload(binary.LittleEndian, 6, "")
	soi := []byte{0xFF, 0xD8}
	cat := func(parts ...[]byte) []byte {
		var out []byte
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	// tweak returns a copy of the payload with b written at offset i.
	tweak := func(i int, b ...byte) []byte {
		p := append([]byte{}, good...)
		copy(p[i:], b)
		return p
	}
	const tiff = 6 // payload offset where the TIFF header starts

	return map[string][]byte{
		"empty":  {},
		"no SOI": cat([]byte{0x00, 0x01}, app1(good)),
		"segment length runs past end": cat(soi,
			[]byte{0xFF, 0xE1, 0xFF, 0xFF}, good),
		"segment length below minimum": cat(soi, []byte{0xFF, 0xE1, 0x00, 0x01}, good),
		"truncated marker":             cat(soi, []byte{0xFF}),
		"APP1 too short for TIFF header": cat(soi,
			app1(good[:tiff+4])),
		"bad byte order":            cat(soi, app1(tweak(tiff, 'X', 'X'))),
		"bad magic number":          cat(soi, app1(tweak(tiff+2, 43, 0))),
		"IFD offset beyond slice":   cat(soi, app1(tweak(tiff+4, 0xFF, 0, 0, 0))),
		"entry count too large":     cat(soi, app1(tweak(tiff+8, 0xFF, 0xFF))),
		"orientation value 9":       cat(soi, app1(exifPayload(binary.LittleEndian, 9, ""))),
		"orientation value 0":       cat(soi, app1(exifPayload(binary.LittleEndian, 0, ""))),
		"wrong tag type":            cat(soi, app1(tweak(tiff+8+2+2, 4, 0))),
		"offset that overflows int": cat(soi, app1(tweak(tiff+4, 0xFF, 0xFF, 0xFF, 0xFF))),
		"stops at start of scan":    cat(soi, []byte{0xFF, 0xDA, 0x00, 0x02}, app1(good)),
	}
}

func TestOrientation_Malformed(t *testing.T) {
	for name, data := range malformedCases() {
		t.Run(name, func(t *testing.T) {
			if got := orientation(data); got != 1 {
				t.Errorf("got %d, want 1", got)
			}
		})
	}
}

func FuzzOrientation(f *testing.F) {
	seg := app1(exifPayload(binary.LittleEndian, 6, ""))
	f.Add(withSegment(jpegBytes(f, 4, 4, red), append([]byte{0xFF}, seg...)))
	f.Add(withSegment(jpegBytes(f, 4, 4, red), append([]byte{0xFF, 0xFF, 0xFF}, seg...)))
	f.Add(withExif(jpegBytes(f, 4, 4, red), binary.LittleEndian, 6, ""))
	f.Add(withExif(jpegBytes(f, 4, 4, red), binary.BigEndian, 8, "extra"))
	for _, data := range malformedCases() {
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if got := orientation(data); got < 1 || got > 8 {
			t.Fatalf("orientation = %d, want 1-8", got)
		}
	})
}
