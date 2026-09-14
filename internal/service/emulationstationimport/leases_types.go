package emulationstationimport

import "time"

type Leases struct {
	repository LeaseRepository
	now        func() time.Time
}

func NewLeases(repository LeaseRepository, now func() time.Time) *Leases {
	return &Leases{repository: repository, now: now}
}
