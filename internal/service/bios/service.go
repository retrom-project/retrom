package bios

import (
	"context"
	"fmt"
	"io"
	"path"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"
	"retrom/internal/runtimeclient"
	"retrom/internal/storage"

	"github.com/google/uuid"
)

type Service struct {
	Repository *persistence.Repository
	Runtime    *runtimeclient.Client
	Storage    *storage.Store
	Now        func() time.Time
}

func (s *Service) List(ctx context.Context) ([]model.BiosRequirement, error) {
	requirements, err := s.Runtime.AllBios(ctx)
	if err != nil {
		return nil, wrap(err)
	}
	installed, err := s.Repository.BiosFiles(ctx)
	if err != nil {
		return nil, wrap(err)
	}
	byKey := make(map[string]model.BiosFile, len(installed))
	for _, file := range installed {
		byKey[file.RequirementKey] = file
	}
	result := make([]model.BiosRequirement, 0, len(requirements))
	positions := make(map[string]int, len(requirements))
	for _, req := range requirements {
		value := model.BiosRequirement{
			Key: req.RequirementKey, Name: req.LogicalName,
			Requirements: []model.BiosValidationRequirement{{
				CoreID: req.CoreID, SizeBytes: req.SizeBytes, SHA256: req.SHA256, MD5: req.MD5,
			}},
			CoreIDs: []string{req.CoreID}, PlatformIDs: []string{}, Required: req.Required,
		}
		for _, binding := range s.Runtime.Bindings {
			if binding.CoreID == req.CoreID {
				value.PlatformIDs = appendUnique(value.PlatformIDs, binding.PlatformIDs...)
			}
		}
		if file, exists := byKey[req.RequirementKey]; exists {
			value.Installed = true
			value.Filename = file.Filename
			value.SizeBytes = file.SizeBytes
			value.SHA256 = file.SHA256
		}
		if i, exists := positions[value.Key]; exists {
			result[i].Requirements = append(result[i].Requirements, value.Requirements...)
			result[i].CoreIDs = appendUnique(result[i].CoreIDs, value.CoreIDs...)
			result[i].PlatformIDs = appendUnique(result[i].PlatformIDs, value.PlatformIDs...)
			result[i].Required = result[i].Required || value.Required
		} else {
			positions[value.Key] = len(result)
			result = append(result, value)
		}
	}
	return result, nil
}

func (s *Service) Upload(ctx context.Context, p model.Principal, key, name string,
	reader io.Reader,
) (model.BiosRequirement, error) {
	if err := p.Admin(); err != nil {
		return model.BiosRequirement{}, wrap(err)
	}
	requirements, err := s.Runtime.AllBios(ctx)
	if err != nil {
		return model.BiosRequirement{}, wrap(err)
	}
	valid := false
	for _, req := range requirements {
		if req.RequirementKey == key {
			valid = true
		}
	}
	if !valid {
		return model.BiosRequirement{}, model.ErrNotFound
	}
	id := uuid.NewString()
	prepared, err := s.Storage.Write(ctx, "bios", id, reader, 512*1024*1024)
	if err != nil {
		return model.BiosRequirement{}, wrap(err)
	}
	if prepared.Size == 0 {
		return model.BiosRequirement{}, model.ErrInvalid
	}
	file := model.BiosFile{
		ID: id, RequirementKey: key, Filename: path.Base(name), StorageKey: prepared.Key,
		SizeBytes: prepared.Size, SHA256: prepared.SHA256,
	}
	err = s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		return wrap(r.WriteBios(ctx,
			file, s.Now().UnixMilli()))
	})
	if err != nil {
		return model.BiosRequirement{}, wrap(err)
	}
	values, err := s.List(ctx)
	if err != nil {
		return model.BiosRequirement{}, err
	}
	for _, value := range values {
		if value.Key == key {
			return value, nil
		}
	}
	return model.BiosRequirement{}, model.ErrNotFound
}

func (s *Service) Delete(ctx context.Context, p model.Principal, key string) error {
	if err := p.Admin(); err != nil {
		return wrap(err)
	}
	return wrap(s.Repository.DeleteBios(ctx, key, s.Now().UnixMilli()))
}

func appendUnique(existing []string, values ...string) []string {
	for _, value := range values {
		found := false
		for _, previous := range existing {
			if previous == value {
				found = true
			}
		}
		if !found {
			existing = append(existing, value)
		}
	}
	return existing
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("BIOS operation: %w", err)
}
