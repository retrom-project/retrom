package platforminstance

import (
	"context"
	"fmt"
	"time"

	model "retrom/internal/model/platforminstance"

	"github.com/google/uuid"

	"retrom/internal/capability/runtime/platformcatalog"
)

type Service struct {
	repository model.Repository
	now        func() time.Time
}

func New(repository model.Repository, now func() time.Time) *Service {
	return &Service{repository: repository, now: now}
}

func (service *Service) ValidateCatalog(ctx context.Context) error {
	catalog := platformcatalog.Current()
	if err := platformcatalog.Validate(catalog); err != nil {
		return fmt.Errorf("%w: %w", model.ErrCatalogInvalid, err)
	}
	_, err := service.repository.LoadCatalogReferences(ctx, catalog)
	return repositoryError("validate catalog", err)
}

func (service *Service) Recommendations(ctx context.Context) (model.Recommendations, error) {
	catalog := platformcatalog.Current()
	if err := platformcatalog.Validate(catalog); err != nil {
		return model.Recommendations{}, fmt.Errorf("%w: %w", model.ErrCatalogInvalid, err)
	}
	references, err := service.repository.LoadCatalogReferences(ctx, catalog)
	if err != nil {
		return model.Recommendations{}, repositoryError("resolve catalog", err)
	}
	rows, err := service.repository.LoadDirectories(ctx)
	if err != nil {
		return model.Recommendations{}, repositoryError("read directories", err)
	}
	return model.ProjectRecommendations(catalog, references, rows), nil
}

func (service *Service) Create(
	ctx context.Context,
	actor model.AuditActor,
	input model.CreateInput,
) (model.Instance, error) {
	if !validText(input.Name, 1, 200, false) || !validText(input.Description, 0, 10_000, true) {
		return model.Instance{}, model.ErrInvalid
	}
	id, err := uuid.NewV7()
	if err != nil {
		return model.Instance{}, fmt.Errorf("platforminstance: create id: %w", err)
	}
	auditID, err := uuid.NewV7()
	if err != nil {
		return model.Instance{}, fmt.Errorf("platforminstance: create audit id: %w", err)
	}
	created, err := service.repository.CommitCreate(ctx, model.CreateCommand{
		Actor: actor, Input: input, Action: "PLATFORM_INSTANCE_CREATED",
		NowMS: service.now().UnixMilli(), ID: id.String(), AuditID: auditID.String(),
	})
	return created, repositoryError("create", err)
}

func repositoryError(operation string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("platforminstance: %s: %w", operation, err)
}
