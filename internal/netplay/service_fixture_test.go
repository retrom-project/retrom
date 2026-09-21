package netplay

import (
	"database/sql"
	"time"

	"retrom/internal/composition"
	validationrepository "retrom/internal/persistence/corevalidation"
	repository "retrom/internal/persistence/netplay"
	"retrom/internal/service/corevalidation"
	application "retrom/internal/service/netplay"
)

// Service groups application dependencies used by database-backed regression fixtures.
// Production composition lives in internal/composition.
type Service struct {
	*application.Service
	database    *sql.DB
	registry    *Registry
	credentials *Credentials
	clock       Clock
	options     Options
	preparation *application.ParticipantPreparation
}

func NewService(database *sql.DB, registry *Registry, credentials *Credentials, options Options, now func() time.Time) *Service {
	if now == nil {
		now = time.Now
	}
	exit := application.NewRoomExit(repository.NewRoomExit(database), options.WaitingIdle, now)
	return &Service{Service: composition.NewNetplay(database, registry, credentials, options, now), database: database, registry: registry, credentials: credentials, clock: clockFunc(now), options: options, preparation: application.NewParticipantPreparation(repository.NewParticipantPreparation(database), credentials, exit, now)}
}

func (service *Service) eligibility() *application.Eligibility {
	return application.NewEligibility(repository.NewEligibility(service.database), service.registry, nil, corevalidation.New(validationrepository.New(service.database)))
}
