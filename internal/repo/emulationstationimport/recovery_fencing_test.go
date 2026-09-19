package emulationstationimport

import (
	"context"
	"reflect"
	"testing"
	"time"

	model "retrom/internal/model/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
)

type recoveryCASRepo struct {
	model.RecoveryRepository
	scenario string
}

func (r recoveryCASRepo) CommitRecovery(ctx context.Context, change model.RecoveryChange) error {
	before := &change.Before
	switch r.scenario {
	case "job version":
		before.JobVersion++
	case "plan version":
		before.ImportVersion++
	case "attempt":
		before.Attempt++
	case "execution":
		before.ExecutionNo++
	case "deadline":
		before.DeadlineAtMS = 900
	case "kind":
		before.Kind = "SERVER_EMULATIONSTATION_IMPORT"
	case "scope":
		before.ImportID = "import-1"
	case "link":
		before.JobID = "other"
	case "lease":
		before.LeaseUntilMS = 1000
	case "worker":
		before.WorkerID = "replacement"
	}
	return r.RecoveryRepository.CommitRecovery(ctx, change)
}

func TestRecoveryCASRejectsReplacedScopeBudgetAndOwner(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"job version", "plan version", "attempt", "execution", "deadline", "kind", "scope", "link", "lease", "worker"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			db, _ := recoveryDatabase(t, false, true)
			seedLeaseOrphan(t, db)
			before := planRows(t, db)

			repo := recoveryCASRepo{
				RecoveryRepository: NewRecovery(db),
				scenario:           scenario,
			}
			err := emulationstationimportservice.NewRecovery(repo, func() time.Time { return time.UnixMilli(1500) }).Recover(t.Context())
			if err != nil {
				t.Fatalf("stale candidate wasn't skipped: %v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("stale CAS retained recovery or replacement writes")
			}
		})
	}
}
