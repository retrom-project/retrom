package serverimport

import (
	"context"
	"fmt"

	"retrom/internal/model/serverimport"
)

type candidateDatabaseValues struct {
	md5, sha1, sha256, crc32                  any
	staticExact, staticSize, safe, launchable any
	matched, aliased, mismatched, missing     any
	extra, rank, notSelected                  any
}

func (records discoveryRecords) candidate(
	ctx context.Context,
	unit serverimport.Work,
	value serverimport.CandidateWrite,
	now int64,
) error {
	candidate := value.Evidence
	values := candidateValues(value)
	_, err := records.executor.ExecContext(ctx, `
INSERT INTO server_bios_import_candidates(id,server_import_id,requirement_id,relative_path,basename,
association_kind,size_bytes,md5,sha1,sha256,crc32,state,exact_hash,expected_size_match,exact_basename,
safe_archive,launchable,matched_count,aliased_count,mismatched_count,missing_count,extra_count,rank_ordinal,
not_selected_reason,evaluation_details_json,created_at_ms,updated_at_ms,evaluated_at_ms)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
`, candidate.ID, unit.ImportID, candidate.RequirementID, candidate.Facts.RelativePath, candidate.Facts.Basename,
		candidate.Association, candidate.Facts.SizeBytes, values.md5, values.sha1, values.sha256, values.crc32,
		candidate.State, values.staticExact, values.staticSize,
		value.ExactBasename, values.safe, values.launchable,
		values.matched, values.aliased, values.mismatched, values.missing, values.extra, values.rank,
		values.notSelected, string(value.Details), now, now, now)
	if err != nil {
		return fmt.Errorf("persist server import candidate: %w", err)
	}
	return nil
}

func candidateValues(value serverimport.CandidateWrite) candidateDatabaseValues {
	candidate := value.Evidence
	var values candidateDatabaseValues
	if candidate.Facts.SHA256 != "" {
		values.md5, values.sha1 = candidate.Facts.MD5, candidate.Facts.SHA1
		values.sha256, values.crc32 = candidate.Facts.SHA256, candidate.Facts.CRC32
	}
	if candidate.Static != nil {
		values.staticExact = candidate.Static.ExactHash
		values.staticSize = candidate.Static.ExpectedSizeMatched
	}
	if candidate.DAT != nil {
		values.safe, values.launchable = candidate.DAT.SafeArchive, candidate.DAT.Launchable
		values.matched, values.aliased = candidate.DAT.MatchedCount, candidate.DAT.AliasedCount
		values.mismatched, values.missing = candidate.DAT.MismatchedCount, candidate.DAT.MissingCount
		values.extra = candidate.DAT.ExtraCount
	}
	values.rank = value.Rank
	values.notSelected = value.NotSelected
	return values
}
