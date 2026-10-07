package model

import "encoding/json"

type Run struct {
	ID                string          `json:"id"`
	GameID            string          `json:"gameId"`
	Purpose           string          `json:"purpose"`
	CoreID            string          `json:"coreId"`
	ProviderID        string          `json:"providerId"`
	TargetID          string          `json:"targetId"`
	CoreFingerprint   string          `json:"coreFingerprint"`
	ROMHash           string          `json:"romHash"`
	ProviderModuleURL string          `json:"providerModuleUrl"`
	Envelope          json.RawMessage `json:"envelope"`
	ExpiresAtMs       int64           `json:"expiresAtMs"`
	Extinfo           Extinfo         `json:"extinfo"`
	Save              *Save           `json:"save"`
}
type RunInput struct {
	GameID  string `json:"gameId"`
	Purpose string `json:"purpose"`
	CoreID  string `json:"coreId"`
	SaveID  string `json:"saveId"`
}
