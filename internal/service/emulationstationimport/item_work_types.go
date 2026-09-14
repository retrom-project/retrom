package emulationstationimport

import "time"

type ItemWork struct {
	repository ItemWorkRepository
	now        func() time.Time
}

func NewItemWork(repository ItemWorkRepository, now func() time.Time) *ItemWork {
	return &ItemWork{repository: repository, now: now}
}
