package files

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/storage"
)

type Service struct {
	Repository *persistence.Repository
	Storage    *storage.Store
	Now        func() time.Time
	Grace      time.Duration
	scanner    *storage.Scanner
}

func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	defer func() {
		if s.scanner != nil {
			s.scanner.Close()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Sweep(ctx); err != nil {
				slog.Error("managed file cleanup", "error", err)
			}
		}
	}
}

func (s *Service) Sweep(ctx context.Context) error {
	if err := s.Repository.ExpireScanProgress(ctx, s.Now().Add(-24*time.Hour).UnixMilli()); err != nil {
		return wrap(err)
	}
	users, err := s.Repository.DeletedUsers(ctx)
	if err != nil {
		return wrap(err)
	}
	for _, id := range users {
		if err = s.Repository.RetireUserResources(ctx, id, s.Now().UnixMilli()); err != nil {
			return wrap(err)
		}
	}
	games, err := s.Repository.DeletedGames(ctx, s.Now().Add(-s.Grace).UnixMilli())
	if err != nil {
		return wrap(err)
	}
	for _, id := range games {
		if err = s.game(ctx, id); err != nil {
			slog.Error("purge deleted game", "gameId", id, "error", err)
		}
	}
	retired, err := s.Repository.RetiredFiles(ctx, s.Now().Add(-s.Grace).UnixMilli())
	if err != nil {
		return wrap(err)
	}
	for _, file := range retired {
		if err = s.retired(ctx, file); err != nil {
			slog.Error("purge retired file", "id", file.ID, "error", err)
		}
	}
	return s.orphans(ctx)
}

func (s *Service) retired(ctx context.Context, file model.RetiredFile) error {
	if err := s.removeRetired(ctx, file); err != nil {
		return err
	}
	return wrap(s.Repository.PurgeFile(ctx, file, s.Now().UnixMilli()))
}

func (s *Service) removeRetired(ctx context.Context, file model.RetiredFile) error {
	if file.Table == "save_tab" {
		return wrap(s.Storage.RemoveOwner("saves", file.ID))
	}
	referenced, err := s.Repository.Referenced(ctx, file.Key)
	if err != nil {
		return wrap(err)
	}
	if referenced {
		return nil
	}
	return wrap(s.Storage.Remove(file.Key))
}

func (s *Service) game(ctx context.Context, id string) error {
	saves, err := s.Repository.GameSaveIDs(ctx, id)
	if err != nil {
		return wrap(err)
	}
	for _, save := range saves {
		if err = s.Storage.RemoveOwner("saves", save); err != nil {
			return wrap(err)
		}
	}
	if err = s.Storage.RemoveOwner("games", id); err != nil {
		return wrap(err)
	}
	return wrap(s.Repository.Transaction(ctx,
		func(r *persistence.Repository) error { return wrap(r.PurgeGame(ctx, id, s.Now().UnixMilli())) }))
}

func (s *Service) orphans(ctx context.Context) error {
	if s.scanner == nil {
		s.scanner = s.Storage.Scanner()
	}
	entries, finished, err := s.scanner.Next(1000)
	if err != nil {
		s.scanner.Close()
		s.scanner = nil
		return wrap(err)
	}
	for _, entry := range entries {
		if entry.ModifiedAt.After(s.Now().Add(-s.Grace)) {
			continue
		}
		if !strings.HasPrefix(entry.Key, "temporary/") {
			referenced, readErr := s.Repository.Referenced(ctx, entry.Key)
			if readErr != nil {
				return wrap(readErr)
			}
			if referenced {
				continue
			}
		}
		if err = s.Storage.Remove(entry.Key); err != nil {
			slog.Error("remove unreferenced managed file", "key", entry.Key, "error", err)
		}
	}
	if finished {
		s.scanner.Close()
		s.scanner = nil
	}
	return nil
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("file lifecycle: %w", err)
}
