//go:build integration

package refactor

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRefactorRF01_characterization(t *testing.T) {
	t.Parallel()
	expected, err := os.ReadFile(filepath.Join("testdata", "baseline-82834ba.json"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := captureCharacterization(t)
	actual, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	actual = append(actual, '\n')
	if !bytes.Equal(actual, expected) {
		wantHash, gotHash := sha256.Sum256(expected), sha256.Sum256(actual)
		t.Fatalf("baseline behavior changed: expected=%s actual=%s; investigate without regenerating the golden",
			hex.EncodeToString(wantHash[:]), hex.EncodeToString(gotHash[:]))
	}
}
