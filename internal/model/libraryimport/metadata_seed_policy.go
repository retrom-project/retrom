package libraryimport

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// MetadataSeedInput provides all values needed to build a metadata seed plan.
type MetadataSeedInput struct {
	ItemID      string
	Metadata    ServerMetadata
	MaximumYear int
	NowMS       int64
	AuditID     string
	ActorKind   string
	ActorUserID *string
	ActorLabel  *string
}

// MetadataSeedPlan describes metadata changes ready for persistence.
type MetadataSeedPlan struct {
	Changed       bool
	Change        MetadataChange
	ResultVersion int64
}

// BuildMetadataSeed normalizes metadata, compares with current draft, and
// prepares the change plan and warnings. It does not perform any I/O.
func BuildMetadataSeed(
	before MetadataDraft, input MetadataSeedInput,
) (MetadataSeedPlan, []ServerMetadataWarning, error) {
	if input.ItemID == "" {
		return MetadataSeedPlan{}, nil, ErrInvalid
	}
	normalized, warnings, err := NormalizeServerReviewMetadata(input.Metadata, input.MaximumYear)
	if err != nil {
		return MetadataSeedPlan{}, nil, err
	}
	if before.Version < 1 {
		return MetadataSeedPlan{}, nil, ErrVersionConflict
	}
	encoded, err := EncodeServerMetadata(normalized)
	if err != nil {
		return MetadataSeedPlan{}, nil, err
	}
	if before.MetadataJSON == encoded {
		return MetadataSeedPlan{ResultVersion: before.Version}, warnings, nil
	}
	if before.Version == math.MaxInt64 {
		return MetadataSeedPlan{}, nil, ErrVersionConflict
	}
	audit, err := BuildMetadataAudit(before.MetadataJSON, encoded, input.AuditID, input.ActorKind, input.ActorUserID, input.ActorLabel)
	if err != nil {
		return MetadataSeedPlan{}, nil, err
	}
	return MetadataSeedPlan{
		Changed:       true,
		ResultVersion: before.Version + 1,
		Change: MetadataChange{
			ItemID: input.ItemID, Before: before, MetadataJSON: encoded,
			SearchText: strings.ToLower(normalized.Title), Audit: audit, NowMS: input.NowMS,
		},
	}, warnings, nil
}

// EncodeServerMetadata serializes server review metadata with the established field order.
func EncodeServerMetadata(metadata ServerMetadata) (string, error) {
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

var errMetadataAuditObject = fmt.Errorf("server review metadata audit must be an object")

// BuildMetadataAudit creates the audit entry for a metadata change.
func BuildMetadataAudit(beforeJSON, afterJSON, auditID, actorKind string, actorUserID, actorLabel *string) (MetadataAudit, error) {
	beforeEvent, err := metadataEventJSON(beforeJSON)
	if err != nil {
		return MetadataAudit{}, err
	}
	afterEvent, err := metadataEventJSON(afterJSON)
	if err != nil {
		return MetadataAudit{}, err
	}
	return MetadataAudit{
		ID: auditID, ActorKind: actorKind, ActorUserID: actorUserID, ActorLabel: actorLabel,
		BeforeJSON: beforeEvent, AfterJSON: afterEvent,
	}, nil
}

func metadataEventJSON(metadata string) (string, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(metadata), &fields); err != nil {
		return "", fmt.Errorf("decode server review metadata audit: %w", err)
	}
	if fields == nil {
		return "", errMetadataAuditObject
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
