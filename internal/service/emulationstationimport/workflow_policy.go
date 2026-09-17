package emulationstationimport

import (
	"fmt"

	model "retrom/internal/model/emulationstationimport"

	"github.com/google/uuid"
)

func newRetryPlan(before model.RetrySnapshot, actor string) (model.RetryPlan, error) {
	plan := model.RetryPlan{Before: before, Execution: before.Execution + 1, ActorID: actor}
	for _, target := range []*string{&plan.ExecutionID, &plan.AuditID} {
		id, err := uuid.NewV7()
		if err != nil {
			return model.RetryPlan{}, fmt.Errorf("generate EmulationStation retry identity: %w", err)
		}
		*target = id.String()
	}
	return plan, nil
}
