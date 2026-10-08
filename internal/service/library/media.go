package library

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"

	"github.com/google/uuid"
)

func (s *Service) UploadMedia(ctx context.Context, p model.Principal, id, kind string, version int64,
	reader io.Reader,
) (model.GameDetail, error) {
	if err := p.Admin(); err != nil {
		return model.GameDetail{}, wrap(err)
	}
	if !model.OneOf(kind, "cover", "video", "screenshot") || version < 1 {
		return model.GameDetail{}, model.ErrInvalid
	}
	prefix := make([]byte, 512)
	count, err := io.ReadFull(reader, prefix)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return model.GameDetail{}, fmt.Errorf("read media header: %w", err)
	}
	prefix = prefix[:count]
	mediaType := http.DetectContentType(prefix)
	if !validMedia(kind, mediaType) {
		return model.GameDetail{}, model.ErrInvalid
	}
	file, err := s.Storage.Write(ctx, "games", id, io.MultiReader(bytes.NewReader(prefix), reader), 512*1024*1024)
	if err != nil {
		return model.GameDetail{}, wrap(err)
	}
	media := model.Media{
		ID: uuid.NewString(), Kind: kind, StorageKey: file.Key, SizeBytes: file.Size,
		SHA256: file.SHA256, MediaType: mediaType,
	}
	err = s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		status, current, readErr := r.LockGame(ctx, id)
		if readErr != nil {
			return wrap(readErr)
		}
		if !model.OneOf(status, "pending_review", "published") || version != current {
			return model.ErrConflict
		}
		return wrap(r.ChangeMedia(ctx, id, media, current, s.Now().UnixMilli()))
	})
	if err != nil {
		return model.GameDetail{}, wrap(err)
	}
	status := "published"
	if _, readErr := s.Repository.Game(ctx, p.User.ID, id, status); readErr != nil {
		status = "pending_review"
	}
	return s.Detail(ctx, p, id, status)
}

func validMedia(kind, mediaType string) bool {
	if kind == "video" {
		return model.OneOf(mediaType, "video/mp4", "video/webm")
	}
	return model.OneOf(mediaType, "image/jpeg", "image/png", "image/webp", "image/gif")
}

func (s *Service) RemoveMedia(ctx context.Context, p model.Principal, id, mediaID string, version int64) error {
	if err := p.Admin(); err != nil {
		return wrap(err)
	}
	return wrap(s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		status, current, err := r.LockGame(ctx, id)
		if err != nil {
			return wrap(err)
		}
		if !model.OneOf(status, "pending_review", "published") || version != current {
			return model.ErrConflict
		}
		if err = r.DeleteMedia(ctx, id, mediaID, time.Now().UnixMilli()); err != nil {
			return wrap(err)
		}
		return wrap(r.BumpGame(ctx, id, version, s.Now().UnixMilli()))
	}))
}
