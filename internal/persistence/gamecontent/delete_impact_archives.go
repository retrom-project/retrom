package gamecontent

import (
	"context"
	"fmt"
	"math"
	"sort"

	"retrom/internal/cleanup"
	dbapi "retrom/internal/database"
	application "retrom/internal/service/gamecontent"
)

// expandImpactArchives projects only the derived references that this deletion
// actually removes. A shared archive continues to protect all of its members.
func expandImpactArchives(
	ctx context.Context,
	executor dbapi.Executor,
	roots []application.ImpactBlob,
) ([]application.ImpactBlob, error) {
	blobs := make(map[string]application.ImpactBlob, len(roots))
	pending := make([]string, 0, len(roots))
	for _, root := range roots {
		blobs[root.ID] = root
		pending = append(pending, root.ID)
	}
	expanded := make(map[string]bool)
	for len(pending) > 0 {
		id := pending[0]
		pending = pending[1:]
		blob := blobs[id]
		if blob.GameReferences > blob.ProtectiveReferences {
			return nil, fmt.Errorf("%w: inconsistent archive references for %s", application.ErrImpactInvalid, id)
		}
		if expanded[id] || blob.GameReferences == 0 || blob.GameReferences != blob.ProtectiveReferences {
			continue
		}
		expanded[id] = true
		members, err := impactArchiveMembers(ctx, executor, id)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			previous, found := blobs[member.ID]
			if found {
				if previous.GameReferences > math.MaxInt64-member.GameReferences {
					return nil, fmt.Errorf("%w: archive count overflow", application.ErrImpactInvalid)
				}
				member.GameReferences += previous.GameReferences
			}
			blobs[member.ID] = member
			pending = append(pending, member.ID)
		}
	}
	result := make([]application.ImpactBlob, 0, len(blobs))
	for _, blob := range blobs {
		result = append(result, blob)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

func impactArchiveMembers(ctx context.Context, executor dbapi.Executor, id string) ([]application.ImpactBlob, error) {
	rows, err := executor.QueryContext(ctx, `SELECT blob.id,blob.size_bytes,blob.ref_count,count(*)
 FROM archive_entries entry JOIN blobs blob ON blob.id=entry.materialized_blob_id
 WHERE entry.archive_blob_id=? AND entry.materialized_blob_id<>entry.archive_blob_id
 GROUP BY blob.id,blob.size_bytes,blob.ref_count`, id)
	if err != nil {
		return nil, fmt.Errorf("read impact archive members: %w", err)
	}
	defer func() { cleanup.Error("close impact archive members", rows.Close()) }()
	var result []application.ImpactBlob
	for rows.Next() {
		var blob application.ImpactBlob
		if err := rows.Scan(&blob.ID, &blob.SizeBytes, &blob.ProtectiveReferences, &blob.GameReferences); err != nil {
			return nil, fmt.Errorf("decode impact archive member: %w", err)
		}
		result = append(result, blob)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate impact archive members: %w", err)
	}
	return result, nil
}
