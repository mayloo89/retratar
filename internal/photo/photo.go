// Package photo turns an untrusted uploaded image into a safe, small JPEG.
//
// It is made of pure functions: no HTTP, no database, no disk. The output is
// always a freshly encoded JPEG, so nothing from the upload survives except
// its pixels: no EXIF, no GPS, no embedded thumbnails.
package photo

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // registers the PNG decoder for image.DecodeConfig and image.Decode
	"io"

	"golang.org/x/image/draw"
)

// Limits and output settings. The byte and pixel limits bound the work one
// upload can cause.
const (
	MaxEdge   = 1200       // longest side of the output, in pixels
	MaxPixels = 50_000_000 // width*height above this is rejected before decoding
	MaxBytes  = 10 << 20   // largest accepted upload; task 17 also enforces it at the HTTP layer
	Quality   = 82         // JPEG output quality
)

// Errors Process returns; match them with errors.Is.
var (
	ErrTooLarge          = errors.New("photo: image too large")    // bytes > MaxBytes, or pixels > MaxPixels
	ErrUnsupportedFormat = errors.New("photo: unsupported format") // decodes, but is not JPEG or PNG
	ErrInvalidImage      = errors.New("photo: invalid image")      // not an image, truncated, zero-sized
)

// paper is the page background. Transparent pixels are painted over it
// because JPEG has no alpha channel and would otherwise turn them black.
var paper = color.RGBA{R: 0xff, G: 0xfd, B: 0xf9, A: 0xff}

// Result is a processed photo, ready to store and serve.
type Result struct {
	JPEG          []byte
	Width, Height int // of the output, after rotation; for <img width height>
}

// Process reads at most MaxBytes from r and returns the re-encoded photo.
func Process(r io.Reader) (Result, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return Result{}, fmt.Errorf("photo: read upload: %w", err)
	}
	if len(data) > MaxBytes {
		return Result{}, ErrTooLarge
	}

	// The header alone declares the dimensions. Checking them before any
	// pixel is decoded is what stops a small file from claiming a huge
	// canvas and exhausting memory.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrInvalidImage, err)
	}
	// The decoder registry is global: another package may register GIF or
	// WebP, so the accepted formats are named here, not inferred.
	if format != "jpeg" && format != "png" {
		return Result{}, fmt.Errorf("%w: %s", ErrUnsupportedFormat, format)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return Result{}, fmt.Errorf("%w: zero-sized", ErrInvalidImage)
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return Result{}, ErrTooLarge
	}

	orient := 1
	if format == "jpeg" {
		orient = orientation(data)
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrInvalidImage, err)
	}

	// Scale before rotating: turning a 1200px image is cheap, turning a
	// 50 megapixel one is not.
	out := orient8(scale(src), orient)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, out, &jpeg.Options{Quality: Quality}); err != nil {
		return Result{}, fmt.Errorf("photo: encode: %w", err)
	}
	b := out.Bounds()
	return Result{JPEG: buf.Bytes(), Width: b.Dx(), Height: b.Dy()}, nil
}

// scale fits src's long side to MaxEdge, keeping the aspect ratio. It never
// upscales. The result is opaque: src is painted over the paper colour.
func scale(src image.Image) *image.RGBA {
	sb := src.Bounds()
	w, h := sb.Dx(), sb.Dy()
	if long := max(w, h); long > MaxEdge {
		w = max(1, (w*MaxEdge+long/2)/long)
		h = max(1, (h*MaxEdge+long/2)/long)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(paper), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, draw.Over, nil)
	return dst
}

// orient8 applies EXIF orientation o (1-8) so the image displays upright.
// Values 5-8 swap width and height. Any other value leaves src unchanged.
func orient8(src *image.RGBA, o int) *image.RGBA {
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
			var sx, sy int // the source pixel that lands at (x, y)
			switch o {
			case 2: // flip horizontally
				sx, sy = w-1-x, y
			case 3: // rotate 180
				sx, sy = w-1-x, h-1-y
			case 4: // flip vertically
				sx, sy = x, h-1-y
			case 5: // transpose
				sx, sy = y, x
			case 6: // rotate 90 clockwise
				sx, sy = y, h-1-x
			case 7: // transverse
				sx, sy = w-1-y, h-1-x
			case 8: // rotate 90 counter-clockwise
				sx, sy = w-1-y, x
			}
			si := src.PixOffset(sx, sy)
			di := dst.PixOffset(x, y)
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return dst
}
