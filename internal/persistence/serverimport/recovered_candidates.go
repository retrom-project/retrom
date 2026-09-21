package serverimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"retrom/internal/cleanup"
	"retrom/internal/firmware"
	"retrom/internal/service/serverimport"
)

func (repository *Recovery) Candidates(ctx context.Context, importID string) ([]serverimport.CandidateEvidence, error) {
	rows, err := repository.database.QueryContext(ctx, `
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
	result := make([]serverimport.CandidateEvidence, 0)
	for rows.Next() {
		var candidate serverimport.CandidateEvidence
		var md5Value, sha1Value, sha256Value, crc32Value, details sql.NullString
		var exactHash, expectedSize, exactName, safeArchive, launchable sql.NullInt64
		var matched, aliased, mismatched, missing, extra sql.NullInt64
		if err := rows.Scan(&candidate.ID, &candidate.RequirementID, &candidate.Facts.RelativePath, &candidate.Facts.Basename,
			&candidate.Association, &candidate.Facts.SizeBytes, &md5Value, &sha1Value, &sha256Value, &crc32Value,
			&candidate.State, &exactHash, &expectedSize, &exactName, &safeArchive, &launchable, &matched,
			&aliased, &mismatched, &missing, &extra, &details); err != nil {
			return nil, fmt.Errorf("scan persisted server import candidate: %w", err)
		}
		candidate.Facts.MD5, candidate.Facts.SHA1 = md5Value.String, sha1Value.String
		candidate.Facts.SHA256, candidate.Facts.CRC32 = sha256Value.String, crc32Value.String
		candidate.Details = map[string]any{}
		if details.Valid {
			if err := json.Unmarshal([]byte(details.String), &candidate.Details); err != nil {
				return nil, fmt.Errorf("decode recovered candidate evidence: %w", err)
			}
		}
		if exactHash.Valid {
			candidate.Static = &firmware.StaticEvaluation{
				Facts:               candidate.Facts,
				ExactHash:           exactHash.Int64 == 1,
				ExpectedSizeMatched: expectedSize.Int64 == 1,
				ExactBasename:       exactName.Int64 == 1,
			}
		}
		if safeArchive.Valid {
			candidate.DAT = &firmware.DATEvaluation{
				Facts:       candidate.Facts,
				SafeArchive: safeArchive.Int64 == 1,
				Launchable:  launchable.Int64 == 1,
				MatchedCount: int(
					matched.Int64,
				),
				AliasedCount: int(
					aliased.Int64,
				),
				MismatchedCount: int(
					mismatched.Int64,
				),
				MissingCount: int(
					missing.Int64,
				),
				ExtraCount: int(
					extra.Int64,
				),
				ExactBasename: exactName.Int64 == 1,
			}
		}
		result = append(result, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recovered candidates: %w", err)
	}
	return result, nil
}
