package gamecontent

import (
	"context"

	"retrom/internal/service/payloadrelease"
)

type RetirementOwnerKind string

const (
	RetirementLaunch  RetirementOwnerKind = "LAUNCH"
	RetirementPlay    RetirementOwnerKind = "PLAY"
	RetirementNetplay RetirementOwnerKind = "NETPLAY"
	RetirementRoom    RetirementOwnerKind = "ROOM"
	RetirementVariant RetirementOwnerKind = "VARIANT"
)

type RetirementReferenceKind string

const (
	RetirementSave              RetirementReferenceKind = "SAVE"
	RetirementLaunchContent     RetirementReferenceKind = "LAUNCH_CONTENT"
	RetirementLaunchExternal    RetirementReferenceKind = "LAUNCH_EXTERNAL"
	RetirementVariantFile       RetirementReferenceKind = "VARIANT_FILE"
	RetirementVariantDependency RetirementReferenceKind = "VARIANT_DEPENDENCY"
)

type RetirementOwner struct {
	Kind       RetirementOwnerKind
	ID, State  string
	Version    int64
	SaveID     *string
	FinishedAt *int64
}
type RetirementChange struct {
	Before                RetirementOwner
	GameID, State, Reason string
	Now                   int64
	FinishedAt            *int64
}
type RetirementReference struct {
	OwnerID, Key, Qualifier string
	BlobID, ExtraBlobID     string
}
type RetirementReader interface {
	Blobs(context.Context, string) ([]string, error)
	Owners(context.Context, string) ([]RetirementOwner, error)
	References(context.Context, string, RetirementReferenceKind, int) ([]RetirementReference, error)
	Consumption(context.Context, string) (string, error)
}
type RetirementWriter interface {
	Change(context.Context, RetirementChange) error
	Remove(context.Context, string, RetirementReferenceKind, []RetirementReference) error
}
type RetirementScope struct {
	Read    RetirementReader
	Write   RetirementWriter
	GC      payloadrelease.GCScope
	Payload payloadrelease.SchedulingScope
}
