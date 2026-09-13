package platforminstance

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"retrom/internal/contentprofile"
	"retrom/internal/platformcatalog"
)

type Service struct {
	repository Repository
	now        func() time.Time
}

func New(repository Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (service *Service) ValidateCatalog(ctx context.Context) error {
	catalog := platformcatalog.Current()
	if err := platformcatalog.Validate(catalog); err != nil {
		return fmt.Errorf("%w: %w", ErrCatalogInvalid, err)
	}
	return repositoryError("validate catalog", service.repository.WithRead(ctx, func(reader Reader) error {
		_, err := reader.CatalogReferences(ctx, catalog)
		return repositoryError("resolve catalog", err)
	}))
}

func (service *Service) Recommendations(ctx context.Context) (Recommendations, error) {
	catalog := platformcatalog.Current()
	if err := platformcatalog.Validate(catalog); err != nil {
		return Recommendations{}, fmt.Errorf("%w: %w", ErrCatalogInvalid, err)
	}
	var result Recommendations
	err := service.repository.WithRead(ctx, func(reader Reader) error {
		references, err := reader.CatalogReferences(ctx, catalog)
		if err != nil {
			return repositoryError("resolve catalog", err)
		}
		rows, err := reader.Directories(ctx)
		if err != nil {
			return repositoryError("read directories", err)
		}
		result = projectRecommendations(catalog, references, rows)
		return nil
	})
	return result, repositoryError("recommendations", err)
}

func (service *Service) Create(ctx context.Context, actor AuditActor, input CreateInput) (Instance, error) {
	if !validText(input.Name, 1, 200, false) || !validText(input.Description, 0, 10_000, true) {
		return Instance{}, ErrInvalid
	}
	var created Instance
	err := service.repository.WithWrite(ctx, func(scope WriteScope) error {
		var err error
		created, err = service.createInstance(
			ctx, scope, actor, input, "", "PLATFORM_INSTANCE_CREATED", service.now().UnixMilli(),
		)
		return err
	})
	return created, repositoryError("create", err)
}

func (service *Service) createInstance(
	ctx context.Context, scope WriteScope, actor AuditActor, input CreateInput, catalogKey, action string, now int64,
) (Instance, error) {
	enabled, err := scope.Reader.CoreEnabled(ctx, input.PlatformID, input.DefaultCoreID)
	if err != nil {
		return Instance{}, repositoryError("validate default core", err)
	}
	if !enabled {
		return Instance{}, ErrDefaultCoreInvalid
	}
	base := SlugBase(input.Name, input.PlatformID)
	slugs, err := scope.Reader.UsedSlugs(ctx, input.PlatformID, base)
	if err != nil {
		return Instance{}, repositoryError("read slugs", err)
	}
	slug, err := NextSlug(base, slugs)
	if err != nil {
		return Instance{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Instance{}, fmt.Errorf("platforminstance: create id: %w", err)
	}
	directory := NewDirectory{ID: id.String(), Slug: slug, CatalogKey: catalogKey, Input: input, CreatedAtMS: now}
	if err := scope.Directories.Insert(ctx, directory); err != nil {
		return Instance{}, repositoryError("insert directory", err)
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return Instance{}, fmt.Errorf("platforminstance: create audit id: %w", err)
	}
	if err := scope.Directories.RecordCreation(ctx, CreationAudit{
		ID: auditID.String(), Action: action, Actor: actor, Directory: directory,
	}); err != nil {
		return Instance{}, repositoryError("record creation", err)
	}
	result, err := scope.Reader.Instance(ctx, directory.ID)
	if err != nil {
		return Instance{}, repositoryError("read created directory", err)
	}
	result.SupportedExtensions = contentprofile.SupportedExtensions(result.PlatformID)
	return result, nil
}

func repositoryError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("platforminstance: %s: %w", operation, err)
}
