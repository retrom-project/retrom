package libraryimport

import (
	"database/sql"
	"testing"

	"retrom/internal/corevalidation"
)

func TestRPGMakerApprovalUsesProviderValidationInputDigest(t *testing.T) {
	input := approvalValidationDigestInput{
		ContentID: "content-1", ContentKind: "RPG_MAKER_PROJECT",
		ProviderID: "retrom-runtime", TargetID: "rpgmaker-2000",
		DATID:        sql.NullString{},
		ValidationID: "validation-1", SnapshotValid: false,
	}

	got, err := approvalValidationInputDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	want, err := corevalidation.ProviderValidationInputDigest(
		input.ProviderID, input.TargetID, input.ContentID, input.DATID,
		corevalidation.Snapshot{SchemaVersion: corevalidation.SnapshotSchemaVersion, Kind: corevalidation.SnapshotKindStatic, BIOS: []corevalidation.BIOSDependency{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("approval digest=%q, want Provider digest %q", got, want)
	}
}
