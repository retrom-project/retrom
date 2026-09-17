package gamemetadata

import (
	model "retrom/internal/model/gamemetadata"
	"time"
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
