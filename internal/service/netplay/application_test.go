package netplay

import (
	"context"
	model "retrom/internal/model/netplay"
	"testing"
	"testing/synctest"
	"time"
)

type countingMaintenance struct {
	*roomMaintenanceMemory
	reads int
}

func (repository *countingMaintenance) Passive(ctx context.Context, cutoffs model.ExpiryCutoffs) ([]model.ExpiryCandidate, error) {
	repository.reads++
	return repository.roomMaintenanceMemory.Passive(ctx, cutoffs)
}

func TestNetplayApplicationMaintenanceStartsAndStopsOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		repo := &countingMaintenance{roomMaintenanceMemory: &roomMaintenanceMemory{}}
		app := NewService(Components{Maintenance: NewRoomMaintenance(repo, repo, time.Now)}, nil)
		app.StartMaintenance()
		app.StartMaintenance()
		time.Sleep(31 * time.Second)
		synctest.Wait()
		if repo.reads != 1 {
			t.Fatalf("maintenance ran %d times", repo.reads)
		}
		app.Close()
		app.Close()
		app.StartMaintenance()
		time.Sleep(31 * time.Second)
		synctest.Wait()
		if repo.reads != 1 {
			t.Fatalf("closed maintenance ran %d times", repo.reads)
		}
	})
}

func TestNetplayApplicationCanCloseBeforeStart(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(_ *testing.T) {
		app := NewService(Components{}, nil)
		app.Close()
		app.StartMaintenance()
		app.Close()
	})
}
