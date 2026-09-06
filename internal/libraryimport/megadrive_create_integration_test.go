//go:build integration

package libraryimport

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/contentcapability"
	"retrom/internal/testsupport"
)

func TestMegaDriveROMImportPreservesPayloadAndReachesReview(t *testing.T) {
	t.Parallel()
	// Synthetic import payload; actual core compatibility is checked through Player.
	payload := []byte("Retrom synthetic Mega Drive import payload")

	for _, source := range []struct {
		name        string
		contentName string
		body        []byte
	}{
		{name: "Game.SMD", contentName: "Game.SMD", body: payload},
		{name: "SMD.zip", contentName: "Game.SMD", body: makeZIP(t, map[string][]byte{"folder/Game.SMD": payload, "README.txt": []byte("readme")})},
		{name: "Game.BIN", contentName: "Game.BIN", body: payload},
		{name: "BIN.zip", contentName: "Game.BIN", body: makeZIP(t, map[string][]byte{"folder/Game.BIN": payload, "README.txt": []byte("readme")})},
	} {
		t.Run(source.name, func(t *testing.T) {
			ctx := t.Context()
			database, blobs, _ := openImportGroupFixture(t, ctx)
			metadata, err := blobs.Put(bytes.NewReader(source.body))
			if err != nil {
				t.Fatal(err)
			}
			blobID, err := blobstore.EnsureRecord(ctx, database.SQL, metadata, "application/octet-stream", time.Now().UnixMilli())
			if err != nil {
				t.Fatal(err)
			}
			service := New(database.SQL, time.Now).WithBlobStore(blobs)
			result, err := service.CreateServerSource(ctx,
				testsupport.MustPlatformInstanceID(t, database.SQL, "megadrive/genesis_plus_gx"),
				contentcapability.ModeStandard,
				[]ServerSourceFile{{RelativePath: source.name, BlobID: blobID, SizeBytes: metadata.Size}}, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			if result.Created.State != "REVIEW_PENDING" || len(result.Items) != 1 || len(result.RejectedCodes) != 0 {
				t.Fatalf("Mega Drive import = %#v", result)
			}
			item := result.Items[0]
			if item.State != "REVIEW_PENDING" || item.CoreID != "genesis_plus_gx" || item.ContentKind != "SINGLE_FILE" {
				t.Fatalf("Mega Drive review = %#v", item)
			}
			var name, digest string
			if err := database.SQL.QueryRowContext(ctx, `
SELECT source.logical_name,blob.sha256
FROM import_item_source_files source JOIN blobs blob ON blob.id=source.blob_id
WHERE source.import_item_id=? AND source.role='CONTENT'
`, item.ItemID).Scan(&name, &digest); err != nil {
				t.Fatal(err)
			}
			if name != source.contentName || digest != fmt.Sprintf("%x", sha256.Sum256(payload)) {
				t.Fatalf("Mega Drive content was renamed or transformed: name=%s digest=%s", name, digest)
			}
		})
	}
}
