package gamemetadata

import "time"

type Service struct {
	repository CandidateApplyRepository
	now        func() time.Time
}

func New(repository CandidateApplyRepository, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	return &Service{repository: repository, now: now}
}
