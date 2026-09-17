package emulationstationimport

import (
	model "retrom/internal/model/emulationstationimport"
	"time"
)

type Leases struct {
	repository model.LeaseRepository
	now        func() time.Time
}

func NewLeases(repository model.LeaseRepository, now func() time.Time) *Leases {
	return &Leases{repository: repository, now: now}
}
