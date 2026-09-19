package libraryimport

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	model "retrom/internal/model/libraryimport"

	"retrom/internal/capability/security/authn"
)

type multidiscAdmissionMemory struct {
	model.MultiDiscAttachmentRepository
	writes int
}

func (memory *multidiscAdmissionMemory) WithAttachmentAdmission(_ context.Context, work func(model.MultiDiscAttachmentScope) error) error {
	memory.writes++
	return work(model.MultiDiscAttachmentScope{})
}

func (memory *multidiscAdmissionMemory) CommitAttachmentAdmission(_ context.Context, _ model.AttachmentAdmissionCommand) (model.MultiDiscAttachmentCreated, error) {
	memory.writes++
	return model.MultiDiscAttachmentCreated{State: "QUEUED"}, nil
}

func TestMultiDiscAdmissionRejectsInvalidVersionBeforeTransaction(t *testing.T) {
	memory := &multidiscAdmissionMemory{}
	service := NewMultiDiscAttachments(memory, MultiDiscAttachmentOptions{Now: time.Now, StorageAvailable: true})
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: "actor"})
	_, err := service.Create(ctx, "item", math.MaxInt64, model.MultiDiscAttachmentRequest{UploadID: "upload"})
	if model.MultiDiscAttachmentErrorCode(err) != model.MultiDiscAttachmentErrorInvalid || memory.writes != 0 {
		t.Fatalf("invalid version touched transaction: writes=%d err=%v", memory.writes, err)
	}
}

func TestMultiDiscAdmissionIdentityFailurePrecedesTransaction(t *testing.T) {
	cause := errors.New("attachment identity unavailable")
	memory := &multidiscAdmissionMemory{}
	service := NewMultiDiscAttachments(memory, MultiDiscAttachmentOptions{Now: time.Now, StorageAvailable: true})
	service.newID = func() (string, error) { return "", cause }
	ctx := authn.WithPrincipal(t.Context(), authn.Principal{UserID: "actor"})
	_, err := service.Create(ctx, "item", 1, model.MultiDiscAttachmentRequest{UploadID: "upload"})
	if !errors.Is(err, cause) || memory.writes != 0 {
		t.Fatalf("identity failure lost: writes=%d err=%v", memory.writes, err)
	}
}
