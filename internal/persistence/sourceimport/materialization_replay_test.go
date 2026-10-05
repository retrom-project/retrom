package sourceimport

import (
	"context"
	"testing"
	"time"

	"retrom/internal/filestore"
	application "retrom/internal/service/sourceimport"
)

type copiedMaterial struct {
	metadata filestore.Metadata
}

func (files copiedMaterial) CopyTo(context.Context, string, string, string) (filestore.Metadata, error) {
	return files.metadata, nil
}

func TestMaterializationReplaysPersistedContentWithLocalLocator(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"", "COVER"} {
		t.Run(kind, func(t *testing.T) {
			db, key, blob := materialDatabase(t)
			key.Kind = kind
			repository := NewMaterialization(db)
			var source application.MaterialSource
			if err := repository.WithMaterialization(t.Context(), func(scope application.MaterialScope) error {
				before, err := scope.Read.Source(t.Context(), key)
				source = before.Source
				return err
			}); err != nil {
				t.Fatal(err)
			}
			files := copiedMaterial{metadata: filestore.Metadata{
				Record: blob.ID, Path: t.TempDir() + "/copied-file", SHA256: blob.SHA256,
				MD5: blob.MD5, SHA1: blob.SHA1, CRC32: blob.CRC32, Size: blob.Size,
			}}
			clock := func() time.Time { return time.UnixMilli(10) }
			identity := application.ExecutionIdentity{
				JobID: "work", ImportID: "import-0", WorkerID: "old-worker", ExecutionNo: 1, Attempt: 1,
			}
			service := application.NewMaterialization(repository, files, clock)
			first, err := service.Copy(t.Context(), identity, source, blob)
			if err != nil {
				t.Fatal(err)
			}
			before := materialRows(t, db)
			// A reconstructed repository has only durable facts, not the host path.
			files.metadata.Path = t.TempDir() + "/copied-file"
			restarted := application.NewMaterialization(NewMaterialization(db), files, clock)
			again, err := restarted.Copy(t.Context(), identity, source, blob)
			if err != nil || again != first {
				t.Fatalf("copied replay=%q want=%q error=%v", again, first, err)
			}
			after := materialRows(t, db)
			for table, rows := range before {
				if rows != after[table] {
					t.Fatalf("copied replay changed %s", table)
				}
			}
		})
	}
}
