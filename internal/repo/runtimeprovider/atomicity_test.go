package runtimeprovider

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"retrom/internal/capability/runtime/runtimebundle"
	"retrom/internal/capability/runtime/runtimecatalog"
	runtimeprovidermodel "retrom/internal/model/runtimeprovider"
	"retrom/internal/repo/recordstore"
	runtimeproviderservice "retrom/internal/service/runtimeprovider"
)

func TestProviderActivationAndSessionTerminationCommitTogether(t *testing.T) {
	for _, failAudit := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "audit failure"}[failAudit], func(t *testing.T) {
			assertProviderActivationTransaction(t, failAudit)
		})
	}
}

func assertProviderActivationTransaction(t *testing.T, failAudit bool) {
	t.Helper()
	database := openProjectionDatabase(t)
	initial := netplayProjectionFixture(t, "1.0.0", "a", []string{"state-v1"})
	activation := runtimeproviderservice.New(New(database.SQL))
	if err := activation.Reconcile(t.Context(), initial, time.UnixMilli(1)); err != nil {
		t.Fatal(err)
	}
	seedProviderSession(t, database.SQL)
	if failAudit {
		if _, err := database.SQL.ExecContext(t.Context(), "DROP TABLE audit_events"); err != nil {
			t.Fatal(err)
		}
	}
	upgrade := netplayProjectionFixture(t, "1.1.0", "b", []string{"state-v1", "state-v2"})
	err := activation.Reconcile(t.Context(), upgrade, time.UnixMilli(2))
	if (err != nil) != failAudit {
		t.Fatalf("audit failure=%v error=%v", failAudit, err)
	}
	var provider, room, session, digest string
	if err := database.SQL.QueryRowContext(t.Context(), `
SELECT (SELECT provider_version FROM runtime_providers WHERE provider_id='fixture'),
 (SELECT state FROM netplay_rooms WHERE id='room'),
 (SELECT state FROM netplay_sessions WHERE id='session'),
 (SELECT bundle_sha256 FROM runtime_providers WHERE provider_id='fixture')
`).Scan(&provider, &room, &session, &digest); err != nil {
		t.Fatal(err)
	}
	if failAudit {
		if provider != "1.0.0" || room != "RUNNING" || session != "RUNNING" || digest != strings.Repeat("a", 64) {
			t.Fatalf("failed activation leaked changes: %s %s %s %s", provider, room, session, digest)
		}
	} else if provider != "1.1.0" || room != "ENDED" || session != "FAILED" || digest != strings.Repeat("b", 64) {
		t.Fatalf("incomplete activation: %s %s %s %s", provider, room, session, digest)
	}
}

func seedProviderSession(t *testing.T, database *sql.DB) {
	t.Helper()
	statements := `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('profile','Host',1);
INSERT INTO platform_instances(id,platform_id,default_core_id,name,slug,sort_order,enabled,version,created_at_ms,updated_at_ms)
VALUES('folder','gbc','gambatte','Folder','folder',1,1,1,1,1);
INSERT INTO games(id,platform_instance_id,title,title_initial,description,developer,publisher,genre,players,
metadata_source_kind,content_kind,content_source_kind,content_source_ref_id,source_manifest_json,source_manifest_digest,
status,search_text,version,created_at_ms,updated_at_ms)
VALUES('game','folder','Game','G','','','','',2,'ADMIN_EDIT','SINGLE_FILE','ADMIN_REPLACE','fixture','{}',
?,'PUBLISHED','game',1,1,1);
INSERT INTO game_variants(id,game_id,core_id,provider_id,target_id,emulator_game_id,status,compatibility_code,
dependency_snapshot_json,version,created_at_ms,updated_at_ms)
VALUES('variant','game','gambatte','fixture','target',1,'READY','READY',
'{"schemaVersion":1,"kind":"STATIC","bios":[]}',1,1,1);
INSERT INTO netplay_rooms(id,host_profile_id,state,selected_game_id,selected_game_variant_id,netplay_profile_id,
profile_digest,max_players,version,expires_at_ms,created_at_ms,updated_at_ms)
VALUES('room','profile','WAITING','game','variant','fixture',?,2,1,10000,1,1);
INSERT INTO netplay_sessions(id,room_id,session_no,state,game_id,game_variant_id,provider_id,target_id,bundle_sha256,
netplay_profile_id,profile_json,profile_digest,player_count,occupied_seat_mask,version,created_at_ms,updated_at_ms)
VALUES('session','room',1,'RUNNING','game','variant','fixture','target',?,'fixture','{}',?,2,3,1,1,1);
UPDATE netplay_rooms SET state='RUNNING',current_session_id='session' WHERE id='room';
`
	arguments := map[int][]any{
		2: {strings.Repeat("d", 64)},
		4: {strings.Repeat("e", 64)},
		5: {strings.Repeat("a", 64), strings.Repeat("e", 64)},
	}
	for index, statement := range strings.Split(statements, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := database.ExecContext(t.Context(), statement, arguments[index]...); err != nil {
			t.Fatal(err)
		}
	}
	if err := recordstore.ValidateNetplayRooms(t.Context(), database, "room"); err != nil {
		t.Fatal(err)
	}
	if err := recordstore.ValidateNetplaySessions(t.Context(), database, "session"); err != nil {
		t.Fatal(err)
	}
}

func netplayProjectionFixture(t *testing.T, version, digest string, formats []string) runtimeprovidermodel.Projection {
	t.Helper()
	initial := projectionFixture(version, digest, formats)
	provider := initial.Providers[0].Active
	target := initial.Providers[0].Targets[0].Target
	target.Capabilities.NetplayPort = true
	projection, err := runtimeprovidermodel.NewProjection(
		runtimebundle.ActiveDescriptor{SchemaVersion: 1, Source: "candidate", Providers: []runtimebundle.ActiveProvider{provider}},
		map[string]runtimebundle.Manifest{"fixture": {
			SchemaVersion: 1, ProviderID: "fixture", ProviderVersion: version, ProviderAPI: 1,
			ClientModulePath: "client.mjs", Targets: []runtimebundle.Target{target},
		}},
		runtimecatalog.Catalog{SchemaVersion: 1, Definitions: initial.Definitions, Bindings: initial.Bindings},
	)
	if err != nil {
		t.Fatal(err)
	}
	return projection
}
