package cleanupjobs

import "fmt"

type effectError struct {
	code  string
	cause error
}

func (err effectError) Error() string {
	if err.cause == nil {
		return err.code
	}
	return fmt.Sprintf("%s: %v", err.code, err.cause)
}
func (err effectError) Code() string         { return err.code }
func (err effectError) Unwrap() error        { return err.cause }
func Failure(code string, cause error) error { return effectError{code: code, cause: cause} }

var (
	ErrOwnerNotTerminal  = Failure("OWNER_CLEANUP_SCOPE_NOT_TERMINAL", nil)
	ErrDependencyPending = Failure("OWNER_CLEANUP_DEPENDENCY_PENDING", nil)
)
