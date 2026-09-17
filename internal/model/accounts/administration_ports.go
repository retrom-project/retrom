package accounts

import "context"

type UserPatch struct {
	Role, Status     *string
	ConfirmAdminRole bool
}
type ManagedUser struct {
	User      AdminUser
	ProfileID string
}
type UserSecurity struct {
	Reason                                        string
	Sessions, CreatedLinks, TargetLinks, Launches bool
}
type UserChange struct {
	Role, Status string
	Security     UserSecurity
}
type AdministrationUpdate struct {
	Before ManagedUser
	Change UserChange
	Now    int64
}
type AdministrationDeletion struct {
	Before           ManagedUser
	Security         UserSecurity
	ClearTestDefault bool
	Now              int64
}
type AccountOperation struct {
	PrincipalID, Operation, Key, Digest string
	Now                                 int64
}
type AccountReplay struct {
	Digest string
	Body   []byte
	Found  bool
}
type AccountReceipt struct {
	Operation AccountOperation
	Status    int
	Body      []byte
	ExpiresAt int64
}
type AccountAudit struct {
	ID, ActorID, Action, ResourceType, ResourceID string
	BeforeJSON, AfterJSON                         *string
	Now                                           int64
}
type AdministrationReader interface {
	Current(context.Context, string, int64) (ManagedUser, bool, error)
	AnotherEnabledAdmin(context.Context, string) (bool, error)
	Replay(context.Context, AccountOperation) (AccountReplay, error)
}
type AdministrationWriter interface {
	Update(context.Context, AdministrationUpdate) error
	Delete(context.Context, AdministrationDeletion) error
	Audit(context.Context, AccountAudit) error
	Remember(context.Context, AccountReceipt) error
}

// UpdateUserCommand captures all inputs for updating a managed user account.
type UpdateUserCommand struct {
	Operation AccountOperation
	TargetID  string
	Version   int64
	Patch     UserPatch
}

// UpdateUserResult carries the outcome of a user update.
type UpdateUserResult struct {
	User     AdminUser
	Replayed bool
}

// DeleteUserCommand captures all inputs for deleting a managed user account.
type DeleteUserCommand struct {
	Operation    AccountOperation
	TargetID     string
	Version      int64
	ActorID      string
	Confirmation string
}

type AdministrationScope struct {
	Read  AdministrationReader
	Write AdministrationWriter
}
type AdministrationRepository interface {
	CommitUpdateUser(context.Context, UpdateUserCommand) (UpdateUserResult, error)
	CommitDeleteUser(context.Context, DeleteUserCommand) (bool, error)
}
