package maintenance

import (
	"time"

	"retrom/internal/model/diagnostics"

	model "retrom/internal/model/maintenance"
)

type Service struct {
	repository model.Repository
	now        func() time.Time
	locks      model.DataRootLocker
	reporter   diagnostics.ErrorReporter
}

func New(
	repository model.Repository,
	now func() time.Time,
	locks model.DataRootLocker,
	reporter diagnostics.ErrorReporter,
) *Service {
	if reporter == nil {
		panic("maintenance requires a diagnostic reporter")
	}
	return &Service{repository: repository, now: now, locks: locks, reporter: reporter}
}
