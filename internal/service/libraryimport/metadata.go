package libraryimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	model "retrom/internal/model/libraryimport"
	"strings"
	"time"

	"github.com/google/uuid"

	"retrom/internal/capability/security/authn"
)

var errMetadataObject = errors.New("server review metadata audit must be an object")

type MetadataSeeder struct {
	repository model.MetadataRepository
	now        func() time.Time
}

func NewMetadataSeeder(repository model.MetadataRepository, now func() time.Time) *MetadataSeeder {
	return &MetadataSeeder{repository: repository, now: now}
}

func (service *MetadataSeeder) Seed(
	ctx context.Context, itemID string, metadata model.ServerMetadata, maximumYear int,
) (int64, []model.ServerMetadataWarning, error) {
	var version int64
	var warnings []model.ServerMetadataWarning
	err := service.repository.WithMetadata(ctx, func(scope model.MetadataScope) error {
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
	ctx context.Context, scope model.MetadataScope, itemID string, metadata model.ServerMetadata, maximumYear int,
) (int64, []model.ServerMetadataWarning, error) {
	if itemID == "" {
		return 0, nil, model.ErrInvalid
	}
	normalized, warnings, err := NormalizeServerReviewMetadata(metadata, maximumYear)
	if err != nil {
		return 0, nil, err
	}
	before, err := scope.CurrentMetadata(ctx, itemID)
	if err != nil {
		return 0, nil, fmt.Errorf("read server review metadata: %w", err)
	}
	if before.Version < 1 {
		return 0, nil, model.ErrVersionConflict
	}
	encoded, err := encodeMetadata(normalized)
	if err != nil {
		return 0, nil, err
	}
	if before.MetadataJSON == encoded {
		return before.Version, warnings, nil
	}
	if before.Version == math.MaxInt64 {
		return 0, nil, model.ErrVersionConflict
	}
	audit, err := metadataAudit(ctx, before.MetadataJSON, encoded)
	if err != nil {
		return 0, nil, err
	}
	change := model.MetadataChange{
		ItemID: itemID, Before: before, MetadataJSON: encoded,
		SearchText: strings.ToLower(normalized.Title), Audit: audit, NowMS: service.now().UnixMilli(),
	}
	if err := scope.SaveMetadata(ctx, change); err != nil {
		return 0, nil, fmt.Errorf("save server review metadata: %w", err)
	}
	return before.Version + 1, warnings, nil
}

func encodeMetadata(metadata model.ServerMetadata) (string, error) {
	// Match the established metadata object order so repeated handoffs retain
	// the current draft version and do not append duplicate audit events.
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

func metadataAudit(ctx context.Context, before, after string) (model.MetadataAudit, error) {
	beforeJSON, err := metadataEvent(before)
	if err != nil {
		return model.MetadataAudit{}, err
	}
	afterJSON, err := metadataEvent(after)
	if err != nil {
		return model.MetadataAudit{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return model.MetadataAudit{}, fmt.Errorf("create server review event ID: %w", err)
	}
	audit := model.MetadataAudit{ID: id.String(), ActorKind: "SYSTEM", BeforeJSON: beforeJSON, AfterJSON: afterJSON}
	label := "release-setup"
	audit.ActorLabel = &label
	if principal, ok := authn.PrincipalFromContext(ctx); ok && principal.UserID != "" {
		audit.ActorKind = "USER"
		audit.ActorUserID = &principal.UserID
		audit.ActorLabel = nil
	}
	return audit, nil
}

func metadataEvent(metadata string) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(metadata), &fields); err != nil {
		return "", fmt.Errorf("decode server review metadata audit: %w", err)
	}
	if fields == nil {
		return "", errMetadataObject
	}
	encoded, err := json.Marshal(struct {
		Metadata      json.RawMessage `json:"metadata"`
		SchemaVersion int             `json:"schemaVersion"`
	}{json.RawMessage(metadata), 2})
	if err != nil {
		return "", fmt.Errorf("encode server review metadata audit: %w", err)
	}
	return string(encoded), nil
}
