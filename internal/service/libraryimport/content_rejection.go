package libraryimport

import "retrom/internal/content/diagnostic"

// ContentRejectedError is an intentional content decision, not an infrastructure
// failure. Callers persist the facts without repeating preparation or parsing text.
type ContentRejectedError struct{ Rejection diagnostic.Rejection }

func (err *ContentRejectedError) Error() string { return err.Rejection.Code }

func rejectedPrimaryContent(plan PreparedImport, primary []string) error {
	for _, name := range primary {
		for _, disposition := range plan.Dispositions {
			if disposition.File.Path != name || disposition.Disposition != "REJECTED" {
				continue
			}
			if disposition.Rejection != nil {
				return &ContentRejectedError{Rejection: *disposition.Rejection}
			}
			return &ContentRejectedError{Rejection: diagnostic.Rejection{Code: disposition.Reason, RelativePath: name}}
		}
	}
	return nil
}
