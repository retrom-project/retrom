package saves

import (
	"encoding/json"
	"fmt"
)

type ManualResult struct {
	ResourceKind     string
	SaveStateID      string
	PreviewID        string
	CheckpointFormat string
	ScreenshotURL    *string
	CreatedAtMS      int64
	Name             string
	DiscIndex        *int
	Version          int64
	ActiveDurationMS int64
}

func (result ManualResult) MarshalJSON() ([]byte, error) {
	if result.ResourceKind == "REVIEW_PREVIEW_CHECKPOINT" {
		contents, err := json.Marshal(struct {
			ResourceKind     string `json:"resourceKind"`
			PreviewID        string `json:"previewId"`
			CheckpointFormat string `json:"checkpointFormat"`
			CreatedAtMS      int64  `json:"createdAtMs"`
		}{
			result.ResourceKind, result.PreviewID, result.CheckpointFormat,
			result.CreatedAtMS,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal review checkpoint result: %w", err)
		}
		return contents, nil
	}
	contents, err := json.Marshal(struct {
		ResourceKind     string  `json:"resourceKind"`
		SaveStateID      string  `json:"saveStateId"`
		CheckpointFormat string  `json:"checkpointFormat"`
		ScreenshotURL    *string `json:"screenshotUrl"`
		CreatedAtMS      int64   `json:"createdAtMs"`
	}{
		result.ResourceKind, result.SaveStateID, result.CheckpointFormat,
		result.ScreenshotURL, result.CreatedAtMS,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal save checkpoint result: %w", err)
	}
	return contents, nil
}

func (result *ManualResult) UnmarshalJSON(contents []byte) error {
	var wire struct {
		ResourceKind     string  `json:"resourceKind"`
		SaveStateID      string  `json:"saveStateId"`
		PreviewID        string  `json:"previewId"`
		CheckpointFormat string  `json:"checkpointFormat"`
		ScreenshotURL    *string `json:"screenshotUrl"`
		CreatedAtMS      int64   `json:"createdAtMs"`
	}
	if err := json.Unmarshal(contents, &wire); err != nil {
		return fmt.Errorf("unmarshal checkpoint result: %w", err)
	}
	*result = ManualResult{
		ResourceKind: wire.ResourceKind, SaveStateID: wire.SaveStateID, PreviewID: wire.PreviewID,
		CheckpointFormat: wire.CheckpointFormat,
		ScreenshotURL:    wire.ScreenshotURL,
		CreatedAtMS:      wire.CreatedAtMS,
	}
	return nil
}
