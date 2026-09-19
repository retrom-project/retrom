package emulationstationimport

import model "retrom/internal/model/emulationstationimport"

func validateImportExecution(before model.LeaseSnapshot, unit model.Execution, now int64) error {
	return model.ValidateImportExecution(before, unit, now)
}
func validItemVersion(version int64) bool { return model.ValidItemVersion(version) }
func workingItemState(state string) bool  { return model.WorkingItemState(state) }
func validItemOutcome(outcome model.ItemOutcome) bool {
	return model.ValidItemOutcome(outcome)
}
