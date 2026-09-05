//go:build integration

package saves

import (
	"strings"
	"testing"

	"retrom/internal/testassert"
	"retrom/internal/testsupport"

	"github.com/google/uuid"
)

func (fixture *saveFixture) createPendingRPGReview(t *testing.T) {
	t.Helper()
	now := fixture.now.UnixMilli()
	target, err := testsupport.LookupRuntimeTarget(fixture.ctx, fixture.database.SQL, "rpgmaker")
	testassert.False(t, err != nil, err)
	userID := uuid.NewString()
	ids := map[string]string{
		"directory": uuid.NewString(), "upload": uuid.NewString(),
		"import": uuid.NewString(), "item": uuid.NewString(), "snapshot": uuid.NewString(),
		"review": uuid.NewString(),
	}
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'local',?,'Save Admin','ADMIN','ENABLED',?,?)`, userID, "save-admin-"+userID[:8], now, now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO platform_instances(
 id,platform_id,default_core_id,name,slug,sort_order,enabled,version,created_at_ms,updated_at_ms,catalog_template_key)
VALUES(?,'rpgmaker','rpgmaker','RPG Maker 2000 Save','rpg-maker-save-test',999,1,1,?,?,NULL)`,
		ids["directory"], now, now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO upload_sessions(
 id,purpose,state,source_type,total_files,total_bytes,manifest_digest,expires_at_ms,created_at_ms,updated_at_ms)
VALUES(?,'PROJECT','COMPLETE','DIRECTORY',1,10,?,?,?,?)`, ids["upload"],
		strings.Repeat("d", 64), now+1_000_000, now, now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO import_jobs(
 id,upload_session_id,target_platform_instance_id,platform_instance_version,platform_id,default_core_id,
 provider_id,target_id,metadata_provider,config_snapshot_json,config_snapshot_digest,state,total_item_count,
 review_pending_item_count,created_at_ms,updated_at_ms)
VALUES(?,?,?,1,'rpgmaker','rpgmaker',?,?,'NONE','{}',?,'REVIEW_PENDING',1,1,?,?)`,
		ids["import"], ids["upload"], ids["directory"], target.ProviderID, target.TargetID,
		strings.Repeat("e", 64), now, now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO import_items(
 id,import_job_id,group_key,state,source_manifest_json,source_manifest_digest,search_text,created_at_ms,updated_at_ms)
VALUES(?,?,?,'REVIEW_PENDING','{}',?,'save validation fixture',?,?)`, ids["item"], ids["import"],
		strings.Repeat("1", 64), strings.Repeat("2", 64), now, now)
	manifest := `{"schemaVersion":2,"contentKind":"RPG_MAKER_PROJECT","fileCount":1,"totalBytes":10,"filesDigest":"` +
		strings.Repeat("3", 64) + `"}`
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO import_item_source_snapshots(
 id,import_item_id,content_kind,source_manifest_json,source_manifest_digest,created_by,created_at_ms)
VALUES(?,?,'RPG_MAKER_PROJECT',?,?,'IDENTIFICATION',?)`, ids["snapshot"], ids["item"], manifest,
		strings.Repeat("4", 64), now)
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO review_drafts(
 id,import_item_id,target_platform_instance_id,metadata_json,version,
 created_at_ms,updated_at_ms,effective_source_snapshot_id)
VALUES(?,?,?,'{}',1,?,?,?)`, ids["review"], ids["item"], ids["directory"], now, now, ids["snapshot"])
	mustSaveSQL(t, fixture.database.SQL, `
INSERT INTO rpgmaker_review_profiles(
 review_draft_id,generation,evidence_family,evidence_generation,evidence_confidence,
 file_count,total_bytes,project_fingerprint,requirements_sha256,analysis_json,self_contained_override,
 provider_id,target_id,dependency_snapshot_sha256,
 created_at_ms,updated_at_ms)
VALUES(?,'RPG2000','RPG2K','RPG2000','MATCHED',1,10,?,?,'{}',1,
 ?,?,?,?,?)`, ids["review"], strings.Repeat("5", 64), strings.Repeat("6", 64),
		target.ProviderID, target.TargetID, strings.Repeat("7", 64), now, now)
}
