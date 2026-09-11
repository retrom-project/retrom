package uploads

import "testing"

func TestUploadRejectsRetiredRuntimePackPurpose(t *testing.T) {
	request := CreateRequest{Purpose: "RUNTIME_ASSET_PACK", SourceType: "DIRECTORY", Files: []FileDeclaration{{ClientFileID: "file", RelativePath: "Music/theme.wav", SizeBytes: 1}}}
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
