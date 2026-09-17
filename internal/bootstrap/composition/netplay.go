package composition

import (
	"database/sql"
	"time"

	netplaymodel "retrom/internal/model/netplay"
	validationrepository "retrom/internal/repo/corevalidation"
	repository "retrom/internal/repo/netplay"
	"retrom/internal/service/corevalidation"
	netplayservice "retrom/internal/service/netplay"
	"retrom/internal/transport/netplay/profile"
)

func NewNetplay(
	database *sql.DB,
	registry *profile.Registry,
	signer netplaymodel.CredentialSigner,
	options netplayservice.Options,
	now func() time.Time,
) *netplayservice.Service {
	if now == nil {
		now = time.Now
	}
	exit := netplayservice.NewRoomExit(repository.NewRoomExit(database), options.WaitingIdle, now)
	components := netplayservice.Components{
		Eligibility: netplayservice.NewEligibility(
			repository.NewEligibility(database),
			registry,
			newTagService(database, now),
			corevalidation.New(validationrepository.New(database)),
		),
		Queries: netplayservice.NewRoomQueries(repository.NewRoomQueries(database), now),
		Creation: netplayservice.NewRoomCreation(
			repository.NewRoomCreation(database),
			options.MaxActiveRooms,
			options.DraftIdle,
			now,
		),
		Controls: netplayservice.NewRoomControl(
			repository.NewRoomControl(database),
			registry,
			options.DraftIdle,
			options.WaitingIdle,
			now,
		),
		Starter:     netplayservice.NewSessionStart(repository.NewSessionStart(database), registry, now),
		Exit:        exit,
		Sessions:    netplayservice.NewSessionControl(repository.NewSessionControl(database), options.ReconnectLease, now),
		Maintenance: netplayservice.NewRoomMaintenance(repository.NewRoomMaintenance(database), exit, now),
		Events:      netplayservice.NewRoomEvents(repository.NewRoomEvents(database)),
		Access:      netplayservice.NewParticipantAccess(repository.NewParticipantAccess(database), signer),
		Preparation: netplayservice.NewParticipantPreparation(repository.NewParticipantPreparation(database), signer, exit, now),
	}
	return netplayservice.NewService(components, registry)
}
