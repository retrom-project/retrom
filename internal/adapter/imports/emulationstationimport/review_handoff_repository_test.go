package emulationstationimport

import (
	"context"
	"database/sql/driver"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	emulationstationimportmodel "retrom/internal/model/emulationstationimport"
	persistence "retrom/internal/repo/emulationstationimport"
	emulationstationimportservice "retrom/internal/service/emulationstationimport"
	library "retrom/internal/service/libraryimport"
	"retrom/internal/testkit/testsupport"
)

type handoffCallbackFailure struct {
	emulationstationimportmodel.ReviewHandoffRepository
}

func (repository handoffCallbackFailure) WithReviewHandoff(
	ctx context.Context,
	run func(emulationstationimportmodel.ReviewHandoffScope) error,
) error {
	return repository.ReviewHandoffRepository.WithReviewHandoff(ctx, func(scope emulationstationimportmodel.ReviewHandoffScope) error {
		if err := run(scope); err != nil {
			return err
		}
		return errExecutionReviewFault
	})
}

func TestESReviewHandoffLateCallbackRollsBackAllWrites(t *testing.T) {
	fixture := newLifecycleFixture(t)
	_, unit := startLifecycleImport(t, fixture, "", "nes")
	item, ordinary := reserveExecutionReview(t, fixture, unit)
	before := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID)
	service := emulationstationimportservice.NewReviewHandoff(
		handoffCallbackFailure{persistence.NewReviewHandoff(fixture.database)},
		library.NewMetadataSeeder(nil, fixture.service.now),
		fixture.service.now,
	)
	err := service.Complete(
		fixture.context,
		emulationstationimportmodel.ReviewHandoffRequest{Execution: unit, ItemID: item.ID, LibraryJobID: ordinary.Created.ImportJobID, LibraryItemID: ordinary.Items[0].ItemID},
	)
	if !errors.Is(err, errExecutionReviewFault) {
		t.Fatalf("callback cause=%v", err)
	}
	if after := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID); after != before {
		t.Fatal("late callback retained metadata, audit, source or progress")
	}
}

func TestESReviewHandoffReadFailuresRetainCauses(t *testing.T) {
	for _, prefix := range []string{
		"SELECT source.id,source.execution_state",
		"SELECT draft.metadata_json",
		"SELECT count(*)",
	} {
		t.Run(prefix, func(t *testing.T) {
			fixture := newLifecycleFixture(t)
			_, unit := startLifecycleImport(t, fixture, "", "nes")
			item, ordinary := reserveExecutionReview(t, fixture, unit)
			before := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID)
			var hits atomic.Int64
			faultDB := testsupport.OpenSQLFaultDatabase(
				t,
				fixture.database,
				testsupport.SQLFaultHooks{BeforeQuery: func(_ context.Context, query string, args []driver.NamedValue) error {
					if !strings.HasPrefix(strings.Join(strings.Fields(query), " "), prefix) {
						return nil
					}
					for _, arg := range args {
						if arg.Value == item.ID || arg.Value == ordinary.Items[0].ItemID || arg.Value == unit.ImportID {
							hits.Add(1)
							return errExecutionReviewFault
						}
					}
					return nil
				}},
			)
			fixture.service.database = faultDB
			err := fixture.service.finalizeReviewHandoff(
				fixture.context,
				unit,
				item,
				ordinary.Created.ImportJobID,
				ordinary.Items[0].ItemID,
				nil,
			)
			if !errors.Is(err, errExecutionReviewFault) || hits.Load() != 1 {
				t.Fatalf("read cause=%v hits=%d", err, hits.Load())
			}
			if after := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID); after != before {
				t.Fatal("failed read retained handoff changes")
			}
		})
	}
}
