package sourceimport

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"retrom/internal/testsupport/importfixture"

	dependencypersistence "retrom/internal/persistence/dependencies"
	dependencyservice "retrom/internal/service/dependencies"

	"retrom/internal/authn"
	"retrom/internal/cleanup"
	"retrom/internal/dependencies"
	"retrom/internal/filestore"
	"retrom/internal/libraryimport"
	tagpersistence "retrom/internal/persistence/tagging"
	retromruntime "retrom/internal/runtime"
	"retrom/internal/serversource"
	"retrom/internal/service/tagging"
	"retrom/internal/store"
	"retrom/internal/testassert"
	"retrom/internal/testsupport"
)

type sourceLifecycle struct {
	ctx                              context.Context
	dataDir                          string
	database                         *store.DB
	importer                         *libraryimport.Service
	service                          *Service
	blobs                            *filestore.Store
	tagService                       *tagging.Service
	mappedTag, externalTag, driftTag tagging.AdminItem
}

func newSourceLifecycle(t *testing.T, format string) sourceLifecycle {
	t.Helper()
	ctx := context.Background()
	dataDir := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(dataDir, "retrom.db"), time.Now)
	testassert.False(t, err != nil, err)
	err = testsupport.SeedPlatformInstances(ctx, database.SQL)
	testassert.False(t, err != nil, err)
	t.Cleanup(func() { cleanup.Error("close", database.Close()) })
	_, filename, _, _ := runtime.Caller(0)
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	dependencySet, err := dependencies.Load(filepath.Join(repositoryRoot, "data"), []string{"4.2.3"}, "4.2.3")
	testassert.False(t, err != nil, err)
	err = testsupport.SeedRuntimeProviders(ctx, database.SQL, dependencySet.RuntimeCatalog)
	testassert.False(t, err != nil, err)
	err = dependencyservice.New(dependencySet, dependencypersistence.New(database.SQL)).Bootstrap(ctx, time.Now())
	testassert.False(t, err != nil, err)
	mustExecSourceTest(ctx, t, database.SQL, `
INSERT INTO profiles(id,display_name,created_at_ms) VALUES('source-profile','Source Test',1);
INSERT INTO users(id,profile_id,username,display_name,role,status,created_at_ms,updated_at_ms)
VALUES('01980000-0000-7000-8000-000000000800','source-profile','source-test','Source Test','ADMIN','ENABLED',1,1)`)
	ctx = authn.WithPrincipal(ctx, authn.Principal{
		UserID: "01980000-0000-7000-8000-000000000800", ProfileID: "source-profile", Role: "ADMIN",
	})
	tagService := tagging.New(tagpersistence.New(database.SQL), time.Now)
	mappedTag, err := tagService.Create(ctx, "01980000-0000-7000-8000-000000000800", "扫描选择")
	testassert.False(t, err != nil, err)
	externalTag, err := tagService.Create(ctx, "01980000-0000-7000-8000-000000000800", "External")
	testassert.False(t, err != nil, err)
	driftTag, err := tagService.Create(ctx, "01980000-0000-7000-8000-000000000800", "映射后删除")
	testassert.False(t, err != nil, err)
	root := createOrganizedSource(t, dataDir, format)
	blobs, err := filestore.Open(dataDir)
	testassert.False(t, err != nil, err)
	credentials, err := retromruntime.LoadOrCreateCredentials(dataDir)
	testassert.False(t, err != nil, err)
	importer := importfixture.New(t, database.SQL, blobs, importfixture.Options{Now: time.Now})
	service := New(
		database.SQL,
		blobs,
		importer,
		credentials,
		[]serversource.Root{{ID: "games", Label: "Games", Path: root}},
		time.Now,
	)
	return sourceLifecycle{ctx: ctx, dataDir: dataDir, database: database, importer: importer, service: service, blobs: blobs, tagService: tagService, mappedTag: mappedTag, externalTag: externalTag, driftTag: driftTag}
}
