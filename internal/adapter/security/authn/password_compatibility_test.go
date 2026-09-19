package authn

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	authnpolicy "retrom/internal/capability/security/authn"
)

type passwordGolden struct {
	Samples []struct {
		Password string `json:"password"`
		SaltHex  string `json:"saltHex"`
		PHC      string `json:"phc"`
	} `json:"samples"`
	Rejected []struct {
		PHC          string `json:"phc"`
		Error        string `json:"error"`
		IsCredential bool   `json:"isCredential"`
	} `json:"rejected"`
	Parallelism int `json:"parallelism"`
}

func loadPasswordGolden(t *testing.T) passwordGolden {
	t.Helper()
	data, err := os.ReadFile("testdata/password-golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden passwordGolden
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	return golden
}

func TestPasswordHasherMatchesOldGoPHC(t *testing.T) {
	t.Parallel()
	golden := loadPasswordGolden(t)
	if len(golden.Samples) != 2 || len(golden.Rejected) != 5 || golden.Parallelism != 4 {
		t.Fatal("old implementation characterization was changed")
	}
	if cap(NewPasswordHasher().semaphore) != golden.Parallelism {
		t.Fatal("production Argon2 concurrency limit changed")
	}
	for _, sample := range golden.Samples {
		salt, err := hex.DecodeString(sample.SaltHex)
		if err != nil {
			t.Fatal(err)
		}
		hasher := newPasswordHasher(bytes.NewReader(salt), 1)
		encoded, err := hasher.Hash(t.Context(), sample.Password)
		if err != nil || encoded != sample.PHC {
			t.Fatalf("PHC differs from old Go bytes: got %q err=%v", encoded, err)
		}
		matches, err := hasher.Verify(t.Context(), sample.Password, sample.PHC)
		if err != nil || !matches {
			t.Fatalf("old credential no longer verifies: %t, %v", matches, err)
		}
		if len(hasher.semaphore) != 0 {
			t.Fatal("Hash or Verify retained a worker slot")
		}
	}
}

func TestPasswordHasherMatchesOldGoRejections(t *testing.T) {
	t.Parallel()
	golden := loadPasswordGolden(t)
	if len(golden.Rejected) != 5 {
		t.Fatal("old implementation rejection cases were changed")
	}
	for _, sample := range golden.Rejected {
		_, err := NewPasswordHasher().Verify(t.Context(), "public fixture", sample.PHC)
		if err == nil || err.Error() != sample.Error || errors.Is(err, authnpolicy.ErrCredential) != sample.IsCredential {
			t.Fatalf("old rejected PHC changed result: %v", err)
		}
	}
}

func TestPasswordHasherPreservesPreparationErrorPrecedence(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	hasher := newPasswordHasher(bytes.NewReader(nil), 1)
	encoded, err := hasher.Hash(ctx, "public fixture")
	if encoded != "" || !errors.Is(err, io.EOF) || !strings.HasPrefix(err.Error(), "generate password salt: ") {
		t.Fatalf("salt failure was hidden by cancellation: %q, %v", encoded, err)
	}
	matches, err := hasher.Verify(ctx, "public fixture", "invalid PHC")
	if matches || !errors.Is(err, authnpolicy.ErrCredential) {
		t.Fatalf("credential validation no longer precedes worker admission: %t, %v", matches, err)
	}
}

func TestPasswordHasherCanceledWorkerAdmission(t *testing.T) {
	t.Parallel()
	golden := loadPasswordGolden(t)
	salt, err := hex.DecodeString(golden.Samples[0].SaltHex)
	if err != nil {
		t.Fatal(err)
	}
	hasher := newPasswordHasher(bytes.NewReader(salt), 4)
	// Fill the bounded worker slots explicitly. A canceled request cannot
	// acquire a fifth slot, and must not remove a slot owned by another job.
	for range cap(hasher.semaphore) {
		hasher.semaphore <- struct{}{}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	encoded, err := hasher.Hash(ctx, golden.Samples[0].Password)
	if encoded != "" || !errors.Is(err, context.Canceled) ||
		!strings.HasPrefix(err.Error(), "acquire password hash worker: wait for password worker: ") {
		t.Fatalf("hash admission did not retain cancellation: %q, %v", encoded, err)
	}
	matches, err := hasher.Verify(ctx, golden.Samples[0].Password, golden.Samples[0].PHC)
	if matches || !errors.Is(err, context.Canceled) ||
		!strings.HasPrefix(err.Error(), "acquire password verification worker: wait for password worker: ") {
		t.Fatalf("verify admission did not retain cancellation: %t, %v", matches, err)
	}
	if len(hasher.semaphore) != cap(hasher.semaphore) {
		t.Fatal("failed admission released someone else's worker slot")
	}
}
