package serverimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"retrom/internal/blobstore"
	"retrom/internal/cleanup"
	"retrom/internal/firmware"
)

func rankCandidates(values []*evaluatedCandidate) []*evaluatedCandidate {
	eligible := make([]*evaluatedCandidate, 0, len(values))
	for _, value := range values {
		if value.State == "ELIGIBLE" {
			eligible = append(eligible, value)
		}
	}
	sort.Slice(eligible, func(left, right int) bool {
		if eligible[left].Static != nil {
			return firmware.CompareStatic(*eligible[left].Static, *eligible[right].Static) < 0
		}
		return firmware.CompareDAT(*eligible[left].DAT, *eligible[right].DAT) < 0
	})
	return eligible
}

func selectedStatus(candidate *evaluatedCandidate) (string, string) {
	if candidate.Static != nil {
		return candidate.Static.Status, candidate.Static.Method
	}
	return candidate.DAT.Status, candidate.DAT.Method
}

// Catalog snapshot projection intentionally mirrors the frozen import-item schema.
func (service *Service) loadItems(ctx context.Context, importID string) ([]catalogItem, error) {
	rows, err := service.database.QueryContext(ctx, `
SELECT requirement_id,requirement_version,core_id,core_name_snapshot,provider_id,target_id,target_contract_sha256,
source_kind,logical_name,requirement_mode,condition_code,delivery_kind,emulator_path,catalog_digest,
	activation_options_json,source_version,dat_version_id,dat_machine_name,expected_size_bytes,
	expected_md5,expected_sha1,expected_sha256,
active_installation_id_snapshot,active_installation_version_snapshot,active_blob_sha256_snapshot,
active_status_snapshot,active_validated_requirement_version_snapshot,state
FROM server_bios_import_items WHERE server_import_id=? ORDER BY requirement_id COLLATE BINARY`, importID)
	if err != nil {
		return nil, fmt.Errorf("query server import items: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make([]catalogItem, 0)
	for rows.Next() {
		var item catalogItem
		if err := rows.Scan(
			&item.RequirementID, &item.RequirementVersion, &item.CoreID, &item.CoreName,
			&item.ProviderID, &item.TargetID, &item.TargetContractSHA256, &item.SourceKind, &item.LogicalName,
			&item.RequirementMode, &item.ConditionCode, &item.DeliveryKind, &item.EmulatorPath,
			&item.CatalogDigest, &item.ActivationOptionsJSON, &item.SourceVersion, &item.DATVersionID,
			&item.DATMachineName, &item.ExpectedSize, &item.ExpectedMD5, &item.ExpectedSHA1,
			&item.ExpectedSHA256, &item.ActiveInstallationID, &item.ActiveInstallationVersion,
			&item.ActiveBlobSHA256, &item.ActiveStatus, &item.ActiveValidatedVersion, &item.State,
		); err != nil {
			return nil, fmt.Errorf("scan server import item: %w", err)
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate server import items: %w", err)
	}
	return result, nil
}

// The phase probe is the persisted discovery-resume boundary.
func (service *Service) discoveryWasPersisted(ctx context.Context, importID string) (bool, error) {
	var phase sql.NullString
	if err := service.database.QueryRowContext(ctx, `
SELECT phase FROM server_imports WHERE id=?
`, importID).Scan(&phase); err != nil {
		return false, fmt.Errorf("read server import discovery phase: %w", err)
	}
	if !phase.Valid {
		return false, nil
	}
	switch phase.String {
	case "DISCOVERY_COMPLETED", "RANKING", "INSTALLING", "QUEUEING_REVALIDATION":
		return true, nil
	default:
		return false, nil
	}
}

// One row reconstructs the complete candidate evaluation needed for crash recovery.
func (service *Service) loadPersistedCandidates(
	ctx context.Context,
	importID string,
	items []catalogItem,
) (map[string][]*evaluatedCandidate, error) {
	itemByID := make(map[string]catalogItem, len(items))
	for _, item := range items {
		itemByID[item.RequirementID] = item
	}
	rows, err := service.database.QueryContext(ctx, `
SELECT id,requirement_id,relative_path,basename,association_kind,size_bytes,md5,sha1,sha256,crc32,state,
exact_hash,expected_size_match,exact_basename,safe_archive,launchable,matched_count,aliased_count,
mismatched_count,missing_count,extra_count,evaluation_details_json
FROM server_bios_import_candidates WHERE server_import_id=?
ORDER BY requirement_id COLLATE BINARY,COALESCE(rank_ordinal,9223372036854775807),id
`, importID)
	if err != nil {
		return nil, fmt.Errorf("query persisted server import candidates: %w", err)
	}
	defer func() { cleanup.Error("close", rows.Close()) }()
	result := make(map[string][]*evaluatedCandidate)
	datExpected := make(map[string][]firmware.ExpectedDATEntry)
	for rows.Next() {
		var candidate evaluatedCandidate
		var requirementID string
		var md5Value, sha1Value, sha256Value, crc32Value, details sql.NullString
		var exactHash, expectedSize, exactName, safeArchive, launchable sql.NullInt64
		var matched, aliased, mismatched, missing, extra sql.NullInt64
		if err := rows.Scan(&candidate.ID, &requirementID, &candidate.File.RelativePath, &candidate.File.Basename,
			&candidate.Association, &candidate.File.SizeBytes, &md5Value, &sha1Value, &sha256Value, &crc32Value,
			&candidate.State, &exactHash, &expectedSize, &exactName, &safeArchive, &launchable, &matched,
			&aliased, &mismatched, &missing, &extra, &details); err != nil {
			return nil, fmt.Errorf("scan persisted server import candidate: %w", err)
		}
		item, ok := itemByID[requirementID]
		if !ok {
			return nil, ErrCatalogInvalid
		}
		candidate.Item = item
		candidate.File.Name = candidate.File.Basename
		candidate.Metadata = blobstore.Metadata{
			Size: candidate.File.SizeBytes,
			MD5:  nullableString(md5Value), SHA1: nullableString(sha1Value),
			SHA256: nullableString(sha256Value), CRC32: nullableString(crc32Value),
		}
		if candidate.Metadata.SHA256 != "" {
			candidate.Metadata.Path = service.blobs.Path(candidate.Metadata.SHA256)
		}
		candidate.Details = map[string]any{}
		if details.Valid {
			_ = json.Unmarshal([]byte(details.String), &candidate.Details)
		}
		facts := firmware.FileFacts{
			RelativePath: candidate.File.RelativePath, Basename: candidate.File.Basename,
			SizeBytes: candidate.Metadata.Size, MD5: candidate.Metadata.MD5, SHA1: candidate.Metadata.SHA1,
			SHA256: candidate.Metadata.SHA256, CRC32: candidate.Metadata.CRC32,
		}
		switch {
		case item.SourceKind == "STATIC" && exactHash.Valid:
			candidate.Static = &firmware.StaticEvaluation{
				Facts: facts, ExactHash: exactHash.Int64 == 1, ExpectedSizeMatched: expectedSize.Int64 == 1,
				ExactBasename: exactName.Int64 == 1,
			}
			candidate.Static.Status, candidate.Static.Method = staticStatusMethod(*candidate.Static)
		case item.SourceKind == "DAT_MACHINE" && safeArchive.Valid:
			candidate.DAT = &firmware.DATEvaluation{
				Facts: facts, SafeArchive: safeArchive.Int64 == 1, Launchable: launchable.Int64 == 1,
				MatchedCount: int(matched.Int64), AliasedCount: int(aliased.Int64),
				MismatchedCount: int(mismatched.Int64), MissingCount: int(missing.Int64),
				ExtraCount: int(extra.Int64), ExactBasename: exactName.Int64 == 1,
			}
			candidate.DAT.Status, candidate.DAT.Method = datStatusMethod(*candidate.DAT)
			expected, exists := datExpected[requirementID]
			if !exists {
				expected, err = service.expectedDATEntries(ctx, item)
				if err != nil {
					return nil, fmt.Errorf("restore expected DAT entries: %w", err)
				}
				datExpected[requirementID] = expected
			}
			candidate.ExpectedDATEntries = expected
		}
		result[requirementID] = append(result[requirementID], &candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate persisted server import candidates: %w", err)
	}
	return result, nil
}

func nullableString(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}
