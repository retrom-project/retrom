package maintenance

import "time"

type Service struct {
	repository Repository
	now        func() time.Time
}

func New(repository Repository, now func() time.Time) *Service { return &Service{repository, now} }
