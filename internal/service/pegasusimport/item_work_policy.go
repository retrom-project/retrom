package pegasusimport

import model "retrom/internal/model/pegasusimport"

func validItemVersion(version int64) bool             { return model.ValidItemVersion(version) }
func validItemOutcome(outcome model.ItemOutcome) bool { return model.ValidItemOutcome(outcome) }
