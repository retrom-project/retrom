package gamecontent

import (
	"bytes"
	"errors"
	"testing"

	contentcapability "retrom/internal/content/capability"
)

func TestReplacementRejectsDisguisedAndTruncatedWASMBeforePublication(t *testing.T) {
	for _, contents := range [][]byte{
		[]byte("not a WebAssembly cartridge"),
		{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c, 0, 4},
		{0, 'a', 's', 'm', 1, 0, 0, 0, 1, 20, 0},
	} {
		files := constructorFiles(t)
		stored, err := files.Put(bytes.NewReader(contents))
		if err != nil {
			t.Fatal(err)
		}
		service := &Service{blobs: files}
		_, err = service.prepareReplacement(t.Context(), JobSnapshot{
			PlatformID: "wasm4", ContentMode: contentcapability.ModeStandard,
			ContentPolicy: contentcapability.NewPolicy(contentcapability.ModeStandard),
		}, []UploadedFile{{
			LogicalName: "disguised.wasm", FileRecord: stored.Record, SHA256: stored.SHA256, SizeBytes: stored.Size,
		}})
		var invalid *replacementValidationError
		if !errors.As(err, &invalid) {
			t.Fatalf("invalid cartridge reached publication preparation: %v", err)
		}
	}
}
