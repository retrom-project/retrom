package emulationstationimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	application "retrom/internal/service/emulationstationimport"
)

func TestCreationPersistsFrozenYearAndInputDigestWithResponse(t *testing.T) {
	t.Parallel()
	db := creationDatabase(t)
	plan := creationPlan(0)
	var response application.Summary
	err := NewCreation(db).WithCreate(t.Context(), func(writer application.CreationWriter) error {
		var err error
		response, err = writer.Insert(t.Context(), plan)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	var encoded, digest string
	var storedYear int
	if err := db.QueryRowContext(t.Context(), `SELECT input.input_json,input.input_digest,plan.release_year_max FROM job_input_snapshots input JOIN emulationstation_imports plan ON plan.scan_job_id=input.job_id WHERE plan.id=?`, plan.ImportID).Scan(&encoded, &digest, &storedYear); err != nil {
		t.Fatal(err)
	}
	var input struct {
		Kind        string `json:"kind"`
		ExecutionID string `json:"executionId"`
		Inputs      struct {
			ReleaseYearMax   int    `json:"releaseYearMax"`
			RootConfigDigest string `json:"rootConfigDigest"`
		} `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(encoded), &input); err != nil {
		t.Fatal(err)
	}
	if storedYear != plan.ReleaseYearMax || input.Inputs.ReleaseYearMax != storedYear || input.Inputs.RootConfigDigest != plan.Root.Digest {
		t.Fatalf("frozen input=%#v year=%d", input, storedYear)
	}
	expected := sha256.Sum256([]byte(encoded))
	if digest != hex.EncodeToString(expected[:]) || input.ExecutionID != plan.ExecutionID || input.Kind != "SERVER_EMULATIONSTATION_SCAN" {
		t.Fatalf("execution input=%#v digest=%s", input, digest)
	}
	if response.CreatedAtMS != plan.NowMS || response.ExpiresAtMS != plan.ExpiresAtMS || response.CreatedBy.ID != plan.ActorID {
		t.Fatalf("creation response=%#v", response)
	}
}
