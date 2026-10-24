package model

// ContentReplacement is the latest short-lived content maintenance operation for one game.
// It is never stored on Game or in a business table.
type ContentReplacement struct {
	ID          string                    `json:"id"`
	GameID      string                    `json:"gameId"`
	Status      string                    `json:"status"`
	Phase       string                    `json:"phase"`
	CanCancel   bool                      `json:"canCancel"`
	Result      *ContentReplacementResult `json:"result"`
	Error       *ContentReplacementError  `json:"error"`
	CreatedAtMs int64                     `json:"createdAtMs"`
	UpdatedAtMs int64                     `json:"updatedAtMs"`
	ExpiresAtMs int64                     `json:"expiresAtMs"`
}

type ContentReplacementResult struct {
	Version     int64  `json:"version"`
	ContentHash string `json:"contentHash"`
}

type ContentReplacementError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
