package extractor

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func encodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pngHeaderOnly builds a PNG whose IHDR claims w×h; DecodeConfig reads only this.
func pngHeaderOnly(w, h uint32) []byte { return pngHeaderWith(w, h, 8, 2) }

// pngHeaderWith is pngHeaderOnly with an explicit bit depth and color type.
func pngHeaderWith(w, h uint32, depth, colorType byte) []byte {
	var b bytes.Buffer
	b.Write([]byte("\x89PNG\r\n\x1a\n"))
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = depth, colorType
	binary.Write(&b, binary.BigEndian, uint32(len(ihdr)))
	chunk := append([]byte("IHDR"), ihdr...)
	b.Write(chunk)
	binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func TestSniffImageType(t *testing.T) {
	var gifBuf bytes.Buffer
	_ = gif.Encode(&gifBuf, image.NewPaletted(image.Rect(0, 0, 2, 2), []color.Color{color.Black}), nil)
	cases := map[string]string{
		string(encodePNG(t, 4, 4)): "image/png",
		"not an image":             "",
		gifBuf.String():            "",
		"\x00\x00\x00\x18ftypheic": "",
	}
	for in, want := range cases {
		if got := SniffImageType([]byte(in)); got != want {
			t.Errorf("sniff = %q want %q", got, want)
		}
	}
}

func TestPrepareImageDownscalesToJPEG(t *testing.T) {
	out, err := PrepareImage(encodePNG(t, 3200, 1000), 1600)
	if err != nil {
		t.Fatal(err)
	}
	if SniffImageType(out) != "image/jpeg" {
		t.Fatal("output is not JPEG")
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 1600 || cfg.Height != 500 {
		t.Fatalf("size = %dx%d, want 1600x500", cfg.Width, cfg.Height)
	}
}

func TestPrepareImageKeepsSmallImageSize(t *testing.T) {
	out, err := PrepareImage(encodePNG(t, 800, 600), 1600)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := jpeg.DecodeConfig(bytes.NewReader(out))
	if cfg.Width != 800 || cfg.Height != 600 {
		t.Fatalf("size = %dx%d", cfg.Width, cfg.Height)
	}
}

func TestPrepareImageRejects(t *testing.T) {
	if _, err := PrepareImage([]byte("hello"), 1600); err != ErrUnsupportedImage {
		t.Fatalf("text: %v", err)
	}
	if _, err := PrepareImage(pngHeaderOnly(10000, 9000), 1600); err != ErrImageTooLarge {
		t.Fatalf("90MP: %v", err)
	}
}

func TestPrepareImageBudgetsDecodedBytes(t *testing.T) {
	// 6400x6250 = 40 MP (at the pixel cap) but 16-bit RGBA decodes to ~305 MiB.
	if _, err := PrepareImage(pngHeaderWith(6400, 6250, 16, 6), 1600); !errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("16-bit 40MP: %v", err)
	}
	// 8-bit RGB 4000x3000 passes the config stage; the missing IDAT fails later.
	_, err := PrepareImage(pngHeaderWith(4000, 3000, 8, 2), 1600)
	if errors.Is(err, ErrImageTooLarge) {
		t.Fatalf("8-bit 12MP must not be too large: %v", err)
	}
}
