package scans

import (
	"context"
	"errors"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
)

func (s *Service) confirmCommit(ctx context.Context, scan *model.Scan, next model.Scan, id, table string,
	err error,
) error {
	if err == nil {
		*scan = next
		return nil
	}
	if !errors.Is(err, persistence.ErrCommitUncertain) {
		return wrap(err)
	}
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	persisted, readErr := s.Repository.Scan(bounded, scan.ID)
	if readErr != nil || persisted.ProcessedCount < next.ProcessedCount {
		return wrap(err)
	}
	if next.ImportedCount > scan.ImportedCount {
		exists, checkErr := s.Repository.ImportExists(bounded, table, id)
		if checkErr != nil || !exists {
			return wrap(err)
		}
	}
	*scan = persisted
	return nil
}

func (s *Service) interruptUncertain(ctx context.Context, id string) {
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.Repository.StopScan(bounded, id, "interrupted", "COMMIT_RESULT_UNAVAILABLE",
		s.Now().UnixMilli()); err != nil {
		logProgress(id, err)
	}
}

func (s *Service) commitGame(ctx context.Context, p model.Principal, game model.PreparedGame, scan *model.Scan) error {
	next := *scan
	next.ProcessedCount++
	next.UpdatedAtMs = s.Now().UnixMilli()
	err := s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		directory, readErr := r.Directory(ctx, game.Input.PlatformInstanceID)
		if readErr != nil {
			return wrap(readErr)
		}
		if !directory.Enabled {
			return model.ErrConflict
		}
		duplicate, readErr := r.DuplicateGame(ctx, game.Input.PlatformInstanceID, game.ContentHash)
		if readErr != nil {
			return wrap(readErr)
		}
		if duplicate {
			next.SkippedCount++
		} else {
			if writeErr := r.CreateGame(ctx, p.User.ID, game, s.Now().UnixMilli()); writeErr != nil {
				return wrap(writeErr)
			}
			next.ImportedCount++
		}
		return wrap(r.UpdateScan(ctx, next))
	})
	return s.confirmCommit(ctx, scan, next, game.ID, "game_tab", err)
}
