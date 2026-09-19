package gamemetadata

import (
	"time"

	model "retrom/internal/model/gamemetadata"
)

type Service struct {
	repository model.CandidateApplyRepository
	now        func() time.Time
}

func New(repository model.CandidateApplyRepository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, now: now}
}
