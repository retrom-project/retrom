package firmware

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/capability/content/firmware"
	"retrom/internal/capability/format/importing"
	model "retrom/internal/model/firmware"
)

func TestInstallRechecksSourceBeforePublishing(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*installMemory)
	}{
		{"changed requirement", func(memory *installMemory) { memory.current.Version++ }},
		{"disabled", func(memory *installMemory) { memory.current.Enabled = false }},
		{"changed blob", func(memory *installMemory) { memory.currentUpload.BlobID = "replacement" }},
		{"changed digest", func(memory *installMemory) { memory.currentUpload.SHA256 = "replacement" }},
		{"unfinished upload", func(memory *installMemory) { memory.currentUpload.State = "FAILED" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			memory := installFixture()
			test.change(memory)
			_, err := New(memory, time.Now).WithPayloadRelease(memory).Install(t.Context(), "requirement", 1, model.InstallRequest{UploadFileID: "file"})
			if !errors.Is(err, model.ErrInvalid) || memory.created != nil || memory.consumption != nil || memory.retired || memory.signals != 0 {
				t.Fatalf("changed source published: memory=%+v error=%v", memory, err)
			}
		})
	}
}

func TestInstallRecordsWarningAndSignalsAfterCommit(t *testing.T) {
	memory := installFixture()
	expected := "expected"
	memory.initial.SHA256, memory.current.SHA256 = &expected, &expected
	result, err := New(memory, func() time.Time { return time.UnixMilli(1234) }).WithPayloadRelease(memory).
		Install(t.Context(), "requirement", 1, model.InstallRequest{UploadFileID: "file"})
	if err != nil || result.Status != "HASH_WARNING" || !result.Active || result.CreatedAtMS != 1234 {
		t.Fatalf("installation=%+v error=%v", result, err)
	}
	if !memory.retired || memory.created == nil || memory.created.ID != result.InstallationID ||
		memory.consumption == nil || memory.consumption.InstallationID != result.InstallationID || memory.signals != 1 {
		t.Fatalf("installation records incomplete: %+v", memory)
	}
	if memory.signalInsideWrite {
		t.Fatal("release worker signaled before transaction committed")
	}
}

func TestStaticArchivesRejectAliasesWhileDATRemainsAdvisory(t *testing.T) {
	expected := []firmware.ExpectedDATEntry{{Name: "bios.bin", SizeBytes: 1, CRC32: "11111111"}}
	actual := []importing.ArchiveEntry{{NormalizedPath: "renamed.bin", Size: 1, CRC32: "11111111"}}
	for _, strict := range []bool{false, true} {
		status, details := evaluateArchive(expected, actual, strict)
		want := "MATCHED"
		if strict {
			want = "INVALID"
		}
		warnings, ok := details["warnings"].([]string)
		if status != want || !ok || len(warnings) != 1 {
			t.Fatalf("strict=%v status=%s findings=%v", strict, status, details)
		}
	}
}

type installMemory struct {
	model.Repository
	model.InstallationWriter

	initial, current               model.Requirement
	upload, currentUpload          model.Upload
	created                        *model.InstallationWrite
	consumption                    *model.Consumption
	retired                        bool
	signals                        int
	insideWrite, signalInsideWrite bool
}

func installFixture() *installMemory {
	requirement := model.Requirement{ID: "requirement", Enabled: true, Version: 1, FileKind: "RAW", SourceKind: "STATIC"}
	upload := model.Upload{ID: "file", SessionID: "upload", State: "COMPLETE", BlobID: "blob", SHA256: "digest"}
	return &installMemory{initial: requirement, current: requirement, upload: upload, currentUpload: upload}
}

func (memory *installMemory) WithRead(_ context.Context, work func(model.ReadScope) error) error {
	return work(model.ReadScope{Requirements: requirementMemory{value: memory.initial}, Uploads: uploadMemory{memory.upload}})
}

func (memory *installMemory) WithWrite(_ context.Context, work func(model.WriteScope) error) error {
	memory.insideWrite = true
	defer func() { memory.insideWrite = false }()
	return work(model.WriteScope{ReadScope: model.ReadScope{
		Requirements: requirementMemory{value: memory.current},
		Uploads:      uploadMemory{memory.currentUpload},
	}, Installations: memory, Retirements: model.SupersessionScope{Read: memory, Write: memory}})
}

func (memory *installMemory) Create(_ context.Context, value model.InstallationWrite) error {
	memory.created = &value
	return nil
}

func (memory *installMemory) Consume(_ context.Context, value model.Consumption) error {
	memory.consumption = &value
	return nil
}

func (memory *installMemory) Deactivate(context.Context, model.SupersededInstallation, int64) error {
	memory.retired = true
	return nil
}

func (memory *installMemory) Signal() {
	memory.signals++
	memory.signalInsideWrite = memory.insideWrite
}

type requirementMemory struct {
	model.RequirementRecords

	value model.Requirement
}

func (records requirementMemory) Get(context.Context, string) (model.Requirement, bool, error) {
	return records.value, true, nil
}

type uploadMemory struct{ value model.Upload }

func (records uploadMemory) Get(context.Context, string) (model.Upload, bool, error) {
	return records.value, true, nil
}

func (memory *installMemory) Current(context.Context, string) (model.SupersededInstallation, bool, error) {
	return model.SupersededInstallation{ID: "old", RequirementID: "requirement", BlobID: "blob", Version: 1}, true, nil
}
func (memory *installMemory) Consumption(context.Context, string) (string, error) { return "", nil }
