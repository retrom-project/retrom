package scans

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"

	"github.com/google/uuid"
)

type biosCandidate struct {
	Name   string
	SHA256 string
}

func (s *Service) CreateBios(ctx context.Context, p model.Principal, input model.BiosScanInput) (model.Scan, error) {
	if err := p.Admin(); err != nil {
		return model.Scan{}, wrap(err)
	}
	root, err := s.Sources.Open(input.Path)
	if err != nil {
		return model.Scan{}, wrap(err)
	}
	closeRoot(root)
	if len(input.CoreIDs) == 0 && len(input.PlatformIDs) == 0 {
		return model.Scan{}, model.ErrInvalid
	}
	all, err := s.Runtime.AllBios(ctx)
	if err != nil {
		return model.Scan{}, wrap(err)
	}
	requirements := make([]runtimeclient.BiosRequirement, 0)
	for _, req := range all {
		if matchesScope(s.Runtime, req, input) {
			requirements = append(requirements, req)
		}
	}
	if len(requirements) == 0 {
		return model.Scan{}, model.ErrInvalid
	}
	scan := model.Scan{
		ID: uuid.NewString(), ScanType: "bios", Status: "running",
		CreatedAtMs: s.Now().UnixMilli(), UpdatedAtMs: s.Now().UnixMilli(),
	}
	worker, cancel := context.WithCancel(context.WithoutCancel(ctx))
	if err = s.admit(scan.ID, cancel); err != nil {
		cancel()
		return model.Scan{}, err
	}
	if err = s.Repository.CreateScan(ctx, scan, p.User.ID); err != nil {
		s.release(scan.ID)
		s.workers.Done()
		return scan, wrap(err)
	}
	stop := s.onShutdown(cancel)
	go func() { defer stop(); defer s.workers.Done(); s.runBios(worker, scan, input, requirements) }()
	return scan, nil
}

func matchesScope(runtime *runtimeclient.Client, req runtimeclient.BiosRequirement, input model.BiosScanInput) bool {
	for _, core := range input.CoreIDs {
		if core == req.CoreID {
			return true
		}
	}
	for _, binding := range runtime.Bindings {
		if binding.CoreID != req.CoreID {
			continue
		}
		for _, platform := range input.PlatformIDs {
			for _, candidate := range binding.PlatformIDs {
				if candidate == platform {
					return true
				}
			}
		}
	}
	return false
}

func (s *Service) runBios(ctx context.Context, scan model.Scan, input model.BiosScanInput,
	requirements []runtimeclient.BiosRequirement,
) {
	defer s.release(scan.ID)
	root, err := s.Sources.Open(input.Path)
	if err != nil {
		s.failed(ctx, &scan, "SOURCE_UNAVAILABLE")
		return
	}
	defer closeRoot(root)
	unique := uniqueRequirements(requirements)
	scan.TotalKnown = true
	scan.TotalCount = int64(len(unique))
	s.progress(ctx, &scan)
	candidates, err := s.biosCandidates(ctx, root, requirements)
	if err != nil {
		if ctx.Err() != nil {
			scan.Status = s.stopStatus()
			s.progress(context.WithoutCancel(ctx), &scan)
		} else {
			s.failed(context.WithoutCancel(ctx), &scan, "SOURCE_INVALID")
		}
		return
	}
	for _, req := range unique {
		if ctx.Err() != nil {
			scan.Status = s.stopStatus()
			s.progress(context.WithoutCancel(ctx), &scan)
			return
		}
		prepared, prepareErr := s.prepareBios(ctx, root, req, candidates[req.RequirementKey])
		if err = s.commitBios(ctx, &scan, prepared, prepareErr); err != nil {
			if errors.Is(err, persistence.ErrCommitUncertain) {
				s.interruptUncertain(ctx, scan.ID)
				return
			}
			s.failed(context.WithoutCancel(ctx), &scan, "DATABASE_UNAVAILABLE")
			return
		}
	}
	scan.Status = "completed"
	s.progress(ctx, &scan)
}

func uniqueRequirements(requirements []runtimeclient.BiosRequirement) []runtimeclient.BiosRequirement {
	result := make([]runtimeclient.BiosRequirement, 0, len(requirements))
	seen := make(map[string]bool, len(requirements))
	for _, req := range requirements {
		if !seen[req.RequirementKey] {
			seen[req.RequirementKey] = true
			result = append(result, req)
		}
	}
	return result
}

func (s *Service) biosCandidates(ctx context.Context, root *os.Root,
	requirements []runtimeclient.BiosRequirement,
) (map[string][]biosCandidate, error) {
	names, err := sourceFiles(root, ".", 0)
	if err != nil {
		return nil, err
	}
	result := make(map[string][]biosCandidate)
	for _, name := range names {
		if err = ctx.Err(); err != nil {
			return nil, fmt.Errorf("BIOS scan cancelled: %w", err)
		}
		facts, readErr := storage.Measure(ctx, root, name, 512*1024*1024)
		if readErr != nil {
			slog.Warn("skip unreadable BIOS source", "error", readErr)
			continue
		}
		facts.Name = path.Base(name)
		var matched []runtimeclient.BiosRequirement
		if readErr = s.Runtime.Call(ctx, "bios-identify", map[string]any{
			"file": facts,
			"path": filepath.Join(root.Name(), filepath.FromSlash(name)), "requirements": requirements,
		},
			&matched); readErr != nil {
			slog.Warn("skip unrecognized BIOS source", "error", readErr)
			continue
		}
		for _, req := range matched {
			result[req.RequirementKey] = appendCandidate(result[req.RequirementKey], biosCandidate{
				Name:   name,
				SHA256: facts.SHA256,
			})
		}
	}
	return result, nil
}

func appendCandidate(existing []biosCandidate, value biosCandidate) []biosCandidate {
	for _, candidate := range existing {
		if candidate.SHA256 == value.SHA256 {
			return existing
		}
	}
	return append(existing, value)
}

func (s *Service) prepareBios(ctx context.Context, root *os.Root, req runtimeclient.BiosRequirement,
	candidates []biosCandidate,
) (model.BiosFile, error) {
	if _, err := s.Repository.Bios(ctx, req.RequirementKey); err == nil {
		return model.BiosFile{}, nil
	} else if !errors.Is(err, model.ErrNotFound) {
		return model.BiosFile{}, wrap(err)
	}
	if len(candidates) != 1 {
		return model.BiosFile{}, nil
	}
	id := uuid.NewString()
	source, err := root.Open(candidates[0].Name)
	if err != nil {
		return model.BiosFile{}, fmt.Errorf("open matched BIOS: %w", err)
	}
	defer closeFile(source)
	prepared, err := s.Storage.Write(ctx, "bios", id, source, 512*1024*1024)
	if err != nil {
		return model.BiosFile{}, wrap(err)
	}
	if prepared.SHA256 != candidates[0].SHA256 {
		return model.BiosFile{}, model.ErrConflict
	}
	return model.BiosFile{
		ID: id, RequirementKey: req.RequirementKey, Filename: path.Base(candidates[0].Name),
		StorageKey: prepared.Key, SizeBytes: prepared.Size, SHA256: prepared.SHA256,
	}, nil
}

func (s *Service) commitBios(ctx context.Context, scan *model.Scan, file model.BiosFile, prepareErr error) error {
	next := *scan
	next.ProcessedCount++
	next.UpdatedAtMs = s.Now().UnixMilli()
	err := s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		switch {
		case prepareErr != nil:
			next.FailedCount++
		case file.ID == "":
			next.SkippedCount++
		default:
			installed, writeErr := r.InstallBios(ctx, file, s.Now().UnixMilli())
			if writeErr != nil {
				return wrap(writeErr)
			}
			if installed {
				next.ImportedCount++
			} else {
				next.SkippedCount++
			}
		}
		return wrap(r.UpdateScan(ctx, next))
	})
	return s.confirmCommit(ctx, scan, next, file.ID, "bios_file_tab", err)
}

func sourceFiles(root *os.Root, directory string, depth int) ([]string, error) {
	if depth > 12 {
		return nil, model.ErrInvalid
	}
	entries, err := readDir(root, directory)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := path.Join(directory, entry.Name())
		if entry.IsDir() {
			children, walkErr := sourceFiles(root, name, depth+1)
			if walkErr != nil {
				return nil, walkErr
			}
			result = append(result, children...)
		} else {
			result = append(result, name)
		}
		if len(result) > 10000 {
			return nil, model.ErrInvalid
		}
	}
	return result, nil
}
