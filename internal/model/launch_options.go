package model

import "encoding/json"

// Runtime projects selectable configurations from immutable game content.
type LaunchOptions struct {
	Items      []LaunchOption `json:"items"`
	SelectedID string         `json:"selectedId"`
}

type LaunchOption struct {
	ID            string          `json:"id"`
	Label         string          `json:"label"`
	Configuration json.RawMessage `json:"configuration"`
}
