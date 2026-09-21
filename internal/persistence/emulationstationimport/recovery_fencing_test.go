package emulationstationimport

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"retrom/internal/dbexec"
	application "retrom/internal/service/emulationstationimport"
)

type recoveryConcurrentChange struct {
	*Recovery
	scenario string
}

func (repository recoveryConcurrentChange) WithRecovery(ctx context.Context, work func(application.RecoveryScope) error) error {
	return repository.Recovery.WithRecovery(ctx, func(scope application.RecoveryScope) error {
		records, ok := scope.Write.(recoveryRecords)
		if !ok {
			return errors.New("unexpected recovery scope")
		}
		scope.Write = recoveryConcurrentWriter{RecoveryWriter: records, executor: records.executor, scenario: repository.scenario}
		return work(scope)
	})
}

type recoveryConcurrentWriter struct {
	application.RecoveryWriter
	executor dbexec.Executor
	scenario string
}

func (writer recoveryConcurrentWriter) Apply(ctx context.Context, change application.RecoveryChange) error {
	if err := (leaseConcurrentWriter{executor: writer.executor, scenario: writer.scenario}).replace(ctx, change.Before); err != nil {
		return err
	}
	return writer.RecoveryWriter.Apply(ctx, change)
}

func TestRecoveryCASRejectsReplacedScopeBudgetAndOwner(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"job version", "plan version", "attempt", "execution", "deadline", "kind", "scope", "link", "lease", "worker"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			db, _ := recoveryDatabase(t, false, true)
			seedLeaseOrphan(t, db)
			before := planRows(t, db)
			err := application.NewRecovery(recoveryConcurrentChange{Recovery: NewRecovery(db), scenario: scenario}, func() time.Time { return time.UnixMilli(1500) }).Recover(t.Context())
			if err != nil {
				t.Fatalf("stale candidate wasn't skipped: %v", err)
			}
			if !reflect.DeepEqual(before, planRows(t, db)) {
				t.Fatal("stale CAS retained recovery or replacement writes")
			}
		})
	}
}
