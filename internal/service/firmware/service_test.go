package firmware

import (
	"context"
	"errors"
	"testing"
	"time"

	model "retrom/internal/model/firmware"

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
				model.InstallRequest{UploadFileID: "file"})
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
	comparisons, missing, mismatched, warnings := firmware.CompareArchiveEntries(expected, actual)
	for _, strict := range []bool{false, true} {
		details := map[string]any{
			"schemaVersion": 1, "missingEntries": missing,
			"mismatchedEntries": mismatched, "warnings": warnings,
		}
		var status string
		switch {
		case strict && (len(missing) > 0 || len(mismatched) > 0 || len(warnings) > 0):
			status = "INVALID"
		case len(comparisons) == 0 || len(expected) == 0 || len(missing) > 0:
			status = "MISSING_ENTRY"
		case len(mismatched) > 0:
			status = "HASH_WARNING"
		default:
			status = "MATCHED"
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

func (memory *installMemory) LoadInstallFacts(_ context.Context, _ string, expectedVersion int64, _ string) (model.InstallFacts, error) {
	req := memory.initial
	if !req.Enabled || req.Version != expectedVersion {
		return model.InstallFacts{}, model.ErrInvalid
	}
	up := memory.upload
	if up.State != "COMPLETE" {
		return model.InstallFacts{}, model.ErrInvalid
	}
	return model.InstallFacts{
		SourceKind: req.SourceKind,
		FileKind:   req.FileKind,
		BlobID:     up.BlobID,
		SHA256:     up.SHA256,
	}, nil
}

func (memory *installMemory) LoadArchiveInspection(context.Context, string) (model.ArchiveInspection, error) {
	return model.ArchiveInspection{}, nil
}

func (memory *installMemory) CommitBrowserInstall(_ context.Context, cmd model.BrowserInstallCommand) (model.Installation, error) {
	memory.insideWrite = true
	defer func() { memory.insideWrite = false }()
	if err := memory.validateBrowserInstall(cmd); err != nil {
		return model.Installation{}, err
	}
	memory.retired = true
	id := "generated-id"
	memory.created = &model.InstallationWrite{
		ID: id, RequirementID: cmd.RequirementID, BlobID: memory.currentUpload.BlobID,
		Filename: memory.currentUpload.RelativePath,
		MD5:      memory.currentUpload.MD5, SHA1: memory.currentUpload.SHA1,
		SHA256: memory.currentUpload.SHA256, Size: memory.currentUpload.Size,
		Status: "MATCHED", RequirementVersion: memory.current.Version,
		AtMS: cmd.NowMS, SourceKind: "BROWSER_UPLOAD",
	}
	memory.consumption = &model.Consumption{
		ID: "consumption-id", UploadID: memory.currentUpload.SessionID,
		FileID: memory.currentUpload.ID, InstallationID: id, AtMS: cmd.NowMS,
	}
	status := installStatus(memory.current, memory.currentUpload)
	return model.Installation{
		InstallationID: id, RequirementID: cmd.RequirementID, Status: status, Active: true,
		ValidatedRequirementVersion: memory.current.Version, CreatedAtMS: cmd.NowMS,
	}, nil
}

func (memory *installMemory) validateBrowserInstall(cmd model.BrowserInstallCommand) error {
	if memory.current.SourceKind != cmd.PreparedSourceKind ||
		memory.current.FileKind != cmd.PreparedFileKind ||
		memory.currentUpload.BlobID != cmd.PreparedBlobID ||
		memory.currentUpload.SHA256 != cmd.PreparedSHA256 {
		return model.ErrInvalid
	}
	if !memory.current.Enabled || memory.current.Version != cmd.Version {
		return model.ErrInvalid
	}
	if memory.currentUpload.State != "COMPLETE" {
		return model.ErrInvalid
	}
	return nil
}

func installStatus(req model.Requirement, upload model.Upload) string {
	if (req.SHA256 != nil && *req.SHA256 != upload.SHA256) ||
		(req.Size != nil && *req.Size != upload.Size) ||
		(req.MD5 != nil && *req.MD5 != upload.MD5) ||
		(req.SHA1 != nil && *req.SHA1 != upload.SHA1) {
		return "HASH_WARNING"
	}
	return "MATCHED"
}

func (memory *installMemory) CommitServerInstall(context.Context, model.ServerInstallCommand) (model.ServerInstallResult, error) {
	return model.ServerInstallResult{}, nil
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

func (memory *installMemory) Current(context.Context, string) (model.SupersededInstallation, bool, error) {
	return model.SupersededInstallation{ID: "old", RequirementID: "requirement", BlobID: "blob", Version: 1}, true, nil
}
func (memory *installMemory) Consumption(context.Context, string) (string, error) { return "", nil }
