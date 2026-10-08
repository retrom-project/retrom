package saves

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // Register JPEG for the public screenshot image boundary.
	_ "image/png"  // Register PNG for the public screenshot image boundary.
	"io"
	"reflect"
	"strings"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/service/runs"
	"retrom/internal/storage"
)

type Service struct {
	Repository *persistence.Repository
	Runs       *runs.Service
	Storage    *storage.Store
	Now        func() time.Time
}

func (s *Service) List(ctx context.Context,
	p model.Principal,
	gameID string,
	q model.Query) (model.SavePage,
	error,
) {
	if !model.OneOf(q.Kind, "", "checkpoint", "game_save") || !model.OneOf(q.Sort, "", "recent", "title") {
		return model.SavePage{}, model.ErrInvalid
	}
	page, err := s.Repository.Saves(ctx, p.User.ID, gameID, q)
	if err != nil {
		return page, wrap(err)
	}
	if len(page.Items) > 0 {
		if err = s.availabilityBatch(ctx, page.Items); err != nil {
			return page, err
		}
	}
	return page, nil
}

func (s *Service) availability(ctx context.Context, p model.Principal, save *model.Save) error {
	game, err := s.Repository.GameDetail(ctx, p.User.ID, save.Game.ID, "published")
	if errors.Is(err, model.ErrNotFound) {
		save.RestoreReason = "game_unavailable"
		return nil
	}
	if err != nil {
		return wrap(err)
	}
	directory, err := s.Repository.Directory(ctx, game.Game.PlatformInstanceID)
	if err != nil {
		return wrap(err)
	}
	prepared, err := s.Runs.Runtime.Identity(ctx, directory, game.RuntimeConfig, game.Files, save.Extinfo.CoreID)
	if errors.Is(err, model.ErrInvalid) {
		save.RestoreReason = "core_unavailable"
		return nil
	}
	if err != nil {
		return wrap(err)
	}
	current := model.Extinfo{
		CoreID:          prepared.CoreID,
		ProviderID:      prepared.ProviderID,
		TargetID:        prepared.TargetID,
		CoreFingerprint: prepared.CoreFingerprint,
		ROMHash:         prepared.ROMHash,
	}
	result, err := s.Runs.Restorable(ctx, true, *save, true, &current, prepared.ReadFormats)
	if err != nil {
		return wrap(err)
	}
	save.Restorable = result.Restorable
	save.RestoreReason = ""
	if result.Reason != nil {
		save.RestoreReason = strings.ToLower(*result.Reason)
	}
	return nil
}

func (s *Service) Write(ctx context.Context,
	p model.Principal,
	id string,
	input model.SaveInput,
	payload,
	screenshot io.Reader) (model.Save,
	error,
) {
	if !model.UUID(input.CommitID) {
		return model.Save{}, model.ErrInvalid
	}
	create := id == ""
	if create {
		id = input.CommitID
	}
	existing, readErr := s.Repository.Save(ctx, p.User.ID, id)
	if readErr == nil && existing.LastCommitID == input.CommitID {
		return s.retry(ctx, p, existing, input, payload)
	}
	if err := readExisting(readErr, create); err != nil {
		return model.Save{}, err
	}
	run, err := s.Runs.Get(ctx, p, input.RunID)
	if err != nil {
		return model.Save{}, wrap(err)
	}
	if err = validateInput(input, run); err != nil {
		return model.Save{}, err
	}
	if readErr == nil && (existing.Game.ID != run.Run.GameID || existing.Kind != input.Kind || create) {
		return model.Save{}, model.ErrConflict
	}
	save, err := s.prepare(ctx, p, id, input, run, payload, screenshot)
	if err != nil {
		return model.Save{}, err
	}

	err = s.commit(ctx, save, create)
	if err != nil {
		return model.Save{}, err
	}
	value, err := s.Repository.Save(ctx, p.User.ID, id)
	if err != nil {
		return value, wrap(err)
	}
	err = s.availability(ctx, p, &value)
	return value, err
}

func (s *Service) commit(ctx context.Context, save model.Save, create bool) error {
	return wrap(s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		status, _, lockErr := r.LockGame(ctx, save.Game.ID)
		if lockErr != nil {
			return wrap(lockErr)
		}
		if status != "published" {
			return model.ErrNotFound
		}
		previous, readErr := r.Save(ctx, save.UserID, save.ID)
		if readErr == nil && previous.LastCommitID == save.LastCommitID {
			if previous.PayloadHash == save.PayloadHash && equalInfo(previous.Extinfo, save.Extinfo) {
				return nil
			}
			return model.ErrConflict
		}
		if readErr != nil && !errors.Is(readErr, model.ErrNotFound) {
			return wrap(readErr)
		}
		return wrap(r.WriteSave(ctx, save, s.Now().UnixMilli(), create))
	}))
}

func validateInput(input model.SaveInput, run runs.Context) error {
	if run.Run.Purpose != "play" || run.Checkpoint == nil {
		return model.ErrForbidden
	}
	if !model.Text(input.Name, 120) || !model.OneOf(input.Kind, "checkpoint", "game_save") {
		return model.ErrInvalid
	}
	if input.Slot != nil && !model.Text(*input.Slot, 120) {
		return model.ErrInvalid
	}
	if !equalInfo(input.Extinfo, run.Run.Extinfo) {
		return model.ErrInvalid
	}
	native := run.Checkpoint.Semantics == "GAME_SAVE"
	if native != (input.Kind == "game_save") {
		return model.ErrInvalid
	}
	return nil
}

func equalInfo(left, right model.Extinfo) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	var first, second any
	if json.Unmarshal(a, &first) != nil || json.Unmarshal(b, &second) != nil {
		return false
	}
	return reflect.DeepEqual(first, second)
}

func (s *Service) retry(ctx context.Context,
	p model.Principal,
	existing model.Save,
	input model.SaveInput,
	payload io.Reader) (model.Save,
	error,
) {
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(payload, existing.SizeBytes+1))
	if err != nil {
		return existing, fmt.Errorf("hash save retry: %w", err)
	}
	if size != existing.SizeBytes || hex.EncodeToString(hash.Sum(nil)) != existing.PayloadHash || !equalInfo(input.Extinfo,
		existing.Extinfo) || input.Kind != existing.Kind {
		return existing, model.ErrConflict
	}
	if err = s.availability(ctx, p, &existing); err != nil {
		return existing, err
	}
	return existing, nil
}

func (s *Service) Rename(ctx context.Context, p model.Principal, id, name string, version int64) (model.Save, error) {
	if !model.Text(name, 120) || version < 1 {
		return model.Save{}, model.ErrInvalid
	}
	if _, err := s.Repository.Save(ctx, p.User.ID, id); err != nil {
		return model.Save{}, wrap(err)
	}
	if err := s.Repository.RenameSave(ctx, p.User.ID, id, name, version, s.Now().UnixMilli()); err != nil {
		return model.Save{}, wrap(err)
	}
	result, err := s.Repository.Save(ctx, p.User.ID, id)
	if err != nil {
		return result, wrap(err)
	}
	err = s.availability(ctx, p, &result)
	return result, err
}

func (s *Service) Delete(ctx context.Context, p model.Principal, id string, version int64) error {
	if version < 1 {
		return model.ErrInvalid
	}
	if _, err := s.Repository.Save(ctx, p.User.ID, id); err != nil {
		return wrap(err)
	}
	return wrap(s.Repository.DeleteSave(ctx, p.User.ID, id, version, s.Now().UnixMilli()))
}

func (s *Service) File(ctx context.Context, p model.Principal, id string, image bool) (storage.File, error) {
	save, err := s.Repository.Save(ctx, p.User.ID, id)
	if err != nil {
		return storage.File{}, wrap(err)
	}
	if image {
		if save.ScreenshotKey == "" {
			return storage.File{}, model.ErrNotFound
		}
		return storage.File{Key: save.ScreenshotKey}, nil
	}
	return storage.File{Key: save.StorageKey, SHA256: save.PayloadHash, Size: save.SizeBytes}, nil
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("save operation: %w", err)
}

func (s *Service) prepare(ctx context.Context, p model.Principal, id string, input model.SaveInput,
	run runs.Context, payload, screenshot io.Reader,
) (model.Save, error) {
	file, err := s.Storage.Write(ctx, "saves", id, payload, run.Checkpoint.MaxBytes)
	if err != nil {
		return model.Save{}, wrap(err)
	}
	if file.Size == 0 {
		return model.Save{}, model.ErrInvalid
	}
	save := model.Save{
		ID:           id,
		UserID:       p.User.ID,
		Game:         model.Game{ID: run.Run.GameID},
		Kind:         input.Kind,
		Name:         input.Name,
		Slot:         input.Slot,
		Version:      input.Version,
		Extinfo:      run.Run.Extinfo,
		StorageKey:   file.Key,
		PayloadHash:  file.SHA256,
		SizeBytes:    file.Size,
		LastCommitID: input.CommitID,
	}
	if screenshot != nil {
		save.ScreenshotKey, err = s.screenshot(ctx, id, screenshot)
		if err != nil {
			return model.Save{}, err
		}
	}
	return save, nil
}

func readExisting(err error, create bool) error {
	if err == nil {
		return nil
	}
	if !create || !errors.Is(err, model.ErrNotFound) {
		return wrap(err)
	}
	return nil
}

func (s *Service) screenshot(ctx context.Context, id string, screenshot io.Reader) (string, error) {
	raw, readErr := io.ReadAll(io.LimitReader(screenshot, 10*1024*1024+1))
	if readErr != nil {
		return "", fmt.Errorf("read save screenshot: %w", readErr)
	}
	if len(raw) > 10*1024*1024 {
		return "", model.ErrInvalid
	}
	config, format, decodeErr := image.DecodeConfig(bytes.NewReader(raw))
	if decodeErr != nil || !model.OneOf(format, "png", "jpeg") ||
		config.Width < 1 || config.Height < 1 || config.Width > 8192 || config.Height > 8192 {
		return "", model.ErrInvalid
	}
	image, writeErr := s.Storage.Write(ctx, "saves", id, bytes.NewReader(raw), 10*1024*1024)
	if writeErr != nil {
		return "", wrap(writeErr)
	}
	return image.Key, nil
}
