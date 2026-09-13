//go:build integration

package metadatascrape_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/composition"
	"retrom/internal/dependencies"
	"retrom/internal/hasheous"
	"retrom/internal/libraryimport"
	dependencypersistence "retrom/internal/persistence/dependencies"
	uploadpersistence "retrom/internal/persistence/uploads"
	dependencyservice "retrom/internal/service/dependencies"
	metadataservice "retrom/internal/service/metadatascrape"
	"retrom/internal/service/uploads"
	"retrom/internal/store"
	"retrom/internal/testsupport"
)

func mediaFixtureNow() time.Time { return time.Date(2028, 4, 5, 6, 7, 8, 0, time.UTC) }

type mediaImportFixture struct {
	database *store.DB
	blobs    *blobstore.Store
	scraper  *metadataservice.Service
	importID string
}

func createMediaImport(t *testing.T, client hasheous.HTTPDoer) (*store.DB, string) {
	t.Helper()
	fixture := createMediaImportFixture(t, client)
	return fixture.database, fixture.importID
}

func createMediaImportFixture(t *testing.T, client hasheous.HTTPDoer) mediaImportFixture {
	t.Helper()
	root := t.TempDir()
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(root, "retrom.db"), mediaFixtureNow)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Error(err)
		}
	})
	catalog, err := dependencies.Load(filepath.Join("..", "..", "..", "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencyservice.New(catalog, dependencypersistence.New(database.SQL)).Bootstrap(t.Context(), mediaFixtureNow()); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	resolver := resolverFunc(func(context.Context, string) ([]net.IPAddr, error) {
		return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
	})
	scraper := composition.NewMetadata(database.SQL, blobs, hasheous.New(client, resolver, mediaFixtureNow), mediaFixtureNow)
	t.Cleanup(scraper.Close)
	uploadID := uploadMediaContent(t, database, blobs, root)
	importer := libraryimport.New(database.SQL, mediaFixtureNow, scraper).WithBlobStore(blobs)
	created, err := importer.Create(t.Context(), libraryimport.CreateRequest{
		UploadID:                 uploadID,
		TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "gba/mgba"), MetadataProvider: "HASHEOUS",
	})
	if err != nil {
		t.Fatal(err)
	}
	return mediaImportFixture{database, blobs, scraper, created.ImportJobID}
}

func uploadMediaContent(t *testing.T, database *store.DB, blobs *blobstore.Store, root string) string {
	t.Helper()
	contents := []byte("deterministic Retrom metadata media fixture")
	service := uploads.New(uploadpersistence.New(database.SQL), blobs, root, mediaFixtureNow)
	upload, err := service.Create(t.Context(), uploads.CreateRequest{SourceType: "FILES", Files: []uploads.FileDeclaration{
		{ClientFileID: "game", RelativePath: "Media.gba", SizeBytes: int64(len(contents))},
	}})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(contents)
	header := "sha-256=:" + base64.StdEncoding.EncodeToString(digest[:]) + ":"
	if err := service.PutPart(t.Context(), upload.ID, upload.Files[0].ID, 0,
		fmt.Sprintf("bytes 0-%d/%d", len(contents)-1, len(contents)), header, bytes.NewReader(contents)); err != nil {
		t.Fatal(err)
	}
	current, err := service.Get(t.Context(), upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := service.Complete(t.Context(), upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitForState(t, database.SQL.QueryRowContext, `SELECT state FROM jobs WHERE id=?`, jobID, "SUCCEEDED")
	return upload.ID
}
