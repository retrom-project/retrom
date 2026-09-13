package emulationstationimport

import (
	"context"
	"fmt"
)

func (service *Materialization) Copy(
	ctx context.Context,
	id Execution,
	source MaterialSource,
	blob VerifiedBlob,
) (string, error) {
	if blob.Size < 0 || blob.Size != source.Size || blob.SHA256 == "" {
		return "", ErrInvalid
	}
	var result string
	err := service.repository.WithMaterialization(ctx, func(scope MaterialScope) error {
		before, now, err := service.source(ctx, scope.Read, id, source)
		if err != nil {
			return err
		}
		if before.State == "COPIED" {
			if before.BlobID == "" || before.Blob != blob {
				return ErrVersionConflict
			}
			result = before.BlobID
			return nil
		}
		if before.State != "DISCOVERED" {
			return ErrVersionConflict
		}
		result, err = scope.Write.Bind(ctx, MaterialBinding{Before: before, Blob: blob, NowMS: now})
		if err != nil {
			return fmt.Errorf("write material binding: %w", err)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("bind EmulationStation material: %w", err)
	}
	return result, nil
}

func (service *Materialization) source(
	ctx context.Context,
	reader MaterialReader,
	id Execution,
	source MaterialSource,
) (MaterialSnapshot, int64, error) {
	before, err := reader.Source(ctx, source.Key)
	if err != nil {
		return MaterialSnapshot{}, 0, fmt.Errorf("read EmulationStation material: %w", err)
	}
	now := service.now().UnixMilli()
	if err := validateImportExecution(before.Before.Execution, id, now); err != nil {
		return MaterialSnapshot{}, 0, err
	}
	if before.Before.Execution.Kind != "SERVER_EMULATIONSTATION_IMPORT" || before.Before.Execution.JobState != "RUNNING" ||
		before.Before.Item.State != "COPYING" || before.Before.Item.ID != source.Key.ItemID ||
		before.Before.Item.ImportID != id.ImportID ||
		!validItemVersion(before.Before.Item.Version) || !sameMaterialSource(before.Source, source) {
		return MaterialSnapshot{}, 0, ErrVersionConflict
	}
	return before, now, nil
}

func sameMaterialSource(left, right MaterialSource) bool {
	return left.Key == right.Key && left.Path == right.Path && left.Facts == right.Facts && left.Size == right.Size &&
		left.MediaType == right.MediaType && equalDimension(
		left.Width,
		right.Width,
	) && equalDimension(
		left.Height,
		right.Height,
	)
}

func equalDimension(left, right *int64) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func (service *Materialization) Warning(
	ctx context.Context,
	id Execution,
	source MaterialSource,
	code string,
) error {
	if !validMaterialWarning(source.Key.Kind, code) {
		return ErrInvalid
	}
	err := service.repository.WithMaterialization(ctx, func(scope MaterialScope) error {
		before, now, err := service.source(ctx, scope.Read, id, source)
		if err != nil {
			return err
		}
		state := "READ_FAILED"
		if code == "EMULATIONSTATION_SOURCE_CHANGED" {
			state = "SOURCE_CHANGED"
		}
		if before.State == state && before.WarningCode == code {
			return nil
		}
		if before.State != "DISCOVERED" {
			return ErrVersionConflict
		}
		warnings := append([]map[string]any{}, before.Warnings...)
		field := map[string]string{"COVER": "image", "VIDEO": "video"}[source.Key.Kind]
		found := false
		for _, warning := range warnings {
			if warning["code"] == code && warning["field"] == field {
				found = true
				break
			}
		}
		if !found {
			warnings = append(warnings, map[string]any{"code": code, "field": field, "pathKind": source.Key.Kind})
		}
		return scope.Write.Warn(ctx, MaterialWarning{
			Before: before, State: state, Code: code, Warnings: BoundedWarnings(warnings), NowMS: now,
		})
	})
	if err != nil {
		return fmt.Errorf("record EmulationStation media warning: %w", err)
	}
	return nil
}

func validMaterialWarning(kind, code string) bool {
	if kind != "COVER" && kind != "VIDEO" {
		return false
	}
	return code == "EMULATIONSTATION_SOURCE_CHANGED" || code == "EMULATIONSTATION_MEDIA_READ_FAILED" ||
		kind == "COVER" && code == "EMULATIONSTATION_IMAGE_INVALID" ||
		kind == "VIDEO" && code == "EMULATIONSTATION_VIDEO_UNSUPPORTED"
}
