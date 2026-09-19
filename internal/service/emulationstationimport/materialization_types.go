package emulationstationimport

import (
	"time"

	model "retrom/internal/model/emulationstationimport"
)

type Materialization struct {
	repository model.MaterialRepository
	now        func() time.Time
}

func NewMaterialization(repository model.MaterialRepository, now func() time.Time) *Materialization {
	return &Materialization{repository: repository, now: now}
}
