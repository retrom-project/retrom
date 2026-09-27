package libraryimport

import "time"

// DraftValidator refreshes validation within the transaction supplied by the
// review command. It never captures an Importer or a transaction at construction.
type DraftValidator struct{ now func() time.Time }

func NewDraftValidator(now func() time.Time) *DraftValidator {
	if now == nil {
		now = time.Now
	}
	return &DraftValidator{now: now}
}
