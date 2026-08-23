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
	"retrom/internal/payloadrelease"

	"github.com/google/uuid"
)

var (
	ErrInvalid              = errors.New("BIOS_INSTALLATION_INVALID")
	ErrArchiveFactsNotFound = errors.New("BIOS_ARCHIVE_FACTS_NOT_FOUND")
)

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

type ArchiveEntryFacts struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	CRC32     string `json:"crc32,omitempty"`
}

type ArchiveEntryComparison struct {
	Status   string             `json:"status"`
	Expected *ArchiveEntryFacts `json:"expected"`
	Actual   *ArchiveEntryFacts `json:"actual"`
}

type ArchiveInspection struct {
	RequirementID      string                   `json:"requirementId"`
	LogicalName        string                   `json:"logicalName"`
	InstallationID     string                   `json:"installationId"`
	InstallationStatus string                   `json:"installationStatus"`
	Entries            []ArchiveEntryComparison `json:"entries"`
}

type Service struct {
	database *sql.DB
	blobs    *blobstore.Store
	releases *payloadrelease.Service
	now      func() time.Time
}

func New(database *sql.DB, now func() time.Time) *Service {
	return &Service{database: database, now: now}
}

func (service *Service) WithBlobStore(blobs *blobstore.Store) *Service {
	service.blobs = blobs
	return service
}

func (service *Service) WithPayloadRelease(releases *payloadrelease.Service) *Service {
	service.releases = releases
	return service
}

// Contract branches stay contiguous for a single auditable decision.
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
	snapshot, err := validateInstallSnapshot(ctx, transaction, requirementID, expectedVersion, request, prepared)
	if err != nil {
		return Installation{}, err
	}
	status, details, err := service.evaluateInstall(ctx, transaction, requirementID, prepared, snapshot)
	if err != nil {
		return Installation{}, err
	}
	return persistInstallation(
		ctx, transaction, requirementID, request.UploadFileID, snapshot, status, details, service.now(),
		service.releases,
	)
}

type installSnapshot struct {
	sourceKind, logicalName                string
	uploadID, originalName, blobID         string
	md5, sha1, sha256                      string
	size, version                          int64
	expectedSize                           sql.NullInt64
	expectedMD5, expectedSHA1, expectedSHA sql.NullString
}

func validateInstallSnapshot(
	ctx context.Context,
	transaction *sql.Tx,
	requirementID string,
	expectedVersion int64,
	request InstallRequest,
	prepared preparedInstall,
) (installSnapshot, error) {
	var snapshot installSnapshot
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
		&snapshot.sourceKind,
		&snapshot.logicalName,
		&snapshot.expectedSize,
		&snapshot.expectedMD5,
		&snapshot.expectedSHA1,
		&snapshot.expectedSHA,
		&snapshot.version,
	); err != nil ||
		snapshot.version != expectedVersion {
		return installSnapshot{}, ErrInvalid
	}
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
		&snapshot.uploadID,
		&snapshot.originalName,
		&snapshot.blobID,
		&snapshot.size,
		&snapshot.md5,
		&snapshot.sha1,
		&snapshot.sha256,
	); err != nil {
		return installSnapshot{}, ErrInvalid
	}
	if snapshot.sourceKind != prepared.sourceKind || snapshot.blobID != prepared.blobID ||
		snapshot.sha256 != prepared.sha256 {
		return installSnapshot{}, ErrInvalid
	}
	return snapshot, nil
}

func (service *Service) evaluateInstall(
	ctx context.Context,
	transaction *sql.Tx,
	requirementID string,
	prepared preparedInstall,
	snapshot installSnapshot,
) (string, map[string]any, error) {
	status := "MATCHED"
	details := map[string]any{
		"logicalName":   snapshot.logicalName,
		"sourceKind":    snapshot.sourceKind,
		"sizeMatched":   !snapshot.expectedSize.Valid || snapshot.expectedSize.Int64 == snapshot.size,
		"md5Matched":    !snapshot.expectedMD5.Valid || snapshot.expectedMD5.String == snapshot.md5,
		"sha1Matched":   !snapshot.expectedSHA1.Valid || snapshot.expectedSHA1.String == snapshot.sha1,
		"sha256Matched": !snapshot.expectedSHA.Valid || snapshot.expectedSHA.String == snapshot.sha256,
	}
	if details["sizeMatched"] == false || details["md5Matched"] == false || details["sha1Matched"] == false ||
		details["sha256Matched"] == false {
		status = "HASH_WARNING"
	}
	if snapshot.sourceKind == "DAT_MACHINE" {
		if err := persistArchiveEntries(
			ctx, transaction, snapshot.blobID, prepared.archiveEntries, service.now().UnixMilli(),
		); err != nil {
			return "", nil, err
		}
		var err error
		status, details, err = validateDATMachineArchive(ctx, transaction, requirementID, prepared.archiveEntries)
		if err != nil {
			return "", nil, err
		}
	}
	return status, details, nil
}

func persistInstallation(
	ctx context.Context,
	transaction *sql.Tx,
	requirementID, uploadFileID string,
	snapshot installSnapshot,
	status string,
	details map[string]any,
	nowTime time.Time,
	releases *payloadrelease.Service,
) (Installation, error) {
	detailsJSON, _ := json.Marshal(details)
	now := nowTime.UnixMilli()
	id, _ := uuid.NewV7()
	consumptionID, _ := uuid.NewV7()
	retired, err := payloadrelease.RetireSupersededBIOS(ctx, transaction, requirementID, now)
	if err != nil {
		return Installation{}, fmt.Errorf("%w: retire installation: %w", ErrInvalid, err)
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
		snapshot.blobID,
		snapshot.originalName,
		snapshot.size,
		snapshot.md5,
		snapshot.sha1,
		snapshot.sha256,
		snapshot.version,
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
`, consumptionID.String(), snapshot.uploadID, uploadFileID, id.String(), now); err != nil {
		return Installation{}, fmt.Errorf("%w: consume upload: %w", ErrInvalid, err)
	}
	if releases != nil {
		if err := releases.StageCandidates(ctx, transaction, retired.BlobIDs); err != nil {
			return Installation{}, fmt.Errorf("%w: stage retired installation: %w", ErrInvalid, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return Installation{}, fmt.Errorf("firmware/service: %w", err)
	}
	if releases != nil {
		releases.Signal()
	}
	return Installation{
		InstallationID:              id.String(),
		RequirementID:               requirementID,
		Status:                      status,
		Active:                      true,
		ValidatedRequirementVersion: snapshot.version,
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

type archiveQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
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

// validateDATMachineArchive accepts a catalog filename alias only when its
// bytes match the active DAT entry. Exact-name files with different bytes stay
// visible as HASH_WARNING; absent content remains blocking.
func validateDATMachineArchive(
	ctx context.Context,
	transaction *sql.Tx,
	requirementID string,
	actual []importing.ArchiveEntry,
) (string, map[string]any, error) {
	expected, err := loadExpectedArchiveEntries(ctx, transaction, requirementID)
	if err != nil {
		return "", nil, err
	}
	comparisons, missing, mismatched, warnings := compareArchiveEntries(expected, actual)
	details := map[string]any{
		"schemaVersion":     1,
		"missingEntries":    missing,
		"mismatchedEntries": mismatched,
		"warnings":          warnings,
	}
	if len(comparisons) == 0 || len(expected) == 0 || len(missing) > 0 {
		return "MISSING_ENTRY", details, nil
	}
	if len(mismatched) > 0 {
		return "HASH_WARNING", details, nil
	}
	return "MATCHED", details, nil
}

func loadExpectedArchiveEntries(
	ctx context.Context,
	queryer archiveQueryer,
	requirementID string,
) ([]expectedArchiveEntry, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT r.name,
r.size_bytes,
r.crc32,
r.sha1
FROM dat_rom_entries r
JOIN dat_versions d ON d.id=r.dat_version_id
JOIN bios_requirements q ON q.core_artifact_id=d.core_artifact_id
AND q.dat_machine_name=r.machine_name
AND q.source_version=r.dat_version_id
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
		return nil, fmt.Errorf("firmware/DAT entries: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	expected := make([]expectedArchiveEntry, 0)
	for rows.Next() {
		var entry expectedArchiveEntry
		if err := rows.Scan(&entry.name, &entry.size, &entry.crc32, &entry.sha1); err != nil {
			return nil, fmt.Errorf("firmware/DAT entries: %w", err)
		}
		expected = append(expected, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("firmware/DAT entries: %w", err)
	}
	return expected, nil
}

func compareArchiveEntries(
	expected []expectedArchiveEntry,
	actual []importing.ArchiveEntry,
) ([]ArchiveEntryComparison, []map[string]any, []map[string]any, []string) {
	comparisons := make([]ArchiveEntryComparison, 0, len(expected)+len(actual))
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
			comparisons = append(comparisons, archiveComparison("MATCHED", wanted, &actual[exact]))
			continue
		}
		alias := findArchiveEntry(actual, used, func(entry importing.ArchiveEntry) bool {
			return archiveEntryMatches(wanted, entry)
		})
		if alias >= 0 {
			used[alias] = struct{}{}
			warnings = append(warnings, fmt.Sprintf("%s 已按内容识别为 %s", wanted.name, actual[alias].NormalizedPath))
			comparisons = append(comparisons, archiveComparison("ALIASED", wanted, &actual[alias]))
			continue
		}
		if exact >= 0 {
			used[exact] = struct{}{}
			mismatched = append(mismatched, archiveEntryDifference(wanted, actual[exact]))
			comparisons = append(comparisons, archiveComparison("MISMATCHED", wanted, &actual[exact]))
			continue
		}
		missing = append(missing, map[string]any{"name": wanted.name, "expected": expectedEntryFacts(wanted)})
		comparisons = append(comparisons, archiveComparison("MISSING", wanted, nil))
	}
	for index := range actual {
		if _, exists := used[index]; exists {
			continue
		}
		comparisons = append(comparisons, ArchiveEntryComparison{
			Status: "EXTRA",
			Actual: actualEntryFacts(actual[index]),
		})
	}
	return comparisons, missing, mismatched, warnings
}

func archiveComparison(
	status string,
	expected expectedArchiveEntry,
	actual *importing.ArchiveEntry,
) ArchiveEntryComparison {
	comparison := ArchiveEntryComparison{Status: status, Expected: expectedArchiveEntryFacts(expected)}
	if actual != nil {
		comparison.Actual = actualEntryFacts(*actual)
	}
	return comparison
}

func expectedArchiveEntryFacts(entry expectedArchiveEntry) *ArchiveEntryFacts {
	crc32Value := ""
	if entry.crc32.Valid {
		crc32Value = entry.crc32.String
	}
	return &ArchiveEntryFacts{Name: entry.name, SizeBytes: entry.size, CRC32: crc32Value}
}

func actualEntryFacts(entry importing.ArchiveEntry) *ArchiveEntryFacts {
	return &ArchiveEntryFacts{Name: entry.NormalizedPath, SizeBytes: entry.Size, CRC32: entry.CRC32}
}

// InspectArchive compares an active DAT-backed BIOS installation with the
// exact DAT version captured by its requirement. It reads the persisted safe
// archive catalog and never reopens user content on an HTTP request.
func (service *Service) InspectArchive(ctx context.Context, requirementID string) (ArchiveInspection, error) {
	var inspection ArchiveInspection
	var sourceKind, blobID string
	if err := service.database.QueryRowContext(ctx, `
SELECT q.id,
q.logical_name,
q.source_kind,
i.id,
i.status,
i.blob_id
FROM bios_requirements q
JOIN bios_installations i ON i.requirement_id=q.id
AND i.is_active=1
WHERE q.id=?
AND q.enabled=1
`, requirementID).Scan(
		&inspection.RequirementID,
		&inspection.LogicalName,
		&sourceKind,
		&inspection.InstallationID,
		&inspection.InstallationStatus,
		&blobID,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ArchiveInspection{}, ErrArchiveFactsNotFound
		}
		return ArchiveInspection{}, fmt.Errorf("firmware/archive inspection: %w", err)
	}
	if sourceKind != "DAT_MACHINE" {
		return ArchiveInspection{}, ErrArchiveFactsNotFound
	}
	expected, err := loadExpectedArchiveEntries(ctx, service.database, requirementID)
	if err != nil {
		return ArchiveInspection{}, err
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT ordinal,
original_relative_path,
normalized_path,
ascii_casefold_path,
archive_format,
compression_profile,
uncompressed_size_bytes,
crc32,
md5,
sha1,
sha256
FROM archive_entries
WHERE archive_blob_id=?
ORDER BY ordinal
`, blobID)
	if err != nil {
		return ArchiveInspection{}, fmt.Errorf("firmware/archive inspection: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	actual := make([]importing.ArchiveEntry, 0)
	for rows.Next() {
		var entry importing.ArchiveEntry
		if err := rows.Scan(
			&entry.Ordinal,
			&entry.OriginalPath,
			&entry.NormalizedPath,
			&entry.ASCIICasefoldPath,
			&entry.ArchiveFormat,
			&entry.CompressionProfile,
			&entry.Size,
			&entry.CRC32,
			&entry.MD5,
			&entry.SHA1,
			&entry.SHA256,
		); err != nil {
			return ArchiveInspection{}, fmt.Errorf("firmware/archive inspection: %w", err)
		}
		actual = append(actual, entry)
	}
	if err := rows.Err(); err != nil {
		return ArchiveInspection{}, fmt.Errorf("firmware/archive inspection: %w", err)
	}
	inspection.Entries, _, _, _ = compareArchiveEntries(expected, actual)
	return inspection, nil
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
