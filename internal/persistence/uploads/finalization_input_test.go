package uploads

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	uploadservice "retrom/internal/service/uploads"
)

func TestFinalizationDamagedInputPreservesJSONCause(t *testing.T) {
	for _, input := range []string{`{"schemaVersion":`, `{"schemaVersion":"bad"}`} {
		t.Run(input, func(t *testing.T) {
			fixture := newFinalizationFixture(t)
			session := fixture.upload(t, []byte("bytes"))
			fixture.service.Close()
			job := fixture.complete(t, session)
			digest := sha256.Sum256([]byte(input))
			if _, err := fixture.database.ExecContext(t.Context(), `UPDATE job_input_snapshots SET input_json=?,input_digest=? WHERE job_id=? AND execution_no=1`, input, hex.EncodeToString(digest[:]), job); err != nil {
				t.Fatal(err)
			}
			err := uploadservice.New(New(fixture.database), fixture.blobs, fixture.root, finalizationNow).Run(t.Context(), job)
			var syntax *json.SyntaxError
			var value *json.UnmarshalTypeError
			if !errors.Is(err, uploadservice.ErrInputInvalid) || !errors.As(err, &syntax) && !errors.As(err, &value) {
				t.Fatalf("JSON cause lost: %v", err)
			}
			awaitFinalizeState(t, fixture.database, job, "FAILED")
			var count int
			if err := fixture.database.QueryRowContext(t.Context(), `SELECT count(*) FROM blobs`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("invalid input published %d blobs", count)
			}
		})
	}
}
