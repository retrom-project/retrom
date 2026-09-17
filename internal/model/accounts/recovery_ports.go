package accounts

import "context"

type RecoveryTarget struct {
	UserID, Username, DisplayName, Role, Status string
	Version                                     int64
}
type RecoveryPlan struct {
	Target                                       RecoveryTarget
	PasswordHash, AuditID, BeforeJSON, AfterJSON string
	ClearTestDefault                             bool
	Now                                          int64
}
type RecoveryReader interface {
	Current(context.Context, string) (RecoveryTarget, bool, error)
}
type RecoveryWriter interface {
	Reset(context.Context, RecoveryPlan) error
}

// RecoveryCommand captures all inputs for an offline admin password recovery.
type RecoveryCommand struct {
	UserID       string
	Version      int64
	PasswordHash string
	AuditID      string
	NowMS        int64
}

type RecoveryScope struct {
	Read  RecoveryReader
	Write RecoveryWriter
}
type RecoveryRepository interface {
	ByUsername(context.Context, string) (RecoveryTarget, bool, error)
	CommitRecovery(context.Context, RecoveryCommand) error
}
