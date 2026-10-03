//go:build integration

package httpapi

import (
	"errors"
	"strings"
	"testing"
	"time"

	dbapi "retrom/internal/database"
	"retrom/internal/libraryimport"
	dependencypersistence "retrom/internal/persistence/dependencies"
	dependencyservice "retrom/internal/service/dependencies"
	"retrom/internal/testsupport/importfixture"
)

func TestNamelessProjectImportCannotPersistEmptyReview(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	ctx := t.Context()
	if err := dependencyservice.New(server.dependencies, dependencypersistence.New(server.database)).Bootstrap(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	files := rpgMakerHTTPFixture(t, "rpg2000")
	for index := range files {
		files[index].path = strings.TrimPrefix(files[index].path, "project/")
	}
	uploadID := completeRPGMakerHTTPUpload(t, ctx, server, files)
	var platformID string
	if err := dbapi.QueryRowContext(ctx, server.database, `SELECT id FROM platform_instances WHERE catalog_template_key='rpgmaker/rpgmaker'`).Scan(&platformID); err != nil {
		t.Fatal(err)
	}
	_, err := importfixture.New(t, server.database, server.contentDeps.Files, importfixture.Options{}).Create(ctx, libraryimport.CreateRequest{UploadID: uploadID, TargetPlatformInstanceID: platformID, MetadataProvider: "NONE", ContentMode: "RPG_MAKER_PROJECT"})
	if !errors.Is(err, libraryimport.ErrInvalid) {
		t.Fatalf("nameless project import: %v", err)
	}
	var items, snapshots int
	if err = dbapi.QueryRowContext(ctx, server.database, `SELECT (SELECT count(*) FROM import_items),(SELECT count(*) FROM import_item_source_snapshots)`).Scan(&items, &snapshots); err != nil || items != 0 || snapshots != 0 {
		t.Fatalf("failed import persisted items=%d snapshots=%d: %v", items, snapshots, err)
	}
}
