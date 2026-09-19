package maintenance

import (
	"time"

	model "retrom/internal/model/maintenance"
)

type Service struct {
	repository model.Repository
	now        func() time.Time
	locks      model.DataRootLocker
}

func New(repository model.Repository, now func() time.Time, locks model.DataRootLocker) *Service {
	return &Service{repository: repository, now: now, locks: locks}
}
