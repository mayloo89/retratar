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

	// MaxSide bounds either dimension. It keeps per-row decoder buffers and
	// extreme aspect ratios in check; MaxPixels alone allows 50,000,000x1.
	MaxSide = 8192
	// MaxDecodeBytes is the budget for estimatedPeak: the memory one photo may
	// take to decode and scale.
	MaxDecodeBytes = 256 << 20
)

// scalerCellBytes is the size of one cell of the scaler's temporary buffer,
// a [4]float64 (draw.kernel.Scale in golang.org/x/image/draw).
const scalerCellBytes = 32

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
	// Pixel count alone does not bound memory: a 16-bit PNG needs eight bytes
	// per pixel, the scaler needs more again, and a very wide image can pass
	// the pixel limit with a huge row. Bound the sides and the estimate too.
	if cfg.Width > MaxSide || cfg.Height > MaxSide ||
		int64(cfg.Width)*int64(cfg.Height) > MaxPixels ||
		estimatedPeak(cfg) > MaxDecodeBytes {
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

// scaledSize fits a w x h image's long side to MaxEdge, keeping the aspect
// ratio. It never upscales. The arithmetic is int64 so it cannot overflow on
// a 32-bit platform whatever the input.
func scaledSize(w, h int64) (int64, int64) {
	if long := max(w, h); long > MaxEdge {
		w = max(1, (w*MaxEdge+long/2)/long)
		h = max(1, (h*MaxEdge+long/2)/long)
	}
	return w, h
}

// estimatedPeak is an upper bound on the memory Process needs for an image
// with this header: the decoded image plus the scaler's temporary buffer,
// which holds the destination width by the source height.
func estimatedPeak(cfg image.Config) int64 {
	w, h := int64(cfg.Width), int64(cfg.Height)
	decoded := w * h * bytesPerPixel(cfg.ColorModel)
	// With no scaling needed the destination is as wide as the source; that
	// overstates the buffer, which is fine for a bound.
	sw, _ := scaledSize(w, h)
	return decoded + sw*h*scalerCellBytes
}

// bytesPerPixel is what the decoder allocates per pixel for a colour model.
// Unknown models get the largest size, so they err towards rejection.
func bytesPerPixel(m color.Model) int64 {
	if _, ok := m.(color.Palette); ok {
		return 4 // converted to RGBA when scaled
	}
	switch m {
	case color.RGBA64Model, color.NRGBA64Model:
		return 8
	case color.RGBAModel, color.NRGBAModel, color.CMYKModel:
		return 4
	case color.YCbCrModel:
		return 3 // upper bound for every chroma subsampling
	case color.Gray16Model:
		return 2
	case color.GrayModel:
		return 1
	}
	return 8
}

// scale fits src's long side to MaxEdge, keeping the aspect ratio. It never
// upscales. The result is opaque: src is painted over the paper colour.
func scale(src image.Image) *image.RGBA {
	sb := src.Bounds()
	w64, h64 := scaledSize(int64(sb.Dx()), int64(sb.Dy()))
	w, h := int(w64), int(h64) // at most MaxEdge, or the decoded size
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
