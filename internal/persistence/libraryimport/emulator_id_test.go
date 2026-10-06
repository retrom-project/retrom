package libraryimport

import (
	"testing"

	dbapi "retrom/internal/database"
)

func TestEmulatorNumbersAreReservedAcrossOverlappingTransactions(t *testing.T) {
	db := metadataDatabase(t)
	first, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(first)
	second, err := db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dbapi.Rollback(second)
	firstID, err := (reviewApprovalRecords{transaction: first}).NextEmulatorID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := (reviewApprovalRecords{transaction: second}).NextEmulatorID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if firstID == secondID {
		t.Fatalf("overlapping transactions allocated the same number: %d", firstID)
	}
	if err := first.Rollback(); err != nil {
		t.Fatal(err)
	}
	thirdID, err := (reviewApprovalRecords{transaction: second}).NextEmulatorID(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if thirdID <= max(firstID, secondID) {
		t.Fatalf("rolled-back number was reused: %d", thirdID)
	}
	if err := second.Commit(); err != nil {
		t.Fatal(err)
	}
}
