//go:build integration

package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	firmwaresource "retrom/internal/adapter/content/firmwaremanifest"

	uploadsmodel "retrom/internal/model/uploads"
	dependencypersistence "retrom/internal/repo/dependencies"
	dependencyservice "retrom/internal/service/dependencies"

	"retrom/internal/adapter/integration/libraryimport"
	"retrom/internal/testkit/testassert"
)

func TestRPGMakerReviewDetailUsesDetectedCoreBehindVirtualPlatform(t *testing.T) {
	t.Parallel()
	server := newTestServer(t)
	ctx := context.Background()
	if err := dependencyservice.New(server.dependencies, dependencypersistence.New(server.database), firmwaresource.Source{}).Bootstrap(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	uploadID := completeRPGMakerHTTPUpload(ctx, t, server, rpgMakerHTTPFixture(t, "rpg2000"))
	var platformInstanceID string
	if err := server.database.QueryRowContext(ctx, `
SELECT id FROM platform_instances WHERE catalog_template_key='rpgmaker/rpgmaker'
`).Scan(&platformInstanceID); err != nil {
		t.Fatal(err)
	}
	created, err := libraryimport.New(server.database, time.Now).WithBlobStore(server.blobs).Create(
		ctx,
		libraryimport.CreateRequest{
			UploadID: uploadID, TargetPlatformInstanceID: platformInstanceID,
			MetadataProvider: "NONE", ContentMode: "RPG_MAKER_PROJECT",
		},
	)
	testassert.False(t, err != nil, err)
	var itemID string
	if err := server.database.QueryRowContext(ctx, `
SELECT id FROM import_items WHERE import_job_id=?
`, created.ImportJobID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/admin/reviews/"+itemID, nil)
	request.SetPathValue("importItemId", itemID)
	server.review(recorder, request)
	testassert.Falsef(t, testassert.Any(
		func() bool { return recorder.Code != http.StatusOK },
		func() bool { return !strings.Contains(recorder.Body.String(), `"selectedCoreId":"rpgmaker"`) },
	), "RPG Maker virtual review detail = %d %s", recorder.Code, recorder.Body.String())
}

type rpgMakerHTTPFixtureFile struct {
	path     string
	contents []byte
}

func rpgMakerHTTPFixture(t *testing.T, generation string) []rpgMakerHTTPFixtureFile {
	t.Helper()
	_, current, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(current), "..", "..", "..", "testdata", "public-roms", "rpgmaker-smoke", generation)
	files := make([]rpgMakerHTTPFixtureFile, 0)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		relative, relativeErr := filepath.Rel(root, path)
		if relativeErr != nil {
			return relativeErr
		}
		files = append(files, rpgMakerHTTPFixtureFile{
			path: filepath.ToSlash(filepath.Join("project", relative)), contents: contents,
		})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Slice(files, func(left, right int) bool { return files[left].path < files[right].path })
	return files
}

func completeRPGMakerHTTPUpload(
	ctx context.Context,
	t *testing.T,
	server *Server,
	files []rpgMakerHTTPFixtureFile,
) string {
	t.Helper()
	declarations := make([]uploadsmodel.FileDeclaration, 0, len(files))
	for index, file := range files {
		declarations = append(declarations, uploadsmodel.FileDeclaration{
			ClientFileID: fmt.Sprintf("rpg-%d", index), RelativePath: file.path,
			SizeBytes: int64(len(file.contents)),
		})
	}
	upload, err := server.uploads.Create(ctx, uploadsmodel.CreateRequest{
		Purpose: "PROJECT", SourceType: "DIRECTORY", Files: declarations,
	})
	testassert.False(t, err != nil, err)
	for index, file := range files {
		for start, part := 0, 0; start < len(file.contents); start, part = start+int(uploadsmodel.PartSize), part+1 {
			end := min(start+int(uploadsmodel.PartSize), len(file.contents))
			digest := sha256.Sum256(file.contents[start:end])
			if err := server.uploads.PutPart(
				ctx, upload.ID, upload.Files[index].ID, part,
				fmt.Sprintf("bytes %d-%d/%d", start, end-1, len(file.contents)),
				"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":",
				bytes.NewReader(file.contents[start:end]),
			); err != nil {
				t.Fatal(err)
			}
		}
	}
	current, err := server.uploads.Get(ctx, upload.ID)
	testassert.False(t, err != nil, err)
	jobID, _, err := server.uploads.Complete(ctx, upload.ID, current.Version)
	testassert.False(t, err != nil, err)
	waitForHTTPJob(ctx, t, server.database, jobID)
	return upload.ID
}
