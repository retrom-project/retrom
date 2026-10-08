//go:build integration

package bios

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"retrom/internal/model"
	"retrom/internal/runtimeclient"
	"retrom/internal/testsupport"
)

func TestListKeepsRuntimeValidationRequirementsSeparateFromInstalledBytes(t *testing.T) {
	t.Parallel()
	f := testsupport.Library(t)
	sizeA, sizeB := int64(16384), int64(32768)
	sha, md5 := "runtime-sha256", "runtime-md5"
	s := &Service{Repository: f.Repository, Runtime: &runtimeclient.Client{
		BiosRequirements: []runtimeclient.BiosRequirement{
			{RequirementKey: "firmware.bin", LogicalName: "firmware.bin", CoreID: "core-a", Required: true, SizeBytes: &sizeA, SHA256: &sha},
			{RequirementKey: "firmware.bin", LogicalName: "firmware.bin", CoreID: "core-b", SizeBytes: &sizeB, MD5: &md5},
			{RequirementKey: "unknown.zip", LogicalName: "unknown.zip", CoreID: "arcade"},
		},
	}}
	want := []model.BiosValidationRequirement{
		{CoreID: "core-a", SizeBytes: &sizeA, SHA256: &sha},
		{CoreID: "core-b", SizeBytes: &sizeB, MD5: &md5},
	}
	before, err := s.List(t.Context())
	if err != nil || len(before) != 2 || !reflect.DeepEqual(before[0].Requirements, want) || before[0].Installed {
		t.Fatalf("uninstalled requirements=%+v error=%v", before, err)
	}
	if !reflect.DeepEqual(before[1].Requirements, []model.BiosValidationRequirement{{CoreID: "arcade"}}) {
		t.Fatalf("unknown validation requirements=%+v", before[1])
	}
	file := model.BiosFile{ID: uuid.NewString(), RequirementKey: "firmware.bin", Filename: "installed.bin", StorageKey: "bios/installed", SizeBytes: 123, SHA256: "installed-sha256"}
	if err = f.Repository.WriteBios(t.Context(), file, 1000); err != nil {
		t.Fatal(err)
	}
	after, err := s.List(t.Context())
	if err != nil || !after[0].Installed || after[0].SizeBytes != 123 || after[0].SHA256 != file.SHA256 || !reflect.DeepEqual(after[0].Requirements, want) {
		t.Fatalf("installed requirements=%+v error=%v", after, err)
	}
}
