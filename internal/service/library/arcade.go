package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/storage"
)

func (s *Service) ArcadeParentOptions(ctx context.Context, p model.Principal, id, core string) (
	model.ArcadeParentOptions, error,
) {
	if err := p.Admin(); err != nil {
		return model.ArcadeParentOptions{}, wrap(err)
	}
	detail, err := s.managedGame(ctx, p, id)
	if err != nil {
		return model.ArcadeParentOptions{}, err
	}
	directory, err := s.Repository.Directory(ctx, detail.Game.PlatformInstanceID)
	if err != nil {
		return model.ArcadeParentOptions{}, wrap(err)
	}
	result, err := s.Runtime.ArcadeParents(ctx, directory, detail.RuntimeConfig, detail.Files, core, "")
	return result.ArcadeParentOptions, wrap(err)
}

func (s *Service) UploadParent(ctx context.Context, p model.Principal, id, core, filename string,
	version int64, reader io.Reader,
) (model.GameDetail, error) {
	if err := p.Admin(); err != nil {
		return model.GameDetail{}, wrap(err)
	}
	if version < 1 || !storage.SafeRelative(filename) || path.Base(filename) != filename {
		return model.GameDetail{}, model.ErrInvalid
	}
	old, err := s.managedGame(ctx, p, id)
	if err != nil {
		return model.GameDetail{}, err
	}
	if old.Game.Version != version {
		return model.GameDetail{}, model.ErrConflict
	}
	directory, err := s.Repository.Directory(ctx, old.Game.PlatformInstanceID)
	if err != nil {
		return model.GameDetail{}, wrap(err)
	}
	stored, err := s.Storage.Write(ctx, "games", id, reader, storage.MaximumFileSize)
	if err != nil {
		return model.GameDetail{}, wrap(err)
	}
	file := model.GameFile{
		ID: uuid.NewString(), LogicalKey: filename, Role: "content",
		StorageKey: stored.Key, SizeBytes: stored.Size, SHA256: stored.SHA256,
	}
	committed := false
	err = s.attachParent(ctx, old, directory, core, file)
	if errors.Is(err, persistence.ErrCommitUncertain) {
		committed, err = s.confirmParent(ctx, id, file.ID, err)
	} else {
		committed = err == nil
	}
	if !committed {
		err = errors.Join(err, s.Storage.Remove(stored.Key))
	}
	if err != nil {
		return model.GameDetail{}, wrap(err)
	}
	return s.Detail(ctx, p, id, old.Game.Status)
}

func (s *Service) attachParent(ctx context.Context, old model.GameDetail,
	directory model.Directory, core string, file model.GameFile,
) error {
	files := make([]model.GameFile, 0, len(old.Files)+1)
	for _, existing := range old.Files {
		if existing.LogicalKey != file.LogicalKey {
			files = append(files, existing)
		}
	}
	files = append(files, file)
	projection, err := s.Runtime.ArcadeParents(ctx, directory, old.RuntimeConfig, files, core, file.LogicalKey)
	if err != nil {
		return wrap(err)
	}
	identity, err := s.Runtime.Identity(ctx, directory, projection.Config, files, core)
	if err != nil {
		return wrap(err)
	}
	err = s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		return wrap(r.ChangeParent(ctx, old.Game.ID, old.Game.Status, old.Game.Version,
			file, projection.Config, identity.ROMHash, s.Now().UnixMilli()))
	})
	return wrap(err)
}

func (s *Service) confirmParent(ctx context.Context, gameID, fileID string, commitErr error) (bool, error) {
	bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	exists, err := s.Repository.ParentCommitted(bounded, gameID, fileID)
	if err != nil {
		// Unknown commit outcome keeps the allocated bytes for reference-safe normal cleanup.
		return true, errors.Join(commitErr, fmt.Errorf("confirm parent commit: %w", err))
	}
	if !exists {
		return false, commitErr
	}
	return true, nil
}

func (s *Service) managedGame(ctx context.Context, p model.Principal, id string) (model.GameDetail, error) {
	detail, err := s.Repository.GameDetail(ctx, p.User.ID, id, "pending_review")
	if errors.Is(err, model.ErrNotFound) {
		detail, err = s.Repository.GameDetail(ctx, p.User.ID, id, "published")
	}
	return detail, wrap(err)
}
