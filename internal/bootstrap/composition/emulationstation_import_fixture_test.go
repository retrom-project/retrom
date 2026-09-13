package composition

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/adapter/files/serversource"
	"retrom/internal/adapter/integration/libraryimport"
	"retrom/internal/adapter/runtime/dependencies"
	retromruntime "retrom/internal/adapter/runtime/runtime"
	"retrom/internal/capability/security/authn"
	"retrom/internal/foundation/cleanup"
	dependencyrepository "retrom/internal/persistence/dependencies"
	dependencyservice "retrom/internal/service/dependencies"
	application "retrom/internal/service/emulationstationimport"
	"retrom/internal/testkit/testsupport"
)

const esCompositionActor = "01980000-0000-7000-8000-000000001398"

type esCompositionFixture struct {
	ctx      context.Context
	database *sql.DB
	service  *application.Service
	now      time.Time
}

func newESCompositionFixture(t *testing.T) esCompositionFixture {
	t.Helper()
	current := time.Date(2026, time.August, 24, 0, 0, 0, 0, time.UTC)
	clock := func() time.Time { return current }
	root := t.TempDir()
	database, err := testsupport.OpenDatabase(t.Context(), filepath.Join(root, "retrom.db"), clock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup.Error("close ES composition database", database.Close()) })
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: esCompositionActor, Role: "ADMIN"})
	if _, err := database.SQL.ExecContext(ctx, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('es-composition-profile','Composition',0);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES(?,'es-composition-profile','es-composition','Composition','ADMIN','ENABLED',0,0)`, esCompositionActor); err != nil {
		t.Fatal(err)
	}
	deps, err := dependencies.Load(filepath.Join("..", "..", "..", "data"), []string{"4.2.3"}, "4.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if err := dependencyservice.New(deps, dependencyrepository.New(database.SQL)).Bootstrap(ctx, current); err != nil {
		t.Fatal(err)
	}
	blobs, err := blobstore.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	credentials, err := retromruntime.LoadOrCreateCredentials(root)
	if err != nil {
		t.Fatal(err)
	}
	source := writeESCompositionSource(t)
	importer := libraryimport.New(database.SQL, clock).WithBlobStore(blobs)
	service := NewEmulationStationImport(database.SQL, blobs, importer, credentials, []serversource.Root{{ID: "games", Label: "Games", Path: source}}, clock)
	t.Cleanup(service.Close)
	return esCompositionFixture{ctx: ctx, database: database.SQL, service: service, now: current}
}

func writeESCompositionSource(t *testing.T) string {
	t.Helper()
	source := t.TempDir()
	payload, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "public-roms", "nes-smoke", "nes-smoke.nes"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "smoke.nes"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	xml := `<gameList><game><path>./smoke.nes</path><name>Composition smoke</name><releasedate>20200101T000000</releasedate></game></gameList>`
	if err := os.WriteFile(filepath.Join(source, "gamelist.xml"), []byte(xml), 0o600); err != nil {
		t.Fatal(err)
	}
	return source
}

func (fixture esCompositionFixture) await(t *testing.T, id, state string) application.Summary {
	t.Helper()
	for range 250 {
		result, err := fixture.service.Get(fixture.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if result.State == state {
			return result
		}
		if result.State == "FAILED" || result.State == "PARTIAL_FAILURE" {
			t.Fatalf("unexpected worker result: %#v", result)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("ES worker did not reach %s within 2.5 seconds", state)
	return application.Summary{}
}
