package directory

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"retrom/internal/model"
	"retrom/internal/persistence"

	"github.com/google/uuid"
)

type Service struct {
	Repository *persistence.Repository
	Catalog    func(context.Context) (model.Catalog, error)
	Now        func() time.Time
}

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,79}$`)

func (s *Service) Validate(ctx context.Context, input model.DirectoryInput) error {
	if !model.Text(input.Name, 120) || !slugPattern.MatchString(input.Slug) ||
		!model.BoundedText(input.Description, 10000) {
		return model.ErrInvalid
	}
	if len(input.CoreIDs) < 1 || len(input.CoreIDs) > 100 {
		return model.ErrInvalid
	}
	catalog, err := s.Catalog(ctx)
	if err != nil {
		return fmt.Errorf("read runtime catalog: %w", err)
	}
	selected := make(map[string]bool, len(input.CoreIDs))
	for _, id := range input.CoreIDs {
		if selected[id] {
			return model.ErrInvalid
		}
		supported := false
		for _, core := range catalog.Cores {
			if core.ID != id {
				continue
			}
			for _, platform := range core.PlatformIDs {
				if platform == input.PlatformID {
					supported = true
				}
			}
		}
		if !supported {
			return model.ErrInvalid
		}
		selected[id] = true
	}
	if !selected[input.DefaultCoreID] {
		return model.ErrInvalid
	}
	return nil
}

func (s *Service) List(ctx context.Context, admin bool) ([]model.Directory, error) {
	items, err := s.Repository.Directories(ctx, admin)
	return items, wrap(err)
}

func (s *Service) Write(ctx context.Context,
	p model.Principal,
	id string,
	input model.DirectoryInput) (model.Directory,
	error,
) {
	if err := p.Admin(); err != nil {
		return model.Directory{}, wrap(err)
	}
	if err := s.Validate(ctx, input); err != nil {
		return model.Directory{}, err
	}
	create := id == ""
	if create {
		id = uuid.NewString()
	} else if input.Version < 1 {
		return model.Directory{}, model.ErrInvalid
	}
	err := s.Repository.Transaction(ctx, func(r *persistence.Repository) error {
		return wrap(r.WriteDirectory(ctx, id, input, s.Now().UnixMilli(), create))
	})
	if err != nil {
		return model.Directory{}, wrap(err)
	}
	item, err := s.Repository.Directory(ctx, id)
	return item, wrap(err)
}

func (s *Service) Delete(ctx context.Context, p model.Principal, id string, version int64) error {
	if err := p.Admin(); err != nil {
		return wrap(err)
	}
	if version < 1 {
		return model.ErrInvalid
	}
	return wrap(s.Repository.Transaction(ctx,
		func(r *persistence.Repository) error {
			return wrap(r.DeleteDirectory(ctx,
				id,
				version))
		}))
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("directory operation: %w", err)
}
