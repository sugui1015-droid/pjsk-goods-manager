package paymentqr

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// makePNG returns a minimal valid PNG of the given size.
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 0, G: 0, B: 0, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// makeJPEG returns a minimal valid JPEG.
func makeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// makeWebP builds a tiny but structurally valid lossy (VP8 ) WebP container.
// The pixel payload is not a real VP8 bitstream, but validateWebP verifies the
// RIFF/WEBP container and chunk framing, not the codec bitstream.
func makeWebP(payloadLen int) []byte {
	payload := make([]byte, payloadLen)
	chunk := make([]byte, 0, 8+payloadLen)
	chunk = append(chunk, []byte("VP8 ")...)
	var sizeBuf [4]byte
	binary.LittleEndian.PutUint32(sizeBuf[:], uint32(payloadLen))
	chunk = append(chunk, sizeBuf[:]...)
	chunk = append(chunk, payload...)

	body := append([]byte("WEBP"), chunk...)
	out := make([]byte, 0, 8+len(body))
	out = append(out, []byte("RIFF")...)
	var riffSize [4]byte
	binary.LittleEndian.PutUint32(riffSize[:], uint32(len(body)))
	out = append(out, riffSize[:]...)
	out = append(out, body...)
	return out
}

func TestValidateImageAcceptsPNGJPEGWebP(t *testing.T) {
	cases := []struct {
		name string
		data []byte
		mime string
	}{
		{"png", makePNG(t, 64, 64), "image/png"},
		{"jpeg", makeJPEG(t, 64, 64), "image/jpeg"},
		{"webp", makeWebP(64), "image/webp"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateImage(tc.data)
			if err != nil {
				t.Fatalf("ValidateImage(%s) error = %v", tc.name, err)
			}
			if got.MimeType != tc.mime {
				t.Fatalf("mime = %q, want %q", got.MimeType, tc.mime)
			}
			if got.ByteSize != len(tc.data) {
				t.Fatalf("byte size = %d, want %d", got.ByteSize, len(tc.data))
			}
			if len(got.SHA256) != 64 {
				t.Fatalf("sha256 length = %d, want 64", len(got.SHA256))
			}
		})
	}
}

func TestValidateImageRejectsEmpty(t *testing.T) {
	if _, err := ValidateImage(nil); err != ErrEmptyImage {
		t.Fatalf("err = %v, want ErrEmptyImage", err)
	}
	if _, err := ValidateImage([]byte{}); err != ErrEmptyImage {
		t.Fatalf("err = %v, want ErrEmptyImage", err)
	}
}

func TestValidateImageRejectsOversize(t *testing.T) {
	data := make([]byte, MaxImageBytes+1)
	if _, err := ValidateImage(data); err != ErrImageTooLarge {
		t.Fatalf("err = %v, want ErrImageTooLarge", err)
	}
}

func TestValidateImageRejectsDisguisedAndForeignFormats(t *testing.T) {
	// A real PNG header truncated so DecodeConfig fails: signature only.
	pngSigOnly := []byte("\x89PNG\r\n\x1a\n")

	cases := []struct {
		name string
		data []byte
	}{
		{"html_named_png", []byte("<!DOCTYPE html><html><body>not an image</body></html>")},
		{"svg", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"><rect/></svg>`)},
		{"svg_no_prolog", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`)},
		{"gif", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00,")},
		{"pdf", []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n")},
		{"windows_exe", []byte("MZ\x90\x00\x03\x00\x00\x00\x04\x00\x00\x00")},
		{"elf", []byte("\x7fELF\x02\x01\x01\x00")},
		{"plain_text", []byte("just some plain text that is definitely not an image at all")},
		{"png_signature_only", pngSigOnly},
		{"fake_png_prefix_plus_junk", append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x00, 0xFF}, 32)...)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ValidateImage(tc.data); err == nil {
				t.Fatalf("ValidateImage(%s) = nil error, want rejection", tc.name)
			}
		})
	}
}

func TestValidateImageRejectsPixelBombPNG(t *testing.T) {
	// A valid PNG header declaring absurd dimensions must be rejected by the
	// pixel cap, before any full decode.
	data := makePNG(t, 8, 8)
	// Patch the IHDR width/height (bytes 16..23) to 60000x60000 (3.6e9 px).
	binary.BigEndian.PutUint32(data[16:20], 60000)
	binary.BigEndian.PutUint32(data[20:24], 60000)
	// DecodeConfig reads dimensions from IHDR without validating the CRC, so it
	// will report the patched size; the pixel cap must reject it.
	if _, err := ValidateImage(data); err == nil {
		t.Fatalf("oversized-dimension PNG accepted, want rejection")
	}
}

func TestValidateWebPRejectsBadContainers(t *testing.T) {
	good := makeWebP(32)
	cases := map[string][]byte{
		"too_short":      good[:10],
		"bad_riff_magic": append([]byte("XXXX"), good[4:]...),
		"bad_form_type":  patched(good, 8, []byte("XXXX")),
		"unknown_chunk":  patched(good, 12, []byte("ZZZZ")),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateWebP(data); err == nil {
				t.Fatalf("validateWebP(%s) = nil, want error", name)
			}
		})
	}
	// Sanity: the unmodified container passes.
	if err := validateWebP(good); err != nil {
		t.Fatalf("validateWebP(good) = %v, want nil", err)
	}
}

func patched(src []byte, at int, replacement []byte) []byte {
	out := append([]byte(nil), src...)
	copy(out[at:], replacement)
	return out
}

func TestValidateImageErrorMessagesAreChinese(t *testing.T) {
	// The user-facing messages must be Chinese (no raw English leaking through).
	for _, err := range []error{ErrEmptyImage, ErrImageTooLarge, ErrUnsupportedImage} {
		if !strings.ContainsAny(err.Error(), "图片支持格式二维码大小") {
			t.Fatalf("error message not Chinese: %q", err.Error())
		}
	}
}
