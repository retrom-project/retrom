//go:build integration

package libraryimport

import (
	"bytes"
	"errors"
	"reflect"
	"slices"
	"testing"

	"retrom/internal/persistence/blobcatalog"
	application "retrom/internal/service/libraryimport"
)

func TestOwnedSourceKeepsAllDuplicateMatchesInOneBoundItem(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	first, err := fixture.service.CreateServerSource(fixture.ctx, fixture.platform, "STANDARD", request.Files, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.service.CreateServerSource(fixture.ctx, fixture.platform, "STANDARD", request.Files, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	original, err := fixture.service.Approve(fixture.ctx, first.Items[0].ItemID, 1)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := fixture.service.ApproveWithDecision(fixture.ctx, second.Items[0].ItemID, 1, ApprovalDecision{DuplicatePolicy: "ALLOW_NEW", AcknowledgedGameIDs: []string{original.GameID}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("duplicate ownership: %#v %v", result, err)
	}
	expected := []string{original.GameID, duplicate.GameID}
	slices.Sort(expected)
	actual := make([]string, 0, len(result.Items[0].ExistingMatches))
	for _, match := range result.Items[0].ExistingMatches {
		actual = append(actual, match.GameID)
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("duplicate fanout collapsed incorrectly: %v want %v", actual, expected)
	}
	assertOwnedSourceBinding(t, fixture, result)
}

func TestOwnedServerSourceRejectsUndeclaredPrimaryWithoutReview(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	request.Files[0].RelativePath = "games/companion.gba"
	result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if !errors.Is(err, ErrVersionConflict) || errors.Is(err, application.ErrSourceGrouping) || result.Created.ImportJobID != "" || ownedImportCount(t, fixture) != 0 {
		t.Fatalf("companion became unowned review: %#v %v", result, err)
	}
}

func TestOwnedSourceRejectsDifferentCopiedBlobAtDeclaredPath(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	different, err := fixture.blobs.Put(bytes.NewBufferString("different source payload"))
	if err != nil {
		t.Fatal(err)
	}
	blobID, err := blobcatalog.EnsureRecord(fixture.ctx, fixture.database, different, "application/octet-stream", ownedSourceNow().UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	request.Files[0].BlobID = blobID
	request.Files[0].SizeBytes = different.Size
	result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if !errors.Is(err, ErrVersionConflict) || result.Created.ImportJobID != "" || ownedImportCount(t, fixture) != 0 {
		t.Fatalf("frozen source content replaced: %#v %v", result, err)
	}
}

func TestOwnedSourceRejectsZeroContentGroupsWithoutReview(t *testing.T) {
	t.Parallel()
	fixture, request := ownedSourceFixture(t)
	fixture.execute(t, `UPDATE pegasus_import_item_files SET relative_path='unsupported.txt' WHERE item_id='unlinked-source'`)
	request.Intent.PrimaryPaths = []string{"unsupported.txt"}
	request.Files[0].RelativePath = "unsupported.txt"
	result, err := fixture.service.CreateOwnedServerSource(fixture.ctx, request)
	if !errors.Is(err, ErrInvalid) || !errors.Is(err, application.ErrSourceGrouping) || result.Created.ImportJobID != "" || ownedImportCount(t, fixture) != 0 {
		t.Fatalf("rejected source created review: %#v %v", result, err)
	}
}
