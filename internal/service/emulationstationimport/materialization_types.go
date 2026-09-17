package emulationstationimport

import (
	model "retrom/internal/model/emulationstationimport"
	"time"
)

type Materialization struct {
	repository model.MaterialRepository
	now        func() time.Time
}

func NewMaterialization(repository model.MaterialRepository, now func() time.Time) *Materialization {
	return &Materialization{repository: repository, now: now}
}
