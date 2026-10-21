package scans

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"sync"
	"time"

	"retrom/internal/model"
	"retrom/internal/storage"

	"github.com/google/uuid"
)

const (
	// uploadWindow is how long a session stays resumable after its last write.
	uploadWindow = 24 * time.Hour
	// uploadReserve is free space kept beyond twice the declared total (staging + managed copy).
	uploadReserve int64 = 10 * 1024 * 1024 * 1024
)

// Temporary is the narrow Redis capability upload sessions use.
type Temporary interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key, value string, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
}

// Upload sessions are temporary: the owner lives in Redis with a sliding TTL and the bytes
// in the staging directory; neither is a business fact.
type uploads struct {
	mutex   sync.Mutex
	writing sync.Map // "<id>/<index>" → struct{} while a chunk is being written
	scanned map[string]bool
}

func uploadKey(id string) string { return "upload:" + id }

func (s *Service) CreateUpload(ctx context.Context, p model.Principal, input model.UploadInput) (model.Upload, error) {
	if err := p.Admin(); err != nil {
		return model.Upload{}, wrap(err)
	}
	total, err := validateUploadFiles(input.Files)
	if err != nil {
		return model.Upload{}, err
	}
	s.uploadState.mutex.Lock()
	defer s.uploadState.mutex.Unlock()
	active, err := s.activeUploads(ctx)
	if err != nil {
		return model.Upload{}, err
	}
	if active >= model.UploadMaximumSessions {
		return model.Upload{}, model.ErrUploadLimit
	}
	free, err := s.Uploads.FreeBytes()
	if err != nil {
		return model.Upload{}, wrap(err)
	}
	if free < 2*total+uploadReserve {
		return model.Upload{}, model.ErrStorageFull
	}
	id := uuid.NewString()
	if err = s.Uploads.Create(id, input.Files); err != nil {
		return model.Upload{}, errors.Join(wrap(err), s.Uploads.Remove(id))
	}
	if err = s.Redis.Set(ctx, uploadKey(id), p.User.ID, uploadWindow); err != nil {
		return model.Upload{}, errors.Join(wrap(err), s.Uploads.Remove(id))
	}
	return s.uploadView(id)
}

func (s *Service) Upload(ctx context.Context, p model.Principal, id string) (model.Upload, error) {
	if err := s.ownUpload(ctx, p, id); err != nil {
		return model.Upload{}, err
	}
	return s.uploadView(id)
}

// WriteUpload appends one chunk; a wrong offset or a chunk already being written for the
// same file returns ErrUploadOffset so the client re-reads the received bytes.
func (s *Service) WriteUpload(ctx context.Context, p model.Principal, id string, index int, offset int64,
	reader io.Reader,
) (model.UploadProgress, error) {
	if err := s.ownUpload(ctx, p, id); err != nil {
		return model.UploadProgress{}, err
	}
	key := fmt.Sprintf("%s/%d", id, index)
	if _, busy := s.uploadState.writing.LoadOrStore(key, struct{}{}); busy {
		return model.UploadProgress{}, model.ErrUploadOffset
	}
	defer s.uploadState.writing.Delete(key)
	if err := s.Redis.Set(ctx, uploadKey(id), p.User.ID, uploadWindow); err != nil {
		return model.UploadProgress{}, wrap(err)
	}
	received, err := s.Uploads.Append(id, index, offset, reader)
	if err != nil {
		return model.UploadProgress{}, wrap(err)
	}
	files, _, err := s.Uploads.Files(id)
	if err != nil {
		return model.UploadProgress{}, wrap(err)
	}
	return model.UploadProgress{ReceivedBytes: received, Completed: received == files[index].SizeBytes}, nil
}

func (s *Service) DeleteUpload(ctx context.Context, p model.Principal, id string) error {
	if err := s.ownUpload(ctx, p, id); err != nil {
		return err
	}
	if err := s.Redis.Delete(ctx, uploadKey(id)); err != nil {
		return wrap(err)
	}
	return wrap(s.Uploads.Remove(id))
}

// uploadSource returns the staged tree of a complete upload owned by p.
func (s *Service) uploadSource(ctx context.Context, p model.Principal, id string) (string, error) {
	if err := s.ownUpload(ctx, p, id); err != nil {
		return "", err
	}
	files, received, err := s.Uploads.Files(id)
	if err != nil {
		return "", wrap(err)
	}
	for index := range files {
		if received[index] != files[index].SizeBytes {
			return "", model.ErrInvalid
		}
	}
	source, err := s.Uploads.Source(id)
	return source, wrap(err)
}

// consumeUpload hands a complete upload to a starting scan: it can no longer be written,
// inspected or scanned again, and finishUpload removes it when the scan ends.
func (s *Service) consumeUpload(ctx context.Context, p model.Principal, id string) error {
	if _, err := s.uploadSource(ctx, p, id); err != nil {
		return err
	}
	s.uploadState.mutex.Lock()
	defer s.uploadState.mutex.Unlock()
	if s.uploadState.scanned == nil {
		s.uploadState.scanned = make(map[string]bool)
	}
	s.uploadState.scanned[id] = true
	return wrap(s.Redis.Delete(ctx, uploadKey(id)))
}

// finishUpload removes the staged files after any scan outcome (design D3).
func (s *Service) finishUpload(id string) {
	if err := s.Uploads.Remove(id); err != nil {
		slog.Error("remove scanned upload", "uploadId", id, "error", err)
	}
	s.uploadState.mutex.Lock()
	delete(s.uploadState.scanned, id)
	s.uploadState.mutex.Unlock()
}

// RunUploadCleanup removes staging left behind by lost sessions once a day (design D9).
func (s *Service) RunUploadCleanup(ctx context.Context) {
	ticker := time.NewTicker(uploadWindow)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.CleanupUploads(ctx); err != nil {
				slog.Error("clean abandoned uploads", "error", err)
			}
		}
	}
}

func (s *Service) CleanupUploads(ctx context.Context) error {
	s.uploadState.mutex.Lock()
	defer s.uploadState.mutex.Unlock()
	ids, err := s.Uploads.IDs()
	if err != nil {
		return wrap(err)
	}
	cutoff := s.Now().Add(-uploadWindow)
	var failures error
	for id, changed := range ids {
		if s.uploadState.scanned[id] || changed.After(cutoff) {
			continue
		}
		if _, getErr := s.Redis.Get(ctx, uploadKey(id)); !errors.Is(getErr, model.ErrContextExpired) {
			failures = errors.Join(failures, getErr)
			continue
		}
		failures = errors.Join(failures, s.Uploads.Remove(id))
	}
	return wrap(failures)
}

func (s *Service) activeUploads(ctx context.Context) (int, error) {
	ids, err := s.Uploads.IDs()
	if err != nil {
		return 0, wrap(err)
	}
	active := 0
	for id := range ids {
		_, getErr := s.Redis.Get(ctx, uploadKey(id))
		if getErr == nil {
			active++
		} else if !errors.Is(getErr, model.ErrContextExpired) {
			return 0, wrap(getErr)
		}
	}
	return active, nil
}

func (s *Service) ownUpload(ctx context.Context, p model.Principal, id string) error {
	if err := p.Admin(); err != nil {
		return wrap(err)
	}
	if !model.UUID(id) {
		return model.ErrNotFound
	}
	owner, err := s.Redis.Get(ctx, uploadKey(id))
	if errors.Is(err, model.ErrContextExpired) || (err == nil && owner != p.User.ID) {
		return model.ErrNotFound
	}
	return wrap(err)
}

func (s *Service) uploadView(id string) (model.Upload, error) {
	files, received, err := s.Uploads.Files(id)
	if err != nil {
		return model.Upload{}, wrap(err)
	}
	view := model.Upload{
		ID: id, ChunkSizeBytes: model.UploadChunkBytes, Completed: true,
		ExpiresAtMs: s.Now().Add(uploadWindow).UnixMilli(), Files: make([]model.UploadFile, len(files)),
	}
	for index, file := range files {
		view.Files[index] = model.UploadFile{Path: file.Path, SizeBytes: file.SizeBytes, ReceivedBytes: received[index]}
		view.Completed = view.Completed && received[index] == file.SizeBytes
	}
	return view, nil
}

// validateUploadFiles enforces the declared tree before anything is staged: safe unique
// relative paths no deeper than a scan walks, no file doubling as a directory, and limits.
func validateUploadFiles(files []model.UploadFileInput) (int64, error) {
	if len(files) == 0 || len(files) > model.UploadMaximumFiles {
		return 0, model.ErrInvalid
	}
	names := make(map[string]bool, len(files))
	var total int64
	for _, file := range files {
		if !storage.SafeRelative(file.Path) || file.Path == "." || strings.Count(file.Path, "/") > 24 ||
			names[file.Path] || file.SizeBytes < 0 || file.SizeBytes > model.UploadMaximumFileBytes {
			return 0, model.ErrInvalid
		}
		names[file.Path] = true
		total += file.SizeBytes
	}
	if total > model.UploadMaximumTotalBytes {
		return 0, model.ErrInvalid
	}
	for name := range names {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if names[parent] {
				return 0, model.ErrInvalid
			}
		}
	}
	return total, nil
}
