package saves

import (
	"image"

	// Register the JPEG and PNG decoders used for save-state screenshots.
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"

	"retrom/internal/cleanup"
)

func validateScreenshot(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", ErrInvalid
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	header := make([]byte, 512)
	read, _ := io.ReadFull(file, header)
	mediaType := http.DetectContentType(header[:read])
	if mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/webp" {
		return "", ErrInvalid
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", ErrInvalid
	}
	configuration, _, err := image.DecodeConfig(file)
	if err != nil || configuration.Width <= 0 || configuration.Height <= 0 ||
		int64(configuration.Width)*int64(configuration.Height) > maxPixels {
		if mediaType != "image/webp" || !validWebPDimensions(path) {
			return "", ErrInvalid
		}
	}
	return mediaType, nil
}

func validWebPDimensions(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { cleanup.Error("close", file.Close()) }()
	data := make([]byte, 30)
	read, err := io.ReadFull(file, data)
	if err != nil || read < 30 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return false
	}
	if string(data[12:16]) != "VP8X" {
		return false
	}
	width := int64(data[24]) | int64(data[25])<<8 | int64(data[26])<<16
	height := int64(data[27]) | int64(data[28])<<8 | int64(data[29])<<16
	width++
	height++
	return width > 0 && height > 0 && width*height <= maxPixels
}
