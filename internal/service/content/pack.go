package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/storage"
)

type Runtime interface {
	StorageLayout(context.Context, model.Directory, json.RawMessage) (string, error)
}

type Service struct {
	Repository *persistence.Repository
	Storage    *storage.Store
	Runtime    Runtime
	Now        func() time.Time
}

type PackResult struct {
	GameID          string `json:"gameId"`
	Status          string `json:"status"`
	Members         int    `json:"members"`
	ContentBytes    int64  `json:"contentBytes"`
	AdditionalBytes int64  `json:"additionalBytes"`
	Error           string `json:"error,omitempty"`
}

func (s *Service) PackExisting(ctx context.Context, id string, apply bool) (PackResult, error) {
	result := PackResult{GameID: id}
	snapshot, err := s.Repository.ContentSnapshot(ctx, id)
	if err != nil {
		return result, wrap(err)
	}
	layout, err := s.Runtime.StorageLayout(ctx, snapshot.Directory, snapshot.Config)
	if err != nil {
		return result, wrap(err)
	}
	if layout != "ZIP_STORE" {
		result.Status = "out_of_scope"
		return result, nil
	}
	files, err := s.Repository.ContentFiles(ctx, snapshot.Files)
	if err != nil {
		return result, wrap(err)
	}
	if len(snapshot.Files) == 1 && snapshot.Files[0].Role == "project_package" {
		if err = s.Storage.ValidatePackageDirectory(ctx, snapshot.Files[0], files); err != nil {
			return result, wrap(err)
		}
		result.Members = len(files)
		result.Status = "already_packed"
		return result, nil
	}
	result.Members = len(files)
	result.ContentBytes, err = s.contentBytes(ctx, files)
	if err != nil {
		return result, err
	}
	result.AdditionalBytes = result.ContentBytes + int64(len(files))*(2*1024+256) + 1024
	original, err := originalOwner(snapshot.Files)
	if err != nil {
		return result, err
	}
	if original != nil {
		result.AdditionalBytes += original.ExtInfo.ConversionInfo.OriginInfo.SizeBytes
	}
	if !apply {
		result.Status = "candidate"
		return result, nil
	}
	file, err := s.preparePackage(ctx, id, files, original)
	if err != nil {
		return result, err
	}
	if err = s.commitPackage(ctx, snapshot, file); err != nil {
		return result, err
	}

	result.Status = "packed"
	return result, nil
}

func (s *Service) contentBytes(ctx context.Context, files []model.ContentFile) (int64, error) {
	if err := storage.ValidateContent(files); err != nil {
		return 0, wrap(err)
	}
	var size int64
	for _, file := range files {
		if err := s.verifySource(ctx, file); err != nil {
			return 0, err
		}
		size += file.SizeBytes
	}
	return size, nil
}

func (s *Service) preparePackage(ctx context.Context, id string, files []model.ContentFile,
	original *model.GameFile,
) (model.GameFile, error) {
	file, err := s.Storage.BuildPackage(ctx, id, files)
	if err != nil {
		return model.GameFile{}, wrap(err)
	}
	if original != nil {
		file.ExtInfo.ConversionInfo, err = s.Storage.CopyOrigin(ctx, id, *original, file.ID)
		if err != nil {
			return model.GameFile{}, wrap(errors.Join(err, s.removePackage(id, file)))
		}
	}
	return file, nil
}

func (s *Service) verifySource(ctx context.Context, file model.ContentFile) error {
	physical, err := s.Storage.Read(file.StorageKey)
	if err != nil {
		return wrap(err)
	}
	info, statErr := physical.Stat()
	closeErr := physical.Close()
	if statErr != nil || closeErr != nil {
		return wrap(errors.Join(statErr, closeErr))
	}
	if info.Size() != file.SizeBytes {
		return fmt.Errorf("stored source size changed: %w", model.ErrInvalid)
	}
	reader, err := s.Storage.OpenContent(file.StorageKey, file.Offset, file.SizeBytes)
	if err != nil {
		return wrap(err)
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			slog.Error("close conversion source", "error", closeErr)
		}
	}()
	hash := sha256.New()
	buffer := make([]byte, 128*1024)
	var size int64
	for {
		if err = ctx.Err(); err != nil {
			return fmt.Errorf("verify source: %w", err)
		}
		n, readErr := reader.Read(buffer)
		if n > 0 {
			_, _ = hash.Write(buffer[:n])
			size += int64(n)
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return fmt.Errorf("verify source: %w", readErr)
		}
	}
	if size != file.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != file.SHA256 {
		return fmt.Errorf("stored content changed: %w", model.ErrInvalid)
	}
	return nil
}

func (s *Service) commitPackage(ctx context.Context, snapshot persistence.ContentSnapshot, file model.GameFile) error {
	err := s.Repository.Transaction(ctx, func(tx *persistence.Repository) error {
		return tx.SwitchContentStorage(ctx, snapshot, file, s.Now().UnixMilli())
	})
	if errors.Is(err, persistence.ErrCommitUncertain) {
		bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		committed, checkErr := s.Repository.ParentCommitted(bounded, snapshot.ID, file.ID)
		if checkErr != nil {
			return wrap(errors.Join(err, checkErr))
		}
		if committed {
			return nil
		}
	}
	if err != nil {
		return wrap(errors.Join(err, s.removePackage(snapshot.ID, file)))
	}
	return nil
}

func originalOwner(files []model.GameFile) (*model.GameFile, error) {
	var owner *model.GameFile
	for _, file := range files {
		if file.ExtInfo.ConversionInfo == nil || file.ExtInfo.ConversionInfo.OriginInfo == nil {
			continue
		}
		if owner != nil {
			return nil, fmt.Errorf("multiple original attachments cannot become one owner: %w", model.ErrInvalid)
		}
		owner = &file
	}
	return owner, nil
}

func (s *Service) removePackage(gameID string, file model.GameFile) error {
	err := s.Storage.Remove(file.StorageKey)
	if file.ExtInfo.ConversionInfo != nil && file.ExtInfo.ConversionInfo.OriginInfo != nil {
		err = errors.Join(err, s.Storage.RemoveOrigin(gameID, file.ID))
	}
	return err
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("content conversion: %w", err)
}
