package libraryimport

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	"retrom/internal/content/requirements"
	"retrom/internal/filestore"
)

func TestPrepared3DSRejectsEncryptedContentBeforeReviewCreation(t *testing.T) {
	blobs, err := filestore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, decrypted := range []bool{false, true} {
		data := make([]byte, 4096)
		copy(data[0x100:], "NCCH")
		binary.LittleEndian.PutUint32(data[0x104:], 8)
		data[0x18d] = 2
		if decrypted {
			data[0x18f] = 4
		}
		meta, err := blobs.Put(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		file := ImportFile{ID: "upload", FileRecord: meta.Record, SHA256: meta.SHA256, Size: meta.Size}
		plan := PreparedImport{Target: ImportTarget{}, Groups: []PreparedGroup{{Sources: []PreparedSource{{File: file, Role: "CONTENT", LogicalName: "game.cci"}}}}, Dispositions: []PreparedDisposition{{File: file, Disposition: "SOURCE"}}}
		plan.Target.Policy.Requirements = &requirements.Policy{Kind: requirements.Decrypted3DS}
		service := NewImportPreparation(nil, nil, blobs, ImportPreparationOptions{})
		if err := service.applyInputRequirements(context.Background(), &plan); err != nil {
			t.Fatal(err)
		}
		if decrypted {
			if len(plan.Groups) != 1 || plan.Groups[0].ContentFacts == nil || plan.Groups[0].ContentFacts.Nintendo3DS.Partitions[0].Encrypted {
				t.Fatalf("decrypted facts lost: %+v", plan.Groups)
			}
		} else if len(plan.Groups) != 0 || plan.Dispositions[0].Reason != "THREEDS_ENCRYPTED_CONTENT" || plan.Dispositions[0].Rejection.RelativePath != "game.cci" {
			t.Fatalf("encrypted content accepted: %+v", plan)
		}
	}
}
