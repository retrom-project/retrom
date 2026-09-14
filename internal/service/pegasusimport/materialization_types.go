package pegasusimport

import "time"

type Materialization struct {
	repository MaterialRepository
	now        func() time.Time
}

func NewMaterialization(repository MaterialRepository, now func() time.Time) *Materialization {
	return &Materialization{repository: repository, now: now}
}
