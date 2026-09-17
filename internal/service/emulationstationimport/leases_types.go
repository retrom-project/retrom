package emulationstationimport

import (
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type Leases struct {
	repository model.LeaseRepository
	now        func() time.Time
}

func NewLeases(repository model.LeaseRepository, now func() time.Time) *Leases {
	return &Leases{repository: repository, now: now}
}
