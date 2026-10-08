package model

import "encoding/json"

type Extinfo struct {
	CoreID           string          `json:"coreId"`
	ProviderID       string          `json:"providerId"`
	TargetID         string          `json:"targetId"`
	CoreFingerprint  string          `json:"coreFingerprint"`
	ROMHash          string          `json:"romHash"`
	CheckpointFormat string          `json:"checkpointFormat"`
	Content          json.RawMessage `json:"content"`
	RuntimeOptions   json.RawMessage `json:"runtimeOptions"`
}
type Save struct {
	ID            string  `json:"id"`
	Game          Game    `json:"game"`
	Kind          string  `json:"kind"`
	Name          string  `json:"name"`
	Slot          *string `json:"slot"`
	Version       int64   `json:"version"`
	SizeBytes     int64   `json:"sizeBytes"`
	CreatedAtMs   int64   `json:"createdAtMs"`
	UpdatedAtMs   int64   `json:"updatedAtMs"`
	ScreenshotURL *string `json:"screenshotUrl"`
	Restorable    bool    `json:"restorable"`
	RestoreReason string  `json:"restoreReason"`
	Extinfo       Extinfo `json:"extinfo"`
	StorageKey    string  `json:"-"`
	ScreenshotKey string  `json:"-"`
	PayloadHash   string  `json:"-"`
	UserID        string  `json:"-"`
	LastCommitID  string  `json:"-"`
}
type SaveInput struct {
	CommitID string  `json:"commitId"`
	RunID    string  `json:"runId"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"`
	Slot     *string `json:"slot"`
	Extinfo  Extinfo `json:"extinfo"`
	Version  int64   `json:"version"`
}
type (
	HomeSummary struct {
		GameCount int64 `json:"gameCount"`
		SaveCount int64 `json:"saveCount"`
	}
	Recent struct {
		Game           Game  `json:"game"`
		LastPlayedAtMs int64 `json:"lastPlayedAtMs"`
	}
	Folder struct {
		ID        string `json:"id"`
		Name      string `json:"name"`
		GameCount int64  `json:"gameCount"`
		Version   int64  `json:"version"`
	}
	Home struct {
		Summary     HomeSummary `json:"summary"`
		Recent      []Recent    `json:"recent"`
		Saves       []Save      `json:"saves"`
		Favorites   []Game      `json:"favorites"`
		Directories []Directory `json:"directories"`
	}
)

type SavePage struct {
	Page[Save]
	GameCount int64 `json:"gameCount"`
}
