package platforminstance

import (
	"errors"

	"retrom/internal/contentcapability"
)

const applyOperationID = "postAdminPlatformInstanceRecommendationsApply"

var (
	ErrCatalogInvalid     = errors.New("PLATFORM_CATALOG_INVALID")
	ErrDefaultCoreInvalid = errors.New("PLATFORM_DEFAULT_CORE_INVALID")
	ErrIdempotencyReused  = errors.New("IDEMPOTENCY_KEY_REUSED")
	ErrInvalid            = errors.New("INVALID_REQUEST")
	ErrSlugExhausted      = errors.New("platform slug space exhausted")
	ErrNotFound           = errors.New("platform instance not found")
	ErrVersionConflict    = errors.New("platform instance version conflict")
	ErrOrderStale         = errors.New("platform instance order stale")
	ErrNotEmpty           = errors.New("platform instance is not empty")
	ErrImpactStale        = errors.New("platform impact is stale")
	ErrInvalidCore        = errors.New("platform core is invalid")
	ErrDefaultCoreBlocked = errors.New("platform default core is blocked")
)

type State string

const (
	StateActive              State = "ACTIVE"
	StateCustomized          State = "CUSTOMIZED"
	StateCoveredByEquivalent State = "COVERED_BY_EQUIVALENT"
	StateSuppressed          State = "SUPPRESSED"
	StateMissing             State = "MISSING"
)

type Reference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Recommendation struct {
	TemplateKey         string    `json:"templateKey"`
	CatalogOrder        int       `json:"catalogOrder"`
	Name                string    `json:"name"`
	Description         string    `json:"description"`
	Platform            Reference `json:"platform"`
	DefaultCore         Reference `json:"defaultCore"`
	SupportedExtensions []string  `json:"supportedExtensions"`
	State               State     `json:"state"`
	PlatformInstanceID  *string   `json:"platformInstanceId"`
}

type RecommendationSummary struct {
	TotalCount               int `json:"totalCount"`
	ActiveCount              int `json:"activeCount"`
	CustomizedCount          int `json:"customizedCount"`
	CoveredByEquivalentCount int `json:"coveredByEquivalentCount"`
	SuppressedCount          int `json:"suppressedCount"`
	MissingCount             int `json:"missingCount"`
}

type Recommendations struct {
	CatalogVersion int                   `json:"catalogVersion"`
	Summary        RecommendationSummary `json:"summary"`
	Items          []Recommendation      `json:"items"`
}

type Instance struct {
	ID                  string                               `json:"id"`
	PlatformID          string                               `json:"platformId"`
	PlatformName        string                               `json:"platformName"`
	DefaultCoreID       string                               `json:"defaultCoreId"`
	DefaultCoreName     string                               `json:"defaultCoreName"`
	Name                string                               `json:"name"`
	Slug                string                               `json:"slug"`
	Description         string                               `json:"description"`
	SortOrder           int64                                `json:"sortOrder"`
	Enabled             bool                                 `json:"enabled"`
	GameCount           int64                                `json:"gameCount"`
	SupportedExtensions []string                             `json:"supportedExtensions"`
	Version             int64                                `json:"version"`
	CreatedAtMS         int64                                `json:"createdAtMs"`
	UpdatedAtMS         int64                                `json:"updatedAtMs"`
	ContentPolicy       contentcapability.Policy             `json:"-"`
	ImportCapabilities  contentcapability.ImportCapabilities `json:"-"`
}

type ApplySummary struct {
	CreatedCount          int `json:"createdCount"`
	CoveredCount          int `json:"coveredCount"`
	SuppressedCount       int `json:"suppressedCount"`
	RemainingMissingCount int `json:"remainingMissingCount"`
}

type ApplyResult struct {
	CatalogVersion      int              `json:"catalogVersion"`
	CreatedTemplateKeys []string         `json:"createdTemplateKeys"`
	Created             []Instance       `json:"created"`
	Summary             ApplySummary     `json:"summary"`
	Items               []Recommendation `json:"items"`
}

type IdempotentResponse struct {
	Status   int
	Headers  map[string]string
	Body     []byte
	Replayed bool
}

type AuditActor struct {
	Kind      string
	UserID    any
	Label     any
	RequestID string
}

type CreateInput struct {
	PlatformID    string
	DefaultCoreID string
	Name          string
	Description   string
	SortOrder     int64
}
