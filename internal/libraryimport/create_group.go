package libraryimport

import (
	"errors"
	"fmt"

	"retrom/internal/blobstore"
)

func (run *creationRun) persistGroup(group *preparedGroup) error {
	record, err := run.newGroupRecord(group)
	if err != nil {
		return err
	}
	if err := run.persistGroupSource(record); err != nil {
		return err
	}
	if err := run.prepareCanonicalPlaylist(record); err != nil {
		return err
	}
	run.countGroupSourceFiles(record)
	duplicate, err := run.discardDuplicateGroup(record)
	if err != nil || duplicate {
		return err
	}
	if err := run.persistGroupValidation(record); err != nil {
		return err
	}
	if err := run.persistReviewDraft(record); err != nil {
		return err
	}
	if err := run.persistMultiDiscEvents(record); err != nil {
		return err
	}
	return run.scheduleMetadata(record)
}

func (run *creationRun) prepareCanonicalPlaylist(record *groupRecord) error {
	if record.group.canonicalPlaylist == nil {
		return nil
	}
	blobID, err := blobstore.EnsureRecord(
		run.ctx, run.transaction, *record.group.canonicalPlaylist, "application/vnd.retrom.m3u", run.now,
	)
	if err != nil {
		return fmt.Errorf("libraryimport/service: %w", err)
	}
	record.group.validationFiles = append(record.group.validationFiles, preparedValidationFile{
		role: "MULTI_DISC_PLAYLIST", logicalName: "playlist.m3u", blobID: blobID, sortOrder: 0,
	})
	return nil
}

func (run *creationRun) countGroupSourceFiles(record *groupRecord) {
	for uploadFileID := range record.uploadFileIDs {
		run.sourceCounts[uploadFileID]++
	}
}

func (run *creationRun) discardDuplicateGroup(record *groupRecord) (bool, error) {
	identityDigest, err := importItemContentIdentity(run.ctx, run.transaction, record.itemID)
	if err != nil && !errors.Is(err, errMultiDiscIncomplete) {
		return false, err
	}
	if identityDigest == "" {
		return false, nil
	}
	games, err := findDuplicateGames(run.ctx, run.transaction, record.itemID, run.plan.target.platformID)
	if err != nil {
		return false, err
	}
	if len(games) == 0 {
		return false, nil
	}
	if err := claimContentIdentity(
		run.ctx, run.transaction, run.plan.target.platformID, identityDigest, run.now,
	); err != nil {
		return false, err
	}
	if err := run.insertDuplicateMatches(record.itemID, identityDigest, games); err != nil {
		return false, err
	}
	_, err = run.transaction.ExecContext(run.ctx, `
UPDATE import_items
SET state='DISCARDED',version=version+1,updated_at_ms=?,completed_at_ms=?
WHERE id=?
`, run.now, run.now, record.itemID)
	if err != nil {
		return false, fmt.Errorf("libraryimport/service: %w", err)
	}
	run.duplicateItems++
	for uploadFileID := range record.uploadFileIDs {
		run.duplicateCounts[uploadFileID]++
	}
	return true, nil
}

func (run *creationRun) insertDuplicateMatches(
	itemID string,
	identityDigest string,
	games []DuplicateGame,
) error {
	for _, game := range games {
		_, err := run.transaction.ExecContext(run.ctx, `
INSERT INTO import_item_duplicate_matches(
  import_item_id,existing_game_id,existing_game_content_revision_id,
  content_identity_digest,detected_stage,created_at_ms
) VALUES(?,?,?,?,'IDENTIFICATION',?)
`, itemID, game.GameID, game.CurrentContentRevisionID, identityDigest, run.now)
		if err != nil {
			return fmt.Errorf("libraryimport/service: %w", err)
		}
	}
	return nil
}
