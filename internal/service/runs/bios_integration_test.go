//go:build integration

package runs

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/testsupport"
)

func TestMissingBIOSIsAReadinessFailure(t *testing.T) {
	t.Parallel()
	service := &Service{Repository: testsupport.Database(t)}
	prepared := runtimeclient.Prepared{BiosRequirements: []runtimeclient.BiosRequirement{{
		RequirementKey: "firmware/test.bin", Required: true,
	}}}
	_, err := service.bios(context.Background(), &Context{}, prepared, nil)
	if !errors.Is(err, model.ErrBIOSMissing) || errors.Is(err, model.ErrNotFound) {
		t.Fatalf("missing required BIOS lost its domain reason: %v", err)
	}
	prepared.BiosRequirements[0].Required = false
	resources, err := service.bios(context.Background(), &Context{}, prepared, nil)
	if err != nil || len(resources) != 0 {
		t.Fatalf("optional BIOS prevented launch: %v", err)
	}
}

func TestBIOSDeliveryUsesDeclaredResourceRoles(t *testing.T) {
	t.Parallel()
	service := &Service{Repository: testsupport.Database(t)}
	prepared := runtimeclient.Prepared{}
	for _, delivery := range []string{"BIOS_BUNDLE", "EXTERNAL_FILE"} {
		id := uuid.NewString()
		key := "firmware/" + delivery + ".bin"
		file := model.BiosFile{
			ID: id, RequirementKey: key, Filename: "source.bin",
			StorageKey: "bios/" + id + "/file", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SizeBytes: 1,
		}
		if err := service.Repository.WriteBios(t.Context(), file, 1); err != nil {
			t.Fatal(err)
		}
		prepared.BiosRequirements = append(prepared.BiosRequirements, runtimeclient.BiosRequirement{
			RequirementKey: key, LogicalName: delivery + ".bin", Delivery: delivery, Required: true,
		})
	}
	resources, err := service.bios(t.Context(), &Context{}, prepared, nil)
	if err != nil || len(resources) != 2 {
		t.Fatalf("BIOS resource assembly failed: %v", err)
	}
	if resources[0]["role"] != "bios" || resources[0]["kind"] != "BIOS_BUNDLE" || resources[0]["ordinal"] != 0 ||
		resources[1]["role"] != "external" || resources[1]["kind"] != "EXTERNAL_FILE_SET" || resources[1]["ordinal"] != 0 {
		t.Fatalf("firmware delivery contradicts Target roles: %v", resources)
	}
}
