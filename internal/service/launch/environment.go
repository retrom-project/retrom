package launch

import (
	"context"
	"time"

	model "retrom/internal/model/launch"
)

// ConfigEnvironment holds service-level configuration for launch config
// issuance. Moved from model because it carries func fields.
type ConfigEnvironment struct {
	Now           func() time.Time
	Matches       model.MatchCapability
	SignIsolation func(string) (model.IsolationTicket, error)
	PublicOrigin  string
}

// NetplayCreationEnvironment holds service-level configuration for netplay
// session creation. Moved from model because it carries func fields.
type NetplayCreationEnvironment struct {
	Now            func() time.Time
	NewID          func() (string, error)
	SignCapability func(string) (string, []byte, error)
}

// PreviewEnvironment holds service-level configuration for launch preview
// creation. Moved from model because it carries func fields.
type PreviewEnvironment struct {
	Now            func() time.Time
	NewID          func() (string, error)
	SignCapability func(string) (string, []byte, error)
	SignIsolation  func(string) (model.IsolationTicket, error)
}

// ProductEnvironment holds service-level configuration for product creation.
// Moved from model because it carries func fields.
type ProductEnvironment struct {
	Now              func() time.Time
	NewID            func() (string, error)
	SignCapability   func(string) (string, []byte, error)
	SignIsolation    func(string) (model.IsolationTicket, error)
	ResumeValidation func(context.Context, string)
}

// ValidationEnvironment holds service-level configuration for validation
// jobs. Moved from model because it carries func fields.
type ValidationEnvironment struct {
	Now   func() time.Time
	NewID func() (string, error)
}

// ScreenshotEnvironment holds service-level configuration for screenshot
// operations. Moved from model because it carries func fields.
type ScreenshotEnvironment struct {
	Now     func() time.Time
	Matches model.MatchCapability
	NewID   func() (string, error)
}

// ValidationWorkerEnvironment holds service-level configuration for
// validation workers. Moved from model because it carries func fields.
type ValidationWorkerEnvironment struct {
	Now       func() time.Time
	NewID     func() (string, error)
	NewTicker func(time.Duration) model.ValidationTicker
}
