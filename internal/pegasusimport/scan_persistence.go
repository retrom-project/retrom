package pegasusimport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"

	"retrom/internal/pegasusmeta"
	repository "retrom/internal/persistence/pegasusimport"
	application "retrom/internal/service/pegasusimport"
)

func (service *Service) scanPublication() *application.ScanPublication {
	return application.NewScanPublication(repository.NewScanPublication(service.database), service.now)
}

func (service *Service) persistScan(ctx context.Context, unit work, result scanResult) error {
	if err := service.scanPublication().Save(ctx, unit.Identity(), result.projection()); err != nil {
		return fmt.Errorf("publish Pegasus scan: %w", err)
	}
	return nil
}

func (service *Service) persistScanHeaders(ctx context.Context, unit work, result scanResult, _ int64) error {
	if err := service.scanPublication().Headers(ctx, unit.Identity(), result.projection().Headers); err != nil {
		return fmt.Errorf("stage Pegasus scan headers: %w", err)
	}
	return nil
}

func (service *Service) persistScanItems(ctx context.Context, unit work, items []scannedItem, _ int64) error {
	for offset := 0; offset < len(items); offset += 500 {
		if err := service.scanPublication().Items(
			ctx,
			unit.Identity(),
			items[offset:min(offset+500, len(items))],
		); err != nil {
			return fmt.Errorf("stage Pegasus scan items: %w", err)
		}
	}
	return nil
}

func (service *Service) finishScan(ctx context.Context, unit work, result scanResult, _ int64) error {
	if err := service.scanPublication().Finish(ctx, unit.Identity(), result.projection().Summary); err != nil {
		return fmt.Errorf("finish Pegasus scan: %w", err)
	}
	return nil
}

func (result scanResult) projection() application.ScanProjection {
	metadata := make([]application.ScanMetadata, 0, len(result.Metadata))
	for _, value := range result.Metadata {
		metadata = append(metadata, application.ScanMetadata{
			Path: value.Path, Digest: value.Digest, Facts: value.Facts,
			State: value.State, ErrorCode: value.ErrorCode, Size: value.Size,
		})
	}
	return application.ScanProjection{
		Headers: application.ScanHeaders{Metadata: metadata, Collections: result.Collections},

		Items: result.Items,
		Summary: application.ScanSummary{
			SnapshotDigest: result.SnapshotDigest, MediaWarnings: result.MediaWarnings,
			Shape: application.ScanShape{
				Metadata: int64(len(result.Metadata)), InvalidMetadata: result.InvalidMetadata,
				Collections: int64(
					len(result.Collections),
				), Items: int64(
					len(result.Items),
				), Blocked: result.Blocked, Covers: result.Covers,
				Videos: result.Videos, EstimatedBytes: result.EstimatedBytes,
			},
		},
	}
}

func parserErrorCode(err error) string {
	switch {
	case errors.Is(err, pegasusmeta.ErrTooLarge):
		return pegasusmeta.ErrTooLarge.Error()
	case errors.Is(err, pegasusmeta.ErrInvalidUTF8):
		return pegasusmeta.ErrInvalidUTF8.Error()
	default:
		return pegasusmeta.ErrSyntax.Error()
	}
}

func asciiFold(value string) string {
	return strings.Map(func(character rune) rune {
		if character >= 'A' && character <= 'Z' {
			return character + ('a' - 'A')
		}
		return character
	}, value)
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func stringPointer(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func int64Pointer(value int64) *int64 { return &value }
