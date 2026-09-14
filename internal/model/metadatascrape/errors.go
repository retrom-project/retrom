package metadatascrape

import "errors"

var (
	ErrArcadeSnapshotInvalid = errors.New("ARCADE_EVIDENCE_SNAPSHOT_INVALID")
	ErrGameVersionConflict   = errors.New("GAME_VERSION_CONFLICT")
	ErrProviderInvalid       = errors.New("METADATA_PROVIDER_INVALID")
	ErrReviewVersionConflict = errors.New("REVIEW_VERSION_CONFLICT")
)
