package pegasusimport

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

var errCreationEntropy = errors.New("creation entropy unavailable")

type unavailableCreationEntropy struct{}

func (unavailableCreationEntropy) Read([]byte) (int, error) { return 0, errCreationEntropy }

// Sequential: uuid's random reader is process-wide and must be restored before parallel tests resume.
func TestCreatePropagatesEntropyFailureWithoutPersistingPlan(t *testing.T) {
	db := newPegasusRetryDatabase(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Roms"), 0o700); err != nil {
		t.Fatal(err)
	}
	service := &Service{database: db, now: time.Now, wake: make(chan struct{}, 1), roots: map[string]Root{"games": {ID: "games", Label: "Games", path: root, digest: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
	uuid.SetRand(unavailableCreationEntropy{})
	value, err := func() (Summary, error) {
		defer uuid.SetRand(nil)
		return service.Create(t.Context(), CreateRequest{RootID: "games", SourceRelativePath: "Roms"}, "user")
	}()
	if !errors.Is(err, errCreationEntropy) || value.ID != "" {
		t.Errorf("entropy failure: %#v, %v", value, err)
	}
	var plans int
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM pegasus_imports`).Scan(&plans); err != nil {
		t.Fatal(err)
	}
	if plans != 1 {
		t.Errorf("entropy failure persisted %d extra plans", plans-1)
	}
}
