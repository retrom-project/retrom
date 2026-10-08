package tags

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"retrom/internal/model"
	"retrom/internal/persistence"

	"github.com/google/uuid"
)

type Service struct {
	Repository *persistence.Repository
	Now        func() time.Time
}

func ValidName(value string) bool {
	if !model.Text(value, 40) {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

func (s *Service) List(ctx context.Context, q model.Query) (model.Page[model.Tag], error) {
	result, err := s.Repository.Tags(ctx, q)
	return result, wrap(err)
}

func (s *Service) Write(ctx context.Context, p model.Principal, id, name string, version int64) (model.Tag, error) {
	if err := p.Admin(); err != nil {
		return model.Tag{}, wrap(err)
	}
	name = strings.TrimSpace(name)
	if !ValidName(name) {
		return model.Tag{}, model.ErrInvalid
	}
	if id == "" {
		id = uuid.NewString()
		version = 0
	} else if version < 1 {
		return model.Tag{}, model.ErrInvalid
	}
	if err := s.Repository.WriteTag(ctx, id, p.User.ID, name, version, s.Now().UnixMilli()); err != nil {
		return model.Tag{}, wrap(err)
	}
	item, err := s.Repository.Tag(ctx, id)
	return item, wrap(err)
}

func (s *Service) Delete(ctx context.Context, p model.Principal, id string, version int64) error {
	if err := p.Admin(); err != nil {
		return wrap(err)
	}
	if version < 1 {
		return model.ErrInvalid
	}
	return wrap(s.Repository.DeleteTag(ctx, id, p.User.ID, version, s.Now().UnixMilli()))
}

func wrap(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("tag operation: %w", err)
}
