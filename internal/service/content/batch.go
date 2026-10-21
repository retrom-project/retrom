package content

import (
	"context"
	"errors"
	"fmt"
	"time"

	"retrom/internal/model"
)

// PackBatch runs serially and rechecks actual free space before every package.
func (s *Service) PackBatch(ctx context.Context, id string, limit int, apply bool, emit func(PackResult) error) error {
	processed, failed := 0, 0
	after := ""
	for processed < limit {
		ids := []string{id}
		if id == "" {
			var err error
			ids, err = s.Repository.PackCandidates(ctx, after, 100)
			if err != nil {
				return wrap(err)
			}
		}
		if len(ids) == 0 {
			break
		}
		for _, gameID := range ids {
			count, failures, err := s.reportPack(ctx, gameID, apply, emit)
			processed += count
			failed += failures
			if err != nil {
				return err
			}
			after = gameID
			if processed >= limit {
				break
			}
		}
		if ctx.Err() != nil {
			return fmt.Errorf("conversion interrupted: %w", ctx.Err())
		}
		if id != "" {
			break
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d games failed conversion: %w", failed, model.ErrInvalid)
	}
	return nil
}

func (s *Service) packBounded(ctx context.Context, id string, apply bool) (PackResult, error) {
	bounded, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	return s.PackExisting(bounded, id, apply)
}

func (s *Service) reportPack(ctx context.Context, id string, apply bool,
	emit func(PackResult) error,
) (int, int, error) {
	result, err := s.packBounded(ctx, id, apply)
	failed := 0
	if err != nil {
		result.Status = "failed"
		result.Error = err.Error()
		failed = 1
	}
	processed := 1
	if result.Status == "out_of_scope" || result.Status == "already_packed" {
		processed = 0
	}
	if emitErr := emit(result); emitErr != nil {
		return processed, failed, emitErr
	}
	if errors.Is(err, model.ErrStorageFull) {
		return processed, failed, wrap(err)
	}
	return processed, failed, nil
}
