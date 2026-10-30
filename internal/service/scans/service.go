package scans

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"

	"github.com/google/uuid"
)

// ContentPreparationTimeout includes source staging, normalization, package verification and commit.
// The runtime's one-hour normalization budget leaves thirty minutes for the host's bounded I/O.
const ContentPreparationTimeout = 90 * time.Minute

type Service struct {
	Repository *persistence.Repository
	Runtime    *runtimeclient.Client
	Storage    *storage.Store
	Sources    storage.Sources
	Uploads    storage.Uploads
	Redis      Temporary
	Now        func() time.Time
	Context    context.Context
	mutex      sync.Mutex
	cancel     map[string]context.CancelFunc
	workers    sync.WaitGroup

	uploadState  uploads
	replacements replacements
}

func (s *Service) Inspect(ctx context.Context,
	p model.Principal,
	input model.SourceInput) ([]model.SourceEntry,
	error,
) {
	if err := p.Admin(); err != nil {
		return nil, wrap(err)
	}
	input, err := s.resolveSource(ctx, p, input)
	if err != nil {
		return nil, err
	}
	collections, err := s.inspect(ctx, input)
	if err != nil {
		return nil, err
	}
	result := make([]model.SourceEntry, 0, len(collections))
	for _, collection := range collections {
		result = append(result, collection.Entry)
	}
	return result, nil
}

func (s *Service) Create(ctx context.Context, p model.Principal, input model.GameScanInput) (model.Scan, error) {
	if err := p.Admin(); err != nil {
		return model.Scan{}, wrap(err)
	}
	source, err := s.resolveSource(ctx, p,
		model.SourceInput{Path: input.Path, UploadID: input.UploadID, Format: input.Format})
	if err != nil {
		return model.Scan{}, err
	}
	collections, err := s.inspect(ctx, source)
	if err != nil {
		return model.Scan{}, err
	}
	input.Path = source.Path
	mappings, err := s.validateMappings(ctx, collections, input.Mappings)
	if err != nil {
		return model.Scan{}, err
	}
	scan := model.Scan{
		ID:          uuid.NewString(),
		ScanType:    "game",
		Status:      "running",
		CreatedAtMs: s.Now().UnixMilli(),
		UpdatedAtMs: s.Now().UnixMilli(),
	}
	worker, cancel := context.WithCancel(context.WithoutCancel(ctx))
	if err = s.admit(scan.ID, cancel); err != nil {
		cancel()
		return model.Scan{}, err
	}
	if input.UploadID != "" {
		if err = s.consumeUpload(ctx, p, input.UploadID); err != nil {
			s.release(scan.ID)
			s.workers.Done()
			return model.Scan{}, err
		}
	}
	if err = s.Repository.CreateScan(ctx, scan, p.User.ID); err != nil {
		s.release(scan.ID)
		s.workers.Done()
		if input.UploadID != "" {
			s.finishUpload(input.UploadID)
		}
		return scan, wrap(err)
	}
	stop := s.onShutdown(cancel)
	go func() {
		defer stop()
		defer s.workers.Done()
		if input.UploadID != "" {
			defer s.finishUpload(input.UploadID)
		}
		s.run(worker, scan, p, input, collections, mappings)
	}()
	return scan, nil
}

func (s *Service) run(ctx context.Context,
	scan model.Scan,
	p model.Principal,
	input model.GameScanInput,
	collections []Collection,
	mappings map[string]model.SourceMapping,
) {
	defer s.release(scan.ID)
	root, err := s.Sources.Open(input.Path)
	if err != nil {
		s.failed(ctx, &scan, "SOURCE_UNAVAILABLE")
		return
	}
	defer closeRoot(root)
	scan.TotalKnown = true
	for _, collection := range collections {
		if _, exists := mappings[collection.Entry.Key]; exists {
			scan.TotalCount += int64(len(collection.Games))
		}
	}
	for _, collection := range collections {
		mapping, exists := mappings[collection.Entry.Key]
		if !exists {
			continue
		}
		inventory := &sourceInventory{}
		for _, candidate := range collection.Games {
			if ctx.Err() != nil {
				scan.Status = s.stopStatus()
				s.progress(context.WithoutCancel(ctx), &scan)
				return
			}
			candidateCtx, cancel := context.WithTimeout(ctx, ContentPreparationTimeout)
			importErr := s.importGame(candidateCtx, root, p, collection.Directory, candidate, mapping, &scan, inventory)
			cancel()
			if errors.Is(importErr, persistence.ErrCommitUncertain) {
				s.interruptUncertain(ctx, scan.ID)
				return
			}
			if importErr != nil {
				if ctx.Err() != nil {
					scan.Status = s.stopStatus()
					s.progress(context.WithoutCancel(ctx), &scan)
					return
				}
				scan.ProcessedCount++
				scan.FailedCount++
				code := importFailureCode(importErr)
				scan.Error = &code
				slog.Warn("game scan candidate failed", "scanId", scan.ID, "error", importErr)
				s.progress(ctx, &scan)
			}
		}
	}
	scan.Status = "completed"
	s.progress(ctx, &scan)
}

func (s *Service) importGame(ctx context.Context,
	root *os.Root,
	p model.Principal,
	base string,
	candidate Candidate,
	mapping model.SourceMapping, scan *model.Scan, inventory *sourceInventory,
) error {
	directory, err := s.Repository.Directory(ctx, mapping.DirectoryID)
	if err != nil {
		return wrap(err)
	}
	if !directory.Enabled {
		return model.ErrInvalid
	}
	selected, err := root.OpenRoot(base)
	if err != nil {
		return fmt.Errorf("open source collection: %w", err)
	}
	defer closeRoot(selected)
	id := uuid.NewString()
	defer s.removeStaging(id)
	source, err := s.content(ctx, selected, id, candidate.Files, directory, inventory)
	if err != nil {
		return err
	}
	files, err := s.normalize(ctx, id, directory.PlatformID, source.Files)
	if err != nil {
		return err
	}
	config, err := s.configure(ctx, directory, files)
	if err != nil {
		return err
	}
	prepared, prepareErr := s.Runtime.Identity(ctx, directory, config, files, "")
	if prepareErr != nil {
		return wrap(prepareErr)
	}
	hash := prepared.ROMHash
	candidate.Input.PlatformInstanceID = directory.ID
	candidate.Input.TagIDs = mapping.TagIDs
	candidate.Input.RuntimeConfig = config
	if err = model.ValidateGameFields(candidate.Input); err != nil {
		return wrap(err)
	}
	media, err := s.media(ctx, selected, id, candidate)
	if err != nil {
		return err
	}
	physical, err := s.finalizeSourceContent(ctx, id, id, directory, config, files, source)
	if err != nil {
		return err
	}
	game := model.PreparedGame{ID: id, Input: candidate.Input, Files: physical, Media: media, ContentHash: hash}
	imported := scan.ImportedCount
	err = s.commitGame(ctx, p, game, scan)
	if !errors.Is(err, persistence.ErrCommitUncertain) && (err != nil || scan.ImportedCount == imported) {
		s.removePrepared(id, physical)
	}
	return err
}

func (s *Service) configure(ctx context.Context,
	directory model.Directory,
	files []model.ContentFile) (json.RawMessage,
	error,
) {
	locators := make(map[string]runtimeclient.Locator, len(files))
	for _, file := range files {
		absolute, err := s.Storage.Absolute(file.StorageKey)
		if err != nil {
			return nil, wrap(err)
		}
		locators[file.LogicalKey] = runtimeclient.Locator{Path: absolute, Offset: file.Offset, SizeBytes: file.SizeBytes}
	}
	var config json.RawMessage
	err := s.Runtime.Call(ctx,
		"configure",
		map[string]any{
			"platformId":    directory.PlatformID,
			"coreIds":       directory.CoreIDs,
			"files":         runtimeclient.Files(files),
			"locators":      locators,
			"providerRoots": s.Runtime.ProviderRoots(),
		},
		&config)
	return config, wrap(err)
}

func (s *Service) List(ctx context.Context, p model.Principal, q model.Query) (model.Page[model.Scan], error) {
	if err := p.Admin(); err != nil {
		return model.Page[model.Scan]{}, wrap(err)
	}
	items, err := s.Repository.Scans(ctx, q)
	return items, wrap(err)
}

func (s *Service) Cancel(ctx context.Context, p model.Principal, id string) error {
	if err := p.Admin(); err != nil {
		return wrap(err)
	}
	s.mutex.Lock()
	cancel, exists := s.cancel[id]
	s.mutex.Unlock()
	if exists {
		cancel()
		return nil
	}
	scan, err := s.Repository.Scan(ctx, id)
	if err != nil {
		return wrap(err)
	}
	if scan.Status == "running" {
		return model.ErrConflict
	}
	return nil
}

func (s *Service) release(id string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if cancel := s.cancel[id]; cancel != nil {
		cancel()
	}
	delete(s.cancel, id)
}

func (s *Service) admit(id string, cancel context.CancelFunc) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if len(s.cancel) >= 2 {
		return model.ErrUnavailable
	}
	if s.cancel == nil {
		s.cancel = make(map[string]context.CancelFunc)
	}
	s.cancel[id] = cancel
	s.workers.Add(1)
	return nil
}

func (s *Service) Wait() { s.workers.Wait() }

func (s *Service) stopStatus() string {
	if s.Context.Err() != nil {
		return "interrupted"
	}
	return "cancelled"
}

func (s *Service) progress(ctx context.Context, scan *model.Scan) {
	scan.UpdatedAtMs = s.Now().UnixMilli()
	if err := s.Repository.UpdateScan(ctx, *scan); err != nil {
		slog.Error("write scan progress", "scanId", scan.ID, "error", err)
	}
}

func (s *Service) failed(ctx context.Context, scan *model.Scan, code string) {
	scan.Status = "failed"
	scan.Error = &code
	s.progress(ctx, scan)
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("scan operation: %w", err)
}

func (s *Service) onShutdown(cancel context.CancelFunc) func() bool {
	return context.AfterFunc(s.Context, cancel)
}

func logProgress(id string, err error) { slog.Error("write scan progress", "scanId", id, "error", err) }

func (s *Service) validateMappings(ctx context.Context, collections []Collection, values []model.SourceMapping,
) (map[string]model.SourceMapping, error) {
	mappings := make(map[string]model.SourceMapping, len(values))
	sourceKeys := make(map[string]bool, len(collections))
	for _, collection := range collections {
		sourceKeys[collection.Entry.Key] = true
	}
	for _, mapping := range values {
		if _, exists := mappings[mapping.SourceKey]; exists || !sourceKeys[mapping.SourceKey] {
			return nil, model.ErrInvalid
		}
		directory, readErr := s.Repository.Directory(ctx, mapping.DirectoryID)
		if readErr != nil {
			return nil, wrap(readErr)
		}
		if !directory.Enabled {
			return nil, model.ErrInvalid
		}
		if err := model.ValidateTagIDs(mapping.TagIDs); err != nil {
			return nil, wrap(err)
		}
		for _, id := range mapping.TagIDs {
			if _, err := s.Repository.Tag(ctx, id); err != nil {
				return nil, wrap(err)
			}
		}
		if !model.UUID(mapping.DirectoryID) {
			return nil, model.ErrInvalid
		}
		mappings[mapping.SourceKey] = mapping
	}
	if len(mappings) == 0 {
		return nil, model.ErrInvalid
	}
	return mappings, nil
}
