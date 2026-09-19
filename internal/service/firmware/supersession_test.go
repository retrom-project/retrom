package firmware

import (
	"context"
	"errors"
	"math"
	"testing"

	model "retrom/internal/model/firmware"
)

type supersessionMemory struct {
	before             model.SupersededInstallation
	found, deactivated bool
	failure            error
}

func (memory *supersessionMemory) Current(context.Context, string) (model.SupersededInstallation, bool, error) {
	return memory.before, memory.found, nil
}

func (memory *supersessionMemory) Consumption(context.Context, string) (string, error) {
	return "", memory.failure
}

func (memory *supersessionMemory) Deactivate(context.Context, model.SupersededInstallation, int64) error {
	memory.deactivated = true
	return nil
}

func TestBIOSSupersessionRejectsInvalidIdentityBeforeWriting(t *testing.T) {
	t.Parallel()
	for _, before := range []model.SupersededInstallation{
		{ID: "old", RequirementID: "other", BlobID: "blob", Version: 1},
		{ID: "old", RequirementID: "requirement", BlobID: "blob", Version: math.MaxInt64},
	} {
		memory := &supersessionMemory{before: before, found: true}
		err := SupersedeInScope(t.Context(), model.SupersessionScope{Read: memory, Write: memory}, "requirement", 10)
		if !errors.Is(err, model.ErrInvalid) || memory.deactivated {
			t.Fatalf("invalid supersession mutated owner: %+v %v", before, err)
		}
	}
}

func TestBIOSSupersessionPreservesConsumptionReadCause(t *testing.T) {
	t.Parallel()
	cause := errors.New("read old consumption failure")
	memory := &supersessionMemory{before: model.SupersededInstallation{
		ID: "old", RequirementID: "requirement", BlobID: "blob", Version: 1,
	}, found: true, failure: cause}
	err := SupersedeInScope(t.Context(), model.SupersessionScope{Read: memory, Write: memory}, "requirement", 10)
	if !errors.Is(err, cause) || !memory.deactivated {
		t.Fatalf("supersession cause lost: %v", err)
	}
}

func TestBIOSSupersessionWithoutAnActiveInstallationDoesNotWrite(t *testing.T) {
	t.Parallel()
	memory := &supersessionMemory{}
	err := SupersedeInScope(t.Context(), model.SupersessionScope{Read: memory, Write: memory}, "requirement", 10)
	if err != nil || memory.deactivated {
		t.Fatalf("missing active installation wrote: %v", err)
	}
}
