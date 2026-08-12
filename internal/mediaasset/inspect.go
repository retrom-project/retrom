package mediaasset

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	_ "image/jpeg" // Register the bounded JPEG decoder used by image.DecodeConfig.
	_ "image/png"  // Register the bounded PNG decoder used by image.DecodeConfig.
	"io"
	"math"
	"net/http"

	_ "golang.org/x/image/webp" // Register the bounded WebP decoder used by image.DecodeConfig.
)

const (
	MaxImageBytes  = 10 << 20
	MaxImagePixels = 40_000_000
	MaxVideoBytes  = 256 << 20
)

var (
	ErrImageInvalid     = errors.New("PEGASUS_IMAGE_INVALID")
	ErrVideoUnsupported = errors.New("PEGASUS_VIDEO_UNSUPPORTED")
	ErrVideoTooLarge    = errors.New("PEGASUS_VIDEO_TOO_LARGE")
)

type Image struct {
	MediaType string
	WidthPX   int64
	HeightPX  int64
}

func InspectImage(reader io.ReadSeeker, size int64) (Image, error) {
	if size < 1 || size > MaxImageBytes {
		return Image{}, ErrImageInvalid
	}
	header := make([]byte, min(512, int(size)))
	count, err := io.ReadFull(reader, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Image{}, ErrImageInvalid
	}
	header = header[:count]
	mediaType := http.DetectContentType(header)
	if mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/webp" {
		return Image{}, ErrImageInvalid
	}
	if _, err := reader.Seek(0, io.SeekStart); err != nil {
		return Image{}, ErrImageInvalid
	}
	configuration, format, err := image.DecodeConfig(io.LimitReader(reader, MaxImageBytes+1))
	if err != nil || configuration.Width <= 0 || configuration.Height <= 0 ||
		int64(configuration.Width) > MaxImagePixels/int64(configuration.Height) {
		return Image{}, ErrImageInvalid
	}
	expected := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "webp": "image/webp"}[format]
	if expected == "" || expected != mediaType {
		return Image{}, ErrImageInvalid
	}
	return Image{MediaType: mediaType, WidthPX: int64(configuration.Width), HeightPX: int64(configuration.Height)}, nil
}

func InspectVideo(reader io.ReadSeeker, size int64) (string, error) {
	if size < 1 {
		return "", ErrVideoUnsupported
	}
	if size > MaxVideoBytes {
		return "", ErrVideoTooLarge
	}
	header := make([]byte, min(4096, int(size)))
	count, err := io.ReadFull(reader, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", ErrVideoUnsupported
	}
	header = header[:count]
	if isMP4(header, size) {
		return "video/mp4", nil
	}
	if isWebM(header) {
		return "video/webm", nil
	}
	return "", ErrVideoUnsupported
}

func isMP4(header []byte, size int64) bool {
	if len(header) < 12 || !bytes.Equal(header[4:8], []byte("ftyp")) {
		return false
	}
	boxSize := int64(binary.BigEndian.Uint32(header[:4]))
	if boxSize == 1 {
		if len(header) < 16 {
			return false
		}
		extendedSize := binary.BigEndian.Uint64(header[8:16])
		if extendedSize > math.MaxInt64 {
			return false
		}
		boxSize = int64(extendedSize)
	}
	return boxSize >= 12 && boxSize <= size
}

func isWebM(header []byte) bool {
	if len(header) < 8 || !bytes.Equal(header[:4], []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		return false
	}
	// The bounded EBML header must declare DocType "webm". Requiring both the
	// DocType element and value avoids accepting arbitrary Matroska-family data.
	for index := 4; index+6 <= len(header); index++ {
		if header[index] == 0x42 && header[index+1] == 0x82 && bytes.Contains(header[index+2:], []byte("webm")) {
			return true
		}
	}
	return false
}
