package metadatascrape

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"retrom/internal/adapter/metadata/hasheous"
	"retrom/internal/service/metadatascrape"
)

func TestMediaFinalOwnerChangeCannotPublish(t *testing.T) {
	for _, change := range []string{"cancel", "source", "worker", "scope"} {
		t.Run(change, func(t *testing.T) {
			fixture := newMediaFixture(t)
			source := mediaSource(func(ctx context.Context, _ hasheous.AssetRef, _ int64) (hasheous.AssetData, error) {
				switch change {
				case "cancel":
					mediaFenceSQL(ctx, t, fixture.database, `UPDATE jobs SET state='CANCEL_REQUESTED',cancel_requested_at_ms=?,version=version+1 WHERE id=?`, fixture.now.UnixMilli(), fixture.jobID)
				case "source":
					mediaFenceSQL(ctx, t, fixture.database, `UPDATE scrape_candidate_assets SET source_path='/api/v1/images/changed',version=version+1`)
				case "worker":
					mediaFenceSQL(ctx, t, fixture.database, `UPDATE jobs SET worker_id='new-owner',version=version+1 WHERE id=?`, fixture.jobID)
				case "scope":
					mediaFenceSQL(ctx, t, fixture.database, `UPDATE jobs SET scope_id='another-game',version=version+1 WHERE id=?`, fixture.jobID)
				}
				return hasheous.AssetData{Bytes: []byte("media"), ReceivedBytes: 5, MediaType: "image/png", Width: 1, Height: 1}, nil
			})
			if err := fixture.worker(source).Run(t.Context(), fixture.jobID); err == nil {
				t.Fatal("changed authority returned success")
			}
			snapshot := fixture.snapshot(t)
			if snapshot.Asset.Status == "READY" {
				t.Fatalf("changed %s published media", change)
			}
			var count int
			if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM blobs`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("changed %s registered %d blobs", change, count)
			}
			if change == "cancel" && snapshot.Job.State != "CANCELLED" {
				t.Fatalf("cancel state=%s", snapshot.Job.State)
			}
			if change == "worker" && snapshot.Job.State != "RUNNING" {
				t.Fatalf("stale worker settled replacement: %s", snapshot.Job.State)
			}
		})
	}
}

func TestMediaMalformedSnapshotFailsWithOriginalCause(t *testing.T) {
	fixture := newMediaFixture(t)
	recoveryExec(t, fixture.database, `UPDATE job_input_snapshots SET input_json='{',input_digest=? WHERE job_id=?`,
		fmt.Sprintf("%x", sha256.Sum256([]byte("{"))), fixture.jobID)
	worker := metadatascrape.NewMediaWorker(NewMedia(fixture.database), nil, nil, fixture.clock)
	err := worker.Run(t.Context(), fixture.jobID)
	var syntax *json.SyntaxError
	if !errors.Is(err, metadatascrape.ErrMediaInput) || !errors.As(err, &syntax) {
		t.Fatalf("invalid input cause=%v", err)
	}
	snapshot := fixture.snapshot(t)
	if snapshot.Job.State != "FAILED" || snapshot.Asset.Status != "FAILED" || snapshot.Charged != 0 {
		t.Fatalf("invalid input=%+v", snapshot)
	}
}

func mediaFenceSQL(ctx context.Context, t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(err)
	}
}
