package model

type ReviewReadinessRequest struct {
	CoreID  string   `json:"coreId,omitempty"`
	GameIDs []string `json:"gameIds"`
}

type ApproveReviewRequest struct {
	Version int64  `json:"version"`
	CoreID  string `json:"coreId,omitempty"`
}

type ReviewReadinessError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ReviewReadiness struct {
	ID                string                   `json:"id"`
	Version           *int64                   `json:"version"`
	BIOSSatisfied     *bool                    `json:"biosSatisfied"`
	Error             *ReviewReadinessError    `json:"error"`
	MissingBIOS       []ReviewMissingBIOS      `json:"missingBios"`
	CRCWarnings       []ReviewCRCWarning       `json:"crcWarnings"`
	MissingROMMembers []ReviewMissingROMMember `json:"missingRomMembers"`
}

type ReviewMissingROMMember struct {
	CoreID    string `json:"coreId"`
	ROMSet    string `json:"romSet"`
	Role      string `json:"role"`
	Member    string `json:"member"`
	SizeBytes int64  `json:"sizeBytes"`
	Reason    string `json:"reason"`
}

type ReviewCRCWarning struct {
	Role          string `json:"role"`
	File          string `json:"file"`
	Member        string `json:"member"`
	ExpectedCRC32 string `json:"expectedCRC32"`
	ActualCRC32   string `json:"actualCRC32"`
}

type ReviewMissingBIOS struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	CoreID string `json:"coreId"`
}
