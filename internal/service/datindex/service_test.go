package datindex

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/datindex"
)

type datMemory struct {
	requirements []model.Requirement
	retired      *model.Retirement
	failWrite    error
}

func (*datMemory) Definition(context.Context, string) (model.Definition, error) {
	return model.Definition{CoreID: "core", ProviderID: "provider", TargetID: "target", SHA256: "dat-sha"}, nil
}

func (*datMemory) MachineNames(context.Context, string) ([]string, error) {
	return []string{"bios", "unresolved-parent"}, nil
}

func (*datMemory) RequiredEntries(context.Context, string, string) ([]model.Entry, error) {
	return []model.Entry{}, nil
}

func (memory *datMemory) UpsertRequirement(_ context.Context, requirement model.Requirement) error {
	memory.requirements = append(memory.requirements, requirement)
	return memory.failWrite
}

func (memory *datMemory) DisableStale(_ context.Context, retirement model.Retirement) error {
	memory.retired = &retirement
	return nil
}

func TestRequirementSynchronizationRetainsUnresolvedParentSlot(t *testing.T) {
	t.Parallel()
	memory := &datMemory{}
	err := SyncRequirements(t.Context(), memory, "version", time.UnixMilli(1234))
	if err != nil || len(memory.requirements) != 2 || memory.requirements[1].LogicalName != "unresolved-parent.zip" {
		t.Fatalf("requirements=%+v error=%v", memory.requirements, err)
	}
	if memory.retired == nil || *memory.retired != (model.Retirement{ProviderID: "provider", TargetID: "target", CurrentVersionID: "version", AtMS: 1234}) {
		t.Fatalf("retirement=%+v", memory.retired)
	}
}

func TestRequirementFailurePreventsRetiringExistingCatalog(t *testing.T) {
	t.Parallel()
	failure := errors.New("requirement write failed")
	memory := &datMemory{failWrite: failure}
	err := SyncRequirements(t.Context(), memory, "version", time.UnixMilli(1234))
	if !errors.Is(err, failure) || memory.retired != nil || len(memory.requirements) != 1 {
		t.Fatalf("error=%v retired=%+v requirements=%d", err, memory.retired, len(memory.requirements))
	}
}

func TestRequirementIdentityAndDigestStayStable(t *testing.T) {
	t.Parallel()
	crc := "12345678"
	requirement, err := model.BuildRequirement(model.Definition{CoreID: "core", ProviderID: "provider", TargetID: "target", SHA256: "dat-sha"}, "version", "bios", []model.Entry{{Name: "bios.bin", CRC32: &crc, SizeBytes: 4, Status: "GOOD"}}, 1234)
	if err != nil {
		t.Fatal(err)
	}
	if requirement.ID != "f1954d23-f36c-5d5a-ae40-f029244262ae" ||
		requirement.Digest != "bbd43e8aad0a226a8e0fc0c549358e38659c5a82a7883e85775062beb198e787" ||
		requirement.SourceURL != "retrom:dat:version#bios" || requirement.VersionID != "version" {
		t.Fatalf("requirement=%+v", requirement)
	}
}
