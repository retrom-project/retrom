package runtimeclient

import (
	"context"
	"encoding/json"

	"retrom/internal/model"
)

type BiosRequirement struct {
	RequirementKey    string            `json:"requirementKey"`
	CoreID            string            `json:"coreId"`
	ProviderID        string            `json:"providerId"`
	TargetID          string            `json:"targetId"`
	LogicalName       string            `json:"logicalName"`
	Required          bool              `json:"required"`
	Condition         string            `json:"condition"`
	SizeBytes         *int64            `json:"sizeBytes"`
	SHA256            *string           `json:"sha256"`
	MD5               *string           `json:"md5"`
	Delivery          string            `json:"delivery"`
	VirtualPath       *string           `json:"virtualPath"`
	ActivationOptions map[string]string `json:"activationOptions"`
}

func (c *Client) Bios(ctx context.Context,
	provider,
	target string,
	config json.RawMessage,
	files []model.GameFile) ([]BiosRequirement,
	error,
) {
	var result []BiosRequirement
	err := c.Call(ctx,
		"bios-requirements",
		map[string]any{
			"providerId": provider,
			"targetId":   target,
			"config":     config,
			"files":      Files(files),
		},
		&result)
	return result, err
}

func (c *Client) AllBios(_ context.Context) ([]BiosRequirement, error) {
	return c.BiosRequirements, nil
}
