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
		TargetContractSHA256:  "a2a3240146d711e4a6fc539560ddb6d6476860c6fae1c3dba1e42cc3e92be16c",
		GameCompatibilityLine: "rpgmaker-2000-v1", DATID: sql.NullString{},
		ValidationID: "validation-1", SnapshotValid: false,
	}

	got, err := approvalValidationInputDigest(input)
	if err != nil {
		t.Fatal(err)
	}
	want, err := corevalidation.ProviderValidationInputDigest(
		input.ProviderID, input.TargetID, input.TargetContractSHA256,
		input.GameCompatibilityLine, input.ContentID, input.DATID,
		corevalidation.Snapshot{SchemaVersion: corevalidation.SnapshotSchemaVersion, BIOS: []corevalidation.BIOSDependency{}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("approval digest=%q, want Provider digest %q", got, want)
	}
}
