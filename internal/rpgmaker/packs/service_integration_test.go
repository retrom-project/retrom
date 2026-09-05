//go:build integration

package packs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/payloadrelease"
	"retrom/internal/testsupport"
	"retrom/internal/uploads"
)

func TestRuntimePackUploadInstallListDeleteAndRelease(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := testsupport.OpenDatabase(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close runtime pack test database", database.Close()) })
	userID := seedRuntimePackUser(t, ctx, database.SQL)
	blobs, err := blobstore.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	uploadService := uploads.New(database.SQL, blobs, dataDir, time.Now)
	uploadID := completeRuntimePackDirectory(t, ctx, database.SQL, uploadService)
	releases, err := payloadrelease.New(database.SQL, blobs, time.Now, 7*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(releases.Close)
	service := New(database.SQL, blobs, releases, time.Now)
	sourceNote := "operator-owned integration fixture"
	accepted, err := service.Install(ctx, InstallRequest{
		UploadID: uploadID, DefinitionID: "rpg2000_rtp", CreatorID: userID, SourceNote: &sourceNote,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertRuntimePackValidationJobInput(t, ctx, database.SQL, accepted.JobID, accepted.InstallationID)
	waitForRuntimePackJob(t, ctx, database.SQL, accepted.JobID, "SUCCEEDED")

	list, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Installations) != 1 || list.Installations[0].Status != "READY" ||
		list.Installations[0].FileCount != 1 || list.Installations[0].References != (ReferenceCounts{}) {
		t.Fatalf("runtime pack list = %#v", list.Installations)
	}
	installation := list.Installations[0]
	if installation.BundleSHA256 == nil || installation.ValidatedAtMS == nil ||
		installation.SourceNote == nil || *installation.SourceNote != sourceNote {
		t.Fatalf("READY installation payload = %#v", installation)
	}
	var consumptions int
	if err := database.SQL.QueryRowContext(ctx, `
SELECT count(*) FROM upload_consumptions
WHERE upload_session_id=? AND consumer_type='RUNTIME_ASSET_PACK_INSTALLATION' AND consumer_id=?
`, uploadID, installation.InstallationID).Scan(&consumptions); err != nil || consumptions != 1 {
		t.Fatalf("runtime pack upload consumption = %d, error=%v", consumptions, err)
	}
	if err := service.Delete(ctx, installation.InstallationID, installation.Version+1); err != ErrStale {
		t.Fatalf("stale Delete error = %v", err)
	}
	if err := service.Delete(ctx, installation.InstallationID, installation.Version); err != nil {
		t.Fatal(err)
	}
	deleted, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted.Installations) != 1 || deleted.Installations[0].Status != "DELETED" ||
		deleted.Installations[0].BundleSHA256 != nil || deleted.Installations[0].DeletedAtMS == nil {
		t.Fatalf("deleted runtime pack = %#v", deleted.Installations)
	}
	worked, err := releases.RunOnce(ctx)
	if err != nil || !worked {
		t.Fatalf("payload release = worked %t, error %v", worked, err)
	}
	var releasedAt any
	if err := database.SQL.QueryRowContext(ctx, `
SELECT released_at_ms FROM upload_consumptions
WHERE upload_session_id=? AND consumer_type='RUNTIME_ASSET_PACK_INSTALLATION'
`, uploadID).Scan(&releasedAt); err != nil || releasedAt == nil {
		t.Fatalf("runtime pack consumption release = %v, error=%v", releasedAt, err)
	}
}

func assertRuntimePackValidationJobInput(
	t *testing.T,
	ctx context.Context,
	database interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	jobID, installationID string,
) {
	t.Helper()
	payload := fmt.Sprintf(`{"installationId":"%s","schemaVersion":1}`, installationID)
	digest := sha256.Sum256([]byte(payload))
	var inputJSON, inputDigest, createdType string
	if err := database.QueryRowContext(ctx, `
SELECT input.input_json,input.input_digest,typeof(event.created_at_ms)
FROM job_input_snapshots input
JOIN job_events event ON event.job_id=input.job_id AND event.event_type='QUEUED'
WHERE input.job_id=? AND input.execution_no=1
`, jobID).Scan(&inputJSON, &inputDigest, &createdType); err != nil {
		t.Fatal(err)
	}
	if inputJSON != payload || inputDigest != fmt.Sprintf("%x", digest) || createdType != "integer" {
		t.Fatalf("runtime pack validation input = %q / %q / %q", inputJSON, inputDigest, createdType)
	}
}

func seedRuntimePackUser(t *testing.T, ctx context.Context, database interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
},
) string {
	t.Helper()
	profileID, _ := uuid.NewV7()
	userID, _ := uuid.NewV7()
	_, err := database.ExecContext(
		ctx, `INSERT INTO profiles(id,display_name,created_at_ms) VALUES(?,?,1)`,
		profileID.String(), "Runtime Pack Admin",
	)
	if err == nil {
		_, err = database.ExecContext(ctx, `
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,?,'runtimepackadmin','Runtime Pack Admin','ADMIN','ENABLED',1,1)
`, userID.String(), profileID.String())
	}
	if err != nil {
		t.Fatal(err)
	}
	return userID.String()
}

func completeRuntimePackDirectory(
	t *testing.T,
	ctx context.Context,
	database interface {
		QueryRowContext(context.Context, string, ...any) *sql.Row
	},
	service *uploads.Service,
) string {
	t.Helper()
	contents := []byte("\x89PNG\r\n\x1a\nretrom-layout-fixture")
	session, err := service.Create(ctx, uploads.CreateRequest{
		Purpose: "RUNTIME_ASSET_PACK", SourceType: "DIRECTORY",
		Files: []uploads.FileDeclaration{{
			ClientFileID: "dungeon", RelativePath: "RTP/Backdrop/Dungeon1.png",
			SizeBytes: int64(len(contents)),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	if err := service.PutPart(
		ctx, session.ID, session.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)),
		"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
		bytes.NewReader(contents),
	); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := service.Complete(ctx, session.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitForRuntimePackJob(t, ctx, database, jobID, "SUCCEEDED")
	return session.ID
}

func waitForRuntimePackJob(t *testing.T, ctx context.Context, database interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, jobID, want string,
) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var state string
		if err := database.QueryRowContext(ctx, "SELECT state FROM jobs WHERE id=?", jobID).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == want {
			return
		}
		if state == "FAILED" || time.Now().After(deadline) {
			t.Fatalf("job %s state = %s, want %s", jobID, state, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
