package gamemetadata

import (
	"testing"

	"retrom/internal/filestore"
)

func TestConstructorRejectsMissingDependencies(t *testing.T) {
	files, err := filestore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name       string
		repository CandidateApplyRepository
		files      *filestore.Store
	}{
		{name: "repository", files: files}, {name: "files", repository: &candidateApplyMemory{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("missing required dependency accepted")
				}
			}()
			New(test.repository, test.files, nil)
		})
	}
}
