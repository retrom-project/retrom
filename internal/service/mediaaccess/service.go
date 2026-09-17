package mediaaccess

import (
	"context"
	"errors"
	"fmt"

	model "retrom/internal/model/mediaaccess"
)

var (
	ErrNotFound = errors.New("MEDIA_RESOURCE_NOT_FOUND")
	ErrKind     = errors.New("REVIEW_SOURCE_MEDIA_KIND_INVALID")
)

type Service struct{ repository model.Repository }

func New(repository model.Repository) *Service { return &Service{repository: repository} }

func (service *Service) Game(ctx context.Context, id string) (model.Resource, error) {
	var result model.Resource
	err := service.repository.WithRead(ctx, func(reader model.Reader) error {
		asset, found, err := reader.Game(ctx, id)
		if err != nil {
			return fmt.Errorf("read game media authority: %w", err)
		}
		if !found || asset.GameState != "PUBLISHED" {
			return ErrNotFound
		}
		result = asset.Resource
		return nil
	})
	return result, accessError("authorize game media", err)
}

func (service *Service) Save(ctx context.Context, id, profile string) (model.Resource, error) {
	var result model.Resource
	err := service.repository.WithRead(ctx, func(reader model.Reader) error {
		screenshot, found, err := reader.Save(ctx, id)
		if err != nil {
			return fmt.Errorf("read save screenshot authority: %w", err)
		}
		if !found || screenshot.Deleted || screenshot.ProfileID != profile || screenshot.GameState != "PUBLISHED" {
			return ErrNotFound
		}
		result = screenshot.Resource
		return nil
	})
	return result, accessError("authorize save screenshot", err)
}

func (service *Service) Review(ctx context.Context, id, kind string) (model.Resource, error) {
	var result model.Resource
	err := service.repository.WithRead(ctx, func(reader model.Reader) error {
		assets, err := reader.Review(ctx, id)
		if err != nil {
			return fmt.Errorf("read review media authority: %w", err)
		}
		for _, asset := range assets {
			if visibleReview(asset) {
				result = asset.Resource
				return nil
			}
		}
		result, err = sourceMedia(ctx, reader, id, kind)
		return err
	})
	return result, accessError("authorize review media", err)
}

func sourceMedia(ctx context.Context, reader model.Reader, id, kind string) (model.Resource, error) {
	if kind == "" {
		kind = "COVER"
	}
	if kind != "COVER" && kind != "VIDEO" {
		return model.Resource{}, ErrKind
	}
	assets, err := reader.Sources(ctx, id, kind)
	if err != nil {
		return model.Resource{}, fmt.Errorf("read review source media: %w", err)
	}
	var result model.Resource
	count := 0
	for _, asset := range assets {
		if visibleReview(asset) {
			result = asset.Resource
			count++
		}
	}
	if count != 1 {
		return model.Resource{}, ErrNotFound
	}
	return result, nil
}

func visibleReview(asset model.ReviewAsset) bool {
	owner := asset.ItemState == "REVIEW_PENDING" || asset.TerminalReview
	switch asset.Kind {
	case "CANDIDATE":
		return asset.State == "READY" && (owner || asset.GameState == "PUBLISHED")
	case "UPLOAD", "SCREENSHOT":
		return owner
	case "PEGASUS", "EMULATIONSTATION":
		return asset.State == "COPIED" && owner
	default:
		return false
	}
}

func accessError(operation string, cause error) error {
	if cause == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, cause)
}
