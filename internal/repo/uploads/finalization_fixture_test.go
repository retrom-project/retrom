package uploads

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"path/filepath"
	"testing"
	"time"

	"retrom/internal/adapter/files/blobstore"
	"retrom/internal/repo/store"
	uploadservice "retrom/internal/service/uploads"
)

type finalizationFixture struct {
	root     string
	database *sql.DB
	blobs    *blobstore.Store
	service  *uploadservice.Service
}

func finalizationNow() time.Time { return time.Date(2028, 3, 4, 5, 6, 7, 0, time.UTC) }

func newFinalizationFixture(t *testing.T) *finalizationFixture {
	t.Helper()
	root := t.TempDir()
	database, err := store.Open(t.Context(), filepath.Join(root, "retrom.db"), finalizationNow)
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
	fixture := &finalizationFixture{
		root: root, database: database.SQL, blobs: blobs,
		service: uploadservice.New(New(database.SQL), blobs, root, finalizationNow),
	}
	t.Cleanup(func() { fixture.service.Close() })
	return fixture
}

func (fixture *finalizationFixture) upload(t *testing.T, data []byte) uploadservice.Session {
	t.Helper()
	session, err := fixture.service.Create(t.Context(), uploadservice.CreateRequest{SourceType: "FILES", Files: []uploadservice.FileDeclaration{
		{ClientFileID: "file", RelativePath: "fixture.bin", SizeBytes: int64(len(data))},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for offset := int64(0); offset < int64(len(data)); offset += uploadservice.PartSize {
		end := min(offset+uploadservice.PartSize, int64(len(data)))
		fixture.put(t, session, int(offset/uploadservice.PartSize), data[offset:end])
	}
	session, err = fixture.service.Get(t.Context(), session.ID)
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func (fixture *finalizationFixture) put(t *testing.T, session uploadservice.Session, number int, data []byte) {
	t.Helper()
	sum := sha256.Sum256(data)
	digest := "sha-256=:" + base64.StdEncoding.EncodeToString(sum[:]) + ":"
	offset := int64(number) * uploadservice.PartSize
	span := fmt.Sprintf("bytes %d-%d/%d", offset, offset+int64(len(data))-1, session.TotalBytes)
	if err := fixture.service.PutPart(t.Context(), session.ID, session.Files[0].ID, number, span, digest,
		bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}

func (fixture *finalizationFixture) complete(t *testing.T, session uploadservice.Session) string {
	t.Helper()
	id, _, err := fixture.service.Complete(t.Context(), session.ID, session.Version)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type finalizationBlobs struct {
	put func(io.Reader) (blobstore.Metadata, error)
}

func (blobs finalizationBlobs) Put(reader io.Reader) (blobstore.Metadata, error) {
	return blobs.put(reader)
}
