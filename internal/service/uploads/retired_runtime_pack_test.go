package uploads

import (
	model "retrom/internal/model/uploads"
	"testing"
)

func TestUploadRejectsRetiredRuntimePackPurpose(t *testing.T) {
	request := model.CreateRequest{Purpose: "RUNTIME_ASSET_PACK", SourceType: "DIRECTORY", Files: []model.FileDeclaration{{ClientFileID: "file", RelativePath: "Music/theme.wav", SizeBytes: 1}}}
	if validUploadShape(request) {
		t.Fatal("retired pack upload capability remains active")
	}
	for _, purpose := range []string{"GENERAL", "PROJECT"} {
		request.Purpose = purpose
		if !validUploadShape(request) {
			t.Fatalf("ordinary %s game upload was removed", purpose)
		}
	}
}
