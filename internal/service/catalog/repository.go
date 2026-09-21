package catalog

import (
	"context"

	"retrom/internal/contentcapability"
)

type Repository interface {
	Platforms(context.Context) ([]PlatformRow, error)
	RuntimeTargets(context.Context) ([]RuntimeTarget, error)
	PlatformInstances(context.Context, PlatformInstanceQuery) ([]PlatformInstance, error)
}

type PlatformRow struct {
	ID, Name             string
	SortOrder            int
	Enabled              bool
	CoreID, CoreName     *string
	CoreEnabled          *bool
	ProviderID, TargetID *string
}

type RuntimeTarget struct {
	ProviderID, ProviderVersion string
	ProviderAPIVersion          int
	BundleSHA256                string
	TargetID, DisplayName       string
	CoreID, CoreName            string
	LaunchPolicy                string
}

type PlatformInstanceQuery struct {
	PlatformID *string
	Enabled    *bool
}

type PlatformInstance struct {
	ID, PlatformID, PlatformName   string
	DefaultCoreID, DefaultCoreName string
	Name, Slug, Description        string
	SortOrder                      int
	Enabled                        bool
	Version, UpdatedAtMS           int64
	GameCount                      int64
	ContentPolicy                  contentcapability.Policy
}
