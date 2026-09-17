package firmware

import (
	"context"
	"errors"
	"testing"
	"time"

	"retrom/internal/capability/content/firmware"
	"retrom/internal/capability/format/importing"
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
			_, err := New(memory, time.Now).WithPayloadRelease(memory).Install(t.Context(), "requirement", 1,
				InstallRequest{UploadFileID: "file"})
			if !errors.Is(err, ErrInvalid) || memory.created != nil || memory.consumption != nil || memory.retired || memory.signals != 0 {
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
		Install(t.Context(), "requirement", 1, InstallRequest{UploadFileID: "file"})
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
	comparisons, missing, mismatched, warnings := firmware.CompareArchiveEntries(expected, actual)
	for _, strict := range []bool{false, true} {
		details := map[string]any{
			"schemaVersion": 1, "missingEntries": missing,
			"mismatchedEntries": mismatched, "warnings": warnings,
		}
		status := "MATCHED"
		if strict && (len(missing) > 0 || len(mismatched) > 0 || len(warnings) > 0) {
			status = "INVALID"
		} else if len(comparisons) == 0 || len(expected) == 0 || len(missing) > 0 {
			status = "MISSING_ENTRY"
		} else if len(mismatched) > 0 {
			status = "HASH_WARNING"
		}
		want := "MATCHED"
		if strict {
			want = "INVALID"
		}
		ws, ok := details["warnings"].([]string)
		if status != want || !ok || len(ws) != 1 {
			t.Fatalf("strict=%v status=%s findings=%v", strict, status, details)
		}
	}
}

type installMemory struct {
	Repository
	InstallationWriter
	initial, current               Requirement
	upload, currentUpload          Upload
	created                        *InstallationWrite
	consumption                    *Consumption
	retired                        bool
	signals                        int
	insideWrite, signalInsideWrite bool
}

func installFixture() *installMemory {
	requirement := Requirement{ID: "requirement", Enabled: true, Version: 1, FileKind: "RAW", SourceKind: "STATIC"}
	upload := Upload{ID: "file", SessionID: "upload", State: "COMPLETE", BlobID: "blob", SHA256: "digest"}
	return &installMemory{initial: requirement, current: requirement, upload: upload, currentUpload: upload}
}

func (memory *installMemory) WithRead(_ context.Context, work func(ReadScope) error) error {
	return work(ReadScope{Requirements: requirementMemory{value: memory.initial}, Uploads: uploadMemory{memory.upload}})
}

func (memory *installMemory) CommitBrowserInstall(_ context.Context, cmd BrowserInstallCommand) (Installation, error) {
	memory.insideWrite = true
	defer func() { memory.insideWrite = false }()
	if memory.current.SourceKind != cmd.PreparedSourceKind ||
		memory.current.FileKind != cmd.PreparedFileKind ||
		memory.currentUpload.BlobID != cmd.PreparedBlobID ||
		memory.currentUpload.SHA256 != cmd.PreparedSHA256 {
		return Installation{}, ErrInvalid
	}
	if !memory.current.Enabled || memory.current.Version != cmd.Version {
		return Installation{}, ErrInvalid
	}
	if memory.currentUpload.State != "COMPLETE" {
		return Installation{}, ErrInvalid
	}
	memory.retired = true
	id := "generated-id"
	memory.created = &InstallationWrite{
		ID: id, RequirementID: cmd.RequirementID, BlobID: memory.currentUpload.BlobID,
		Filename: memory.currentUpload.RelativePath,
		MD5: memory.currentUpload.MD5, SHA1: memory.currentUpload.SHA1,
		SHA256: memory.currentUpload.SHA256, Size: memory.currentUpload.Size,
		Status: "MATCHED", RequirementVersion: memory.current.Version,
		AtMS: cmd.NowMS, SourceKind: "BROWSER_UPLOAD",
	}
	memory.consumption = &Consumption{
		ID: "consumption-id", UploadID: memory.currentUpload.SessionID,
		FileID: memory.currentUpload.ID, InstallationID: id, AtMS: cmd.NowMS,
	}
	status := "MATCHED"
	if (memory.current.SHA256 != nil && *memory.current.SHA256 != memory.currentUpload.SHA256) ||
		(memory.current.Size != nil && *memory.current.Size != memory.currentUpload.Size) ||
		(memory.current.MD5 != nil && *memory.current.MD5 != memory.currentUpload.MD5) ||
		(memory.current.SHA1 != nil && *memory.current.SHA1 != memory.currentUpload.SHA1) {
		status = "HASH_WARNING"
	}
	return Installation{
		InstallationID: id, RequirementID: cmd.RequirementID, Status: status, Active: true,
		ValidatedRequirementVersion: memory.current.Version, CreatedAtMS: cmd.NowMS,
	}, nil
}

func (memory *installMemory) CommitServerInstall(context.Context, ServerInstallCommand) (ServerInstallResult, error) {
	return ServerInstallResult{}, nil
}

func (memory *installMemory) Create(_ context.Context, value InstallationWrite) error {
	memory.created = &value
	return nil
}

func (memory *installMemory) Consume(_ context.Context, value Consumption) error {
	memory.consumption = &value
	return nil
}

func (memory *installMemory) Deactivate(context.Context, SupersededInstallation, int64) error {
	memory.retired = true
	return nil
}

func (memory *installMemory) Signal() {
	memory.signals++
	memory.signalInsideWrite = memory.insideWrite
}

type requirementMemory struct {
	RequirementRecords
	value Requirement
}

func (records requirementMemory) Get(context.Context, string) (Requirement, bool, error) {
	return records.value, true, nil
}

type uploadMemory struct{ value Upload }

func (records uploadMemory) Get(context.Context, string) (Upload, bool, error) {
	return records.value, true, nil
}

func (memory *installMemory) Current(context.Context, string) (SupersededInstallation, bool, error) {
	return SupersededInstallation{ID: "old", RequirementID: "requirement", BlobID: "blob", Version: 1}, true, nil
}
func (memory *installMemory) Consumption(context.Context, string) (string, error) { return "", nil }
