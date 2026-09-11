//go:build integration

package launch

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// A missing optional external archive must not turn a READY variant into a blocked launch.
func seedOptionalExternalBIOS(t *testing.T, ctx context.Context, database *sql.DB, providerID, targetID string) {
	t.Helper()
	_, err := database.ExecContext(ctx, `INSERT INTO bios_requirements(id,core_id,provider_id,target_id,source_kind,
logical_name,requirement_mode,catalog_digest,source_url,source_version,enabled,version,created_at_ms,
updated_at_ms,delivery_kind,emulator_path)
VALUES('optional-external-fixture','melonds',?,?,'STATIC','optional.zip','OPTIONAL',?,'retrom:test',
'fixture-v1',1,1,1,1,'EXTERNAL_FILE','/optional.zip')`, providerID, targetID, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
}
