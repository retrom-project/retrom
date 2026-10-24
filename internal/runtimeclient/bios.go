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
	Members           []BiosMember      `json:"members,omitempty"`
	// ResourceRole is the Launch resource role of a SEEKABLE_BLOB requirement; the host does not interpret it.
	ResourceRole string `json:"resourceRole,omitempty"`
	// ArchiveRoot lists directories the installed archive must hold at its root; the runtime verifies them.
	ArchiveRoot []string `json:"archiveRoot,omitempty"`
}

// Verifiable reports whether the runtime can check an installed file beyond its presence.
func (r BiosRequirement) Verifiable() bool {
	return len(r.Members) > 0 || len(r.ArchiveRoot) > 0
}

type BiosMember struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"sizeBytes"`
	CRC32     string `json:"crc32"`
	SHA1      string `json:"sha1"`
	Required  bool   `json:"required"`
}

func (c *Client) Bios(ctx context.Context,
	provider,
	target string,
	config json.RawMessage,
	files []model.ContentFile) ([]BiosRequirement,
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

// VerifyBios inspects installed bytes against the runtime's member requirements.
func (c *Client) VerifyBios(ctx context.Context, requirement BiosRequirement, location string,
) (model.BiosVerification, error) {
	var result model.BiosVerification
	err := c.Call(ctx, "bios-verify", map[string]any{"requirement": requirement, "path": location}, &result)
	return result, err
}
