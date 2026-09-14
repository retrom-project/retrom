package firmware

import (
	"context"

	"retrom/internal/model/payloadrelease"
)

type SupersededInstallation struct {
	ID, RequirementID, BlobID string
	Version                   int64
}

type SupersessionReader interface {
	Current(context.Context, string) (SupersededInstallation, bool, error)
	Consumption(context.Context, string) (string, error)
}

type SupersessionWriter interface {
	Deactivate(context.Context, SupersededInstallation, int64) error
}

type SupersessionScope struct {
	Read    SupersessionReader
	Write   SupersessionWriter
	Payload payloadrelease.SchedulingScope
}
