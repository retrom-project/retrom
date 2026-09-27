//go:build integration

package libraryimport

import (
	"errors"
	"testing"
)

func TestApprovalRequiresCompletedSourceHandoff(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	created, err := fixture.service.CreateOwnedServerSource(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	before := approvalDatabaseRows(t, fixture.database)
	result, err := fixture.service.Approve(t.Context(), created.Items[0].ItemID, 1)
	if !errors.Is(err, ErrInvalid) || result != (Approved{}) {
		t.Fatalf("unhanded source result=%+v err=%v", result, err)
	}
	assertApprovalRowsUnchanged(t, fixture.database, before)
}
