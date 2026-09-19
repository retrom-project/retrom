//go:build integration

package libraryimport

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"retrom/internal/capability/engine/rpgmaker/materializer"
	uploadsmodel "retrom/internal/model/uploads"
	uploadpersistence "retrom/internal/repo/uploads"
	"retrom/internal/service/uploads"
	"retrom/internal/testkit/testsupport"
)

type preparedRPGFile struct {
	path     string
	contents []byte
}

func preparedRPGFixture(t *testing.T) (*Service, CreateRequest, string) {
	t.Helper()
	database, blobs, dataDir := openImportGroupFixture(t, t.Context())
	files := preparedPublicRPGFiles(t, "rpgxp")
	uploadService := uploads.New(uploadpersistence.New(database.SQL), blobs, dataDir, time.Now)
	declarations := make([]uploadsmodel.FileDeclaration, 0, len(files))
	for index, file := range files {
		declarations = append(declarations, uploadsmodel.FileDeclaration{
			ClientFileID: fmt.Sprint(index), RelativePath: "project/" + file.path, SizeBytes: int64(len(file.contents)),
		})
	}
	upload, err := uploadService.Create(t.Context(), uploadsmodel.CreateRequest{Purpose: "PROJECT", SourceType: "DIRECTORY", Files: declarations})
	if err != nil {
		t.Fatal(err)
	}
	for index, file := range files {
		putPreparedRPGFile(t, uploadService, upload.ID, upload.Files[index].ID, file.contents)
	}
	current, err := uploadService.Get(t.Context(), upload.ID)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _, err := uploadService.Complete(t.Context(), upload.ID, current.Version)
	if err != nil {
		t.Fatal(err)
	}
	waitForRPGUploadFinalization(t, t.Context(), database.SQL, jobID)
	service := New(database.SQL, time.Now).WithBlobStore(blobs)
	request := CreateRequest{
		UploadID:                 upload.ID,
		TargetPlatformInstanceID: testsupport.MustPlatformInstanceID(t, database.SQL, "rpgmaker/rpgmaker"),
		MetadataProvider:         "NONE", ContentMode: "RPG_MAKER_PROJECT",
	}
	return service, request, expectedPreparedRPGDigest(t, files)
}

func preparedPublicRPGFiles(t *testing.T, generation string) []preparedRPGFile {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "testdata", "public-roms", "rpgmaker-smoke", generation)
	result := []preparedRPGFile{}
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		result = append(result, preparedRPGFile{path: filepath.ToSlash(relative), contents: contents})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Slice(result, func(a, b int) bool { return result[a].path < result[b].path })
	return result
}

func putPreparedRPGFile(t *testing.T, service *uploads.Service, uploadID, fileID string, contents []byte) {
	t.Helper()
	for start, part := 0, 0; start < len(contents); start, part = start+int(uploadsmodel.PartSize), part+1 {
		end := min(start+int(uploadsmodel.PartSize), len(contents))
		digest := sha256.Sum256(contents[start:end])
		if err := service.PutPart(t.Context(), uploadID, fileID, part,
			fmt.Sprintf("bytes %d-%d/%d", start, end-1, len(contents)),
			"sha-256=:"+base64.StdEncoding.EncodeToString(digest[:])+":", bytes.NewReader(contents[start:end])); err != nil {
			t.Fatal(err)
		}
	}
}

func expectedPreparedRPGDigest(t *testing.T, files []preparedRPGFile) string {
	t.Helper()
	sources := make([]materializer.SourceFile, 0, len(files))
	for _, file := range files {
		sources = append(sources, materializer.SourceFile{
			Path: file.path, Size: int64(len(file.contents)), Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(file.contents)), nil },
		})
	}
	expected, err := materializer.WriteMKXPZ(io.Discard, sources)
	if err != nil {
		t.Fatal(err)
	}
	return expected.SHA256
}
