package extractor

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png" // register PNG decoder
	"net/http"
)

var (
	ErrUnsupportedImage = errors.New("unsupported image format")
	ErrImageTooLarge    = errors.New("image dimensions too large")
)

// maxPixels guards against decompression bombs: a small file can declare a
// huge canvas.
const maxPixels = 40_000_000

// SniffImageType trusts magic bytes, never the client's Content-Type.
func SniffImageType(data []byte) string {
	switch ct := http.DetectContentType(data); ct {
	case "image/jpeg", "image/png":
		return ct
	}
	return ""
}

// PrepareImage decodes a JPEG/PNG, shrinks its long side to maxSide and
// re-encodes it as JPEG quality 90, to cut model tokens and latency.
func PrepareImage(data []byte, maxSide int) ([]byte, error) {
	if SniffImageType(data) == "" {
		return nil, ErrUnsupportedImage
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, ErrUnsupportedImage
	}
	if cfg.Width*cfg.Height > maxPixels {
		return nil, ErrImageTooLarge
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, ErrUnsupportedImage
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, downscale(img, maxSide), &jpeg.Options{Quality: 90}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// downscale averages each source block into one destination pixel (box
// filter). Good enough for text on ID cards; no extra dependency.
func downscale(src image.Image, maxSide int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	long := max(w, h)
	if long <= maxSide {
		return src
	}
	nw, nh := max(1, w*maxSide/long), max(1, h*maxSide/long)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy0, sy1 := b.Min.Y+y*h/nh, b.Min.Y+(y+1)*h/nh
		for x := 0; x < nw; x++ {
			sx0, sx1 := b.Min.X+x*w/nw, b.Min.X+(x+1)*w/nw
			var r, g, bl, n uint64
			for sy := sy0; sy < sy1; sy++ {
				for sx := sx0; sx < sx1; sx++ {
					cr, cg, cb, _ := src.At(sx, sy).RGBA()
					r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
				}
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), 255})
		}
	}
	return dst
}
