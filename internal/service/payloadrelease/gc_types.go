package payloadrelease

import (
	"time"

	model "retrom/internal/model/payloadrelease"
)

type GCOptions struct {
	Now       func() time.Time
	Retention time.Duration
	NewID     func() (string, error)
	Wake      func()
}

type GCScheduler struct {
	repository model.GCRepository
	now        func() time.Time
	newID      func() (string, error)
	retention  time.Duration
	wake       func()
}

func NewGCScheduler(repository model.GCRepository, options GCOptions) (*GCScheduler, error) {
	if options.Retention < 24*time.Hour || options.Retention > 30*24*time.Hour {
		return nil, model.ErrGCRetentionInvalid
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &GCScheduler{
		repository: repository, now: options.Now, newID: NewScheduler(options.NewID).Identity,
		retention: options.Retention, wake: options.Wake,
	}, nil
}
