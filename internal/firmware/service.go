package firmware

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/importing"

	"github.com/google/uuid"
)

var ErrInvalid = errors.New("BIOS_INSTALLATION_INVALID")

type InstallRequest struct {
	UploadFileID string `json:"uploadFileId"`
}

type Installation struct {
	InstallationID              string         `json:"installationId"`
	RequirementID               string         `json:"requirementId"`
	Status                      string         `json:"status"`
	Active                      bool           `json:"active"`
	ValidatedRequirementVersion int64          `json:"validatedRequirementVersion"`
	ValidationDetails           map[string]any `json:"validationDetails"`
	CreatedAtMS                 int64          `json:"createdAtMs"`
}

type Service struct {
	database *sql.DB
	blobs    *blobstore.Store
	now      func() time.Time
}

func New(database *sql.DB, now func() time.Time) *Service {
	return &Service{database: database, now: now}
}

func (service *Service) WithBlobStore(blobs *blobstore.Store) *Service {
	service.blobs = blobs
	return service
}

//nolint:funlen,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) Install(
	ctx context.Context,
	requirementID string,
	expectedVersion int64,
	request InstallRequest,
) (Installation, error) {
	prepared, err := service.prepareInstall(ctx, requirementID, expectedVersion, request.UploadFileID)
	if err != nil {
		return Installation{}, err
	}
	transaction, err := service.database.BeginTx(ctx, nil)
	if err != nil {
		return Installation{}, fmt.Errorf("firmware/service: %w", err)
	}
	defer cleanup.Rollback(transaction)
	var sourceKind, logicalName string
	var size sql.NullInt64
	var expectedMD5, expectedSHA1, expectedSHA256 sql.NullString
	var version int64
	if err := transaction.QueryRowContext(ctx, `
SELECT source_kind,
logical_name,
size_bytes,
md5,
sha1,
sha256,
version
FROM bios_requirements
WHERE id=?
AND enabled=1
`, requirementID).Scan(
		&sourceKind,
		&logicalName,
		&size,
		&expectedMD5,
		&expectedSHA1,
		&expectedSHA256,
		&version,
	); err != nil ||
		version != expectedVersion {
		return Installation{}, ErrInvalid
	}
	var uploadID, originalName, blobID, md5Value, sha1Value, sha256Value string
	var sizeValue int64
	if err := transaction.QueryRowContext(ctx, `
SELECT f.upload_session_id,
f.relative_path,
b.id,
b.size_bytes,
b.md5,
b.sha1,
b.sha256
FROM upload_files f
JOIN blobs b ON b.id=f.final_blob_id
WHERE f.id=?
AND f.state='COMPLETE'
`, request.UploadFileID).Scan(
		&uploadID,
		&originalName,
		&blobID,
		&sizeValue,
		&md5Value,
		&sha1Value,
		&sha256Value,
	); err != nil {
		return Installation{}, ErrInvalid
	}
	if sourceKind != prepared.sourceKind || blobID != prepared.blobID || sha256Value != prepared.sha256 {
		return Installation{}, ErrInvalid
	}
	status := "MATCHED"
	details := map[string]any{
		"logicalName":   logicalName,
		"sourceKind":    sourceKind,
		"sizeMatched":   !size.Valid || size.Int64 == sizeValue,
		"md5Matched":    !expectedMD5.Valid || expectedMD5.String == md5Value,
		"sha1Matched":   !expectedSHA1.Valid || expectedSHA1.String == sha1Value,
		"sha256Matched": !expectedSHA256.Valid || expectedSHA256.String == sha256Value,
	}
	if details["sizeMatched"] == false || details["md5Matched"] == false || details["sha1Matched"] == false ||
		details["sha256Matched"] == false {
		status = "HASH_WARNING"
	}
	if sourceKind == "DAT_MACHINE" {
		if err := persistArchiveEntries(
			ctx, transaction, blobID, prepared.archiveEntries, service.now().UnixMilli(),
		); err != nil {
			return Installation{}, err
		}
		status, details, err = validateDATMachineArchive(ctx, transaction, requirementID, prepared.archiveEntries)
		if err != nil {
			return Installation{}, err
		}
	}
	detailsJSON, _ := json.Marshal(details)
	now := service.now().UnixMilli()
	id, _ := uuid.NewV7()
	consumptionID, _ := uuid.NewV7()
	if _, err := transaction.ExecContext(ctx, `
UPDATE bios_installations
SET is_active=0,
version=version+1,
updated_at_ms=?
WHERE requirement_id=?
AND is_active=1
`, now, requirementID); err != nil {
		return Installation{}, fmt.Errorf("%w: deactivate installation: %w", ErrInvalid, err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO bios_installations(id,
requirement_id,
blob_id,
original_filename,
size_bytes,
md5,
sha1,
sha256,
validated_requirement_version,
status,
validation_details_json,
is_active,
version,
created_at_ms,
updated_at_ms) VALUES(?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
?,
1,
1,
?,
?)
`,
		id.String(),
		requirementID,
		blobID,
		originalName,
		sizeValue,
		md5Value,
		sha1Value,
		sha256Value,
		version,
		status,
		string(detailsJSON),
		now,
		now,
	); err != nil {
		return Installation{}, fmt.Errorf("%w: persist installation: %w", ErrInvalid, err)
	}
	if _, err := transaction.ExecContext(ctx, `
INSERT INTO upload_consumptions(id,
upload_session_id,
upload_file_id,
consumer_type,
consumer_id,
created_at_ms) VALUES(?,
?,
?,
'BIOS_INSTALLATION',
?,
?)
`, consumptionID.String(), uploadID, request.UploadFileID, id.String(), now); err != nil {
		return Installation{}, fmt.Errorf("%w: consume upload: %w", ErrInvalid, err)
	}
	if err := transaction.Commit(); err != nil {
		return Installation{}, fmt.Errorf("firmware/service: %w", err)
	}
	return Installation{
		InstallationID:              id.String(),
		RequirementID:               requirementID,
		Status:                      status,
		Active:                      true,
		ValidatedRequirementVersion: version,
		ValidationDetails:           details,
		CreatedAtMS:                 now,
	}, nil
}

type preparedInstall struct {
	sourceKind     string
	blobID         string
	sha256         string
	archiveEntries []importing.ArchiveEntry
}

func (service *Service) prepareInstall(
	ctx context.Context,
	requirementID string,
	expectedVersion int64,
	uploadFileID string,
) (preparedInstall, error) {
	var prepared preparedInstall
	if err := service.database.QueryRowContext(ctx, `
SELECT q.source_kind,
b.id,
b.sha256
FROM bios_requirements q
JOIN upload_files f ON f.id=? AND f.state='COMPLETE'
JOIN blobs b ON b.id=f.final_blob_id
WHERE q.id=?
AND q.enabled=1
AND q.version=?
`, uploadFileID, requirementID, expectedVersion).Scan(
		&prepared.sourceKind,
		&prepared.blobID,
		&prepared.sha256,
	); err != nil {
		return preparedInstall{}, ErrInvalid
	}
	if prepared.sourceKind == "DAT_MACHINE" {
		if service.blobs == nil {
			return preparedInstall{}, ErrInvalid
		}
		scannedEntries, scanErr := importing.ScanZIP(
			ctx,
			service.blobs.Path(prepared.sha256),
			importing.DefaultArchiveLimits(),
		)
		if scanErr != nil {
			return preparedInstall{}, fmt.Errorf("%w: %w", ErrInvalid, scanErr)
		}
		prepared.archiveEntries = scannedEntries
	}
	return prepared, nil
}

type expectedArchiveEntry struct {
	name  string
	size  int64
	crc32 sql.NullString
	sha1  sql.NullString
}

func persistArchiveEntries(
	ctx context.Context,
	transaction *sql.Tx,
	blobID string,
	entries []importing.ArchiveEntry,
	now int64,
) error {
	for _, entry := range entries {
		if _, err := transaction.ExecContext(ctx, `
INSERT OR IGNORE INTO archive_entries(archive_blob_id,
ordinal,
original_relative_path,
normalized_path,
ascii_casefold_path,
archive_format,
compression_profile,
uncompressed_size_bytes,
crc32,
md5,
sha1,
sha256,
materialized_blob_id,
created_at_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,NULL,?)
`, blobID, entry.Ordinal, entry.OriginalPath, entry.NormalizedPath, entry.ASCIICasefoldPath,
			entry.ArchiveFormat, entry.CompressionProfile, entry.Size, entry.CRC32, entry.MD5, entry.SHA1, entry.SHA256,
			now); err != nil {
			return fmt.Errorf("firmware/archive catalog: %w", err)
		}
	}
	return nil
}

// validateDATMachineArchive accepts a historical filename alias only when its
// bytes match the active DAT entry. Exact-name files with different bytes stay
// visible as HASH_WARNING; absent content remains blocking.
func validateDATMachineArchive(
	ctx context.Context,
	transaction *sql.Tx,
	requirementID string,
	actual []importing.ArchiveEntry,
) (string, map[string]any, error) {
	rows, err := transaction.QueryContext(ctx, `
SELECT r.name,
r.size_bytes,
r.crc32,
r.sha1
FROM dat_rom_entries r
JOIN dat_versions d ON d.id=r.dat_version_id
JOIN bios_requirements q ON q.core_artifact_id=d.core_artifact_id
AND q.dat_machine_name=r.machine_name
WHERE q.id=?
AND COALESCE(r.status,'GOOD')!='NODUMP'
AND (r.bios_name IS NULL OR EXISTS(SELECT 1
FROM dat_bios_sets b
WHERE b.dat_version_id=r.dat_version_id
AND b.machine_name=r.machine_name
AND b.bios_name=r.bios_name
AND b.is_default=1))
ORDER BY r.name COLLATE BINARY,r.ordinal
`, requirementID)
	if err != nil {
		return "", nil, fmt.Errorf("firmware/DAT entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	expected := make([]expectedArchiveEntry, 0)
	for rows.Next() {
		var entry expectedArchiveEntry
		if err := rows.Scan(&entry.name, &entry.size, &entry.crc32, &entry.sha1); err != nil {
			return "", nil, fmt.Errorf("firmware/DAT entries: %w", err)
		}
		expected = append(expected, entry)
	}
	if err := rows.Err(); err != nil {
		return "", nil, fmt.Errorf("firmware/DAT entries: %w", err)
	}
	missing := make([]map[string]any, 0)
	mismatched := make([]map[string]any, 0)
	warnings := make([]string, 0)
	used := make(map[int]struct{}, len(actual))
	for _, wanted := range expected {
		exact := findArchiveEntry(actual, used, func(entry importing.ArchiveEntry) bool {
			return strings.EqualFold(entry.NormalizedPath, wanted.name)
		})
		if exact >= 0 && archiveEntryMatches(wanted, actual[exact]) {
			used[exact] = struct{}{}
			continue
		}
		alias := findArchiveEntry(actual, used, func(entry importing.ArchiveEntry) bool {
			return archiveEntryMatches(wanted, entry)
		})
		if alias >= 0 {
			used[alias] = struct{}{}
			warnings = append(warnings, fmt.Sprintf("%s 已按内容识别为 %s", wanted.name, actual[alias].NormalizedPath))
			continue
		}
		if exact >= 0 {
			used[exact] = struct{}{}
			mismatched = append(mismatched, archiveEntryDifference(wanted, actual[exact]))
			continue
		}
		missing = append(missing, map[string]any{"name": wanted.name, "expected": expectedEntryFacts(wanted)})
	}
	details := map[string]any{
		"schemaVersion":     1,
		"missingEntries":    missing,
		"mismatchedEntries": mismatched,
		"warnings":          warnings,
	}
	if len(expected) == 0 || len(missing) > 0 {
		return "MISSING_ENTRY", details, nil
	}
	if len(mismatched) > 0 {
		return "HASH_WARNING", details, nil
	}
	return "MATCHED", details, nil
}

func findArchiveEntry(
	entries []importing.ArchiveEntry,
	used map[int]struct{},
	matches func(importing.ArchiveEntry) bool,
) int {
	for index, entry := range entries {
		if _, exists := used[index]; !exists && matches(entry) {
			return index
		}
	}
	return -1
}

func archiveEntryMatches(expected expectedArchiveEntry, actual importing.ArchiveEntry) bool {
	if actual.Size != expected.size {
		return false
	}
	if expected.sha1.Valid {
		return strings.EqualFold(actual.SHA1, expected.sha1.String)
	}
	return expected.crc32.Valid && strings.EqualFold(actual.CRC32, expected.crc32.String)
}

func expectedEntryFacts(entry expectedArchiveEntry) map[string]any {
	return map[string]any{
		"sizeBytes": entry.size,
		"crc32":     nullableString(entry.crc32),
		"sha1":      nullableString(entry.sha1),
	}
}

func archiveEntryDifference(expected expectedArchiveEntry, actual importing.ArchiveEntry) map[string]any {
	return map[string]any{
		"name":     expected.name,
		"expected": expectedEntryFacts(expected),
		"actual": map[string]any{
			"name": actual.NormalizedPath, "sizeBytes": actual.Size, "crc32": actual.CRC32, "sha1": actual.SHA1,
		},
	}
}

func nullableString(value sql.NullString) any {
	if value.Valid {
		return value.String
	}
	return nil
}
