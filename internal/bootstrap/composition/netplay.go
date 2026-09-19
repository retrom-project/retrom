package composition

import (
	"database/sql"
	"time"

	netplaymodel "retrom/internal/model/netplay"
	"retrom/internal/model/netplayprofile"

	validationrepository "retrom/internal/repo/corevalidation"
	repository "retrom/internal/repo/netplay"
	tagrepository "retrom/internal/repo/tagging"
	"retrom/internal/service/corevalidation"
	"retrom/internal/service/netplay"
	"retrom/internal/service/tagging"
)

func NewNetplay(
	database *sql.DB,
	registry *netplayprofile.Registry,
	signer netplaymodel.CredentialSigner,
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
