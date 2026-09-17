package serverimport

import model "retrom/internal/model/serverimport"

// AutomaticRetryAt delegates to the model-layer pure function.
func AutomaticRetryAt(
	attempt, maximum, terminalItems, deadline, now int64,
) (int64, bool) {
	return model.AutomaticRetryAt(
		attempt, maximum, terminalItems, deadline, now,
	)
}
