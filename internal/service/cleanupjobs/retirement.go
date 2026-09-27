package cleanupjobs

import (
	"context"
	"errors"
)

var ErrRetirementSnapshotChanged = errors.New("PAYLOAD_RETIREMENT_SNAPSHOT_CHANGED")

type RetirementFile struct{ OwnerID, Name, FileRecord string }

type BIOSRetirement struct {
	Found          bool
	ID, FileRecord string
	Version        int64
	Files          []RetirementFile
}

type RetirementPlay struct {
	ID      string
	Version int64
}

type LaunchRetirement struct {
	Found                               bool
	ID, State                           string
	Version, DueMS, BootstrapMS, HardMS int64
	Idle, Finished                      WorkTime
	Content, External                   []RetirementFile
	Plays                               []RetirementPlay
}

type LaunchRetirementEnd struct {
	Before           LaunchRetirement
	Expire           bool
	State, PlayState string
	NowMS            int64
}

type RetirementCompletion struct {
	ID           string
	DueMS, NowMS int64
}

type BIOSRetirementReader interface {
	BIOS(context.Context, int) (BIOSRetirement, error)
}
type LaunchRetirementReader interface {
	Launch(context.Context, int64, int) (LaunchRetirement, error)
}

type BIOSRetirementWriter interface {
	FenceBIOS(context.Context, BIOSRetirement) error
	ReleaseBIOSFiles(context.Context, BIOSRetirement) error
	CompleteBIOS(context.Context, BIOSRetirement, int64) error
}

type LaunchRetirementWriter interface {
	FenceLaunch(context.Context, LaunchRetirement) error
	TerminateLaunch(context.Context, LaunchRetirementEnd) error
	ReleaseLaunchFiles(context.Context, LaunchRetirement) error
	CompleteLaunch(context.Context, RetirementCompletion) error
}

type BIOSRetirementScope struct {
	Read BIOSRetirementReader
	BIOS BIOSRetirementWriter
}
type LaunchRetirementScope struct {
	Read   LaunchRetirementReader
	Launch LaunchRetirementWriter
}
type BIOSRetirementRepository interface {
	WithBIOSRetirement(context.Context, func(BIOSRetirementScope) error) error
}
type LaunchRetirementRepository interface {
	WithLaunchRetirement(context.Context, func(LaunchRetirementScope) error) error
}
