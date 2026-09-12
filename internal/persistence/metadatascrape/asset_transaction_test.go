package metadatascrape

import (
	"bytes"
	"encoding/base64"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/service/metadatascrape"
	"retrom/internal/testsupport"
)

func TestAssetPublicationConflictReleasesTransaction(t *testing.T) {
	root := t.TempDir()
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(root, "retrom.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	blobs, err := blobstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := blobs.Put(bytes.NewReader(png))
	if err != nil {
		t.Fatal(err)
	}
	err = NewAssets(database.SQL).Publish(t.Context(), metadatascrape.AssetPublication{
		ID: "deleted", Blob: metadata, MediaType: "image/png", Width: 1, Height: 1, Now: 100,
	})
	if !errors.Is(err, metadatascrape.ErrGameDeleted) {
		t.Fatalf("publication conflict: %v", err)
	}
	if stats := database.SQL.Stats(); stats.InUse != 0 {
		t.Fatalf("publication conflict leaked %d database connection(s)", stats.InUse)
	}
	var blobsCount int
	if err := database.SQL.QueryRowContext(t.Context(), `SELECT count(*) FROM blobs`).Scan(&blobsCount); err != nil {
		t.Fatal(err)
	}
	if blobsCount != 0 {
		t.Fatalf("orphan blob registration committed: %d", blobsCount)
	}
}
