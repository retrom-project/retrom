//go:build integration

package libraryimport

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"retrom/internal/authn"
	dbapi "retrom/internal/database"
	commandrepository "retrom/internal/persistence/idempotency"
	repository "retrom/internal/persistence/libraryimport"
	"retrom/internal/service/idempotency"
	service "retrom/internal/service/libraryimport"
	"retrom/internal/testsupport"
)

func TestPublicationReceiptFailureRollsBackGameAndRecoveryCompletesOriginalCommand(t *testing.T) {
	fixture, source := ownedSourceFixture(t)
	created, err := fixture.service.CreateOwnedServerSource(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	itemID := created.Items[0].ItemID
	finishOwnedReviewHandoff(t, fixture, source)
	actor := prepareApprovalSelections(t, fixture, itemID)
	cause := errors.New("publication receipt unavailable")
	hits := 0
	fault := testsupport.OpenSQLFaultDatabase(t, fixture.database, testsupport.SQLFaultHooks{
		BeforeExec: func(_ context.Context, query string, args []driver.NamedValue) error {
			if strings.Contains(query, "INSERT INTO idempotency_records") && len(args) > 1 && args[1].Value == "postAdminReviewApprove" {
				hits++
				return cause
			}
			return nil
		},
	})
	fixture.service.approvals = service.NewReviewApprovals(repository.NewReviewApprovals(fault), fixture.service.tags,
		fixture.service.now, fixture.blobs)
	actorPrincipal, _ := authn.PrincipalFromContext(actor)
	principal := actorPrincipal.UserID
	identity := idempotency.Request{
		PrincipalID: principal, OperationID: "postAdminReviewApprove",
		Key: "01980000-0000-7000-8000-000000000083", Digest: strings.Repeat("3", 64),
	}
	ctx, command := idempotency.NewCommand(actor, identity, publicationReceiptEncoder, fixture.service.now().UnixMilli())
	coordinator := idempotency.New(commandrepository.New(fixture.database))
	err = coordinator.Coordinate(ctx, identity, command, func(ctx context.Context) error {
		_, err := fixture.service.Approve(ctx, itemID, 1)
		return err
	})
	if !errors.Is(err, cause) || hits != 1 {
		t.Fatalf("receipt failure: %v / %d", err, hits)
	}
	if _, found := command.Receipt(); found {
		t.Fatal("failed final transaction exposed successful response")
	}
	gameID := assertPublicationReceiptRollback(t, fixture, itemID)
	// A fresh service has no HTTP context, but the durable intent includes the
	// original authenticated identity and the frozen response.
	fixture.service.approvals = service.NewReviewApprovals(repository.NewReviewApprovals(fixture.database), fixture.service.tags,
		fixture.service.now, fixture.blobs)
	if err := fixture.service.approvals.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertApprovalSourcePublishedOnce(t, fixture, itemID, source.Intent.ItemID, gameID)
	assertApprovalSelectionsPublished(t, fixture, gameID)
	assertOriginalPublicationReceipt(t, coordinator, identity, gameID)
	assertPublicationAfterStaleReceiptLookup(actor, t, fixture, identity, itemID)
	if err := fixture.service.approvals.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertApprovalSourcePublishedOnce(t, fixture, itemID, source.Intent.ItemID, gameID)
}

func publicationReceiptEncoder(_ context.Context, value any) (idempotency.Receipt, error) {
	result, ok := value.(idempotency.Result)
	if !ok {
		return idempotency.Receipt{}, idempotency.ErrInvalidReceipt
	}
	body, err := json.Marshal(result.Value)
	if err != nil {
		return idempotency.Receipt{}, err
	}
	return idempotency.Receipt{
		HTTPStatus: 201, HeadersJSON: `{"Content-Type":"application/json; charset=utf-8"}`,
		Body: append(body, '\n'),
	}, nil
}

func assertPublicationReceiptRollback(t *testing.T, fixture deduplicateFixture, itemID string) string {
	t.Helper()
	var state, gameID string
	var games, receipts int
	if err := dbapi.QueryRowContext(t.Context(), fixture.database, `SELECT state,publication_game_id,
 (SELECT count(*) FROM games),(SELECT count(*) FROM idempotency_records WHERE operation_id='postAdminReviewApprove')
 FROM import_items WHERE id=?`, itemID).Scan(&state, &gameID, &games, &receipts); err != nil {
		t.Fatal(err)
	}
	if state != "PUBLISHING" || gameID == "" || games != 0 || receipts != 0 {
		t.Fatalf("non-atomic publication: %s/%s/%d/%d", state, gameID, games, receipts)
	}
	return gameID
}

func assertOriginalPublicationReceipt(t *testing.T, coordinator *idempotency.Service,
	identity idempotency.Request, gameID string,
) {
	t.Helper()
	receipt, found, err := coordinator.Lookup(t.Context(), identity.OperationID, identity.Key, identity.PrincipalID)
	if err != nil || !found || receipt.HTTPStatus != 201 || receipt.RequestDigest != identity.Digest {
		t.Fatalf("original recovered receipt: %#v / %v / %v", receipt, found, err)
	}
	var approved service.ReviewApproved
	if err := json.Unmarshal(receipt.Body, &approved); err != nil {
		t.Fatal(err)
	}
	if approved.GameID != gameID || approved.Status != "PUBLISHED" {
		t.Fatalf("original result changed: %#v", approved)
	}
}

// A command may read no receipt before waiting for publication's filesystem
// boundary, while recovery completes that same intent and original receipt.
func assertPublicationAfterStaleReceiptLookup(actor context.Context, t *testing.T, fixture deduplicateFixture,
	identity idempotency.Request, itemID string,
) {
	t.Helper()
	var createdBefore, expiresBefore, createdAfter, expiresAfter int64
	query := `SELECT created_at_ms,expires_at_ms FROM idempotency_records WHERE principal_id=? AND operation_id=? AND key=?`
	args := []any{identity.PrincipalID, identity.OperationID, identity.Key}
	if err := dbapi.QueryRowContext(actor, fixture.database, query, args...).Scan(&createdBefore, &expiresBefore); err != nil {
		t.Fatal(err)
	}
	ctx, command := idempotency.NewCommand(actor, identity, publicationReceiptEncoder,
		fixture.service.now().Add(time.Hour).UnixMilli())
	coordinator := idempotency.New(commandrepository.New(fixture.database))
	if err := coordinator.Coordinate(ctx, identity, command, func(ctx context.Context) error {
		_, err := fixture.service.Approve(ctx, itemID, 1)
		return err
	}); err != nil {
		t.Fatalf("recovery completed during command preparation: %v", err)
	}
	if err := dbapi.QueryRowContext(actor, fixture.database, query, args...).Scan(&createdAfter, &expiresAfter); err != nil {
		t.Fatal(err)
	}
	if createdAfter != createdBefore || expiresAfter != expiresBefore {
		t.Fatal("recovery completion extended original receipt lifetime")
	}
	if _, found := command.Receipt(); !found {
		t.Fatal("completed publication did not return its original success")
	}
}
