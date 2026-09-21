//go:build integration

package launch

import (
	"database/sql"
	"testing"
	"time"

	"retrom/internal/testassert"
)

func seedBIOSResumeSave(t *testing.T, database *sql.DB, gameID, launchID, blobID, digest string, size int64) string {
	t.Helper()
	id := newUUID()
	_, err := database.ExecContext(t.Context(), `INSERT INTO save_states(
id,profile_id,game_id,checkpoint_format,payload_blob_id,payload_sha256,payload_size_bytes,
source_launch_session_id,name,active_duration_ms,version,created_at_ms,updated_at_ms)
VALUES(?,'local',?,'test-checkpoint-v1',?,?,?,?,'BIOS resume',0,1,?,?)`, id, gameID, blobID, digest, size, launchID, time.Now().UnixMilli(), time.Now().UnixMilli())
	testassert.False(t, err != nil, err)
	return id
}

func assertApprovedBIOSResume(t *testing.T, service *Service, database *sql.DB,
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
	err = database.QueryRowContext(t.Context(), `SELECT compatibility_code FROM game_variants WHERE id=?`, variantID).Scan(&code)
	testassert.False(t, err != nil, err)
	testassert.True(t, code == reviewScreenshotOverrideCode, "BIOS replacement removed manual approval")
}
