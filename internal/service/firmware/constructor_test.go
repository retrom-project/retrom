package firmware

import (
	"testing"

	"retrom/internal/filestore"
)

func TestConstructorRejectsMissingDependencies(t *testing.T) {
	files, err := filestore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, deps := range []Dependencies{{}, {Files: files}, {Repository: &installMemory{}}} {
		t.Run("missing dependency", func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("missing required dependency accepted")
				}
			}()
			New(deps, nil)
		})
	}
}
