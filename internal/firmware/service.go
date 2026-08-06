package firmware

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"retrom/internal/cleanup"

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
	now      func() time.Time
}

func New(database *sql.DB, now func() time.Time) *Service {
	return &Service{database: database, now: now}
}

//nolint:funlen,gocyclo // Contract branches stay contiguous for a single auditable decision.
func (service *Service) Install(
	ctx context.Context,
	requirementID string,
	expectedVersion int64,
	request InstallRequest,
) (Installation, error) {
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
	// DAT machine slots require the archive-entry catalog. An empty catalog is
	// never accepted as matched, even though the uploaded bytes remain auditable.
	if sourceKind == "DAT_MACHINE" {
		var required, present int64
		if err := transaction.QueryRowContext(ctx, `
SELECT count(*),
sum(CASE WHEN EXISTS(SELECT 1
FROM archive_entries a
WHERE a.archive_blob_id=?
AND a.normalized_path=r.name) THEN 1 ELSE 0 END)
FROM dat_rom_entries r
JOIN dat_versions d ON d.id=r.dat_version_id
JOIN bios_requirements q ON q.core_artifact_id=d.core_artifact_id
AND q.dat_machine_name=r.machine_name
WHERE q.id=?
AND COALESCE(r.status,
'GOOD')!='NODUMP'
`, blobID, requirementID).Scan(&required, &present); err != nil {
			return Installation{}, fmt.Errorf("firmware/service: %w", err)
		}
		details["requiredEntryCount"], details["presentEntryCount"] = required, present
		if required == 0 || present != required {
			status = "MISSING_ENTRY"
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
