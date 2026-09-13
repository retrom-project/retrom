package libraryimport

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"retrom/internal/capability/security/authn"
)

type multidiscAdmissionMemory struct {
	MultiDiscAttachmentRepository
	writes int
}

func (memory *multidiscAdmissionMemory) WithAttachmentAdmission(_ context.Context, work func(MultiDiscAttachmentScope) error) error {
	memory.writes++
	return work(MultiDiscAttachmentScope{})
}

func TestMultiDiscAdmissionRejectsInvalidVersionBeforeTransaction(t *testing.T) {
	memory := &multidiscAdmissionMemory{}
	service := NewMultiDiscAttachments(memory, MultiDiscAttachmentOptions{Now: time.Now, StorageAvailable: true})
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: "actor"})
	_, err := service.Create(ctx, "item", math.MaxInt64, MultiDiscAttachmentRequest{UploadID: "upload"})
	if MultiDiscAttachmentErrorCode(err) != MultiDiscAttachmentErrorInvalid || memory.writes != 0 {
		t.Fatalf("invalid version touched transaction: writes=%d err=%v", memory.writes, err)
	}
}

func TestMultiDiscAdmissionIdentityFailurePrecedesTransaction(t *testing.T) {
	cause := errors.New("attachment identity unavailable")
	memory := &multidiscAdmissionMemory{}
	service := NewMultiDiscAttachments(memory, MultiDiscAttachmentOptions{Now: time.Now, StorageAvailable: true})
	service.newID = func() (string, error) { return "", cause }
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: "actor"})
	_, err := service.Create(ctx, "item", 1, MultiDiscAttachmentRequest{UploadID: "upload"})
	if !errors.Is(err, cause) || memory.writes != 0 {
		t.Fatalf("identity failure lost: writes=%d err=%v", memory.writes, err)
	}
}
