//go:build integration

package launch

import (
	"testing"
	"time"

	"retrom/internal/persistence/recordstore"

	dbapi "retrom/internal/database"
	"retrom/internal/testassert"
)

func seedBIOSResumeSave(t *testing.T, database dbapi.DB, gameID, launchID, fileRecord,
	digest string, size int64,
) string {
	t.Helper()
	id := newUUID()
	_, err := recordstore.InsertRows(t.Context(), database, "save_states", `INSERT INTO save_states(
id,profile_id,game_id,checkpoint_format,payload_file_record,payload_sha256,payload_size_bytes,
source_launch_session_id,name,active_duration_ms,version,created_at_ms,updated_at_ms)
VALUES(?,'local',?,'test-checkpoint-v1',?,?,?,?,'BIOS resume',0,1,?,?)`, id, gameID, fileRecord, digest, size, launchID, time.Now().UnixMilli(), time.Now().UnixMilli())
	testassert.False(t, err != nil, err)
	return id
}

func assertApprovedBIOSResume(t *testing.T, service *Service, database dbapi.DB,
	gameID, variantID, savedID string, requirements []melondsRequirement, capabilities Capabilities,
) {
	t.Helper()
	core := "melonds"
	created, err := service.Create(t.Context(), "local", CreateRequest{
		GameID: gameID, CoreID: &core, SaveStateID: &savedID, ReturnTo: "/games/" + gameID, ClientCapabilities: capabilities,
	})
	testassert.False(t, err != nil, err)
	testassert.True(t, created.LaunchID != "", "manual approval was converted to a blocking validation")
	assertMelonDSLaunch(t, t.Context(), service, created, requirements, true)
	var code string
	err = dbapi.QueryRowContext(t.Context(), database, `SELECT compatibility_code FROM game_variants WHERE id=?`, variantID).Scan(&code)
	testassert.False(t, err != nil, err)
	testassert.True(t, code == reviewScreenshotOverrideCode, "BIOS replacement removed manual approval")
}
