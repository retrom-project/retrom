package composition

import (
	"database/sql"
	"time"

	"retrom/internal/netplay/profile"
	validationrepository "retrom/internal/persistence/corevalidation"
	repository "retrom/internal/persistence/netplay"
	tagrepository "retrom/internal/persistence/tagging"
	"retrom/internal/service/corevalidation"
	"retrom/internal/service/netplay"
	"retrom/internal/service/tagging"
)

func NewNetplay(
	database *sql.DB,
	registry *profile.Registry,
	signer netplay.CredentialSigner,
	options netplay.Options,
	now func() time.Time,
) *netplay.Service {
	if now == nil {
		now = time.Now
	}
	exit := netplay.NewRoomExit(repository.NewRoomExit(database), options.WaitingIdle, now)
	components := netplay.Components{
		Eligibility: netplay.NewEligibility(
			repository.NewEligibility(database),
			registry,
			tagging.New(tagrepository.New(database), now),
			corevalidation.New(validationrepository.New(database)),
		),
		Queries: netplay.NewRoomQueries(repository.NewRoomQueries(database), now),
		Creation: netplay.NewRoomCreation(
			repository.NewRoomCreation(database),
			options.MaxActiveRooms,
			options.DraftIdle,
			now,
		),
		Controls: netplay.NewRoomControl(
			repository.NewRoomControl(database),
			registry,
			options.DraftIdle,
			options.WaitingIdle,
			now,
		),
		Starter:     netplay.NewSessionStart(repository.NewSessionStart(database), registry, now),
		Exit:        exit,
		Sessions:    netplay.NewSessionControl(repository.NewSessionControl(database), options.ReconnectLease, now),
		Maintenance: netplay.NewRoomMaintenance(repository.NewRoomMaintenance(database), exit, now),
		Events:      netplay.NewRoomEvents(repository.NewRoomEvents(database)),
		Access:      netplay.NewParticipantAccess(repository.NewParticipantAccess(database), signer),
		Preparation: netplay.NewParticipantPreparation(repository.NewParticipantPreparation(database), signer, exit, now),
	}
	return netplay.NewService(components, registry)
}
