//go:build integration

package firmware

import (
	"testing"

	"retrom/internal/testsupport"

	dbapi "retrom/internal/database"
	firmwareservice "retrom/internal/service/firmware"
)

func TestFailedUploadConsumptionRestoresActiveBIOS(t *testing.T) {
	database, _, now := retirementFixture(t)
	seedRetiringInstallation(t, database, "018fbe68-0000-7000-8000-000000000033", 1, now)
	var requirementID string
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT requirement_id FROM bios_installations WHERE id='018fbe68-0000-7000-8000-000000000033'`).
		Scan(&requirementID); err != nil {
		t.Fatal(err)
	}
	created := false
	err := New(database).WithWrite(t.Context(), func(scope firmwareservice.WriteScope) error {
		active, found, err := scope.ReadScope.Installations.Active(t.Context(), requirementID)
		if err != nil || !found {
			t.Fatalf("active BIOS missing: found=%v error=%v", found, err)
		}
		if err := firmwareservice.SupersedeInScope(t.Context(), scope.Retirements, requirementID, now); err != nil {
			return err
		}
		if err := scope.Installations.Create(t.Context(), firmwareservice.InstallationWrite{
			ID: "replacement", RequirementID: requirementID,
			FileRecord: testsupport.FileMetadata("replacement").Record, UploadSessionID: "missing-upload",
			Filename: active.Filename,
			Size:     active.Size, MD5: active.MD5, SHA1: active.SHA1, SHA256: active.SHA256,
			Status: active.Status, RequirementVersion: active.ValidatedVersion, DetailsJSON: []byte(`{}`),
			AtMS: now, SourceKind: "BROWSER_UPLOAD",
		}); err != nil {
			return err
		}
		created = true
		return scope.Installations.Consume(t.Context(), firmwareservice.Consumption{
			ID: "consumption", UploadID: "missing-upload", FileID: "missing-file", InstallationID: "replacement", AtMS: now,
		})
	})
	if err == nil || !created {
		t.Fatalf("failure was not injected after replacement: created=%v error=%v", created, err)
	}
	var active, version, replacements int
	if err := dbapi.QueryRowContext(t.Context(), database, `SELECT is_active,version,
(SELECT count(*) FROM bios_installations WHERE id='replacement') FROM bios_installations WHERE
id='018fbe68-0000-7000-8000-000000000033'`).
		Scan(&active, &version, &replacements); err != nil || active != 1 || version != 1 || replacements != 0 {
		t.Fatalf("replacement partially committed: active=%d version=%d replacements=%d error=%v",
			active, version, replacements, err)
	}
	assertBIOSReferenceCounts(t, database, 1, 1)
}
