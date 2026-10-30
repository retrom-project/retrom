package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"sort"
	"strings"

	"retrom/internal/model"

	"github.com/google/uuid"
)

// StageOrigin accepts an outer archive's project-sized bytes without widening the
// limits used for logical content, saves or BIOS. The size is checked by the caller.
func (s *Store) StageOrigin(ctx context.Context, operation string, reader io.Reader, size int64) (File, error) {
	if size < 0 || size > MaximumPackageSize {
		return File{}, model.ErrInvalid
	}
	free, err := s.FreeBytes()
	if err != nil {
		return File{}, err
	}
	if free < size+1024*1024*1024 {
		return File{}, model.ErrStorageFull
	}
	file, err := s.stageLimit(ctx, operation, reader, size)
	if err == nil && file.Size != size {
		err = fmt.Errorf("original source size changed: %w", model.ErrInvalid)
	}
	return file, err
}

// BundleOrigin creates an ordinary ZIP64 archive of selected, unconverted inputs.
// Raw outer archives can be larger than one runtime member; their bytes stay intact.
func (s *Store) BundleOrigin(ctx context.Context, operation string, files []model.ContentFile) (File, error) {
	if !model.UUID(operation) {
		return File{}, model.ErrInvalid
	}
	if err := validateOriginInputs(files); err != nil {
		return File{}, err
	}
	if err := s.packageCapacity(files); err != nil {
		return File{}, err
	}
	dir := "temporary/" + operation
	if err := s.root.MkdirAll(dir, 0o700); err != nil {
		return File{}, diskFailure("create origin staging", err)
	}
	key := dir + "/" + uuid.NewString()
	f, err := s.root.OpenFile(key, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return File{}, diskFailure("create original archive", err)
	}
	defer closeFile(f)
	files = append([]model.ContentFile(nil), files...)
	sort.Slice(files, func(i, j int) bool { return files[i].LogicalKey < files[j].LogicalKey })
	written, err := s.writePackage(ctx, f, files)
	if err != nil {
		return File{}, err
	}
	if _, err = verifyPackage(ctx, f, files, written.SizeBytes, true); err != nil {
		return File{}, err
	}
	return File{Key: key, SHA256: written.SHA256, Size: written.SizeBytes}, nil
}

func validateOriginInputs(files []model.ContentFile) error {
	if len(files) == 0 || len(files) > MaximumMembers {
		return model.ErrInvalid
	}
	seen := make(map[string]bool, len(files))
	var total int64
	for _, file := range files {
		if !SafeRelative(file.LogicalKey) || seen[file.LogicalKey] || !model.Hash(file.SHA256) ||
			file.SizeBytes < 0 || file.SizeBytes > MaximumPackageSize-total {
			return model.ErrInvalid
		}
		total += file.SizeBytes
		seen[file.LogicalKey] = true
	}
	if total+int64(len(files))*(2*1024+256)+1024 > MaximumPackageSize {
		return model.ErrInvalid
	}
	return nil
}

// PublishOrigin consumes operation-owned staging after the result row's ID is known.
// An exclusive attachment directory prevents overwriting another row's original.
func (s *Store) PublishOrigin(ctx context.Context, gameID, fileID, logical string,
	staged File,
) (*model.OriginGameInfo, error) {
	key, err := model.OriginStorageKey(gameID, fileID)
	if err != nil {
		return nil, fmt.Errorf("original owner: %w", err)
	}
	info := &model.OriginGameInfo{StorageKey: key, LogicalKey: logical, SizeBytes: staged.Size, SHA256: staged.SHA256}
	test := model.GameFile{
		ID: fileID, Role: "project_package",
		ExtInfo: model.GameFileTabExtInfo{ConversionInfo: &model.GameFileConversionInfo{OriginInfo: info}},
	}
	parts := strings.Split(staged.Key, "/")
	if model.ValidateGameFileExtInfo(gameID, test) != nil || len(parts) != 3 || parts[0] != "temporary" ||
		!model.UUID(parts[1]) || !model.UUID(parts[2]) {
		return nil, model.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return nil, fmt.Errorf("publish original cancelled: %w", err)
	}
	if err = s.syncOrigin(ctx, staged); err != nil {
		return nil, err
	}
	owner := path.Dir(key)
	if err = s.originParents(gameID, true); err != nil {
		return nil, err
	}
	if err = s.root.Mkdir(owner, 0o700); err != nil {
		return nil, diskFailure("create original owner", err)
	}
	if err = s.publish(staged.Key, key, owner); err != nil {
		return nil, errors.Join(err, s.RemoveOrigin(gameID, fileID))
	}
	// Persist every newly created ancestor, including games/<id> itself.
	if err = s.syncDirectory("games/" + gameID); err != nil {
		return nil, errors.Join(err, s.RemoveOrigin(gameID, fileID))
	}
	if err = s.syncDirectory("games"); err != nil {
		return nil, errors.Join(err, s.RemoveOrigin(gameID, fileID))
	}
	return info, nil
}

func (s *Store) syncOrigin(ctx context.Context, staged File) error {
	f, err := s.Read(staged.Key)
	if err != nil {
		return err
	}
	defer closeFile(f)
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat original staging: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() != staged.Size {
		return model.ErrInvalid
	}
	hash := sha256.New()
	reader := io.LimitReader(&contextReader{ctx: ctx, reader: f}, staged.Size+1)
	size, err := io.Copy(hash, reader)
	if err != nil {
		return fmt.Errorf("verify original staging: %w", err)
	}
	if size != staged.Size || hex.EncodeToString(hash.Sum(nil)) != staged.SHA256 {
		return fmt.Errorf("original staging changed: %w", model.ErrInvalid)
	}
	if err = f.Sync(); err != nil {
		return diskFailure("sync original", err)
	}
	return nil
}

func (s *Store) RemoveOrigin(gameID, fileID string) error {
	key, err := model.OriginStorageKey(gameID, fileID)
	if err != nil {
		return fmt.Errorf("original owner: %w", err)
	}
	if err = s.originParents(gameID, false); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err = s.root.RemoveAll(path.Dir(key)); err != nil {
		return fmt.Errorf("remove original attachment: %w", err)
	}
	for _, directory := range []string{"games/" + gameID + "/origins", "games/" + gameID} {
		if err = s.removeEmptyDirectory(directory); err != nil {
			return err
		}
	}
	return nil
}

// Original attachments may not traverse a symlink into another owner's tree.
func (s *Store) originParents(gameID string, create bool) error {
	for _, directory := range []string{"games", "games/" + gameID, "games/" + gameID + "/origins"} {
		if create {
			if err := s.root.Mkdir(directory, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return diskFailure("create original parent", err)
			}
		}
		info, err := s.root.Lstat(directory)
		if err != nil {
			return fmt.Errorf("stat original parent: %w", err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return model.ErrInvalid
		}
	}
	return nil
}

// CopyOrigin gives a replacement physical row its own bytes, leaving the source
// row and its retirement grace independent of the new row.
func (s *Store) CopyOrigin(ctx context.Context, gameID string, source model.GameFile,
	targetID string,
) (*model.GameFileConversionInfo, error) {
	if err := model.ValidateGameFileExtInfo(gameID, source); err != nil {
		return nil, fmt.Errorf("original source: %w", err)
	}
	conversion := source.ExtInfo.ConversionInfo
	if conversion == nil || conversion.OriginInfo == nil {
		return conversion, nil
	}
	original := conversion.OriginInfo
	if err := s.originParents(gameID, false); err != nil {
		return nil, err
	}
	for _, key := range []string{path.Dir(original.StorageKey), original.StorageKey} {
		info, err := s.root.Lstat(key)
		if err != nil {
			return nil, fmt.Errorf("stat original attachment: %w", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, model.ErrInvalid
		}
	}
	reader, err := s.Read(original.StorageKey)
	if err != nil {
		return nil, err
	}
	defer closeFile(reader)
	operation := uuid.NewString()
	defer func() {
		if removeErr := s.RemoveTemporary("temporary/" + operation); removeErr != nil {
			slog.Error("remove original copy staging", "error", removeErr)
		}
	}()
	staged, err := s.StageOrigin(ctx, operation, reader, original.SizeBytes)
	if err != nil {
		return nil, err
	}
	if staged.SHA256 != original.SHA256 {
		return nil, fmt.Errorf("original attachment changed: %w", model.ErrInvalid)
	}
	info, err := s.PublishOrigin(ctx, gameID, targetID, original.LogicalKey, staged)
	if err != nil {
		return nil, err
	}
	return &model.GameFileConversionInfo{OriginInfo: info, Fingerprint: conversion.Fingerprint}, nil
}
