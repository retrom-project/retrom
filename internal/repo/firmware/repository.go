package firmware

import (
	"context"
	"database/sql"
	"fmt"

	firmwarecap "retrom/internal/capability/content/firmware"
	"retrom/internal/model/firmware"
	"retrom/internal/repo/dbexec"
)

type (
	Repository          struct{ database *sql.DB }
	requirementRecords  struct{ executor dbexec.Executor }
	uploadRecords       struct{ executor dbexec.Executor }
	installationRecords struct{ executor dbexec.Executor }
	archiveRecords      struct{ executor dbexec.Executor }
	writes              struct{ transaction *sql.Tx }
)

func New(database *sql.DB) *Repository { return &Repository{database: database} }
func readScope(executor dbexec.Executor) firmware.ReadScope {
	return firmware.ReadScope{
		Requirements: requirementRecords{executor}, Uploads: uploadRecords{executor},
		Installations: installationRecords{executor}, Archives: archiveRecords{executor},
	}
}

func (repository *Repository) LoadInstallFacts(
	ctx context.Context, requirementID string,
	expectedVersion int64, fileID string,
) (firmware.InstallFacts, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return firmware.InstallFacts{}, fmt.Errorf("begin BIOS snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := readScope(tx)
	requirement, found, err := scope.Requirements.Get(ctx, requirementID)
	if err != nil {
		return firmware.InstallFacts{}, fmt.Errorf("read BIOS requirement: %w", err)
	}
	if !found || !requirement.Enabled || requirement.Version != expectedVersion {
		return firmware.InstallFacts{}, firmware.ErrInvalid
	}
	upload, found, err := scope.Uploads.Get(ctx, fileID)
	if err != nil {
		return firmware.InstallFacts{}, fmt.Errorf("read BIOS upload: %w", err)
	}
	if !found || upload.State != "COMPLETE" {
		return firmware.InstallFacts{}, firmware.ErrInvalid
	}
	if err := tx.Commit(); err != nil {
		return firmware.InstallFacts{}, fmt.Errorf("commit BIOS snapshot: %w", err)
	}
	return firmware.InstallFacts{
		SourceKind: requirement.SourceKind,
		FileKind:   requirement.FileKind,
		BlobID:     upload.BlobID,
		SHA256:     upload.SHA256,
	}, nil
}

func (repository *Repository) LoadArchiveInspection(
	ctx context.Context, requirementID string,
) (firmware.ArchiveInspection, error) {
	tx, err := repository.database.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return firmware.ArchiveInspection{}, fmt.Errorf("begin BIOS snapshot: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := readScope(tx)
	requirement, found, err := scope.Requirements.Get(ctx, requirementID)
	if err != nil {
		return firmware.ArchiveInspection{}, fmt.Errorf("read BIOS inspection requirement: %w", err)
	}
	if !found || !requirement.Enabled || requirement.FileKind != "ARCHIVE" {
		return firmware.ArchiveInspection{}, firmware.ErrArchiveFactsNotFound
	}
	active, found, err := scope.Installations.Active(ctx, requirementID)
	if err != nil {
		return firmware.ArchiveInspection{}, fmt.Errorf("read active BIOS inspection: %w", err)
	}
	if !found {
		return firmware.ArchiveInspection{}, firmware.ErrArchiveFactsNotFound
	}
	expected, err := expectedArchiveFacts(ctx, scope.Requirements, requirement)
	if err != nil {
		return firmware.ArchiveInspection{}, err
	}
	actual, err := scope.Archives.Entries(ctx, active.BlobID)
	if err != nil {
		return firmware.ArchiveInspection{}, fmt.Errorf("read BIOS archive inspection: %w", err)
	}
	comparisons, _, _, _ := firmwarecap.CompareArchiveEntries(expected, actual)
	if err := tx.Commit(); err != nil {
		return firmware.ArchiveInspection{}, fmt.Errorf("commit BIOS snapshot: %w", err)
	}
	return firmware.ArchiveInspection{
		RequirementID: requirementID, LogicalName: requirement.LogicalName,
		InstallationID: active.ID, InstallationStatus: active.Status, Entries: comparisons,
	}, nil
}

func expectedArchiveFacts(
	ctx context.Context,
	records firmware.RequirementRecords,
	requirement firmware.Requirement,
) ([]firmwarecap.ExpectedDATEntry, error) {
	if requirement.ArchiveMembersJSON != nil {
		entries, err := firmwarecap.StaticArchiveExpectations(*requirement.ArchiveMembersJSON)
		if err != nil {
			return nil, fmt.Errorf("decode BIOS archive requirements: %w", err)
		}
		return entries, nil
	}
	entries, err := records.DATEntries(ctx, requirement.ID)
	if err != nil {
		return nil, fmt.Errorf("read BIOS DAT entries: %w", err)
	}
	return entries, nil
}

func (repository *Repository) writeScope(tx *sql.Tx) firmware.WriteScope {
	bound := writes{transaction: tx}
	return firmware.WriteScope{
		ReadScope: readScope(tx), Archives: bound, Installations: bound,
		Retirements: BindSupersession(tx), Server: bound, Blobs: bound,
	}
}

func (repository *Repository) CommitBrowserInstall(
	ctx context.Context,
	cmd firmware.BrowserInstallCommand,
) (firmware.Installation, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return firmware.Installation{}, fmt.Errorf("begin BIOS write: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := repository.writeScope(tx)
	result, err := browserInstall(ctx, scope, cmd)
	if err != nil {
		return firmware.Installation{}, err
	}
	if err := tx.Commit(); err != nil {
		return firmware.Installation{}, fmt.Errorf("commit BIOS write: %w", err)
	}
	return result, nil
}

func (repository *Repository) CommitServerInstall(
	ctx context.Context,
	cmd firmware.ServerInstallCommand,
) (firmware.ServerInstallResult, error) {
	tx, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return firmware.ServerInstallResult{}, fmt.Errorf("begin BIOS write: %w", err)
	}
	defer dbexec.Rollback(tx)
	scope := repository.writeScope(tx)
	result, err := serverInstall(ctx, scope, cmd)
	if err != nil {
		return firmware.ServerInstallResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return firmware.ServerInstallResult{}, fmt.Errorf("commit BIOS write: %w", err)
	}
	return result, nil
}

func changed(result sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("write BIOS record: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("count BIOS write: %w", err)
	}
	if count != 1 {
		return firmware.ErrInvalid
	}
	return nil
}
