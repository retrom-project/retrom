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
	"retrom/internal/testkit/testsupport"
)

// handoffCommitFailure wraps a ReviewHandoffRepository to inject errors.
type handoffCommitFailure struct {
	emulationstationimportmodel.ReviewHandoffRepository
	err error
}

func (h handoffCommitFailure) CommitReviewHandoff(
	ctx context.Context, request emulationstationimportmodel.ReviewHandoffRequest, nowMS int64,
	auditID, actorKind string, actorUserID, actorLabel *string,
) error {
	if err := h.ReviewHandoffRepository.CommitReviewHandoff(ctx, request, nowMS, auditID, actorKind, actorUserID, actorLabel); err != nil {
		return err
	}
	return h.err
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

func TestESReviewHandoffLateCommitFailurePreservesState(t *testing.T) {
	fixture := newLifecycleFixture(t)
	_, unit := startLifecycleImport(t, fixture, "", "nes")
	item, ordinary := reserveExecutionReview(t, fixture, unit)
	before := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID)

	var commits atomic.Int64
	faultDB := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, _ []driver.NamedValue) error {
			if strings.Contains(query, "COMMIT") {
				commits.Add(1)
				return errExecutionReviewFault
			}
			return nil
		},
	})
	repo := persistence.NewReviewHandoff(faultDB)
	err := repo.CommitReviewHandoff(
		fixture.context,
		emulationstationimportmodel.ReviewHandoffRequest{
			Execution: unit, ItemID: item.ID,
			LibraryJobID: ordinary.Created.ImportJobID, LibraryItemID: ordinary.Items[0].ItemID,
		},
		fixture.service.now().UnixMilli(),
		"test-audit-id", "SYSTEM", nil, ptrStr("release-setup"),
	)
	if !errors.Is(err, errExecutionReviewFault) {
		t.Fatalf("commit cause=%v", err)
	}
	if after := executionReviewSnapshot(t, fixture, unit, ordinary.Items[0].ItemID); after != before {
		t.Fatal("late commit retained metadata, audit, source or progress")
	}
}

func ptrStr(s string) *string { return &s }
