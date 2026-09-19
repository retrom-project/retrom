package maintenance

import (
	"time"

	model "retrom/internal/model/maintenance"
)

type Service struct {
	repository model.Repository
	now        func() time.Time
}

func New(repository model.Repository, now func() time.Time) *Service {
	return &Service{repository, now}
}
