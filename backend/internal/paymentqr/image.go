package paymentqr

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image/jpeg"
	"image/png"
	"net/http"
)

// MaxImageBytes bounds an uploaded QR image at 5 MiB. It is enforced both by the
// HTTP layer (http.MaxBytesReader) and re-checked here so the validation is
// correct even when called directly.
const MaxImageBytes = 5 << 20

// maxImagePixels caps decoded dimensions so a small file that declares enormous
// dimensions (a decompression bomb) is rejected without ever allocating the
// full pixel buffer. A collection QR code is well under 1 megapixel; 25 MP is a
// generous ceiling that still blocks abuse.
const maxImagePixels = 25_000_000

var (
	// ErrEmptyImage is returned for a zero-length body.
	ErrEmptyImage = errors.New("图片内容为空")
	// ErrImageTooLarge is returned when the body exceeds MaxImageBytes.
	ErrImageTooLarge = errors.New("图片超过 5 MiB 大小限制")
	// ErrUnsupportedImage is returned when the content is not a genuine
	// PNG, JPEG, or WebP image. The message is deliberately generic so it does
	// not hint at how detection works.
	ErrUnsupportedImage = errors.New("仅支持 PNG、JPEG 或 WebP 图片，且文件必须是真实图片")
)

// ValidatedImage is the result of validating raw upload bytes.
type ValidatedImage struct {
	// MimeType is derived from the actual content, never from the client's
	// declared Content-Type or the filename/extension.
	MimeType string
	// SHA256 is the hex digest of the exact bytes.
	SHA256 string
	// ByteSize is len(data).
	ByteSize int
}

// ValidateImage confirms data is a genuine PNG, JPEG, or WebP image and returns
// the canonical mime type computed from the content. It performs two independent
// checks: an http.DetectContentType magic-number sniff, and a structural parse
// (standard-library DecodeConfig for PNG/JPEG, a RIFF/WEBP container parse for
// WebP). This defeats disguised files (e.g. HTML or SVG with a .png name, or a
// truncated magic prefix) because a valid signature alone is not enough — the
// bytes must also parse as the claimed format with sane dimensions.
func ValidateImage(data []byte) (ValidatedImage, error) {
	return ValidateImageWithLimit(data, MaxImageBytes)
}

// ValidateImageWithLimit is ValidateImage with a caller-chosen byte ceiling, so
// a different feature (e.g. user payment proofs, capped higher than the QR
// image) reuses the exact same content sniffing, structural decode and
// decompression-bomb protection rather than re-implementing security-critical
// validation. maxBytes<=0 falls back to MaxImageBytes.
func ValidateImageWithLimit(data []byte, maxBytes int) (ValidatedImage, error) {
	if maxBytes <= 0 {
		maxBytes = MaxImageBytes
	}
	if len(data) == 0 {
		return ValidatedImage{}, ErrEmptyImage
	}
	if len(data) > maxBytes {
		return ValidatedImage{}, ErrImageTooLarge
	}

	var mime string
	switch http.DetectContentType(data) {
	case "image/png":
		if err := validatePNG(data); err != nil {
			return ValidatedImage{}, err
		}
		mime = "image/png"
	case "image/jpeg":
		if err := validateJPEG(data); err != nil {
			return ValidatedImage{}, err
		}
		mime = "image/jpeg"
	case "image/webp":
		if err := validateWebP(data); err != nil {
			return ValidatedImage{}, err
		}
		mime = "image/webp"
	default:
		return ValidatedImage{}, ErrUnsupportedImage
	}

	sum := sha256.Sum256(data)
	return ValidatedImage{
		MimeType: mime,
		SHA256:   hex.EncodeToString(sum[:]),
		ByteSize: len(data),
	}, nil
}

func validatePNG(data []byte) error {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ErrUnsupportedImage
	}
	if !dimensionsSane(cfg.Width, cfg.Height) {
		return ErrUnsupportedImage
	}
	return nil
}

func validateJPEG(data []byte) error {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ErrUnsupportedImage
	}
	if !dimensionsSane(cfg.Width, cfg.Height) {
		return ErrUnsupportedImage
	}
	return nil
}

// validateWebP parses the RIFF/WEBP container. The standard library cannot
// decode WebP, so this is the "equivalent means" of confirming the bytes really
// form a WebP image rather than trusting the sniff alone: it requires the RIFF
// magic, a consistent declared size, the WEBP form type, and a known lossy /
// lossless / extended chunk fourcc whose declared size fits within the file.
func validateWebP(data []byte) error {
	if len(data) < 16 {
		return ErrUnsupportedImage
	}
	if !bytes.Equal(data[0:4], []byte("RIFF")) || !bytes.Equal(data[8:12], []byte("WEBP")) {
		return ErrUnsupportedImage
	}
	// The RIFF size field counts every byte after itself, i.e. from data[8].
	riffSize := binary.LittleEndian.Uint32(data[4:8])
	if int64(riffSize)+8 > int64(len(data)) || riffSize < 4 {
		return ErrUnsupportedImage
	}
	switch string(data[12:16]) {
	case "VP8 ", "VP8L", "VP8X":
	default:
		return ErrUnsupportedImage
	}
	if len(data) < 20 {
		return ErrUnsupportedImage
	}
	chunkSize := binary.LittleEndian.Uint32(data[16:20])
	// The chunk payload starts at data[20]; a WebP chunk may carry one padding
	// byte when its size is odd, so tolerate a single trailing pad byte.
	if int64(chunkSize)+20 > int64(len(data))+1 {
		return ErrUnsupportedImage
	}
	return nil
}

func dimensionsSane(width, height int) bool {
	if width <= 0 || height <= 0 {
		return false
	}
	return int64(width)*int64(height) <= maxImagePixels
}
