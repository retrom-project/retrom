package imagecontent

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"

	metadatamodel "retrom/internal/model/metadata"

	"golang.org/x/image/webp"
)

func Validate(contents []byte, headerType string) (metadatamodel.AssetData, error) {
	if len(contents) > metadatamodel.MaximumAssetBytes {
		return metadatamodel.AssetData{}, metadatamodel.ErrAssetTooLarge
	}
	detected := http.DetectContentType(contents)
	if !supportedMediaType(detected) {
		return metadatamodel.AssetData{}, metadatamodel.ErrAssetMediaTypeInvalid
	}
	if headerType != "" {
		mediaType := strings.TrimSpace(strings.Split(headerType, ";")[0])
		if !supportedMediaType(mediaType) {
			return metadatamodel.AssetData{}, metadatamodel.ErrAssetMediaTypeMismatch
		}
	}
	configuration, err := decodeConfiguration(contents, detected)
	if err != nil {
		return metadatamodel.AssetData{}, metadatamodel.ErrAssetDecodeFailed
	}
	if configuration.Width <= 0 || configuration.Height <= 0 ||
		int64(configuration.Width)*int64(configuration.Height) > metadatamodel.MaximumAssetPixels {
		return metadatamodel.AssetData{}, metadatamodel.ErrAssetPixelLimit
	}
	return metadatamodel.AssetData{
		Bytes:     contents,
		MediaType: detected,
		Width:     configuration.Width,
		Height:    configuration.Height,
	}, nil
}

func supportedMediaType(value string) bool {
	return value == "image/png" || value == "image/jpeg" || value == "image/webp"
}

// The detected format selects a known decoder without consulting a mutable
// process-wide decoder registry.
func decodeConfiguration(contents []byte, mediaType string) (image.Config, error) {
	var configuration image.Config
	var err error
	switch mediaType {
	case "image/png":
		configuration, err = png.DecodeConfig(bytes.NewReader(contents))
	case "image/jpeg":
		configuration, err = jpeg.DecodeConfig(bytes.NewReader(contents))
	case "image/webp":
		configuration, err = webp.DecodeConfig(bytes.NewReader(contents))
	default:
		return image.Config{}, metadatamodel.ErrAssetMediaTypeInvalid
	}
	if err != nil {
		return image.Config{}, fmt.Errorf("decode image dimensions: %w", err)
	}
	return configuration, nil
}
