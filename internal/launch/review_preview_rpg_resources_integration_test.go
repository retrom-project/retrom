//go:build integration

package launch

import (
	"encoding/json"
	"strings"
	"testing"

	"retrom/internal/testsupport"
)

func TestRPGReviewPreviewServesSelectedRTPThroughOrdinaryContentAuthorization(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	seedReviewRuntimePack(t, fixture)
	preview := fixture.preview(t, "rtp-preview")
	configuration, err := fixture.launcher.ReviewPreviewConfig(t.Context(), preview.PreviewID, preview.Capability)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testsupport.RuntimeEnvelope(t, configuration)
	encoded, err := json.Marshal(envelope["resources"])
	if err != nil || !strings.Contains(string(encoded), "__retrom__/packs/0/index.json") {
		t.Fatalf("ordinary RPG preview omitted its selected RTP: %s %v", encoded, err)
	}
	index, err := fixture.launcher.RuntimePackIndex(t.Context(), preview.PreviewID, preview.Capability, 0)
	if err != nil || !strings.Contains(string(index.Contents), "Music/theme.wav") {
		t.Fatalf("ordinary RTP index: %s %v", index.Contents, err)
	}
	file, err := fixture.launcher.RuntimePackFile(t.Context(), preview.PreviewID, preview.Capability, 0, "Music/theme.wav")
	if err != nil || file.Digest != strings.Repeat("1", 64) {
		t.Fatalf("ordinary RTP file: %+v %v", file, err)
	}
	if _, err := fixture.launcher.RuntimePackFile(t.Context(), preview.PreviewID, "wrong-capability", 0, "Music/theme.wav"); err == nil {
		t.Fatal("RTP content bypassed preview authentication")
	}
	if _, err := fixture.launcher.RuntimePackIndex(t.Context(), preview.PreviewID, preview.Capability, 1); err == nil {
		t.Fatal("RTP endpoint exposed an unselected slot")
	}
}

func seedReviewRuntimePack(t *testing.T, fixture reviewCheckpointFixture) {
	t.Helper()
	mustRPGLaunchSQL(t, fixture.database, `
INSERT INTO runtime_asset_pack_installations(
 id,definition_id,files_digest,file_count,total_bytes,bundle_blob_id,bundle_sha256,status,
 diagnostic_json,created_by_user_id,created_at_ms)
VALUES('01980000-0000-7000-8000-000000000921','rpg2000_rtp',?,1,10,'rpg-index',?,'VALIDATING','{}','reviewer',?)
`, strings.Repeat("c", 64), strings.Repeat("3", 64), fixture.now.UnixMilli())
	mustRPGLaunchSQL(t, fixture.database, `
INSERT INTO runtime_asset_pack_files(installation_id,path,ordinal,blob_id,size_bytes,sha256)
VALUES('01980000-0000-7000-8000-000000000921','Music/theme.wav',0,'rpg-project-a',10,?)`, strings.Repeat("1", 64))
	mustRPGLaunchSQL(t, fixture.database, `
UPDATE runtime_asset_pack_installations SET status='READY',validated_at_ms=?,version=version+1
WHERE id='01980000-0000-7000-8000-000000000921'`, fixture.now.UnixMilli())
	mustRPGLaunchSQL(t, fixture.database, `
INSERT INTO review_draft_runtime_pack_selections(
 review_draft_id,slot,declared_name,normalized_declared_name,definition_id,installation_id,created_at_ms)
VALUES('01980000-0000-7000-8000-000000000901',0,'RPG2000_RTP','rpg2000_rtp','rpg2000_rtp',
 '01980000-0000-7000-8000-000000000921',?)`, fixture.now.UnixMilli())
}

func TestRPGReviewPreviewKeepsUniqueASCIICaseFoldContentLookup(t *testing.T) {
	t.Parallel()
	fixture := newReviewCheckpointFixture(t)
	preview := fixture.preview(t, "folded-preview")
	content, err := fixture.launcher.ReviewPreviewProjectContent(t.Context(), preview.PreviewID, preview.Capability, "rpg_rt.LDB")
	if err != nil || content.Digest != strings.Repeat("1", 64) {
		t.Fatalf("ordinary RPG trial lost case-fold lookup: %+v %v", content, err)
	}
	if _, err := fixture.launcher.ReviewPreviewProjectContent(t.Context(), preview.PreviewID, preview.Capability, "../RPG_RT.ldb"); err == nil {
		t.Fatal("case-fold lookup admitted traversal")
	}
}
