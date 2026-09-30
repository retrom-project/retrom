package libraryimport

import (
	"errors"
	"slices"
	"testing"

	launch "retrom/internal/service/launch"
)

func TestReviewPreviewCommitRechecksActualFilesEvenWhenDependencyJSONIsUnchanged(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*launch.PreviewSnapshot)
	}{
		{"content replaced", func(input *launch.PreviewSnapshot) { input.SourceFiles[0].FileRecord = "changed-content" }},
		{"parent replaced", func(input *launch.PreviewSnapshot) { input.ValidationFiles[0].FileRecord = "changed-parent" }},
		{"external dependency added", func(input *launch.PreviewSnapshot) {
			input.ValidationFiles = append(input.ValidationFiles, launch.PreviewFile{Role: "EXTERNAL_FILE", LogicalName: "device.bin", FileRecord: "new-device", VirtualPath: new("/device.bin")})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			creator, repository, provider, request := previewFixture(t)
			repository.snapshot.ValidationFiles = []launch.PreviewFile{{Role: "PARENT", LogicalName: "parent.zip", FileRecord: "parent"}}
			provider.before = func() {
				current := repository.snapshot
				current.SourceFiles = slices.Clone(current.SourceFiles)
				current.ValidationFiles = slices.Clone(current.ValidationFiles)
				test.change(&current)
				repository.currentSnapshot = &current
			}
			result, err := creator.Create(t.Context(), request)
			if !errors.Is(err, launch.ErrReviewPreviewUnavailable) || result.PreviewID != "" || len(repository.writes) != 0 {
				t.Fatalf("stale resources committed: id=%q writes=%d error=%v", result.PreviewID, len(repository.writes), err)
			}
		})
	}
}
