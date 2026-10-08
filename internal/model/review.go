package model

type ReviewReadinessRequest struct {
	GameIDs []string `json:"gameIds"`
}

type ReviewReadinessError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ReviewReadiness struct {
	ID            string                `json:"id"`
	Version       *int64                `json:"version"`
	BIOSSatisfied *bool                 `json:"biosSatisfied"`
	Error         *ReviewReadinessError `json:"error"`
	MissingBIOS   []ReviewMissingBIOS   `json:"missingBios"`
}

type ReviewMissingBIOS struct {
	Key    string `json:"key"`
	Name   string `json:"name"`
	CoreID string `json:"coreId"`
}
