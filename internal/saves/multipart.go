package saves

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
)

type manualMetadata struct {
	PayloadKind string `json:"payloadKind"`
	Name        string `json:"name,omitempty"`
	DiscIndex   *int   `json:"discIndex,omitempty"`
	namePresent bool
}

type parsedManual struct {
	metadata            manualMetadata
	payload             blobstore.Metadata
	screenshot          *blobstore.Metadata
	screenshotMediaType string
}

func (service *Service) parseManual(request *http.Request, launch launchSnapshot) (parsedManual, error) {
	mediaType, parameters, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || parameters["boundary"] == "" {
		return parsedManual{}, ErrInvalid
	}
	reader := multipart.NewReader(request.Body, parameters["boundary"])
	result, seen, err := service.readManualParts(reader, launch.payloadMaxBytes)
	if err != nil {
		return parsedManual{}, err
	}
	if !seen["metadata"] || !seen["payload"] || result.metadata.PayloadKind != launch.payloadKind {
		return parsedManual{}, ErrCheckpointInvalid
	}
	if !validMetadataForLaunch(result.metadata, launch) {
		return parsedManual{}, ErrInvalid
	}
	return result, nil
}

func (service *Service) readManualParts(
	reader *multipart.Reader, payloadLimit int64,
) (parsedManual, map[string]bool, error) {
	var result parsedManual
	seen := map[string]bool{}
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return parsedManual{}, nil, classifyMultipartError(nextErr)
		}
		name := part.FormName()
		if seen[name] || name != "metadata" && name != "payload" && name != "screenshot" {
			cleanup.Error("close", part.Close())
			return parsedManual{}, nil, ErrInvalid
		}
		seen[name] = true
		err := service.parseManualPart(part, name, payloadLimit, &result)
		cleanup.Error("close", part.Close())
		if err != nil {
			return parsedManual{}, nil, err
		}
	}
	return result, seen, nil
}

func classifyMultipartError(err error) error {
	if strings.Contains(err.Error(), "request body too large") {
		return ErrTooLarge
	}
	return ErrInvalid
}

func (service *Service) parseManualPart(
	part *multipart.Part, name string, payloadLimit int64, result *parsedManual,
) error {
	switch name {
	case "metadata":
		return parseManualMetadata(part, &result.metadata)
	case "payload":
		metadata, err := service.readBounded(part, payloadLimit)
		result.payload = metadata
		return err
	case "screenshot":
		metadata, err := service.readBounded(part, maxScreenshotBytes)
		if err != nil {
			return err
		}
		result.screenshotMediaType, err = validateScreenshot(metadata.Path)
		result.screenshot = &metadata
		return err
	default:
		return ErrInvalid
	}
}

func (service *Service) readBounded(source io.Reader, maximum int64) (blobstore.Metadata, error) {
	metadata, err := service.blobs.Put(io.LimitReader(source, maximum+1))
	if err != nil {
		return blobstore.Metadata{}, fmt.Errorf("saves/service: %w", err)
	}
	if metadata.Size > maximum {
		return blobstore.Metadata{}, ErrTooLarge
	}
	if metadata.Size == 0 {
		return blobstore.Metadata{}, ErrCheckpointInvalid
	}
	return metadata, nil
}

func parseManualMetadata(part *multipart.Part, metadata *manualMetadata) error {
	if value := part.Header.Get("Content-Type"); value != "" && value != "application/json" {
		return ErrInvalid
	}
	decoder := json.NewDecoder(io.LimitReader(part, 4097))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return ErrInvalid
	}
	seen := make(map[string]bool, 3)
	for decoder.More() {
		key, err := decoder.Token()
		name, ok := key.(string)
		if err != nil || !ok || seen[name] {
			return ErrInvalid
		}
		seen[name] = true
		if err := decodeMetadataField(decoder, name, metadata); err != nil {
			return ErrInvalid
		}
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return ErrInvalid
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) || token != nil {
		return ErrInvalid
	}
	return nil
}

func decodeMetadataField(decoder *json.Decoder, name string, metadata *manualMetadata) error {
	switch name {
	case "payloadKind":
		if err := decoder.Decode(&metadata.PayloadKind); err != nil {
			return fmt.Errorf("decode payload kind: %w", err)
		}
	case "name":
		metadata.namePresent = true
		if err := decoder.Decode(&metadata.Name); err != nil {
			return fmt.Errorf("decode save name: %w", err)
		}
	case "discIndex":
		if err := decoder.Decode(&metadata.DiscIndex); err != nil {
			return fmt.Errorf("decode disc index: %w", err)
		}
	default:
		return ErrInvalid
	}
	return nil
}

func validMetadataForLaunch(metadata manualMetadata, launch launchSnapshot) bool {
	if metadata.PayloadKind != "RUNTIME_STATE" && metadata.PayloadKind != "NATIVE_SAVE_BUNDLE_V1" &&
		metadata.PayloadKind != "ONS_SAVE_BUNDLE_V1" {
		return false
	}
	if launch.purpose == "RPG_RUNTIME_VALIDATION" {
		return !metadata.namePresent && metadata.DiscIndex == nil && launch.originalValidationLaunch
	}
	return validName(metadata.Name) && validManualDiscIndex(launch, metadata.DiscIndex)
}

func validName(name string) bool {
	if name != strings.TrimSpace(name) || !utf8.ValidString(name) {
		return false
	}
	count := 0
	for _, value := range name {
		if unicode.IsControl(value) {
			return false
		}
		count++
	}
	return count >= 1 && count <= 120
}

func validManualDiscIndex(launch launchSnapshot, discIndex *int) bool {
	if launch.contentFormat != "RETROM_MULTIDISC_M3U_V1" {
		return discIndex == nil
	}
	return discIndex != nil && *discIndex >= 0 && *discIndex < launch.discCount
}
