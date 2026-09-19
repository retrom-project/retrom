package serverimport

import (
	"context"
	"encoding/json"
	"fmt"

	model "retrom/internal/model/serverimport"
)

func retryExecution(
	ctx context.Context,
	scope model.OutcomeScope,
	unit model.Work,
	counts map[string]int64,
	code string,
	now int64,
) (int64, error) {
	budget, err := scope.Read.Budget(ctx, unit)
	if err != nil {
		return 0, fmt.Errorf("read import retry budget: %w", err)
	}
	var terminal int64
	for state, count := range counts {
		if state != "PENDING" && state != "EVALUATING" {
			terminal += count
		}
	}
	at, retry := AutomaticRetryAt(budget.Attempt, budget.Maximum, terminal, budget.Deadline, now)
	if !retry {
		return 0, nil
	}
	event, err := json.Marshal(
		map[string]any{
			"schemaVersion": 1,
			"attempt":       budget.Attempt,
			"retryAtMs":     at,
			"errorCode":     code,
		},
	)
	if err != nil {
		return 0, fmt.Errorf("encode automatic retry: %w", err)
	}
	if err := scope.Write.Retry(ctx, model.AutomaticRetry{
		Unit:        unit,
		AvailableAt: at,
		Now:         now,
		Event:       event,
	}); err != nil {
		return 0, fmt.Errorf("write automatic retry: %w", err)
	}
	return at, nil
}
