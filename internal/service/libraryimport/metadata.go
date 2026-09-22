package libraryimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrInvalid         = errors.New("IMPORT_INVALID")
	ErrVersionConflict = errors.New("VERSION_CONFLICT")
)

type (
	ServerMetadata struct {
		Title, Description, Developer, Publisher, Genre string
		Players, ReleaseYear                            *int
	}
	ServerMetadataWarning struct {
		Code  string `json:"code"`
		Field string `json:"field"`
	}
	MetadataDraft struct {
		MetadataJSON string
		Version      int64
	}
	MetadataChange struct {
		ItemID                   string
		Before                   MetadataDraft
		MetadataJSON, SearchText string
		NowMS                    int64
	}
	MetadataScope interface {
		CurrentMetadata(context.Context, string) (MetadataDraft, error)
		SaveMetadata(context.Context, MetadataChange) error
	}
	MetadataRepository interface {
		WithMetadata(context.Context, func(MetadataScope) error) error
	}
	MetadataSeeder struct {
		repository MetadataRepository
		now        func() time.Time
	}
)

func NewMetadataSeeder(repository MetadataRepository, now func() time.Time) *MetadataSeeder {
	return &MetadataSeeder{repository: repository, now: now}
}

func (service *MetadataSeeder) Seed(
	ctx context.Context, itemID string, metadata ServerMetadata, maximumYear int,
) (int64, []ServerMetadataWarning, error) {
	var version int64
	var warnings []ServerMetadataWarning
	err := service.repository.WithMetadata(ctx, func(scope MetadataScope) error {
		var err error
		version, warnings, err = service.SeedInScope(ctx, scope, itemID, metadata, maximumYear)
		return err
	})
	if err != nil {
		return 0, nil, fmt.Errorf("seed server review metadata: %w", err)
	}
	return version, warnings, nil
}

// SeedInScope participates in the caller's transaction. Its result becomes
// durable only when the owner commits the entire handoff.
func (service *MetadataSeeder) SeedInScope(
	ctx context.Context, scope MetadataScope, itemID string, metadata ServerMetadata, maximumYear int,
) (int64, []ServerMetadataWarning, error) {
	if itemID == "" {
		return 0, nil, ErrInvalid
	}
	normalized, warnings, err := NormalizeServerReviewMetadata(metadata, maximumYear)
	if err != nil {
		return 0, nil, err
	}
	before, err := scope.CurrentMetadata(ctx, itemID)
	if err != nil {
		return 0, nil, fmt.Errorf("read server review metadata: %w", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(before.MetadataJSON), &fields); err != nil || fields == nil {
		return 0, nil, ErrInvalid
	}
	if before.Version < 1 {
		return 0, nil, ErrVersionConflict
	}
	encoded, err := encodeMetadata(normalized)
	if err != nil {
		return 0, nil, err
	}
	if before.MetadataJSON == encoded {
		return before.Version, warnings, nil
	}
	if before.Version == math.MaxInt64 {
		return 0, nil, ErrVersionConflict
	}
	change := MetadataChange{
		ItemID: itemID, Before: before, MetadataJSON: encoded,
		SearchText: strings.ToLower(normalized.Title), NowMS: service.now().UnixMilli(),
	}
	if err := scope.SaveMetadata(ctx, change); err != nil {
		return 0, nil, fmt.Errorf("save server review metadata: %w", err)
	}
	return before.Version + 1, warnings, nil
}

func encodeMetadata(metadata ServerMetadata) (string, error) {
	// Match the established metadata object order so repeated handoffs retain
	// the current draft version.
	encoded, err := json.Marshal(struct {
		Description string `json:"description"`
		Developer   string `json:"developer"`
		Genre       string `json:"genre"`
		Players     *int   `json:"players"`
		Publisher   string `json:"publisher"`
		ReleaseYear *int   `json:"releaseYear"`
		Title       string `json:"title"`
	}{
		metadata.Description, metadata.Developer, metadata.Genre, metadata.Players,
		metadata.Publisher, metadata.ReleaseYear, metadata.Title,
	})
	if err != nil {
		return "", fmt.Errorf("encode server review metadata: %w", err)
	}
	return string(encoded), nil
}
