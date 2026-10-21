package scans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"slices"
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

// uploadSession is the Redis value of one upload: who may use it, how it will be read and
// when it was last written (its sliding expiry).
type uploadSession struct {
	Owner       string `json:"owner"`
	Format      string `json:"format"`
	TouchedAtMs int64  `json:"touchedAtMs"`
}

func uploadKey(id string) string { return "upload:" + id }

func (s *Service) saveSession(ctx context.Context, id string, session uploadSession) (uploadSession, error) {
	session.TouchedAtMs = s.Now().UnixMilli()
	value, err := json.Marshal(session)
	if err != nil {
		return session, fmt.Errorf("encode upload session: %w", err)
	}
	return session, wrap(s.Redis.Set(ctx, uploadKey(id), string(value), uploadWindow))
}

// readSession returns ErrContextExpired when the session no longer exists.
func (s *Service) readSession(ctx context.Context, id string) (uploadSession, error) {
	value, err := s.Redis.Get(ctx, uploadKey(id))
	if err != nil {
		return uploadSession{}, fmt.Errorf("read upload session: %w", err)
	}
	var session uploadSession
	if err = json.Unmarshal([]byte(value), &session); err != nil {
		return uploadSession{}, fmt.Errorf("decode upload session: %w", err)
	}
	return session, nil
}

func (s *Service) CreateUpload(ctx context.Context, p model.Principal, input model.UploadInput) (model.Upload, error) {
	if err := p.Admin(); err != nil {
		return model.Upload{}, wrap(err)
	}
	total, err := validateUploadFiles(input.Files)
	if err != nil || !model.OneOf(input.Format, "pegasus", "emulationstation", "files") {
		return model.Upload{}, errors.Join(model.ErrInvalid, err)
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
	session, err := s.saveSession(ctx, id, uploadSession{Owner: p.User.ID, Format: input.Format})
	if err != nil {
		return model.Upload{}, errors.Join(err, s.Uploads.Remove(id))
	}
	return s.uploadView(id, session)
}

func (s *Service) Upload(ctx context.Context, p model.Principal, id string) (model.Upload, error) {
	session, err := s.ownUpload(ctx, p, id)
	if err != nil {
		return model.Upload{}, err
	}
	return s.uploadView(id, session)
}

// ListUploads returns p's uploads that have not started a scan, newest first, so an
// administrator can resume or map them after a reload or from another browser.
func (s *Service) ListUploads(ctx context.Context, p model.Principal) ([]model.Upload, error) {
	if err := p.Admin(); err != nil {
		return nil, wrap(err)
	}
	ids, err := s.Uploads.IDs()
	if err != nil {
		return nil, wrap(err)
	}
	order := slices.SortedFunc(func(yield func(string) bool) {
		for id := range ids {
			if !yield(id) {
				return
			}
		}
	}, func(left, right string) int { return ids[right].Compare(ids[left]) })
	result := make([]model.Upload, 0, len(order))
	for _, id := range order {
		session, readErr := s.readSession(ctx, id)
		if errors.Is(readErr, model.ErrContextExpired) || (readErr == nil && session.Owner != p.User.ID) {
			continue
		}
		if readErr != nil {
			return nil, wrap(readErr)
		}
		view, viewErr := s.uploadView(id, session)
		if errors.Is(viewErr, os.ErrNotExist) {
			continue // cancelled or scanned between listing and reading
		}
		if viewErr != nil {
			return nil, viewErr
		}
		result = append(result, view)
	}
	return result, nil
}

// WriteUpload appends one chunk; a wrong offset or a chunk already being written for the
// same file returns ErrUploadOffset so the client re-reads the received bytes.
func (s *Service) WriteUpload(ctx context.Context, p model.Principal, id string, index int, offset int64,
	reader io.Reader,
) (model.UploadProgress, error) {
	session, err := s.ownUpload(ctx, p, id)
	if err != nil {
		return model.UploadProgress{}, err
	}
	key := fmt.Sprintf("%s/%d", id, index)
	if _, busy := s.uploadState.writing.LoadOrStore(key, struct{}{}); busy {
		return model.UploadProgress{}, model.ErrUploadOffset
	}
	defer s.uploadState.writing.Delete(key)
	if _, err = s.saveSession(ctx, id, session); err != nil {
		return model.UploadProgress{}, err
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
	if _, err := s.ownUpload(ctx, p, id); err != nil {
		return err
	}
	if err := s.Redis.Delete(ctx, uploadKey(id)); err != nil {
		return wrap(err)
	}
	return wrap(s.Uploads.Remove(id))
}

// uploadSource returns the staged tree of a complete upload owned by p and remembers the
// format it is read with, which a resumed mapping offers again.
func (s *Service) uploadSource(ctx context.Context, p model.Principal, id, format string) (string, error) {
	session, err := s.ownUpload(ctx, p, id)
	if err != nil {
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
	if format != session.Format && model.OneOf(format, "pegasus", "emulationstation", "files") {
		session.Format = format
		if _, err = s.saveSession(ctx, id, session); err != nil {
			return "", err
		}
	}
	source, err := s.Uploads.Source(id)
	return source, wrap(err)
}

// consumeUpload hands a complete upload to a starting scan: it can no longer be written,
// inspected or scanned again, and finishUpload removes it when the scan ends.
func (s *Service) consumeUpload(ctx context.Context, p model.Principal, id string) error {
	session, err := s.ownUpload(ctx, p, id)
	if err != nil {
		return err
	}
	if _, err = s.uploadSource(ctx, p, id, session.Format); err != nil {
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

func (s *Service) ownUpload(ctx context.Context, p model.Principal, id string) (uploadSession, error) {
	if err := p.Admin(); err != nil {
		return uploadSession{}, wrap(err)
	}
	if !model.UUID(id) {
		return uploadSession{}, model.ErrNotFound
	}
	session, err := s.readSession(ctx, id)
	if errors.Is(err, model.ErrContextExpired) || (err == nil && session.Owner != p.User.ID) {
		return uploadSession{}, model.ErrNotFound
	}
	return session, wrap(err)
}

func (s *Service) uploadView(id string, session uploadSession) (model.Upload, error) {
	files, received, err := s.Uploads.Files(id)
	if err != nil {
		return model.Upload{}, wrap(err)
	}
	view := model.Upload{
		ID: id, Format: session.Format, ChunkSizeBytes: model.UploadChunkBytes, Completed: true,
		ExpiresAtMs: session.TouchedAtMs + uploadWindow.Milliseconds(), Files: make([]model.UploadFile, len(files)),
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
