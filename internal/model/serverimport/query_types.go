package serverimport

import "errors"

var (
	ErrNotFound = errors.New("SERVER_IMPORT_NOT_FOUND")
	ErrQuery    = errors.New("SERVER_IMPORT_QUERY_INVALID")
)

type SummaryCursor struct {
	CreatedAtMS int64
	ID          string
}

type ItemCursor struct{ Core, Name, ID string }

type CandidateCursor struct {
	Rank int64
	ID   string
}

type ListQuery struct {
	State  string
	Before *SummaryCursor
	Limit  int
}

type ItemQuery struct {
	ImportID, Text, Outcome, Method string
	After                           *ItemCursor
	Limit                           int
}

type CandidateQuery struct {
	ImportID, RequirementID string
	After                   *CandidateCursor
	Limit                   int
}

type Counts struct {
	CatalogItems   int64 `json:"catalogItems"`
	Candidates     int64 `json:"candidates"`
	EvaluatedItems int64 `json:"evaluatedItems"`
	Imported       int64 `json:"imported"`
	Matched        int64 `json:"matched"`
	Warnings       int64 `json:"warnings"`
	NotFound       int64 `json:"notFound"`
	Skipped        int64 `json:"skipped"`
	Conflicts      int64 `json:"conflicts"`
	Failed         int64 `json:"failed"`
	Cancelled      int64 `json:"cancelled"`
}

type Summary struct {
	ID                 string    `json:"id"`
	Kind               string    `json:"kind"`
	Root               RootRef   `json:"root"`
	SourceRelativePath string    `json:"sourceRelativePath"`
	ReplaceIfBetter    bool      `json:"replaceIfBetter"`
	State              string    `json:"state"`
	Phase              *string   `json:"phase"`
	Counts             Counts    `json:"counts"`
	JobID              string    `json:"jobId"`
	CreatedBy          CreatedBy `json:"createdBy"`
	LastErrorCode      *string   `json:"lastErrorCode"`
	Version            int64     `json:"version"`
	CreatedAtMS        int64     `json:"createdAtMs"`
	UpdatedAtMS        int64     `json:"updatedAtMs"`
	CompletedAtMS      *int64    `json:"completedAtMs"`
}

type RootRef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}
type CreatedBy struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type Item struct {
	RequirementID              string         `json:"requirementId"`
	CoreID                     string         `json:"coreId"`
	CoreName                   string         `json:"coreName"`
	ProviderID                 string         `json:"providerId"`
	TargetID                   string         `json:"targetId"`
	LogicalName                string         `json:"logicalName"`
	RequirementMode            string         `json:"requirementMode"`
	SourceKind                 string         `json:"sourceKind"`
	State                      string         `json:"state"`
	CandidateCount             int64          `json:"candidateCount"`
	MatchMethod                *string        `json:"matchMethod"`
	OutcomeCode                *string        `json:"outcomeCode"`
	SelectedRelativePath       *string        `json:"selectedRelativePath"`
	PreviousInstallationStatus *string        `json:"previousInstallationStatus"`
	NewInstallationStatus      *string        `json:"newInstallationStatus"`
	Replaced                   bool           `json:"replaced"`
	SelectionDetails           map[string]any `json:"selectionDetails,omitempty"`
}

type Candidate struct {
	ID                string         `json:"id"`
	RelativePath      string         `json:"relativePath"`
	Basename          string         `json:"basename"`
	AssociationKind   string         `json:"associationKind"`
	SizeBytes         int64          `json:"sizeBytes"`
	MD5               *string        `json:"md5"`
	SHA1              *string        `json:"sha1"`
	SHA256            *string        `json:"sha256"`
	CRC32             *string        `json:"crc32"`
	State             string         `json:"state"`
	RankOrdinal       *int64         `json:"rankOrdinal"`
	NotSelectedReason *string        `json:"notSelectedReason"`
	EvaluationDetails map[string]any `json:"evaluationDetails,omitempty"`
}
