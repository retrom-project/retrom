package launch

import (
	"context"
	"time"

	launchmodel "retrom/internal/model/launch"
)

type ProductEnvironment struct {
	Now            func() time.Time
	NewID          func() (string, error)
	SignCapability func(string) (string, []byte, error)
	SignIsolation  func(string) (launchmodel.IsolationTicket, error)
	// ResumeValidation dispatches work after the creation transaction commits.
	ResumeValidation func(context.Context, string)
}

type PreviewEnvironment struct {
	Now            func() time.Time
	NewID          func() (string, error)
	SignCapability func(string) (string, []byte, error)
	SignIsolation  func(string) (launchmodel.IsolationTicket, error)
}

type ConfigEnvironment struct {
	Now           func() time.Time
	Matches       launchmodel.MatchCapability
	SignIsolation func(string) (launchmodel.IsolationTicket, error)
	PublicOrigin  string
}

type ScreenshotEnvironment struct {
	Now     func() time.Time
	Matches launchmodel.MatchCapability
	NewID   func() (string, error)
}

type ValidationEnvironment struct {
	Now   func() time.Time
	NewID func() (string, error)
}

type ValidationWorkerEnvironment struct {
	Now       func() time.Time
	NewID     func() (string, error)
	NewTicker func(time.Duration) launchmodel.ValidationTicker
}

type NetplayCreationEnvironment struct {
	Now            func() time.Time
	NewID          func() (string, error)
	SignCapability func(string) (string, []byte, error)
}
