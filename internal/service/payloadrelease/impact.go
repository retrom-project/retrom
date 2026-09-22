package payloadrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
)

type GameImpact struct {
	ImpactDigest       string   `json:"impactDigest"`
	RegisteredBytes    string   `json:"registeredBytes"`
	ExclusiveBytes     string   `json:"exclusiveBytes"`
	SharedBytes        string   `json:"sharedBytes"`
	BlobCount          int64    `json:"blobCount"`
	SaveStateCount     int64    `json:"saveStateCount"`
	AssetCount         int64    `json:"assetCount"`
	ContentFileCount   int64    `json:"contentFileCount"`
	ActiveLaunchCount  int64    `json:"activeLaunchCount"`
	ActiveNetplayCount int64    `json:"activeNetplayCount"`
	SourceKinds        []string `json:"sourceKinds"`
}

type impactCanonical struct {
	RegisteredBytes    string   `json:"registeredBytes"`
	ExclusiveBytes     string   `json:"exclusiveBytes"`
	SharedBytes        string   `json:"sharedBytes"`
	BlobCount          int64    `json:"blobCount"`
	SaveStateCount     int64    `json:"saveStateCount"`
	AssetCount         int64    `json:"assetCount"`
	ContentFileCount   int64    `json:"contentFileCount"`
	ActiveLaunchCount  int64    `json:"activeLaunchCount"`
	ActiveNetplayCount int64    `json:"activeNetplayCount"`
	SourceKinds        []string `json:"sourceKinds"`
}

// GameDeleteAuditImpact intentionally omits ImpactDigest. The digest protects
// the delete precondition, but is not payload lifecycle audit evidence.
func GameDeleteAuditImpact(impact GameImpact) map[string]any {
	return map[string]any{
		"registeredBytes": impact.RegisteredBytes, "exclusiveBytes": impact.ExclusiveBytes,
		"sharedBytes": impact.SharedBytes, "blobCount": impact.BlobCount,
		"saveStateCount": impact.SaveStateCount, "assetCount": impact.AssetCount,
		"contentFileCount": impact.ContentFileCount, "activeLaunchCount": impact.ActiveLaunchCount,
		"activeNetplayCount": impact.ActiveNetplayCount,
		"sourceKinds":        impact.SourceKinds,
	}
}

var ErrImpactInvalid = errors.New("PAYLOAD_RELEASE_IMPACT_INVALID")

type ImpactBlob struct {
	ID                                              string
	SizeBytes, ProtectiveReferences, GameReferences int64
}

type ImpactCounts struct {
	SaveStates, Assets, ContentFiles, ActiveLaunches, ActiveNetplay int64
}

type ImpactSnapshot struct {
	GameID      string
	Blobs       []ImpactBlob
	Counts      ImpactCounts
	SourceKinds []string
}

type ImpactReader interface {
	ReadImpact(context.Context, string) (ImpactSnapshot, error)
}

type ImpactQueries struct{ repository ImpactReader }

func NewImpactQueries(repository ImpactReader) *ImpactQueries {
	return &ImpactQueries{repository: repository}
}

func (queries *ImpactQueries) Game(ctx context.Context, gameID string) (GameImpact, error) {
	source, err := queries.repository.ReadImpact(ctx, gameID)
	if err != nil {
		return GameImpact{}, fmt.Errorf("read game deletion impact: %w", err)
	}
	if source.GameID != gameID || !validImpactCounts(source.Counts) {
		return GameImpact{}, ErrImpactInvalid
	}
	registered, exclusive, count, err := impactTotals(source.Blobs)
	if err != nil {
		return GameImpact{}, err
	}
	result := GameImpact{
		RegisteredBytes: strconv.FormatInt(registered, 10), ExclusiveBytes: strconv.FormatInt(exclusive, 10),
		SharedBytes: strconv.FormatInt(registered-exclusive, 10), BlobCount: count,
		SaveStateCount: source.Counts.SaveStates, AssetCount: source.Counts.Assets,
		ContentFileCount:  source.Counts.ContentFiles,
		ActiveLaunchCount: source.Counts.ActiveLaunches, ActiveNetplayCount: source.Counts.ActiveNetplay,
		SourceKinds: NormalizeImpactSourceKinds(source.SourceKinds),
	}
	canonical := impactCanonical{
		RegisteredBytes: result.RegisteredBytes, ExclusiveBytes: result.ExclusiveBytes, SharedBytes: result.SharedBytes,
		BlobCount: result.BlobCount, SaveStateCount: result.SaveStateCount, AssetCount: result.AssetCount,
		ContentFileCount: result.ContentFileCount, ActiveLaunchCount: result.ActiveLaunchCount,
		ActiveNetplayCount: result.ActiveNetplayCount,
		SourceKinds:        result.SourceKinds,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return GameImpact{}, fmt.Errorf("encode game deletion impact: %w", err)
	}
	digest := sha256.Sum256(encoded)
	result.ImpactDigest = hex.EncodeToString(digest[:])
	return result, nil
}

func impactTotals(blobs []ImpactBlob) (int64, int64, int64, error) {
	seen := make(map[string]ImpactBlob, len(blobs))
	var registered, exclusive int64
	for _, blob := range blobs {
		if blob.ID == "" || blob.SizeBytes < 0 || blob.ProtectiveReferences < 0 || blob.GameReferences < 0 {
			return 0, 0, 0, ErrImpactInvalid
		}
		if before, found := seen[blob.ID]; found {
			if before != blob {
				return 0, 0, 0, ErrImpactInvalid
			}
			continue
		}
		seen[blob.ID] = blob
		if registered > math.MaxInt64-blob.SizeBytes {
			return 0, 0, 0, ErrImpactInvalid
		}
		registered += blob.SizeBytes
		if blob.ProtectiveReferences <= blob.GameReferences {
			exclusive += blob.SizeBytes
		}
	}
	return registered, exclusive, int64(len(seen)), nil
}

func validImpactCounts(counts ImpactCounts) bool {
	for _, count := range []int64{
		counts.SaveStates, counts.Assets, counts.ContentFiles, counts.ActiveLaunches,
		counts.ActiveNetplay,
	} {
		if count < 0 {
			return false
		}
	}
	return true
}

func NormalizeImpactSourceKinds(values []string) []string {
	seen := map[string]struct{}{}
	for _, value := range values {
		if normalized, ok := normalizedImpactSourceKind(value); ok {
			seen[normalized] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizedImpactSourceKind(source string) (string, bool) {
	switch source {
	case "IMPORT_RECEIVE":
		return "SERVER_SCAN", true
	case "ADMIN_REPLACE":
		return "ADMIN_REPLACE", true
	case "IMPORT_REVIEW":
		return "USER_UPLOAD", true
	default:
		return "", false
	}
}
