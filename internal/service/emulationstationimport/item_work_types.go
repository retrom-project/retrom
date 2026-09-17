package emulationstationimport

import (
	model "retrom/internal/model/emulationstationimport"
	"time"
)

type ItemWork struct {
	repository model.ItemWorkRepository
	now        func() time.Time
}

func NewItemWork(repository model.ItemWorkRepository, now func() time.Time) *ItemWork {
	return &ItemWork{repository: repository, now: now}
}
